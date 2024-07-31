// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package controllers provides the controllers for the machine requests handling.
package controllers

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/controller/generic/qtransform"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/omni/client/pkg/omni/resources/cloud"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/provider/meta"
	"github.com/siderolabs/talemu/internal/pkg/provider/resources"
)

const maxMachineCount = 10000

// MachineRequestStatusController creates the system config patch that contains the maintenance config.
type MachineRequestStatusController = qtransform.QController[*cloud.MachineRequest, *resources.Machine]

// NewMachineRequestStatusController initializes MachineRequestStatusController.
func NewMachineRequestStatusController() *MachineRequestStatusController {
	return qtransform.NewQController(
		qtransform.Settings[*cloud.MachineRequest, *resources.Machine]{
			Name: "provider.MachineRequestStatusController",
			MapMetadataOptionalFunc: func(request *cloud.MachineRequest) optional.Optional[*resources.Machine] {
				providerID, ok := request.Metadata().Labels().Get(omni.LabelCloudProviderID)
				if ok && providerID == meta.ProviderID {
					return optional.Some(resources.NewMachine(emu.NamespaceName, request.Metadata().ID()))
				}

				return optional.None[*resources.Machine]()
			},
			UnmapMetadataFunc: func(m *resources.Machine) *cloud.MachineRequest {
				return cloud.NewMachineRequest(m.Metadata().ID())
			},
			TransformExtraOutputFunc: func(ctx context.Context, r controller.ReaderWriter, logger *zap.Logger, request *cloud.MachineRequest, machine *resources.Machine) error {
				machine.Metadata().Labels().Set(omni.MachineLabelsType, meta.ProviderID)

				schematicID := request.TypedSpec().Value.SchematicId
				talosVersion := request.TypedSpec().Value.TalosVersion

				logger.Info("received machine request", zap.String("schematic_id", schematicID), zap.String("talos_version", talosVersion))

				var err error

				if machine.TypedSpec().Value.Slot == 0 {
					machine.TypedSpec().Value.Slot, err = pickFreeSlot(ctx, r)
					if err != nil {
						return err
					}

					logger.Info("requested machine",
						zap.Int32("slot", machine.TypedSpec().Value.Slot),
						zap.String("uuid", machine.TypedSpec().Value.Uuid),
					)
				}

				uuid := fmt.Sprintf("%05d803-c798-4da7-a410-f09abb48c8d8", machine.TypedSpec().Value.Slot)

				machine.TypedSpec().Value.Schematic = schematicID
				machine.TypedSpec().Value.TalosVersion = talosVersion
				machine.TypedSpec().Value.Uuid = uuid

				return nil
			},
		},
	)
}

func pickFreeSlot(ctx context.Context, r controller.Reader) (int32, error) {
	list, err := safe.ReaderListAll[*resources.Machine](ctx, r)
	if err != nil {
		return 0, err
	}

	used := make(map[int32]struct{}, list.Len())

	list.ForEach(func(r *resources.Machine) {
		used[r.TypedSpec().Value.Slot] = struct{}{}
	})

	for i := int32(1); i < maxMachineCount; i++ {
		_, inUse := used[i]
		if !inUse {
			return i, nil
		}
	}

	return 0, fmt.Errorf("no free machine slots")
}
