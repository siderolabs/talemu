// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/pkg/constants"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

type machineService struct {
	machine.UnimplementedMachineServiceServer
	storage.UnimplementedStorageServiceServer

	state       state.State
	globalState state.State
	machineID   string
}

// ApplyConfiguration implements machine.MachineServiceServer.
func (c *machineService) ApplyConfiguration(ctx context.Context, request *machine.ApplyConfigurationRequest) (*machine.ApplyConfigurationResponse, error) {
	cfgProvider, err := configloader.NewFromBytes(request.GetData())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	cfg := config.NewMachineConfig(cfgProvider)

	if err = c.state.Create(ctx, cfg); err != nil {
		if !state.IsConflictError(err) {
			return nil, err
		}

		var r resource.Resource

		// update config
		r, err = c.state.Get(ctx, cfg.Metadata())
		if err != nil {
			return nil, err
		}

		cfg.Metadata().SetVersion(r.Metadata().Version())

		if err = c.state.Update(ctx, cfg); err != nil {
			return nil, err
		}
	}

	id := cfgProvider.Cluster().ID()

	clusterStatus, err := safe.ReaderGetByID[*emu.ClusterStatus](ctx, c.globalState, id)
	if err != nil {
		if !state.IsNotFoundError(err) {
			return nil, err
		}

		clusterStatus = emu.NewClusterStatus(emu.NamespaceName, id)

		if err = c.globalState.Create(ctx, clusterStatus); err != nil {
			if !state.IsConflictError(err) {
				return nil, err
			}
		}
	}

	if _, err = safe.StateUpdateWithConflicts(ctx, c.globalState, clusterStatus.Metadata(), func(r *emu.ClusterStatus) error {
		if cfgProvider.Machine().Type().IsControlPlane() {
			r.TypedSpec().Value.ControlPlanes++

			return nil
		}

		r.TypedSpec().Value.Workers++

		return nil
	}); err != nil {
		return nil, err
	}

	machineStatus := emu.NewMachineStatus(emu.NamespaceName, c.machineID)

	if _, err = safe.StateUpdateWithConflicts(ctx, c.globalState, machineStatus.Metadata(), func(r *emu.MachineStatus) error {
		r.Metadata().Labels().Set(emu.LabelCluster, id)

		if cfg.Config().Machine().Type().IsControlPlane() {
			r.Metadata().Labels().Set(emu.LabelControlPlaneRole, "")
		} else {
			r.Metadata().Labels().Set(emu.LabelWorkerRole, "")
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &machine.ApplyConfigurationResponse{
		Messages: []*machine.ApplyConfiguration{
			{
				Mode: machine.ApplyConfigurationRequest_REBOOT,
			},
		},
	}, nil
}

// Bootstrap implements machine.MachineServiceServer.
func (c *machineService) Bootstrap(ctx context.Context, _ *machine.BootstrapRequest) (*machine.BootstrapResponse, error) {
	config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, c.state, config.V1Alpha1ID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine is not configured")
		}

		return nil, err
	}

	id := config.Config().Cluster().ID()

	clusterStatus, err := safe.ReaderGetByID[*emu.ClusterStatus](ctx, c.globalState, id)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.Internal, "the emulator doesn't have the cluster with id %q", id)
		}

		return nil, err
	}

	if clusterStatus.TypedSpec().Value.Bootstrapped {
		return nil, status.Errorf(codes.InvalidArgument, "the cluster was already bootstrapped")
	}

	if _, err = safe.StateUpdateWithConflicts(ctx, c.globalState, clusterStatus.Metadata(), func(s *emu.ClusterStatus) error {
		s.TypedSpec().Value.Bootstrapped = true

		return nil
	}); err != nil {
		return nil, err
	}

	return &machine.BootstrapResponse{}, nil
}

// Reset implements machine.MachineServiceServer.
func (c *machineService) Reset(ctx context.Context, request *machine.ResetRequest) (*machine.ResetResponse, error) {
	if slices.IndexFunc(request.SystemPartitionsToWipe, func(s *machine.ResetPartitionSpec) bool {
		return s.Label == "STATE"
	}) == -1 {
		return nil, status.Errorf(codes.Unimplemented, "this reset mode is not supported")
	}

	config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, c.state, config.V1Alpha1ID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine is not configured")
		}

		return nil, err
	}

	if err = c.state.Destroy(ctx, config.Metadata()); err != nil {
		return nil, err
	}

	id := config.Provider().Cluster().ID()

	clusterStatus, err := safe.ReaderGetByID[*emu.ClusterStatus](ctx, c.globalState, id)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.Internal, "the emulator doesn't have the cluster with id %q", id)
		}

		return nil, err
	}

	clusterStatus, err = safe.StateUpdateWithConflicts(ctx, c.globalState, clusterStatus.Metadata(), func(r *emu.ClusterStatus) error {
		if config.Provider().Machine().Type().IsControlPlane() {
			r.TypedSpec().Value.ControlPlanes--

			if clusterStatus.TypedSpec().Value.ControlPlanes == 0 {
				r.TypedSpec().Value.Bootstrapped = false
			}

			return nil
		}

		r.TypedSpec().Value.Workers--

		return nil
	})
	if err != nil {
		return nil, err
	}

	if clusterStatus.TypedSpec().Value.ControlPlanes == 0 && clusterStatus.TypedSpec().Value.Workers == 0 {
		if err = c.globalState.Destroy(ctx, clusterStatus.Metadata()); err != nil {
			return nil, err
		}
	}

	machineStatus := emu.NewMachineStatus(emu.NamespaceName, c.machineID)

	if _, err = safe.StateUpdateWithConflicts(ctx, c.globalState, machineStatus.Metadata(), func(r *emu.MachineStatus) error {
		r.Metadata().Labels().Delete(emu.LabelCluster)
		r.Metadata().Labels().Delete(emu.LabelControlPlaneRole)
		r.Metadata().Labels().Delete(emu.LabelWorkerRole)

		return nil
	}); err != nil {
		return nil, err
	}

	return &machine.ResetResponse{
		Messages: []*machine.Reset{
			{
				// TODO: implement some real actor id
				ActorId: "0",
			},
		},
	}, nil
}

// Version implements machine.MachineServiceServer.
func (c *machineService) Version(ctx context.Context, _ *emptypb.Empty) (*machine.VersionResponse, error) {
	version := fmt.Sprintf("v%s", constants.DefaultTalosVersion)

	res, err := safe.ReaderGetByID[*talos.Version](ctx, c.state, talos.VersionID)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

	if res != nil {
		version = res.TypedSpec().Value.Value
	}

	return &machine.VersionResponse{
		Messages: []*machine.Version{
			{
				Version: &machine.VersionInfo{
					Tag:  version,
					Arch: "amd64",
				},
			},
		},
	}, nil
}

// Upgrade implements machine.MachineServiceServer.
func (c *machineService) Upgrade(ctx context.Context, req *machine.UpgradeRequest) (*machine.UpgradeResponse, error) {
	parts := strings.Split(req.Image, "/")

	var schematic string

	s, version, found := strings.Cut(parts[len(parts)-1], ":")
	if !found {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse the image")
	}

	if parts[0] == "factory.talos.dev" {
		schematic = s
	}

	image := talos.NewImage(talos.NamespaceName, talos.ImageID)
	image.TypedSpec().Value.Schematic = schematic
	image.TypedSpec().Value.Version = version

	if err := c.state.Create(ctx, image); err != nil {
		if state.IsConflictError(err) {
			if _, err = safe.StateUpdateWithConflicts(ctx, c.state, image.Metadata(), func(res *talos.Image) error {
				res.TypedSpec().Value = image.TypedSpec().Value

				return nil
			}); err != nil {
				return nil, err
			}
		}

		return nil, err
	}

	return &machine.UpgradeResponse{
		Messages: []*machine.Upgrade{
			{
				Ack:     "Upgrade request received",
				ActorId: "0",
			},
		},
	}, nil
}

// Disks implements storage.StorageServiceServer.
func (c *machineService) Disks(ctx context.Context, _ *emptypb.Empty) (*storage.DisksResponse, error) {
	disks, err := safe.ReaderListAll[*talos.Disk](ctx, c.state)
	if err != nil {
		return nil, err
	}

	return &storage.DisksResponse{
		Messages: []*storage.Disks{
			{
				Metadata: &common.Metadata{},
				Disks: safe.ToSlice(disks, func(res *talos.Disk) *storage.Disk {
					return res.TypedSpec().Value
				}),
			},
		},
	}, nil
}
