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
	"github.com/siderolabs/image-factory/pkg/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// ExtensionStatusController computes extensions list from the configuration.
// Updates machine status resource.
type ExtensionStatusController struct{}

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
func (ctrl *ExtensionStatusController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: runtime.ExtensionStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *ExtensionStatusController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
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

		var schematic string

		switch {
		case image != nil:
			schematic = image.TypedSpec().Value.Schematic
		case config != nil:
			var found bool

			installImage := config.Container().RawV1Alpha1().Machine().Install().Image()

			if !strings.HasPrefix(installImage, "factory.talos.dev") {
				continue
			}

			parts := strings.Split(installImage, "/")

			schematic, _, found = strings.Cut(parts[len(parts)-1], ":")
			if !found {
				return fmt.Errorf("failed to parse schematic id from the install image")
			}
		}

		if schematic == "" {
			continue
		}

		extensionStatus := runtime.NewExtensionStatus(runtime.NamespaceName, constants.SchematicIDExtensionName)

		// TODO: we might need to populate extra data here too

		if err := safe.WriterModify(ctx, r, extensionStatus, func(res *runtime.ExtensionStatus) error {
			res.TypedSpec().Metadata.Name = constants.SchematicIDExtensionName
			res.TypedSpec().Metadata.Version = schematic

			return nil
		}); err != nil {
			return err
		}
	}
}
