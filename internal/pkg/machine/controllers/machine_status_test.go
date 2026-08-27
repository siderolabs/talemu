// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers_test

import (
	"context"
	"strings"
	"testing"

	cosiruntime "github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/resource/rtestutils"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	configv1alpha1 "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// TestMachineStatusStuckBooting verifies the magic kernel arg that makes the emulated machine act broken:
// with the arg in the boot media the machine reports the booting stage and never becomes ready, and once
// the arg is gone (the fixing schematic is installed) the machine recovers.
func TestMachineStatusStuckBooting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	localState := state.WrapCore(namespaced.NewState(inmem.Build))
	hostState := state.WrapCore(namespaced.NewState(inmem.Build))

	// The disk the config's install section points at.
	require.NoError(t, hostState.Create(ctx, block.NewDisk(block.NamespaceName, "vda")))

	rt, err := cosiruntime.NewRuntime(localState, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(&controllers.MachineStatusController{State: hostState}))

	runtimeCtx, stopRuntime := context.WithCancel(ctx)

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	// A worker config: only the apid service feeds the machine readiness.
	provider, err := container.New(&configv1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &configv1alpha1.MachineConfig{
			MachineType: "worker",
			MachineInstall: &configv1alpha1.InstallConfig{
				InstallDisk:  "/dev/vda",
				InstallImage: "factory.talos.dev/installer/abc123:v1.14.0",
			},
		},
		ClusterConfig: &configv1alpha1.ClusterConfig{ClusterID: "test-cluster"},
	})
	require.NoError(t, err)

	require.NoError(t, localState.Create(ctx, config.NewMachineConfig(provider)))

	apid := v1alpha1.NewService(constants.APIDService)
	apid.TypedSpec().Healthy = true
	apid.TypedSpec().Running = true
	require.NoError(t, localState.Create(ctx, apid))

	version := talos.NewVersion(talos.NamespaceName, talos.VersionID)
	version.TypedSpec().Value.Value = "v1.14.0"
	require.NoError(t, localState.Create(ctx, version))

	machineStatusID := runtime.NewMachineStatus().Metadata().ID()

	// A healthy machine: running and ready.
	rtestutils.AssertResource(ctx, t, localState, machineStatusID, func(res *runtime.MachineStatus, a *assert.Assertions) {
		a.Equal(runtime.MachineStageRunning, res.TypedSpec().Stage)
		a.True(res.TypedSpec().Status.Ready)
	})

	// The boot media now carries the magic arg (a bad schematic was installed).
	cmdline := runtime.NewKernelCmdline()
	cmdline.TypedSpec().Cmdline = "console=ttyS0 " + constants.StuckBootingKernelArg + " talemu=1"
	require.NoError(t, localState.Create(ctx, cmdline))

	rtestutils.AssertResource(ctx, t, localState, machineStatusID, func(res *runtime.MachineStatus, a *assert.Assertions) {
		a.Equal(runtime.MachineStageBooting, res.TypedSpec().Stage)
		a.False(res.TypedSpec().Status.Ready)

		conditions := res.TypedSpec().Status.UnmetConditions
		if a.NotEmpty(conditions) {
			a.Contains(conditions[0].Reason, "kubelet is unhealthy")
		}
	})

	// The fixing schematic without the arg is installed: the machine recovers.
	_, err = safe.StateUpdateWithConflicts(ctx, localState, cmdline.Metadata(), func(res *runtime.KernelCmdline) error {
		res.TypedSpec().Cmdline = strings.ReplaceAll(res.TypedSpec().Cmdline, constants.StuckBootingKernelArg+" ", "")

		return nil
	})
	require.NoError(t, err)

	rtestutils.AssertResource(ctx, t, localState, machineStatusID, func(res *runtime.MachineStatus, a *assert.Assertions) {
		a.Equal(runtime.MachineStageRunning, res.TypedSpec().Stage)
		a.True(res.TypedSpec().Status.Ready)
	})
}
