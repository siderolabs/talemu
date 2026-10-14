// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/machine/services"
)

// recordingStream is a fake server-streaming gRPC stream that records every message sent to it.
// Parameterizing by the response type replaces the per-RPC fake stream servers the tests would
// otherwise duplicate.
type recordingStream[T any] struct {
	grpc.ServerStream

	ctx  context.Context //nolint:containedctx
	sent []T
}

func (s *recordingStream[T]) Context() context.Context { return s.ctx }

func (s *recordingStream[T]) Send(msg T) error {
	s.sent = append(s.sent, msg)

	return nil
}

func newMachineService(t *testing.T) *services.MachineService {
	t.Helper()

	return services.NewMachineService(
		"test-machine-id",
		state.WrapCore(namespaced.NewState(inmem.Build)),
		state.WrapCore(namespaced.NewState(inmem.Build)),
		[]string{"factory.talos.dev"},
		zaptest.NewLogger(t),
		nil,
	)
}

// TestUpgradeNotInstalled asserts MachineService.Upgrade rejects an upgrade when not installed.
func TestUpgradeNotInstalled(t *testing.T) {
	svc := newMachineService(t)

	_, err := svc.Upgrade(t.Context(), &machine.UpgradeRequest{
		Image: "factory.talos.dev/installer/abc123:v1.14.0",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestApplyPartialConfiguration(t *testing.T) {
	partialMachineConfig := `apiVersion: v1alpha1
kind: EventSinkConfig
endpoint: "[fdae:41e4:649b:9303::1]:8090"
---
apiVersion: v1alpha1
kind: KmsgLogConfig
name: omni-kmsg
url: "tcp://[fdae:41e4:649b:9303::1]:8092"`
	apidState := state.WrapCore(namespaced.NewState(inmem.Build))
	globalState := state.WrapCore(namespaced.NewState(inmem.Build))
	logger := zaptest.NewLogger(t)
	machineService := services.NewMachineService("test-machine-id", apidState, globalState, []string{"factory.talos.dev"}, logger, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	resp, err := machineService.ApplyConfiguration(ctx, &machine.ApplyConfigurationRequest{
		Data: []byte(partialMachineConfig),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.NoError(t, err)

	require.Equal(t, machine.ApplyConfigurationRequest_NO_REBOOT, resp.Messages[0].Mode)
}

const (
	testMachineID = "test-machine-id"
	testClusterID = "TqxlaYi/ModMwxjC8OCOgYFSh+ODSt7eB/exsuyUpJs="
)

// completeConfig is a Talos 1.14 style config as Omni generates it: no .machine.install section, since the
// install image reaches the machine through the install and upgrade APIs instead.
const completeConfig = `version: v1alpha1
machine:
    type: worker
cluster: {}
---
apiVersion: v1alpha1
kind: DiscoveryIdentityConfig
clusterID: TqxlaYi/ModMwxjC8OCOgYFSh+ODSt7eB/exsuyUpJs=
clusterSecret: c2VjcmV0
`

// legacyConfigWithInstallImage is the pre-1.14 equivalent, which still carries the installer image.
const legacyConfigWithInstallImage = `version: v1alpha1
machine:
    type: worker
    install:
        image: factory.talos.dev/metal-installer/abc123:v1.13.8
cluster: {}
---
apiVersion: v1alpha1
kind: DiscoveryIdentityConfig
clusterID: TqxlaYi/ModMwxjC8OCOgYFSh+ODSt7eB/exsuyUpJs=
clusterSecret: c2VjcmV0
`

// newMachineServiceWithSecurityState builds a MachineService over a state carrying the SecurityState that
// validateInstaller reads, plus the global MachineStatus that applying a complete config updates.
func newMachineServiceWithSecurityState(t *testing.T, secureBoot bool) (*services.MachineService, state.State) {
	t.Helper()

	globalState := state.WrapCore(namespaced.NewState(inmem.Build))

	require.NoError(t, globalState.Create(t.Context(), emu.NewMachineStatus(emu.NamespaceName, testMachineID)))

	return services.NewMachineService(
		testMachineID,
		newLifecycleState(t, secureBoot),
		globalState,
		factoryHosts,
		zaptest.NewLogger(t),
		nil,
	), globalState
}

// TestApplyCompleteConfigWithoutInstallImage asserts a secure boot machine accepts a config that carries no
// installer image, which is every config generated for Talos 1.14 and later.
func TestApplyCompleteConfigWithoutInstallImage(t *testing.T) {
	svc, globalState := newMachineServiceWithSecurityState(t, true)

	resp, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(completeConfig),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.NoError(t, err)
	require.Equal(t, machine.ApplyConfigurationRequest_REBOOT, resp.Messages[0].Mode) //nolint:staticcheck

	// the machine got allocated to the cluster the config identifies, which only happens once the apply runs
	// all the way through
	machineStatus, err := safe.ReaderGetByID[*emu.MachineStatus](t.Context(), globalState, testMachineID)
	require.NoError(t, err)

	cluster, _ := machineStatus.Metadata().Labels().Get(emu.LabelCluster)
	require.Equal(t, testClusterID, cluster)

	_, isWorker := machineStatus.Metadata().Labels().Get(emu.LabelWorkerRole)
	require.True(t, isWorker)
}

// TestApplyCompleteConfigNonSecureBootInstallImage asserts that a config which does carry an installer image is
// still checked against the secure boot state of the machine.
func TestApplyCompleteConfigNonSecureBootInstallImage(t *testing.T) {
	svc, _ := newMachineServiceWithSecurityState(t, true)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(legacyConfigWithInstallImage),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestReadBootID(t *testing.T) {
	svc := newMachineService(t)
	srv := &recordingStream[*common.Data]{ctx: t.Context()}

	require.NoError(t, svc.Read(&machine.ReadRequest{Path: services.BootIDPath}, srv))
	require.Len(t, srv.sent, 1)
	require.NotEmpty(t, strings.TrimSpace(string(srv.sent[0].GetBytes())))
}

func TestReadUnsupportedPath(t *testing.T) {
	svc := newMachineService(t)

	err := svc.Read(&machine.ReadRequest{Path: "/etc/hostname"}, &recordingStream[*common.Data]{ctx: t.Context()})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

// TestRebootRotatesBootID asserts a reboot rotates the boot ID returned by Read.
func TestRebootRotatesBootID(t *testing.T) {
	svc := newMachineService(t)

	read := func() string {
		srv := &recordingStream[*common.Data]{ctx: t.Context()}
		require.NoError(t, svc.Read(&machine.ReadRequest{Path: services.BootIDPath}, srv))

		return strings.TrimSpace(string(srv.sent[0].GetBytes()))
	}

	before := read()

	_, err := svc.Reboot(t.Context(), &machine.RebootRequest{})
	require.NoError(t, err)

	require.NotEqual(t, before, read())
}

func TestRebootUpdatesBootIDResource(t *testing.T) {
	st := state.WrapCore(namespaced.NewState(inmem.Build))
	svc := services.NewMachineService(testMachineID, st, state.WrapCore(namespaced.NewState(inmem.Build)), factoryHosts, zaptest.NewLogger(t), nil)

	_, err := svc.Reboot(t.Context(), &machine.RebootRequest{})
	require.NoError(t, err)

	srv := &recordingStream[*common.Data]{ctx: t.Context()}
	require.NoError(t, svc.Read(&machine.ReadRequest{Path: services.BootIDPath}, srv))

	bootID, err := safe.ReaderGetByID[*runtime.BootID](t.Context(), st, runtime.BootIDID)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(srv.sent[0].GetBytes())), bootID.TypedSpec().BootID)
}

// triedConfig is completeConfig with an extra document, so that it can be told apart from completeConfig.
const triedConfig = completeConfig + `---
apiVersion: v1alpha1
kind: KmsgLogConfig
name: omni-kmsg
url: "tcp://[fdae:41e4:649b:9303::1]:8092"
`

// newInstalledMachineService returns a MachineService whose machine already runs completeConfig, and the state it stores the config in.
//
// It logs nowhere, as the try mode rollback timer logs after the test returns.
func newInstalledMachineService(t *testing.T) (*services.MachineService, state.State) {
	t.Helper()

	st := newLifecycleState(t, true)
	globalState := state.WrapCore(namespaced.NewState(inmem.Build))

	require.NoError(t, globalState.Create(t.Context(), emu.NewMachineStatus(emu.NamespaceName, testMachineID)))

	svc := services.NewMachineService(testMachineID, st, globalState, factoryHosts, zap.NewNop(), nil)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(completeConfig),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.NoError(t, err)

	return svc, st
}

// isRolledBack reports whether the active config has the given number of documents and the persistent config is destroyed.
func isRolledBack(t *testing.T, st state.State, documents int) bool {
	t.Helper()

	_, err := safe.ReaderGetByID[*config.MachineConfig](t.Context(), st, config.PersistentID)

	return state.IsNotFoundError(err) && activeConfigDocuments(t, st) == documents
}

func requireNoPersistentConfig(t *testing.T, st state.State) {
	t.Helper()

	_, err := safe.ReaderGetByID[*config.MachineConfig](t.Context(), st, config.PersistentID)
	require.True(t, state.IsNotFoundError(err), "the persistent config should be destroyed, got: %v", err)
}

func activeConfigDocuments(t *testing.T, st state.State) int {
	t.Helper()

	cfg, err := safe.ReaderGetByID[*config.MachineConfig](t.Context(), st, config.ActiveID)
	require.NoError(t, err)

	return len(cfg.Provider().Documents())
}

func TestApplyTryModeRollsBack(t *testing.T) {
	svc, st := newInstalledMachineService(t)
	documents := activeConfigDocuments(t, st)

	resp, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(100 * time.Millisecond),
	})
	require.NoError(t, err)
	require.Equal(t, machine.ApplyConfigurationRequest_TRY, resp.Messages[0].Mode)
	require.Equal(t, documents+1, activeConfigDocuments(t, st))

	require.Eventually(t, func() bool {
		return isRolledBack(t, st, documents)
	}, 5*time.Second, 50*time.Millisecond, "the tried config should be rolled back")
}

func TestApplyTryModeConfirmed(t *testing.T) {
	svc, st := newInstalledMachineService(t)
	documents := activeConfigDocuments(t, st)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(100 * time.Millisecond),
	})
	require.NoError(t, err)

	_, err = svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(triedConfig),
		Mode: machine.ApplyConfigurationRequest_NO_REBOOT,
	})
	require.NoError(t, err)

	require.Never(t, func() bool {
		return activeConfigDocuments(t, st) != documents+1
	}, 500*time.Millisecond, 50*time.Millisecond, "the confirmed config should not be rolled back")

	requireNoPersistentConfig(t, st)
}

func TestApplyTryModeConfirmedKeptAfterRestart(t *testing.T) {
	svc, st := newInstalledMachineService(t)
	documents := activeConfigDocuments(t, st)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(time.Hour),
	})
	require.NoError(t, err)

	_, err = svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(triedConfig),
		Mode: machine.ApplyConfigurationRequest_NO_REBOOT,
	})
	require.NoError(t, err)

	restored, err := machineconfig.RestorePersistent(t.Context(), st)
	require.NoError(t, err)
	require.False(t, restored)
	require.Equal(t, documents+1, activeConfigDocuments(t, st))
}

func TestApplyTryModeRepeatedRollsBackToConfigBeforeFirstTry(t *testing.T) {
	svc, st := newInstalledMachineService(t)
	documents := activeConfigDocuments(t, st)

	for range 2 {
		_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
			Data:           []byte(triedConfig),
			Mode:           machine.ApplyConfigurationRequest_TRY,
			TryModeTimeout: durationpb.New(100 * time.Millisecond),
		})
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool {
		return isRolledBack(t, st, documents)
	}, 5*time.Second, 50*time.Millisecond, "the config from before the first try should be restored")
}

func TestApplyTryModeRolledBackAfterRestart(t *testing.T) {
	svc, st := newInstalledMachineService(t)
	documents := activeConfigDocuments(t, st)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, documents+1, activeConfigDocuments(t, st))

	// the machine runs this on startup, the rollback timer is lost with the restart
	restored, err := machineconfig.RestorePersistent(t.Context(), st)
	require.NoError(t, err)
	require.True(t, restored)
	require.Equal(t, documents, activeConfigDocuments(t, st))

	requireNoPersistentConfig(t, st)
}

func TestRebootRollsBackTryMode(t *testing.T) {
	svc, st := newInstalledMachineService(t)
	documents := activeConfigDocuments(t, st)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, documents+1, activeConfigDocuments(t, st))

	_, err = svc.Reboot(t.Context(), &machine.RebootRequest{})
	require.NoError(t, err)
	require.Equal(t, documents, activeConfigDocuments(t, st))

	requireNoPersistentConfig(t, st)
}

func TestResetCancelsTryModeRollback(t *testing.T) {
	svc, st := newInstalledMachineService(t)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(100 * time.Millisecond),
	})
	require.NoError(t, err)

	_, err = svc.Reset(t.Context(), &machine.ResetRequest{
		SystemPartitionsToWipe: []*machine.ResetPartitionSpec{{Label: "STATE"}},
	})
	require.NoError(t, err)

	require.Never(t, func() bool {
		_, getErr := safe.ReaderGetByID[*config.MachineConfig](t.Context(), st, config.ActiveID)

		return !state.IsNotFoundError(getErr)
	}, 500*time.Millisecond, 50*time.Millisecond, "the reset machine should not get the previous config back")

	requireNoPersistentConfig(t, st)
}

func TestApplyTryModeWithoutConfig(t *testing.T) {
	svc, _ := newMachineServiceWithSecurityState(t, true)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(completeConfig),
		Mode: machine.ApplyConfigurationRequest_TRY,
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

const maintenanceConfig = `apiVersion: v1alpha1
kind: EventSinkConfig
endpoint: "[fdae:41e4:649b:9303::1]:8090"
`

const triedMaintenanceConfig = maintenanceConfig + `---
apiVersion: v1alpha1
kind: KmsgLogConfig
name: omni-kmsg
url: "tcp://[fdae:41e4:649b:9303::1]:8092"
`

func TestApplyTryModeInMaintenanceRollsBack(t *testing.T) {
	st := newLifecycleState(t, true)
	svc := services.NewMachineService(testMachineID, st, state.WrapCore(namespaced.NewState(inmem.Build)), factoryHosts, zap.NewNop(), nil)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(maintenanceConfig),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.NoError(t, err)

	resp, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedMaintenanceConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(100 * time.Millisecond),
	})
	require.NoError(t, err)
	require.Equal(t, machine.ApplyConfigurationRequest_TRY, resp.Messages[0].Mode)
	require.Equal(t, 2, activeConfigDocuments(t, st))

	require.Eventually(t, func() bool {
		return isRolledBack(t, st, 1)
	}, 5*time.Second, 50*time.Millisecond, "the tried maintenance config should be rolled back")
}

func TestFailedResetKeepsTryModeRollback(t *testing.T) {
	st := newLifecycleState(t, true)
	svc := services.NewMachineService(testMachineID, st, state.WrapCore(namespaced.NewState(inmem.Build)), factoryHosts, zap.NewNop(), nil)

	_, err := svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data: []byte(maintenanceConfig),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.NoError(t, err)

	_, err = svc.ApplyConfiguration(t.Context(), &machine.ApplyConfigurationRequest{
		Data:           []byte(triedMaintenanceConfig),
		Mode:           machine.ApplyConfigurationRequest_TRY,
		TryModeTimeout: durationpb.New(100 * time.Millisecond),
	})
	require.NoError(t, err)

	_, err = svc.Reset(t.Context(), &machine.ResetRequest{
		SystemPartitionsToWipe: []*machine.ResetPartitionSpec{{Label: "STATE"}},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	require.Eventually(t, func() bool {
		return isRolledBack(t, st, 1)
	}, 5*time.Second, 50*time.Millisecond, "the tried maintenance config should be rolled back after a failed reset")
}
