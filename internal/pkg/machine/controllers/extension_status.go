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
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/image-factory/pkg/constants"
	"github.com/siderolabs/talos/pkg/machinery/extensions"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// ExtensionStatusController computes extensions list from the configuration.
type ExtensionStatusController struct {
	// Source resolves the schematic of the image the machine runs.
	Source BootMediaSource

	// LocalExtensions is reported when the machine has no schematic, which is the
	// case for boot media that was not built by an image factory. Real Talos reads
	// its extensions from local metadata rather than from a factory, so a machine
	// without a schematic still knows what it has.
	LocalExtensions []string
}

// Name implements controller.Controller interface.
func (ctrl *ExtensionStatusController) Name() string {
	return "runtime.ExtensionStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *ExtensionStatusController) Inputs() []controller.Input {
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
func (ctrl *ExtensionStatusController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: runtime.ExtensionStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocognit
func (ctrl *ExtensionStatusController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		image, err := readCurrentImage(ctx, r, ctrl.Source.FactoryHosts())
		if err != nil {
			return fmt.Errorf("failed to read the current image: %w", err)
		}

		touched := map[string]any{}
		extensionList := ctrl.LocalExtensions

		// With no schematic the machine reports the extensions it knows about locally
		// and no schematic ID extension, exactly like a Talos image that an image
		// factory did not build. Otherwise the schematic is the source of truth, and
		// the schematic ID is published as an extension the way a factory image does.
		if image.Schematic != "" {
			sch, schErr := ctrl.Source.GetSchematicByID(ctx, image.Schematic, image.Version, image.Host)
			if schErr != nil {
				return fmt.Errorf("failed to get schematic by ID %q: %w", image.Schematic, schErr)
			}

			extensionList = sch.Customization.SystemExtensions.OfficialExtensions

			extensionStatus := runtime.NewExtensionStatus(runtime.NamespaceName, constants.SchematicIDExtensionName)

			var data []byte

			data, err = sch.Marshal()
			if err != nil {
				return err
			}

			if err = safe.WriterModify(ctx, r, extensionStatus, func(res *runtime.ExtensionStatus) error {
				res.TypedSpec().Metadata.Name = constants.SchematicIDExtensionName
				res.TypedSpec().Metadata.Version = image.Schematic
				res.TypedSpec().Metadata.ExtraInfo = string(data)

				touched[res.Metadata().ID()] = struct{}{}

				return nil
			}); err != nil {
				return err
			}
		}

		for _, extension := range extensionList {
			nameWithoutPrefix := strings.TrimPrefix(extension, emuconst.OfficialExtensionPrefix)

			extensionStatus := runtime.NewExtensionStatus(runtime.NamespaceName, nameWithoutPrefix)

			touched[extensionStatus.Metadata().ID()] = struct{}{}

			if err = safe.WriterModify(ctx, r, extensionStatus, func(res *runtime.ExtensionStatus) error {
				res.TypedSpec().Metadata = extensions.Metadata{
					Name:        nameWithoutPrefix,
					Version:     "1.0.0",
					Author:      "none",
					Description: "fake description",
				}

				return nil
			}); err != nil {
				return err
			}
		}

		list, err := safe.ReaderListAll[*runtime.ExtensionStatus](ctx, r)
		if err != nil {
			return err
		}

		if err := list.ForEachErr(func(res *runtime.ExtensionStatus) error {
			if _, ok := touched[res.Metadata().ID()]; !ok {
				return r.Destroy(ctx, res.Metadata())
			}

			return nil
		}); err != nil {
			return err
		}
	}
}
