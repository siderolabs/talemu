// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// ImageService emulates Talos's image service.
type ImageService struct {
	machine.UnimplementedImageServiceServer

	state  state.State
	logger *zap.Logger
}

// NewImageService creates a new ImageService.
func NewImageService(st state.State, logger *zap.Logger) *ImageService {
	return &ImageService{state: st, logger: logger}
}

// Pull implements machine.ImageServiceServer.
func (s *ImageService) Pull(req *machine.ImageServicePullRequest, srv machine.ImageService_PullServer) error {
	ref := req.GetImageRef()
	if ref == "" {
		return status.Error(codes.InvalidArgument, "image reference is required")
	}

	digest, err := cacheImage(srv.Context(), s.state, ref)
	if err != nil {
		return err
	}

	return srv.Send(&machine.ImageServicePullResponse{
		Response: &machine.ImageServicePullResponse_Name{Name: pinImageReference(ref, digest)},
	})
}

// List implements machine.ImageServiceServer.
func (s *ImageService) List(_ *machine.ImageServiceListRequest, srv machine.ImageService_ListServer) error {
	images, err := safe.ReaderListAll[*talos.CachedImage](srv.Context(), s.state)
	if err != nil {
		return err
	}

	return images.ForEachErr(func(r *talos.CachedImage) error {
		return srv.Send(&machine.ImageServiceListResponse{
			Name:      r.Metadata().ID(),
			Digest:    r.TypedSpec().Value.Digest,
			Size:      r.TypedSpec().Value.Size,
			CreatedAt: timestamppb.New(r.Metadata().Created()),
		})
	})
}

// pinImageReference pins the reference to its digest as name:tag@digest, keeping the tag so setImage
// can still recover the version. An already-pinned reference is returned unchanged.
func pinImageReference(ref, digest string) string {
	if strings.Contains(ref, "@") {
		return ref
	}

	return ref + "@" + digest
}
