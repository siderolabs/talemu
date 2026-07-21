// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services_test

import (
	"context"
	"sync"
	"testing"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
	"github.com/siderolabs/talemu/internal/pkg/machine/services"
)

const (
	factoryHost    = "factory.talos.dev"
	lifecycleImage = "factory.talos.dev/installer/abc123:v1.14.0"
)

// blockingInstallServer blocks inside the first Send until released, keeping the install in flight.
type blockingInstallServer struct {
	grpc.ServerStream

	ctx     context.Context //nolint:containedctx
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingInstallServer) Context() context.Context { return b.ctx }

func (b *blockingInstallServer) Send(*machine.LifecycleServiceInstallResponse) error {
	b.once.Do(func() {
		close(b.started)
		<-b.release
	})

	return nil
}

// newLifecycleState builds an in-memory state seeded with the SecurityState resource that
// validateInstaller requires.
func newLifecycleState(t *testing.T, secureBoot bool) state.State {
	t.Helper()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	securityState := runtime.NewSecurityStateSpec(runtime.NamespaceName)
	securityState.TypedSpec().SecureBoot = secureBoot

	require.NoError(t, st.Create(t.Context(), securityState))

	return st
}

func installRequest(disk string) *machine.LifecycleServiceInstallRequest {
	return &machine.LifecycleServiceInstallRequest{
		Source:      &machine.InstallArtifactsSource{ImageName: lifecycleImage},
		Destination: &machine.InstallDestination{Disk: disk},
	}
}

func TestLifecycleInstall(t *testing.T) {
	st := newLifecycleState(t, false)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	srv := &recordingStream[*machine.LifecycleServiceInstallResponse]{ctx: t.Context()}
	require.NoError(t, svc.Install(installRequest("/dev/sda"), srv))

	// progress messages terminated by an exit code of 0.
	require.NotEmpty(t, srv.sent)
	require.NotEmpty(t, srv.sent[0].GetProgress().GetMessage())
	require.Equal(t, int32(0), srv.sent[len(srv.sent)-1].GetProgress().GetExitCode())

	systemDisk, err := safe.ReaderGetByID[*block.SystemDisk](t.Context(), st, block.SystemDiskID)
	require.NoError(t, err)
	require.Equal(t, "sda", systemDisk.TypedSpec().DiskID)
}

func TestLifecycleInstallEmptyImage(t *testing.T) {
	st := newLifecycleState(t, false)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	err := svc.Install(&machine.LifecycleServiceInstallRequest{
		Source:      &machine.InstallArtifactsSource{ImageName: ""},
		Destination: &machine.InstallDestination{Disk: "/dev/sda"},
	}, &recordingStream[*machine.LifecycleServiceInstallResponse]{ctx: t.Context()})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestLifecycleInstallEmptyDisk(t *testing.T) {
	st := newLifecycleState(t, false)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	err := svc.Install(installRequest(""), &recordingStream[*machine.LifecycleServiceInstallResponse]{ctx: t.Context()})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestLifecycleInstallAlreadyInstalled(t *testing.T) {
	st := newLifecycleState(t, false)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	require.NoError(t, svc.Install(installRequest("/dev/sda"), &recordingStream[*machine.LifecycleServiceInstallResponse]{ctx: t.Context()}))

	err := svc.Install(installRequest("/dev/sda"), &recordingStream[*machine.LifecycleServiceInstallResponse]{ctx: t.Context()})
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestLifecycleInstallSecureBootRejectsNonSecurebootImage(t *testing.T) {
	st := newLifecycleState(t, true)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	err := svc.Install(installRequest("/dev/sda"), &recordingStream[*machine.LifecycleServiceInstallResponse]{ctx: t.Context()})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestLifecycleUpgradeNotInstalled(t *testing.T) {
	st := newLifecycleState(t, false)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	err := svc.Upgrade(&machine.LifecycleServiceUpgradeRequest{
		Source: &machine.InstallArtifactsSource{ImageName: lifecycleImage},
	}, &recordingStream[*machine.LifecycleServiceUpgradeResponse]{ctx: t.Context()})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestLifecycleUpgrade(t *testing.T) {
	st := newLifecycleState(t, false)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	require.NoError(t, svc.Install(installRequest("/dev/sda"), &recordingStream[*machine.LifecycleServiceInstallResponse]{ctx: t.Context()}))

	srv := &recordingStream[*machine.LifecycleServiceUpgradeResponse]{ctx: t.Context()}
	err := svc.Upgrade(&machine.LifecycleServiceUpgradeRequest{
		Source: &machine.InstallArtifactsSource{ImageName: "factory-enterprise.staging.talos.dev/installer/abc123:v1.14.0"},
	}, srv)
	require.NoError(t, err)

	// progress messages terminated by an exit code of 0.
	require.NotEmpty(t, srv.sent)
	require.NotEmpty(t, srv.sent[0].GetProgress().GetMessage())
	require.Equal(t, int32(0), srv.sent[len(srv.sent)-1].GetProgress().GetExitCode())

	image, err := safe.ReaderGetByID[*talos.Image](t.Context(), st, talos.ImageID)
	require.NoError(t, err)
	require.Equal(t, "factory-enterprise.staging.talos.dev", image.TypedSpec().Value.Host)
	require.Equal(t, "v1.14.0", image.TypedSpec().Value.Version)
}

func TestLifecycleConcurrentOperationRejected(t *testing.T) {
	st := newLifecycleState(t, false)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	blocking := &blockingInstallServer{
		ctx:     t.Context(),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	done := make(chan error, 1)

	go func() { done <- svc.Install(installRequest("/dev/sda"), blocking) }()

	<-blocking.started // the install is now in flight, holding the lock.

	err := svc.Upgrade(&machine.LifecycleServiceUpgradeRequest{
		Source: &machine.InstallArtifactsSource{ImageName: lifecycleImage},
	}, &recordingStream[*machine.LifecycleServiceUpgradeResponse]{ctx: t.Context()})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "already in progress")

	close(blocking.release)
	require.NoError(t, <-done)
}

func TestLifecycleUpgradeSecureBootRejectsNonSecurebootImage(t *testing.T) {
	st := newLifecycleState(t, true)
	svc := services.NewLifecycleService(st, factoryHost, zaptest.NewLogger(t), nil)

	err := svc.Upgrade(&machine.LifecycleServiceUpgradeRequest{
		Source: &machine.InstallArtifactsSource{ImageName: lifecycleImage},
	}, &recordingStream[*machine.LifecycleServiceUpgradeResponse]{ctx: t.Context()})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
