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
	"github.com/siderolabs/talos/pkg/machinery/extensions"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

const (
	author = "none"
	desc   = "fake description"
)

var knownSchematics = map[string]struct {
	extensions []extensions.Metadata
}{
	"088171816e905ec439337da75b1bafb81de8c652ee41c099f2b9ef7d90847648": {
		extensions: []extensions.Metadata{
			{
				Name:        "hello-world-service",
				Version:     "1.0.0",
				Author:      author,
				Description: desc,
			},
			{
				Name:        "qemu-guest-agent",
				Version:     "1.0.0",
				Author:      author,
				Description: desc,
			},
		},
	},
	"cf9b7aab9ed7c365d5384509b4d31c02fdaa06d2b3ac6cc0bc806f28130eff1f": {
		extensions: []extensions.Metadata{
			{
				Name:        "hello-world-service",
				Version:     "1.0.0",
				Author:      author,
				Description: desc,
			},
		},
	},
	"5e0ac9d7e10ff9034bc4db865bf0337d40eeaec20683e27804939e1a88b7b654": {
		extensions: []extensions.Metadata{
			{
				Name:        "hello-world-service",
				Version:     "1.0.0",
				Author:      author,
				Description: desc,
			},
		},
	},
}

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
//
//nolint:gocognit
func (ctrl *ExtensionStatusController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
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

		schematic := "5e0ac9d7e10ff9034bc4db865bf0337d40eeaec20683e27804939e1a88b7b654"

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

		touched := map[string]interface{}{}

		extensionStatus := runtime.NewExtensionStatus(runtime.NamespaceName, constants.SchematicIDExtensionName)

		if err = safe.WriterModify(ctx, r, extensionStatus, func(res *runtime.ExtensionStatus) error {
			res.TypedSpec().Metadata.Name = constants.SchematicIDExtensionName
			res.TypedSpec().Metadata.Version = schematic
			// TODO: we might need to populate extra data here too

			touched[res.Metadata().ID()] = struct{}{}

			return nil
		}); err != nil {
			return err
		}

		if config, ok := knownSchematics[schematic]; ok {
			for _, extension := range config.extensions {
				extensionStatus = runtime.NewExtensionStatus(runtime.NamespaceName, extension.Name)

				touched[extensionStatus.Metadata().ID()] = struct{}{}

				if err = safe.WriterModify(ctx, r, extensionStatus, func(res *runtime.ExtensionStatus) error {
					res.TypedSpec().Metadata = extension

					return nil
				}); err != nil {
					return err
				}
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
