// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers_test

import (
	"context"
	"net/netip"
	"testing"

	"github.com/cosi-project/runtime/pkg/controller"
	cosiruntime "github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/rtestutils"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	configv1alpha1 "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
)

const kubeSpanTestClusterID = "8XuV9TZHW08DOk3bVxQjH9ih_TBKjnh-j44tsCLSBzo="

// TestKubeSpanConfigController verifies that the KubeSpan config follows the machine config.
func TestKubeSpanConfigController(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := startKubeSpanRuntime(t, controllers.NewKubeSpanConfigController())

	provider, err := container.New(&configv1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &configv1alpha1.MachineConfig{
			MachineNetwork: &configv1alpha1.NetworkConfig{ //nolint:staticcheck // legacy config
				NetworkKubeSpan: &configv1alpha1.NetworkKubeSpan{ //nolint:staticcheck // legacy config
					KubeSpanEnabled: new(true),
				},
			},
		},
		ClusterConfig: &configv1alpha1.ClusterConfig{
			ClusterID:     kubeSpanTestClusterID,                          //nolint:staticcheck // legacy config
			ClusterSecret: "I+1In7fLnpcRIjUmEoeugZnSyFoTF6MztLxICL5Yu0s=", //nolint:staticcheck // legacy config
		},
	})
	require.NoError(t, err)

	require.NoError(t, st.Create(ctx, config.NewMachineConfig(provider)))

	rtestutils.AssertResource(ctx, t, st, kubespan.ConfigID, func(res *kubespan.Config, a *assert.Assertions) {
		a.True(res.TypedSpec().Enabled)
		a.Equal(kubeSpanTestClusterID, res.TypedSpec().ClusterID)
	})
}

// TestKubeSpanIdentityController verifies that the identity exists only while KubeSpan is enabled, keeps its key
// across config changes, and gets an address in the cluster KubeSpan subnet.
func TestKubeSpanIdentityController(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := startKubeSpanRuntime(t, &controllers.KubeSpanIdentityController{})

	cfg := kubespan.NewConfig(config.NamespaceName, kubespan.ConfigID)
	cfg.TypedSpec().Enabled = true
	cfg.TypedSpec().ClusterID = kubeSpanTestClusterID

	require.NoError(t, st.Create(ctx, cfg))

	var publicKey string

	rtestutils.AssertResource(ctx, t, st, kubespan.LocalIdentity, func(res *kubespan.Identity, a *assert.Assertions) {
		spec := res.TypedSpec()

		a.NotEmpty(spec.PrivateKey)
		a.NotEmpty(spec.PublicKey)
		a.Equal(network.ULAPrefix(kubeSpanTestClusterID, network.ULAKubeSpan), spec.Subnet)
		a.True(spec.Subnet.Contains(spec.Address.Addr()))
		a.Equal(128, spec.Address.Bits())

		publicKey = spec.PublicKey
	})

	// an unrelated config change must not rotate the key
	_, err := safe.StateUpdateWithConflicts(ctx, st, cfg.Metadata(), func(res *kubespan.Config) error {
		res.TypedSpec().MTU = 1380

		return nil
	})
	require.NoError(t, err)

	rtestutils.AssertResource(ctx, t, st, kubespan.LocalIdentity, func(res *kubespan.Identity, a *assert.Assertions) {
		a.Equal(publicKey, res.TypedSpec().PublicKey)
	})

	_, err = safe.StateUpdateWithConflicts(ctx, st, cfg.Metadata(), func(res *kubespan.Config) error {
		res.TypedSpec().Enabled = false

		return nil
	})
	require.NoError(t, err)

	rtestutils.AssertNoResource[*kubespan.Identity](ctx, t, st, kubespan.LocalIdentity)
}

// TestKubeSpanPeerStatusController verifies that peers are built from the affiliates advertising KubeSpan, excluding
// the local node, and are cleaned up once KubeSpan is disabled.
func TestKubeSpanPeerStatusController(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := startKubeSpanRuntime(t, &controllers.KubeSpanPeerStatusController{})

	localKey := newKubeSpanTestKey(t)
	upKey := newKubeSpanTestKey(t)
	noEndpointKey := newKubeSpanTestKey(t)

	endpoint := netip.MustParseAddrPort("10.5.0.3:51820")

	identity := kubespan.NewIdentity(kubespan.NamespaceName, kubespan.LocalIdentity)
	identity.TypedSpec().PublicKey = localKey

	require.NoError(t, st.Create(ctx, identity))

	for _, affiliate := range []struct {
		id        string
		nodename  string
		publicKey string
		endpoints []netip.AddrPort
	}{
		{id: "local", nodename: "local-node", publicKey: localKey, endpoints: []netip.AddrPort{netip.MustParseAddrPort("10.5.0.2:51820")}},
		{id: "up", nodename: "up-node", publicKey: upKey, endpoints: []netip.AddrPort{endpoint}},
		{id: "no-endpoint", nodename: "no-endpoint-node", publicKey: noEndpointKey},
		{id: "no-kubespan", nodename: "no-kubespan-node"},
	} {
		res := cluster.NewAffiliate(cluster.NamespaceName, affiliate.id)
		res.TypedSpec().Nodename = affiliate.nodename
		res.TypedSpec().KubeSpan.PublicKey = affiliate.publicKey
		res.TypedSpec().KubeSpan.Endpoints = affiliate.endpoints

		require.NoError(t, st.Create(ctx, res))
	}

	rtestutils.AssertResource(ctx, t, st, upKey, func(res *kubespan.PeerStatus, a *assert.Assertions) {
		spec := res.TypedSpec()

		a.Equal("up-node", spec.Label)
		a.Equal(kubespan.PeerStateUp, spec.State)
		a.Equal(endpoint, spec.Endpoint)
		a.Equal(endpoint, spec.LastUsedEndpoint)
		a.False(spec.LastHandshakeTime.IsZero())
	})

	rtestutils.AssertResource(ctx, t, st, noEndpointKey, func(res *kubespan.PeerStatus, a *assert.Assertions) {
		a.Equal("no-endpoint-node", res.TypedSpec().Label)
		a.Equal(kubespan.PeerStateUnknown, res.TypedSpec().State)
		a.Zero(res.TypedSpec().ReceiveBytes)
	})

	rtestutils.AssertLength[*kubespan.PeerStatus](ctx, t, st, 2)

	// a peer which loses all its endpoints must not keep reporting the old one
	_, err := safe.StateUpdateWithConflicts(ctx, st, cluster.NewAffiliate(cluster.NamespaceName, "up").Metadata(), func(res *cluster.Affiliate) error {
		res.TypedSpec().KubeSpan.Endpoints = nil

		return nil
	})
	require.NoError(t, err)

	rtestutils.AssertResource(ctx, t, st, upKey, func(res *kubespan.PeerStatus, a *assert.Assertions) {
		a.Equal(kubespan.PeerStateUnknown, res.TypedSpec().State)
		a.False(res.TypedSpec().Endpoint.IsValid())
		a.Equal(endpoint, res.TypedSpec().LastUsedEndpoint)
	})

	require.NoError(t, st.Destroy(ctx, identity.Metadata()))

	rtestutils.AssertLength[*kubespan.PeerStatus](ctx, t, st, 0)
}

func startKubeSpanRuntime(t *testing.T, ctrl controller.Controller) state.State {
	t.Helper()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	rt, err := cosiruntime.NewRuntime(st, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(ctrl))

	runtimeCtx, stopRuntime := context.WithCancel(t.Context())

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	return st
}

func newKubeSpanTestKey(t *testing.T) resource.ID {
	t.Helper()

	key, err := wgtypes.GeneratePrivateKey()
	require.NoError(t, err)

	return key.PublicKey().String()
}
