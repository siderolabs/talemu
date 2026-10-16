// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers_test

import (
	"context"
	"testing"

	"github.com/cosi-project/runtime/pkg/controller"
	cosiruntime "github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/resource/rtestutils"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	configv1alpha1 "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

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
