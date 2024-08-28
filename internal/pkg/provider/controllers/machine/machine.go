// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package machine implements fake machine runner.
package machine

import (
	"context"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/task"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/provider/resources"
)

// TaskSpec runs fake machine.
type TaskSpec struct {
	_ [0]func() // make uncomparable

	Machine     *resources.MachineTask
	GlobalState state.State
	Params      *machine.SideroLinkParams
	Kubernetes  *kubefactory.Kubernetes
	NC          *network.Client
}

// ID implements task.TaskSpec.
func (s TaskSpec) ID() task.ID {
	return s.Machine.Metadata().ID()
}

// Equal implements task.TaskSpec.
func (s TaskSpec) Equal(other TaskSpec) bool {
	return s.Machine.Metadata().ID() == other.Machine.Metadata().ID() && s.Machine.TypedSpec().Value.EqualVT(other.Machine.TypedSpec().Value)
}

// RunTask implements task.TaskSpec.
func (s TaskSpec) RunTask(ctx context.Context, logger *zap.Logger, _ any) error {
	m, err := machine.NewMachine(s.Machine.TypedSpec().Value.Uuid, logger, s.GlobalState)
	if err != nil {
		return err
	}

	defer m.Cleanup(ctx) //nolint:errcheck

	return m.Run(
		ctx,
		s.Params,
		int(s.Machine.TypedSpec().Value.Slot),
		s.Kubernetes,
		machine.WithTalosVersion(s.Machine.TypedSpec().Value.TalosVersion),
		machine.WithSchematic(s.Machine.TypedSpec().Value.Schematic),
		machine.WithNetworkClient(s.NC),
	)
}
