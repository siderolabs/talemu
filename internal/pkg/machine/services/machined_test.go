// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
		"factory.talos.dev",
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
	machineService := services.NewMachineService("test-machine-id", apidState, globalState, "factory.talos.dev", logger, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	resp, err := machineService.ApplyConfiguration(ctx, &machine.ApplyConfigurationRequest{
		Data: []byte(partialMachineConfig),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.NoError(t, err)

	require.Equal(t, machine.ApplyConfigurationRequest_NO_REBOOT, resp.Messages[0].Mode)
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
