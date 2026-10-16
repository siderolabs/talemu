// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/netip"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"go.uber.org/zap"
)

const (
	// kubeSpanPeerRefreshInterval is how often the emulated peer traffic counters grow.
	kubeSpanPeerRefreshInterval = 10 * time.Second

	// kubeSpanPeerHandshakeInterval mimics the WireGuard rekey interval.
	kubeSpanPeerHandshakeInterval = 2 * time.Minute

	// kubeSpanPeerMaxTrafficPerRefresh is the maximum number of bytes added to each counter per refresh.
	kubeSpanPeerMaxTrafficPerRefresh = 64 * 1024
)

// KubeSpanPeerStatusController emulates kubespan.PeerStatus for every other cluster member while KubeSpan is enabled.
//
// There is no actual WireGuard mesh: every peer is reported as up, and the traffic counters grow by random amounts.
type KubeSpanPeerStatusController struct{}

// Name implements controller.Controller interface.
func (ctrl *KubeSpanPeerStatusController) Name() string {
	return "kubespan.PeerStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KubeSpanPeerStatusController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      kubespan.ConfigType,
			ID:        optional.Some(kubespan.ConfigID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: cluster.NamespaceName,
			Type:      cluster.IdentityType,
			ID:        optional.Some(cluster.LocalIdentity),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: cluster.NamespaceName,
			Type:      cluster.MemberType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *KubeSpanPeerStatusController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: kubespan.PeerStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *KubeSpanPeerStatusController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	ticker := time.NewTicker(kubeSpanPeerRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		case <-ticker.C:
		}

		if err := ctrl.reconcile(ctx, r); err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}

func (ctrl *KubeSpanPeerStatusController) reconcile(ctx context.Context, r controller.Runtime) error {
	touchedIDs := map[resource.ID]struct{}{}

	enabled, localID, err := ctrl.localState(ctx, r)
	if err != nil {
		return err
	}

	if enabled {
		if touchedIDs, err = ctrl.updatePeers(ctx, r, localID); err != nil {
			return err
		}
	}

	list, err := safe.ReaderListAll[*kubespan.PeerStatus](ctx, r)
	if err != nil {
		return fmt.Errorf("error listing peer statuses: %w", err)
	}

	for res := range list.All() {
		if _, ok := touchedIDs[res.Metadata().ID()]; !ok {
			if err = r.Destroy(ctx, res.Metadata()); err != nil {
				return fmt.Errorf("error cleaning up peer status: %w", err)
			}
		}
	}

	return nil
}

// updatePeers writes a peer status for every cluster member except the local one, returning the IDs it wrote.
func (ctrl *KubeSpanPeerStatusController) updatePeers(ctx context.Context, r controller.Runtime, localID string) (map[resource.ID]struct{}, error) {
	members, err := safe.ReaderListAll[*cluster.Member](ctx, r)
	if err != nil {
		return nil, fmt.Errorf("error listing members: %w", err)
	}

	touchedIDs := map[resource.ID]struct{}{}
	now := time.Now()

	for member := range members.All() {
		spec := member.TypedSpec()

		if spec.NodeID == "" || spec.NodeID == localID {
			continue
		}

		var endpoint netip.AddrPort

		if len(spec.Addresses) > 0 {
			endpoint = netip.AddrPortFrom(spec.Addresses[0], constants.KubeSpanDefaultPort)
		}

		if err = safe.WriterModify(ctx, r, kubespan.NewPeerStatus(kubespan.NamespaceName, spec.NodeID), func(res *kubespan.PeerStatus) error {
			status := res.TypedSpec()

			status.Label = member.Metadata().ID()
			status.State = kubespan.PeerStateUp

			if status.Endpoint != endpoint {
				status.Endpoint = endpoint
				status.LastUsedEndpoint = endpoint
				status.LastEndpointChange = now
			}

			if now.Sub(status.LastHandshakeTime) >= kubeSpanPeerHandshakeInterval {
				status.LastHandshakeTime = now
			}

			status.ReceiveBytes += rand.Int64N(kubeSpanPeerMaxTrafficPerRefresh)
			status.TransmitBytes += rand.Int64N(kubeSpanPeerMaxTrafficPerRefresh)

			return nil
		}); err != nil {
			return nil, fmt.Errorf("error modifying peer status: %w", err)
		}

		touchedIDs[spec.NodeID] = struct{}{}
	}

	return touchedIDs, nil
}

// localState returns whether KubeSpan is enabled and the local node ID.
func (ctrl *KubeSpanPeerStatusController) localState(ctx context.Context, r controller.Reader) (bool, string, error) {
	cfg, err := safe.ReaderGetByID[*kubespan.Config](ctx, r, kubespan.ConfigID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return false, "", nil
		}

		return false, "", fmt.Errorf("error getting kubespan config: %w", err)
	}

	if !cfg.TypedSpec().Enabled {
		return false, "", nil
	}

	identity, err := safe.ReaderGetByID[*cluster.Identity](ctx, r, cluster.LocalIdentity)
	if err != nil {
		if state.IsNotFoundError(err) {
			return false, "", nil
		}

		return false, "", fmt.Errorf("error getting local identity: %w", err)
	}

	return true, identity.TypedSpec().NodeID, nil
}
