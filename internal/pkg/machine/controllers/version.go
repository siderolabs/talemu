// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"strings"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/omni/client/pkg/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// VersionController computes the current Talos version and version name of the machine from its
// current image source.
type VersionController struct {
	// Checker detects whether the current image source is an enterprise image factory.
	Checker EnterpriseChecker

	// ImageFactoryHost is the host of the configured image factory, used to recover schematic IDs
	// from image refs.
	ImageFactoryHost string

	// BootFactoryURL is the base URL of the image factory the boot media is pretended to come
	// from, deciding the machine identity before anything is installed.
	BootFactoryURL string
}

// Name implements controller.Controller interface.
func (ctrl *VersionController) Name() string {
	return "runtime.VersionController"
}

// Inputs implements controller.Controller interface.
func (ctrl *VersionController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.ActiveID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: talos.NamespaceName,
			Type:      talos.ImageType,
			ID:        optional.Some(talos.ImageID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *VersionController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: talos.VersionType,
			Kind: controller.OutputExclusive,
		},
		{
			Type: runtime.VersionType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *VersionController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		if err := ctrl.reconcile(ctx, r, logger); err != nil {
			return err
		}
	}
}

func (ctrl *VersionController) reconcile(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	config, err := machineconfig.GetComplete(ctx, r)
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	image, err := safe.ReaderGetByID[*talos.Image](ctx, r, talos.ImageID)
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	var (
		version string
		source  string
	)

	switch {
	case image != nil:
		source = "upgrade"

		version = image.TypedSpec().Value.Version
	case config != nil:
		source = "install"

		installImage := config.Container().RawV1Alpha1().Machine().Install().Image()

		// an empty or unparseable install image falls back to the default version instead of
		// erroring, as this write gates the machine boot readiness
		if parsed, parseErr := talos.ParseImageRef(ctrl.ImageFactoryHost, installImage); parseErr == nil {
			version = parsed.Version
		} else {
			logger.Warn("failed to parse the install image, using the default version", zap.Error(parseErr))
		}
	}

	if version == "" {
		version = "v" + constants.DefaultTalosVersion
	}

	// this write gates the machine boot readiness, so it happens first and never depends on
	// the image factory probe below
	if err = safe.WriterModify(ctx, r, talos.NewVersion(talos.NamespaceName, talos.VersionID), func(res *talos.Version) error {
		if version != res.TypedSpec().Value.Value {
			logger.Info("version updated", zap.String("source", source))
		}

		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}

		res.TypedSpec().Value.Value = version
		res.TypedSpec().Value.Architecture = "amd64"

		return nil
	}); err != nil {
		return err
	}

	enterprise, checkErr := ctrl.Checker.IsEnterprise(ctx, resolveImageSourceURL(image, config, ctrl.ImageFactoryHost, ctrl.BootFactoryURL))
	if checkErr != nil {
		logger.Warn("failed to determine the image factory kind, keeping the previously reported name", zap.Error(checkErr))
	}

	if err = safe.WriterModify(ctx, r, runtime.NewVersion(), func(res *runtime.Version) error {
		name := res.TypedSpec().Name

		switch {
		case checkErr == nil && enterprise:
			name = "Talos Enterprise"
		case checkErr == nil:
			name = "Talos"
		case name == "":
			// nothing reported yet, publish the community fallback rather than nothing
			name = "Talos"
		}

		if name != res.TypedSpec().Name && res.TypedSpec().Name != "" {
			logger.Info("version name updated", zap.String("name", name))
		}

		res.TypedSpec().Name = name
		res.TypedSpec().Version = version

		return nil
	}); err != nil {
		return err
	}

	// the fallback state is published above, the error only arranges the backoff retry
	return checkErr
}
