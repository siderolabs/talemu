// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"net/netip"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/xslices"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// NodeAddressController simple version of node addresses generator.
type NodeAddressController struct {
	GlobalState state.State
	MachineID   string
}

// Name implements controller.Controller interface.
func (ctrl *NodeAddressController) Name() string {
	return "network.NodeAddressController"
}

// Inputs implements controller.Controller interface.
func (ctrl *NodeAddressController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: network.NamespaceName,
			Type:      network.AddressSpecType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *NodeAddressController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: network.NodeAddressType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *NodeAddressController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		addresses, err := safe.ReaderListAll[*network.AddressSpec](ctx, r)
		if err != nil {
			return err
		}

		if err = safe.WriterModify(ctx, r, network.NewNodeAddress(network.NamespaceName, network.NodeAddressDefaultID), func(r *network.NodeAddress) error {
			addrs := make([]netip.Prefix, 0, addresses.Len())

			addresses.ForEach(func(r *network.AddressSpec) {
				addrs = append(addrs, r.TypedSpec().Address)
			})

			r.TypedSpec().Addresses = addrs

			_, err = safe.StateUpdateWithConflicts(ctx, ctrl.GlobalState, emu.NewMachineStatus(emu.NamespaceName, ctrl.MachineID).Metadata(),
				func(cm *emu.MachineStatus) error {
					cm.TypedSpec().Value.Addresses = xslices.Map(addrs, func(p netip.Prefix) string {
						return p.Addr().String()
					})

					return nil
				},
			)

			return err
		}); err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}
