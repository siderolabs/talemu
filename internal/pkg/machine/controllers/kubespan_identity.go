// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// KubeSpanIdentityController manages the local KubeSpan identity while KubeSpan is enabled.
//
// Unlike Talos, the identity is not cached on the STATE partition: the key is regenerated when KubeSpan is re-enabled.
// The address host part is derived from the public key, as the emulated machine has no stable MAC address.
type KubeSpanIdentityController struct{}

// Name implements controller.Controller interface.
func (ctrl *KubeSpanIdentityController) Name() string {
	return "kubespan.IdentityController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KubeSpanIdentityController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      kubespan.ConfigType,
			ID:        optional.Some(kubespan.ConfigID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *KubeSpanIdentityController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: kubespan.IdentityType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *KubeSpanIdentityController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		cfg, err := safe.ReaderGetByID[*kubespan.Config](ctx, r, kubespan.ConfigID)
		if err != nil && !state.IsNotFoundError(err) {
			return fmt.Errorf("error getting kubespan configuration: %w", err)
		}

		if cfg != nil && cfg.TypedSpec().Enabled {
			if err = safe.WriterModify(ctx, r, kubespan.NewIdentity(kubespan.NamespaceName, kubespan.LocalIdentity), func(res *kubespan.Identity) error {
				return updateKubeSpanIdentity(res.TypedSpec(), cfg.TypedSpec().ClusterID)
			}); err != nil {
				return fmt.Errorf("error modifying kubespan identity: %w", err)
			}
		} else if err = r.Destroy(ctx, kubespan.NewIdentity(kubespan.NamespaceName, kubespan.LocalIdentity).Metadata()); err != nil && !state.IsNotFoundError(err) {
			return fmt.Errorf("error cleaning up kubespan identity: %w", err)
		}

		r.ResetRestartBackoff()
	}
}

func updateKubeSpanIdentity(spec *kubespan.IdentitySpec, clusterID string) error {
	if spec.PrivateKey == "" {
		key, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return fmt.Errorf("error generating kubespan key: %w", err)
		}

		spec.PrivateKey = key.String()
		spec.PublicKey = key.PublicKey().String()
	}

	spec.Subnet = network.ULAPrefix(clusterID, network.ULAKubeSpan)

	// the subnet is a /64, fill the host part from the public key hash
	addr := spec.Subnet.Addr().As16()
	hash := sha256.Sum256([]byte(spec.PublicKey))

	copy(addr[8:], hash[:8])

	ip := netip.AddrFrom16(addr)
	spec.Address = netip.PrefixFrom(ip, ip.BitLen())

	return nil
}
