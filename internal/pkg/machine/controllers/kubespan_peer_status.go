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
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
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

// KubeSpanPeerStatusController emulates kubespan.PeerStatus for every cluster affiliate which advertises KubeSpan,
// while KubeSpan is enabled locally.
//
// There is no actual WireGuard mesh: every peer with an endpoint is reported as up, and the traffic counters grow by random amounts.
type KubeSpanPeerStatusController struct{}

// Name implements controller.Controller interface.
func (ctrl *KubeSpanPeerStatusController) Name() string {
	return "kubespan.PeerStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KubeSpanPeerStatusController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: kubespan.NamespaceName,
			Type:      kubespan.IdentityType,
			ID:        optional.Some(kubespan.LocalIdentity),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: cluster.NamespaceName,
			Type:      cluster.AffiliateType,
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
	// the ticker only runs while KubeSpan is enabled, so idle machines are not woken up
	var (
		ticker  *time.Ticker
		tickerC <-chan time.Time
	)

	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		case <-tickerC:
		}

		enabled, err := ctrl.reconcile(ctx, r)
		if err != nil {
			return err
		}

		switch {
		case enabled && ticker == nil:
			ticker = time.NewTicker(kubeSpanPeerRefreshInterval)
			tickerC = ticker.C
		case !enabled && ticker != nil:
			ticker.Stop()

			ticker, tickerC = nil, nil
		}

		r.ResetRestartBackoff()
	}
}

// reconcile updates the peer statuses, returning whether KubeSpan is enabled locally.
func (ctrl *KubeSpanPeerStatusController) reconcile(ctx context.Context, r controller.Runtime) (bool, error) {
	touchedIDs := map[resource.ID]struct{}{}

	identity, err := safe.ReaderGetByID[*kubespan.Identity](ctx, r, kubespan.LocalIdentity)
	if err != nil && !state.IsNotFoundError(err) {
		return false, fmt.Errorf("error getting kubespan identity: %w", err)
	}

	if identity != nil {
		if touchedIDs, err = ctrl.updatePeers(ctx, r, identity.TypedSpec().PublicKey); err != nil {
			return false, err
		}
	}

	list, err := safe.ReaderListAll[*kubespan.PeerStatus](ctx, r)
	if err != nil {
		return false, fmt.Errorf("error listing peer statuses: %w", err)
	}

	for res := range list.All() {
		if _, ok := touchedIDs[res.Metadata().ID()]; !ok {
			if err = r.Destroy(ctx, res.Metadata()); err != nil {
				return false, fmt.Errorf("error cleaning up peer status: %w", err)
			}
		}
	}

	return identity != nil, nil
}

// updatePeers writes a peer status for every other affiliate advertising a KubeSpan public key, returning the IDs it wrote.
func (ctrl *KubeSpanPeerStatusController) updatePeers(ctx context.Context, r controller.Runtime, localPublicKey string) (map[resource.ID]struct{}, error) {
	affiliates, err := safe.ReaderListAll[*cluster.Affiliate](ctx, r)
	if err != nil {
		return nil, fmt.Errorf("error listing affiliates: %w", err)
	}

	touchedIDs := map[resource.ID]struct{}{}
	now := time.Now()

	for affiliate := range affiliates.All() {
		spec := affiliate.TypedSpec()
		publicKey := spec.KubeSpan.PublicKey

		if publicKey == "" || publicKey == localPublicKey {
			continue
		}

		if err = safe.WriterModify(ctx, r, kubespan.NewPeerStatus(kubespan.NamespaceName, publicKey), func(res *kubespan.PeerStatus) error {
			status := res.TypedSpec()

			status.Label = spec.Nodename

			// without an endpoint there is no way to connect to the peer
			// LastUsedEndpoint is kept, as it records the last endpoint which was in use
			if len(spec.KubeSpan.Endpoints) == 0 {
				status.State = kubespan.PeerStateUnknown

				if status.Endpoint.IsValid() {
					status.Endpoint = netip.AddrPort{}
					status.LastEndpointChange = now
				}

				return nil
			}

			status.State = kubespan.PeerStateUp

			if endpoint := spec.KubeSpan.Endpoints[0]; status.Endpoint != endpoint {
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

		touchedIDs[publicKey] = struct{}{}
	}

	return touchedIDs, nil
}
