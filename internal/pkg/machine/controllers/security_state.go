// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/go-pointer"
	"github.com/siderolabs/go-procfs/procfs"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// SecurityStateController keeps the FIPS state of the machine in sync with its current image
// source: FIPS is enabled iff the image comes from an enterprise image factory, and strict
// additionally requires the talos.fips140=strict kernel argument.
//
// The security state resource itself is seeded at machine startup (it carries the static secure
// boot and UKI properties, and it must exist before the API serves requests), so this controller
// only updates the FIPS state field, without taking ownership.
type SecurityStateController struct {
	// Checker detects whether the current image source is an enterprise image factory.
	Checker EnterpriseChecker

	// ImageFactoryHost is the host of the configured image factory.
	ImageFactoryHost string

	// BootFactoryURL is the base URL of the image factory the boot media is pretended to come
	// from, deciding the machine identity before anything is installed.
	BootFactoryURL string
}

// Name implements controller.Controller interface.
func (ctrl *SecurityStateController) Name() string {
	return "runtime.SecurityStateController"
}

// Inputs implements controller.Controller interface.
func (ctrl *SecurityStateController) Inputs() []controller.Input {
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
		{
			Namespace: runtime.NamespaceName,
			Type:      runtime.KernelCmdlineType,
			ID:        optional.Some(runtime.KernelCmdlineID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *SecurityStateController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: runtime.SecurityStateType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *SecurityStateController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		config, err := machineconfig.GetComplete(ctx, r)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		image, err := safe.ReaderGetByID[*talos.Image](ctx, r, talos.ImageID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		cmdline, err := safe.ReaderGetByID[*runtime.KernelCmdline](ctx, r, runtime.KernelCmdlineID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		// on a probe failure the previously written state (or the seeded community default) stays
		// published, the error only arranges the backoff retry
		enterprise, err := ctrl.Checker.IsEnterprise(ctx, resolveImageSourceURL(image, config, ctrl.ImageFactoryHost, ctrl.BootFactoryURL))
		if err != nil {
			logger.Warn("failed to determine the image factory kind, keeping the previously reported FIPS state", zap.Error(err))

			return err
		}

		fipsState := runtime.FIPSStateDisabled

		if enterprise {
			fipsState = runtime.FIPSStateEnabled

			if cmdline != nil && cmdlineHasStrictFIPS(cmdline.TypedSpec().Cmdline) {
				fipsState = runtime.FIPSStateStrict
			}
		}

		if err = safe.WriterModify(ctx, r, runtime.NewSecurityStateSpec(runtime.NamespaceName), func(res *runtime.SecurityState) error {
			res.TypedSpec().FIPSState = fipsState

			return nil
		}, controller.WithModifyNoOwner()); err != nil {
			return err
		}
	}
}

// cmdlineHasStrictFIPS reports whether the kernel command line opts into the strict FIPS mode,
// the same way Talos reads it: the first talos.fips140 argument with the value "strict".
func cmdlineHasStrictFIPS(cmdline string) bool {
	value := procfs.NewCmdline(cmdline).Get("talos.fips140").First()

	return pointer.SafeDeref(value) == "strict"
}
