// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
	schematicsvc "github.com/siderolabs/talemu/internal/pkg/schematic"
)

// KernelCmdlineController computes kernel args list from the configuration.
type KernelCmdlineController struct {
	SchematicService *schematicsvc.Service
	ImageFactoryHost string
	BaseKernelArgs   string

	// BootFactoryURL is the factory the machine booted from. It is the fallback for
	// resolving which factory holds the current schematic, and stops applying once an
	// install or an upgrade replaces the image.
	BootFactoryURL string
}

// Name implements controller.Controller interface.
func (ctrl *KernelCmdlineController) Name() string {
	return "runtime.KernelCmdlineController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KernelCmdlineController) Inputs() []controller.Input {
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
func (ctrl *KernelCmdlineController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: runtime.KernelCmdlineType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *KernelCmdlineController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		schematicID, factoryURL, err := readCurrentSchematic(ctx, r, ctrl.ImageFactoryHost, ctrl.BootFactoryURL)
		if err != nil {
			return fmt.Errorf("failed to read current schematic ID: %w", err)
		}

		var extraKernelArgs []string

		if schematicID != "" {
			var sch *schematic.Schematic

			if sch, err = ctrl.SchematicService.GetByID(ctx, schematicID, factoryURL); err != nil {
				return fmt.Errorf("failed to get schematic by ID %q: %w", schematicID, err)
			}

			extraKernelArgs = sch.Customization.ExtraKernelArgs
		}

		// The base args are empty when a schematic describes the boot media, since the
		// schematic then supplies the whole command line, so build this by joining
		// only the parts that exist rather than concatenating blindly.
		parts := slices.Concat(strings.Fields(ctrl.BaseKernelArgs), extraKernelArgs, []string{"talemu=1"})

		cmdline := strings.Join(parts, " ")
		logger.Info("set cmdline", zap.String("cmdline", cmdline), zap.Strings("extra_args", extraKernelArgs))

		if err = safe.WriterModify(ctx, r, runtime.NewKernelCmdline(), func(res *runtime.KernelCmdline) error {
			res.TypedSpec().Cmdline = cmdline

			return nil
		}); err != nil {
			return err
		}
	}
}
