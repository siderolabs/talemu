// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/api/specs"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/provider/resources"
)

const maxMachineCount = 100000

// Provisioner implements Talos emulator infra provider.
type Provisioner struct {
	state state.State
}

// NewProvisioner creates a new provisioner.
func NewProvisioner(state state.State) *Provisioner {
	return &Provisioner{
		state: state,
	}
}

// ProvisionSteps implements infra.Provisioner.
func (p *Provisioner) ProvisionSteps() []provision.Step[*resources.Machine] {
	return []provision.Step[*resources.Machine]{
		provision.NewStep("createMachine", func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
			machineTask, err := safe.ReaderGetByID[*resources.MachineTask](ctx, p.state, pctx.GetRequestID())
			if err != nil && !state.IsNotFoundError(err) {
				return fmt.Errorf("failed to get the machine task: %w", err)
			}

			// the machine is already provisioned, so no need to do anything, just return the current machine state
			if machineTask != nil {
				pctx.SetMachineUUID(machineTask.TypedSpec().Value.Uuid)
				pctx.SetMachineInfraID(fmt.Sprintf("%d", machineTask.TypedSpec().Value.Slot))

				return nil
			}

			machine := pctx.State

			if machine.TypedSpec().Value.Slot == 0 {
				logger.Info("provisioning a new machine")

				machine.TypedSpec().Value.Slot, err = p.pickFreeSlot(ctx)
				if err != nil {
					return fmt.Errorf("failed to pick a free slot: %w", err)
				}

				machine.TypedSpec().Value.Uuid = fmt.Sprintf("%06d03-c798-4da7-a410-f09abb48c8d8", machine.TypedSpec().Value.Slot)

				machine.TypedSpec().Value.Schematic, err = pctx.GenerateSchematicID(ctx, logger, provision.WithoutConnectionParams())
				if err != nil {
					return fmt.Errorf("failed to generate schematic: %w", err)
				}

				machine.TypedSpec().Value.TalosVersion = pctx.GetTalosVersion()
			}

			machineTask = resources.NewMachineTask(emu.NamespaceName, machine.Metadata().ID())

			ms := machine.TypedSpec().Value

			machineTask.TypedSpec().Value = &specs.MachineTaskSpec{
				Slot:           ms.Slot,
				Uuid:           ms.Uuid,
				Schematic:      ms.Schematic,
				TalosVersion:   ms.TalosVersion,
				ConnectionArgs: pctx.ConnectionParams,
			}

			pctx.SetMachineUUID(machineTask.TypedSpec().Value.Uuid)
			pctx.SetMachineInfraID(fmt.Sprintf("%d", machineTask.TypedSpec().Value.Slot))

			if err = p.state.Create(ctx, machineTask); err != nil {
				if state.IsPhaseConflictError(err) {
					return provision.NewRetryError(err, time.Second*15)
				}

				return err
			}

			return nil
		}),
	}
}

// Deprovision implements infra.Provisioner.
func (p *Provisioner) Deprovision(ctx context.Context, logger *zap.Logger, _ *resources.Machine, machineRequest *infra.MachineRequest) error {
	machineTask := resources.NewMachineTask(emu.NamespaceName, machineRequest.Metadata().ID())

	logger.Info("deprovision machine")

	_, err := p.state.Teardown(ctx, machineTask.Metadata())
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

		return err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	_, err = p.state.WatchFor(ctx, machineTask.Metadata(), state.WithFinalizerEmpty())
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

		return err
	}

	err = p.state.Destroy(ctx, machineTask.Metadata())
	if err != nil && state.IsNotFoundError(err) {
		return err
	}

	return nil
}

func (p *Provisioner) pickFreeSlot(ctx context.Context) (int32, error) {
	list, err := safe.ReaderListAll[*resources.MachineTask](ctx, p.state)
	if err != nil {
		return 0, err
	}

	used := make(map[int32]struct{}, list.Len())

	list.ForEach(func(r *resources.MachineTask) {
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
