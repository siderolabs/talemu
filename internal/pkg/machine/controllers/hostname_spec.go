// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// HostnameSpecController applies network.HostnameSpec to the actual interfaces.
type HostnameSpecController struct {
	GlobalState state.State
	MachineID   string
}

// Name implements controller.Controller interface.
func (ctrl *HostnameSpecController) Name() string {
	return "network.HostnameSpecController"
}

// Inputs implements controller.Controller interface.
func (ctrl *HostnameSpecController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: network.NamespaceName,
			Type:      network.HostnameSpecType,
			Kind:      controller.InputStrong,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *HostnameSpecController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: network.HostnameStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocognit
func (ctrl *HostnameSpecController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		// list source network configuration resources
		list, err := r.List(ctx, resource.NewMetadata(network.NamespaceName, network.HostnameSpecType, "", resource.VersionUndefined))
		if err != nil {
			return fmt.Errorf("error listing source network addresses: %w", err)
		}

		// add finalizers for all live resources
		for _, res := range list.Items {
			if res.Metadata().Phase() != resource.PhaseRunning {
				continue
			}

			if err = r.AddFinalizer(ctx, res.Metadata(), ctrl.Name()); err != nil {
				return fmt.Errorf("error adding finalizer: %w", err)
			}
		}

		// loop over specs and sync to statuses
		for _, res := range list.Items {
			spec := res.(*network.HostnameSpec) //nolint:forcetypeassert,errcheck

			switch spec.Metadata().Phase() {
			case resource.PhaseTearingDown:
				md := resource.NewMetadata(network.NamespaceName, network.HostnameStatusType, spec.Metadata().ID(), resource.VersionUndefined)

				if err = r.Destroy(ctx, md); err != nil && !state.IsNotFoundError(err) {
					return fmt.Errorf("error destroying status: %w", err)
				}

				if err = r.RemoveFinalizer(ctx, spec.Metadata(), ctrl.Name()); err != nil {
					return fmt.Errorf("error removing finalizer: %w", err)
				}
			case resource.PhaseRunning:
				if err = r.Modify(ctx, network.NewHostnameStatus(network.NamespaceName, spec.Metadata().ID()), func(r resource.Resource) error {
					status := r.(*network.HostnameStatus) //nolint:forcetypeassert,errcheck

					status.TypedSpec().Hostname = spec.TypedSpec().Hostname
					status.TypedSpec().Domainname = spec.TypedSpec().Domainname

					_, err = safe.StateUpdateWithConflicts(ctx, ctrl.GlobalState, emu.NewMachineStatus(emu.NamespaceName, ctrl.MachineID).Metadata(),
						func(res *emu.MachineStatus) error {
							res.TypedSpec().Value.Hostname = spec.TypedSpec().Hostname

							return nil
						},
					)

					return err
				}); err != nil {
					return fmt.Errorf("error modifying status: %w", err)
				}
			}
		}

		r.ResetRestartBackoff()
	}
}
