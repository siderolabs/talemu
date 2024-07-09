// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// VersionController computes extensions list from the configuration.
// Updates machine status resource.
type VersionController struct{}

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
			ID:        optional.Some(config.V1Alpha1ID),
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
	}
}

// Run implements controller.Controller interface.
func (ctrl *VersionController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, r, config.V1Alpha1ID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		image, err := safe.ReaderGetByID[*talos.Image](ctx, r, talos.ImageID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		var version string

		switch {
		case image != nil:
			version = image.TypedSpec().Value.Version
		case config != nil:
			var found bool

			installImage := config.Container().RawV1Alpha1().Machine().Install().Image()

			_, version, found = strings.Cut(installImage, ":")
			if !found {
				return fmt.Errorf("failed to parse schematic id from the install image")
			}
		}

		if version == "" {
			continue
		}

		if err := safe.WriterModify(ctx, r, talos.NewVersion(talos.NamespaceName, talos.VersionID), func(res *talos.Version) error {
			res.TypedSpec().Value.Value = version

			return nil
		}); err != nil {
			return err
		}
	}
}
