// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package controllers provides the controller for the machine request status.
package controllers

import (
	"context"
	"errors"
	"os"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/task"
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
			Type:      resources.MachineTaskType,
			Kind:      controller.InputStrong,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *MachineController) Outputs() []controller.Output {
	return nil
}

// Run implements controller.Controller interface.
func (ctrl *MachineController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			ctrl.runner.Stop()

			return nil
		case <-r.EventCh():
		}

		machines, err := safe.ReaderListAll[*resources.MachineTask](ctx, r)
		if err != nil {
			return errors.New("error listing machines")
		}

		touchedIDs := make(map[resource.ID]struct{})

		for m := range machines.All() {
			if m.Metadata().Phase() == resource.PhaseTearingDown {
				if err = ctrl.resetMachine(ctx, m, logger); err != nil {
					return err
				}

				if err = r.RemoveFinalizer(ctx, m.Metadata(), ctrl.Name()); err != nil {
					return err
				}

				continue
			}

			if !m.Metadata().Finalizers().Has(ctrl.Name()) {
				if err = r.AddFinalizer(ctx, m.Metadata(), ctrl.Name()); err != nil {
					return err
				}
			}

			var params *machine.SideroLinkParams

			params, err = machine.ParseKernelArgs(m.TypedSpec().Value.ConnectionArgs)
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

		r.ResetRestartBackoff()
	}
}

func (ctrl *MachineController) resetMachine(ctx context.Context, m *resources.MachineTask, logger *zap.Logger) error {
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
