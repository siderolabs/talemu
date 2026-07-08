// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers_test

import (
	"context"
	"testing"

	cosiruntime "github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/resource/rtestutils"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	configv1alpha1 "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

var etcdServiceID = v1alpha1.NewService(constants.ETCDService).Metadata().ID()

// TestEtcdControllerReconcilesOnConfig verifies that a control-plane machine brings up its etcd service off
// its own config event, without needing a cluster-status change to trigger the reconcile. This is the path a
// control plane joining an already-bootstrapped cluster takes (e.g. after a maintenance upgrade delays its
// config apply past bootstrap): the config lands after the bootstrap event, so etcd must come up on the
// config event alone.
func TestEtcdControllerReconcilesOnConfig(t *testing.T) {
	t.Parallel()

	const (
		machineID = "test-machine"
		clusterID = "test-cluster"
	)

	ctx := t.Context()

	globalState := state.WrapCore(namespaced.NewState(inmem.Build))
	localState := startEtcdController(t, ctx, globalState, machineID)

	// A control-plane config with no cluster status present yet. On the fixed controller the config event
	// triggers a reconcile that publishes the etcd service (not yet healthy, still waiting for bootstrap). The
	// pre-fix controller ignored config events, so the service would never appear.
	require.NoError(t, localState.Create(ctx, controlPlaneConfig(t, clusterID)))

	rtestutils.AssertResource(ctx, t, localState, etcdServiceID, func(res *v1alpha1.Service, a *assert.Assertions) {
		a.False(res.TypedSpec().Healthy, "etcd must not be healthy before the cluster is bootstrapped")
		a.False(res.TypedSpec().Running, "etcd must not be running before the cluster is bootstrapped")
	})

	// Bootstrapping the cluster must flip etcd to healthy.
	bootstrapCluster(t, ctx, globalState, clusterID)

	rtestutils.AssertResource(ctx, t, localState, etcdServiceID, func(res *v1alpha1.Service, a *assert.Assertions) {
		a.True(res.TypedSpec().Healthy, "etcd must become healthy once the cluster is bootstrapped")
		a.True(res.TypedSpec().Running)
	})
}

// TestEtcdControllerControlPlanesRegisterOnBootstrap reproduces a rolling control-plane join: several control
// planes, whose configs land at different times relative to the cluster bootstrap, must each bring up etcd.
// The integration failure was a 3-CP cluster stuck at 2 etcd members because one CP never registered.
func TestEtcdControllerControlPlanesRegisterOnBootstrap(t *testing.T) {
	t.Parallel()

	const clusterID = "test-cluster"

	ctx := t.Context()

	globalState := state.WrapCore(namespaced.NewState(inmem.Build))

	// Model different config-vs-bootstrap orderings across the control planes: two apply their config before
	// the cluster bootstraps, the third only after (the case that regressed).
	early := []string{"1", "2"}
	late := "3"

	localStates := map[string]state.State{}

	for _, id := range early {
		localStates[id] = startEtcdController(t, ctx, globalState, id)
		require.NoError(t, localStates[id].Create(ctx, controlPlaneConfig(t, clusterID)))
	}

	localStates[late] = startEtcdController(t, ctx, globalState, late)

	bootstrapCluster(t, ctx, globalState, clusterID)

	require.NoError(t, localStates[late].Create(ctx, controlPlaneConfig(t, clusterID)))

	for id, localState := range localStates {
		rtestutils.AssertResource(ctx, t, localState, etcdServiceID, func(res *v1alpha1.Service, a *assert.Assertions) {
			a.Truef(res.TypedSpec().Healthy, "etcd must be healthy on control plane %q", id)
		})

		rtestutils.AssertResource(ctx, t, globalState, id, func(res *emu.MachineStatus, a *assert.Assertions) {
			a.NotEmptyf(res.TypedSpec().Value.EtcdMemberId, "control plane %q must register an etcd member", id)
		})
	}
}

// startEtcdController stands up an in-memory machine runtime with only the EtcdController registered, sharing
// globalState with the other machines, and returns the machine's local state. It mirrors the provider seeding
// the machine's global status before the runtime starts.
func startEtcdController(t *testing.T, ctx context.Context, globalState state.State, machineID string) state.State {
	t.Helper()

	localState := state.WrapCore(namespaced.NewState(inmem.Build))

	require.NoError(t, globalState.Create(ctx, emu.NewMachineStatus(emu.NamespaceName, machineID)))

	rt, err := cosiruntime.NewRuntime(localState, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(&controllers.EtcdController{
		GlobalState: globalState,
		MachineID:   machineID,
	}))

	// Wait for the runtime to fully stop before the test returns, otherwise its goroutine can log through the
	// zaptest logger after the test has completed and panic.
	runtimeCtx, stopRuntime := context.WithCancel(ctx)

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	return localState
}

func bootstrapCluster(t *testing.T, ctx context.Context, globalState state.State, clusterID string) {
	t.Helper()

	clusterStatus := emu.NewClusterStatus(emu.NamespaceName, clusterID)
	clusterStatus.TypedSpec().Value.Bootstrapped = true

	require.NoError(t, globalState.Create(ctx, clusterStatus))
}

func controlPlaneConfig(t *testing.T, clusterID string) *config.MachineConfig {
	t.Helper()

	provider, err := container.New(&configv1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &configv1alpha1.MachineConfig{
			MachineType: "controlplane",
			MachineInstall: &configv1alpha1.InstallConfig{
				InstallImage: "factory.talos.dev/installer/abc123:v1.14.0",
			},
		},
		ClusterConfig: &configv1alpha1.ClusterConfig{
			ClusterID: clusterID,
		},
	})
	require.NoError(t, err)

	return config.NewMachineConfig(provider)
}
