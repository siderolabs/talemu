// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package controllers provides the controller for the machine request status.
package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/task"
	cloudspecs "github.com/siderolabs/omni/client/api/omni/specs/cloud"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources"
	"github.com/siderolabs/omni/client/pkg/omni/resources/cloud"
	"github.com/siderolabs/omni/client/pkg/omni/resources/siderolink"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	machinetask "github.com/siderolabs/talemu/internal/pkg/provider/controllers/machine"
	"github.com/siderolabs/talemu/internal/pkg/provider/resources"
)

// MachineController runs a machine for each machine request.
type MachineController struct {
	runner      *task.Runner[any, machinetask.TaskSpec]
	kubernetes  *kubefactory.Kubernetes
	nc          *network.Client
	globalState state.State
}

// NewMachineController creates new machine controller.
func NewMachineController(globalState state.State, kubernetes *kubefactory.Kubernetes, nc *network.Client) *MachineController {
	return &MachineController{
		runner:      task.NewEqualRunner[machinetask.TaskSpec](),
		globalState: globalState,
		kubernetes:  kubernetes,
		nc:          nc,
	}
}

// Name implements controller.Controller interface.
func (ctrl *MachineController) Name() string {
	return "provider.MachineController"
}

// Inputs implements controller.Controller interface.
func (ctrl *MachineController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: emu.NamespaceName,
			Type:      resources.MachineType,
			Kind:      controller.InputStrong,
		},
		{
			Namespace: omnires.CloudProviderNamespace,
			Type:      cloud.MachineRequestStatusType,
			Kind:      controller.InputDestroyReady,
		},
		{
			Namespace: omnires.DefaultNamespace,
			Type:      siderolink.ConnectionParamsType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *MachineController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: cloud.MachineRequestStatusType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocognit,gocyclo,cyclop
func (ctrl *MachineController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			ctrl.runner.Stop()

			return nil
		case <-r.EventCh():
		}

		machines, err := safe.ReaderListAll[*resources.Machine](ctx, r)
		if err != nil {
			return errors.New("error listing machines")
		}

		touchedIDs := make(map[resource.ID]struct{})

		for it := machines.Iterator(); it.Next(); {
			m := it.Value()

			if m.Metadata().Phase() == resource.PhaseTearingDown {
				continue
			}

			if !m.Metadata().Finalizers().Has(ctrl.Name()) {
				if err = r.AddFinalizer(ctx, m.Metadata(), ctrl.Name()); err != nil {
					return err
				}
			}

			if err = safe.WriterModify(ctx, r, cloud.NewMachineRequestStatus(m.Metadata().ID()), func(r *cloud.MachineRequestStatus) error {
				*r.Metadata().Labels() = *m.Metadata().Labels()

				r.TypedSpec().Value.Id = m.TypedSpec().Value.Uuid
				r.TypedSpec().Value.Stage = cloudspecs.MachineRequestStatusSpec_PROVISIONED

				return nil
			}); err != nil {
				if state.IsPhaseConflictError(err) {
					continue
				}

				return err
			}

			var (
				connectionParams *siderolink.ConnectionParams
				params           *machine.SideroLinkParams
			)

			connectionParams, err = safe.ReaderGetByID[*siderolink.ConnectionParams](ctx, r, siderolink.ConfigID)
			if err != nil {
				return err
			}

			params, err = machine.ParseKernelArgs(connectionParams.TypedSpec().Value.Args)
			if err != nil {
				return err
			}

			ctrl.runner.StartTask(ctx, logger, m.Metadata().ID(), machinetask.TaskSpec{
				Machine:     m,
				GlobalState: ctrl.globalState,
				Kubernetes:  ctrl.kubernetes,
				Params:      params,
				NC:          ctrl.nc,
			}, nil)

			touchedIDs[m.Metadata().ID()] = struct{}{}
		}

		machineRequestStatuses, err := safe.ReaderListAll[*cloud.MachineRequestStatus](ctx, r)
		if err != nil {
			return fmt.Errorf("error listing resources: %w", err)
		}

		for it := machineRequestStatuses.Iterator(); it.Next(); {
			res := it.Value()

			machine := resources.NewMachine(emu.NamespaceName, res.Metadata().ID())

			machine.TypedSpec().Value.Uuid = res.TypedSpec().Value.Id

			if _, ok := touchedIDs[res.Metadata().ID()]; !ok {
				var ready bool

				ready, err = r.Teardown(ctx, res.Metadata())
				if err != nil {
					return err
				}

				if !ready {
					continue
				}

				if err = ctrl.resetMachine(ctx, machine, logger); err != nil {
					return err
				}

				if err = r.RemoveFinalizer(ctx, machine.Metadata(), ctrl.Name()); err != nil {
					return err
				}

				if err = r.Destroy(ctx, res.Metadata()); err != nil && !state.IsNotFoundError(err) {
					return err
				}
			}
		}

		r.ResetRestartBackoff()
	}
}

func (ctrl *MachineController) resetMachine(ctx context.Context, m *resources.Machine, logger *zap.Logger) error {
	logger.Info("reset machine", zap.String("uuid", m.TypedSpec().Value.Uuid), zap.String("request", m.Metadata().ID()))

	ctrl.runner.StopTask(logger, m.Metadata().ID())

	stateDir := runtime.GetStateDir(m.TypedSpec().Value.Uuid)

	err := os.RemoveAll(stateDir)
	if !os.IsNotExist(err) {
		return err
	}

	err = ctrl.globalState.Destroy(ctx, emu.NewMachineStatus(emu.NamespaceName, m.TypedSpec().Value.Uuid).Metadata())
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	return nil
}
