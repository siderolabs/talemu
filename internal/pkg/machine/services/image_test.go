// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services_test

import (
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
	"github.com/siderolabs/talemu/internal/pkg/machine/services"
)

func newImageService(t *testing.T) (*services.ImageService, state.State) {
	t.Helper()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	return services.NewImageService(st, zaptest.NewLogger(t)), st
}

func TestImagePull(t *testing.T) {
	const ref = "factory.talos.dev/installer/abc123:v1.14.0"

	svc, st := newImageService(t)

	srv := &recordingStream[*machine.ImageServicePullResponse]{ctx: t.Context()}
	require.NoError(t, svc.Pull(&machine.ImageServicePullRequest{ImageRef: ref}, srv))

	// Pull returns the reference pinned to its digest, keeping the tag (canonical name:tag@digest).
	require.Len(t, srv.sent, 1)
	name := srv.sent[0].GetName()
	require.True(t, strings.HasPrefix(name, "factory.talos.dev/installer/abc123:v1.14.0@sha256:"), "got %q", name)

	// The pulled image is recorded so image list calls can observe it.
	cached, err := safe.ReaderGetByID[*talos.CachedImage](t.Context(), st, ref)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(cached.TypedSpec().Value.Digest, "sha256:"))
}

func TestImageList(t *testing.T) {
	const ref = "factory.talos.dev/installer/abc123:v1.14.0"

	svc, _ := newImageService(t)

	// Nothing pulled yet.
	empty := &recordingStream[*machine.ImageServiceListResponse]{ctx: t.Context()}
	require.NoError(t, svc.List(&machine.ImageServiceListRequest{}, empty))
	require.Empty(t, empty.sent)

	require.NoError(t, svc.Pull(&machine.ImageServicePullRequest{ImageRef: ref}, &recordingStream[*machine.ImageServicePullResponse]{ctx: t.Context()}))

	// The pulled image now shows up in the list.
	srv := &recordingStream[*machine.ImageServiceListResponse]{ctx: t.Context()}
	require.NoError(t, svc.List(&machine.ImageServiceListRequest{}, srv))
	require.Len(t, srv.sent, 1)
	require.Equal(t, ref, srv.sent[0].GetName())
	require.True(t, strings.HasPrefix(srv.sent[0].GetDigest(), "sha256:"))
}

func TestImagePullNotFound(t *testing.T) {
	svc, _ := newImageService(t)

	err := svc.Pull(&machine.ImageServicePullRequest{ImageRef: "factory.talos.dev/installer/abc123:v1.14.0-bad"}, &recordingStream[*machine.ImageServicePullResponse]{ctx: t.Context()})
	require.Equal(t, codes.NotFound, status.Code(err))
}

// TestMachineImagePullNotFound asserts the legacy MachineService.ImagePull rejects a "-bad" image
// with the same NotFound code as ImageService.Pull, since both share the cacheImage pull path.
func TestMachineImagePullNotFound(t *testing.T) {
	svc := newMachineService(t)

	_, err := svc.ImagePull(t.Context(), &machine.ImagePullRequest{Reference: "factory.talos.dev/installer/abc123:v1.14.0-bad"})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestImagePullEmptyRef(t *testing.T) {
	svc, _ := newImageService(t)

	err := svc.Pull(&machine.ImageServicePullRequest{ImageRef: ""}, &recordingStream[*machine.ImageServicePullResponse]{ctx: t.Context()})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
