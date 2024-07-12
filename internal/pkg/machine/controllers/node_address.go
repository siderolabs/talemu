// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"net/netip"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/xslices"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
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

		for _, id := range []string{
			network.NodeAddressCurrentID,
			network.FilteredNodeAddressID(network.NodeAddressCurrentID, k8s.NodeAddressFilterNoK8s),
		} {
			// create fake virtual ipv6 addresses
			if err = safe.WriterModify(ctx, r, network.NewNodeAddress(network.NamespaceName, id), func(r *network.NodeAddress) error {
				var addr netip.Prefix

				addr, err = GenerateRandomNodeAddr(networkPrefix(ctrl.MachineID))
				if err != nil {
					return err
				}

				r.TypedSpec().Addresses = []netip.Prefix{addr}

				return nil
			}); err != nil {
				return err
			}
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

// GenerateRandomNodeAddr generates a random node address within the last 8 bytes of the given prefix.
func GenerateRandomNodeAddr(prefix netip.Prefix) (netip.Prefix, error) {
	raw := prefix.Addr().As16()
	salt := make([]byte, 8)

	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		return netip.Prefix{}, err
	}

	copy(raw[8:], salt)

	return netip.PrefixFrom(netip.AddrFrom16(raw), prefix.Bits()), nil
}

func networkPrefix(machineID string) netip.Prefix {
	var prefixData [16]byte

	hash := sha256.Sum256([]byte(machineID))

	// Take the last 16 bytes of the clusterID's hash.
	copy(prefixData[:], hash[sha256.Size-16:])

	// Apply the ULA prefix as per RFC4193
	prefixData[0] = 0xdd

	prefixData[7] = 0x4

	return netip.PrefixFrom(netip.AddrFrom16(prefixData), 64).Masked()
}
