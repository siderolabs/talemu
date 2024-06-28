// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"net/netip"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// ControlPlaneEndpointsController calculates control plane endpoints from the global emulator state.
type ControlPlaneEndpointsController struct {
	GlobalState state.State
}

// Name implements controller.Controller interface.
func (ctrl *ControlPlaneEndpointsController) Name() string {
	return "ControlPlaneEndpointsController"
}

// Inputs implements controller.Controller interface.
func (ctrl *ControlPlaneEndpointsController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			ID:        optional.Some(config.V1Alpha1ID),
			Type:      config.MachineConfigType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *ControlPlaneEndpointsController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: k8s.EndpointType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocognit,gocyclo,cyclop
func (ctrl *ControlPlaneEndpointsController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	var (
		watchCtx    context.Context
		watchCancel context.CancelFunc
		watchEvents = make(chan state.Event)
		cluster     string
	)

	defer func() {
		if watchCancel != nil {
			watchCancel()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-watchEvents:
			list, err := safe.ReaderListAll[*emu.MachineStatus](ctx, ctrl.GlobalState, state.WithLabelQuery(
				resource.LabelEqual(emu.LabelCluster, cluster),
				resource.LabelExists(emu.LabelControlPlaneRole),
			))
			if err != nil {
				return err
			}

			visited := map[resource.ID]struct{}{}

			if err = list.ForEachErr(func(cm *emu.MachineStatus) error {
				if len(cm.TypedSpec().Value.Addresses) == 0 {
					return nil
				}

				visited[cm.Metadata().ID()] = struct{}{}

				return safe.WriterModify(ctx, r, k8s.NewEndpoint(k8s.ControlPlaneNamespaceName, cm.Metadata().ID()), func(endpoint *k8s.Endpoint) error {
					endpoint.TypedSpec().Addresses = make([]netip.Addr, 0, len(cm.TypedSpec().Value.Addresses))

					for _, address := range cm.TypedSpec().Value.Addresses {
						var addr netip.Addr

						addr, err = netip.ParseAddr(address)
						if err != nil {
							return err
						}

						endpoint.TypedSpec().Addresses = append(endpoint.TypedSpec().Addresses, addr)
					}

					return nil
				})
			}); err != nil {
				return err
			}

			endpoints, err := safe.ReaderListAll[*k8s.Endpoint](ctx, r)
			if err != nil {
				return err
			}

			if err = endpoints.ForEachErr(func(endpoint *k8s.Endpoint) error {
				if _, ok := visited[endpoint.Metadata().ID()]; !ok {
					return r.Destroy(ctx, endpoint.Metadata())
				}

				return nil
			}); err != nil {
				return err
			}
		case <-r.EventCh():
			machineConfig, err := safe.ReaderGetByID[*config.MachineConfig](ctx, r, config.V1Alpha1ID)
			if err != nil && !state.IsNotFoundError(err) {
				return nil
			}

			if machineConfig != nil && cluster == machineConfig.Provider().Cluster().ID() {
				continue
			}

			if watchCancel != nil {
				watchCancel()
			}

			if machineConfig == nil {
				cluster = ""

				continue
			}

			cluster = machineConfig.Provider().Cluster().ID()

			// govet isn't smart enough to see where the context is being canceled
			watchCtx, watchCancel = context.WithCancel(ctx) //nolint:govet

			if err = ctrl.GlobalState.WatchKind(watchCtx, emu.NewMachineStatus(emu.NamespaceName, "").Metadata(), watchEvents, state.WatchWithLabelQuery(
				resource.LabelEqual(emu.LabelCluster, cluster),
				resource.LabelExists(emu.LabelControlPlaneRole),
			), state.WithBootstrapContents(true)); err != nil {
				// govet isn't smart enough to see where the context is being canceled
				return err //nolint:govet
			}
		}
	}
}
