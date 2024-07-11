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
	"github.com/siderolabs/talos/pkg/machinery/resources/etcd"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

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

// EtcdMemberList implements machine.MachineServiceServer.
func (c *machineService) EtcdMemberList(ctx context.Context, _ *machine.EtcdMemberListRequest) (*machine.EtcdMemberListResponse, error) {
	config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, c.state, config.V1Alpha1ID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine is not configured")
		}

		return nil, err
	}

	if !config.Provider().Machine().Type().IsControlPlane() {
		return nil, status.Errorf(codes.InvalidArgument, "the machine is not a control plane")
	}

	clusterStatus, err := safe.ReaderGetByID[*emu.ClusterStatus](ctx, c.globalState, config.Provider().Cluster().ID())
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster status: %w", err)
	}

	machines, err := safe.ReaderListAll[*emu.MachineStatus](ctx, c.globalState,
		state.WithLabelQuery(
			resource.LabelEqual(emu.LabelCluster, config.Provider().Cluster().ID()),
			resource.LabelExists(emu.LabelControlPlaneRole),
		),
	)
	if err != nil {
		return nil, err
	}

	members := make([]*machine.EtcdMember, 0, machines.Len())

	err = machines.ForEachErr(func(m *emu.MachineStatus) error {
		id := m.TypedSpec().Value.EtcdMemberId
		if id == "" {
			return nil
		}

		var memberID uint64

		memberID, err = etcd.ParseMemberID(m.TypedSpec().Value.EtcdMemberId)
		if err != nil {
			return err
		}

		if slices.Contains(clusterStatus.TypedSpec().Value.DenyEtcdMembers, m.TypedSpec().Value.EtcdMemberId) {
			return nil
		}

		members = append(members, &machine.EtcdMember{
			Id:       memberID,
			Hostname: m.TypedSpec().Value.Hostname,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute etcd members %w", err)
	}

	res := &machine.EtcdMemberListResponse{
		Messages: []*machine.EtcdMembers{
			{
				Members: members,
			},
		},
	}

	return res, nil
}

// EtcdRemoveMemberByID implements machine.MachineServiceServer.
func (c *machineService) EtcdRemoveMemberByID(ctx context.Context, req *machine.EtcdRemoveMemberByIDRequest) (*machine.EtcdRemoveMemberByIDResponse, error) {
	config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, c.state, config.V1Alpha1ID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine is not configured")
		}

		return nil, err
	}

	if !config.Provider().Machine().Type().IsControlPlane() {
		return nil, status.Errorf(codes.InvalidArgument, "the machine is not a control plane")
	}

	memberID := etcd.FormatMemberID(req.MemberId)

	clusterStatus := emu.NewClusterStatus(emu.NamespaceName, config.Provider().Cluster().ID()).Metadata()

	_, err = safe.StateUpdateWithConflicts(ctx, c.globalState, clusterStatus, func(res *emu.ClusterStatus) error {
		if slices.Contains(res.TypedSpec().Value.DenyEtcdMembers, memberID) {
			return nil
		}

		res.TypedSpec().Value.DenyEtcdMembers = append(res.TypedSpec().Value.DenyEtcdMembers, memberID)

		return nil
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update cluster status %s", err)
	}

	return &machine.EtcdRemoveMemberByIDResponse{
		Messages: []*machine.EtcdRemoveMemberByID{
			{},
		},
	}, nil
}

// EtcdLeaveCluster implements machine.MachineServiceServer.
func (c *machineService) EtcdLeaveCluster(ctx context.Context, _ *machine.EtcdLeaveClusterRequest) (*machine.EtcdLeaveClusterResponse, error) {
	config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, c.state, config.V1Alpha1ID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine is not configured")
		}

		return nil, fmt.Errorf("failed to get machine config %w", err)
	}

	if !config.Provider().Machine().Type().IsControlPlane() {
		return nil, status.Errorf(codes.InvalidArgument, "the machine is not a control plane")
	}

	member, err := safe.ReaderGetByID[*etcd.Member](ctx, c.state, etcd.LocalMemberID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine doesn't have etcd member")
		}

		return nil, fmt.Errorf("failed to get etcd member %w", err)
	}

	if member.TypedSpec().MemberID == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "the machine doesn't have etcd member ID")
	}

	clusterStatus := emu.NewClusterStatus(emu.NamespaceName, config.Provider().Cluster().ID()).Metadata()

	_, err = safe.StateUpdateWithConflicts(ctx, c.globalState, clusterStatus, func(res *emu.ClusterStatus) error {
		if slices.Contains(res.TypedSpec().Value.DenyEtcdMembers, member.TypedSpec().MemberID) {
			return nil
		}

		res.TypedSpec().Value.DenyEtcdMembers = append(res.TypedSpec().Value.DenyEtcdMembers, member.TypedSpec().MemberID)

		return nil
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update cluster status %s", err)
	}

	return &machine.EtcdLeaveClusterResponse{
		Messages: []*machine.EtcdLeaveCluster{
			{},
		},
	}, nil
}

// EtcdForfeidLeadership implements machine.MachineServiceServer.
func (c *machineService) EtcdForfeitLeadership(context.Context, *machine.EtcdForfeitLeadershipRequest) (*machine.EtcdForfeitLeadershipResponse, error) {
	return &machine.EtcdForfeitLeadershipResponse{
		Messages: []*machine.EtcdForfeitLeadership{
			{},
		},
	}, nil
}

// List implements machine.MachineServiceServer.
func (c *machineService) List(req *machine.ListRequest, serv machine.MachineService_ListServer) error {
	if req.Root == "/var/lib/etcd/member" {
		member, err := safe.ReaderGetByID[*etcd.Member](serv.Context(), c.state, etcd.LocalMemberID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		if member == nil {
			return fmt.Errorf("no such file or directory")
		}

		return serv.Send(&machine.FileInfo{
			Name: "db",
			Size: 1024,
		})
	}

	return nil
}

// ServiceList implements machine.MachineServiceServer.
func (c *machineService) ServiceList(ctx context.Context, _ *emptypb.Empty) (*machine.ServiceListResponse, error) {
	res := &machine.ServiceListResponse{}

	services, err := safe.ReaderListAll[*v1alpha1.Service](ctx, c.state)
	if err != nil {
		return nil, err
	}

	res.Messages = []*machine.ServiceList{
		{
			Services: safe.ToSlice(services, func(s *v1alpha1.Service) *machine.ServiceInfo {
				state := "Stopped"
				switch {
				case s.TypedSpec().Running && s.TypedSpec().Healthy:
					state = "Running"
				case s.TypedSpec().Running && !s.TypedSpec().Healthy:
					state = "Starting"
				}

				return &machine.ServiceInfo{
					Id:     s.Metadata().ID(),
					State:  state,
					Events: &machine.ServiceEvents{},
					Health: &machine.ServiceHealth{
						Unknown:    s.TypedSpec().Unknown,
						Healthy:    s.TypedSpec().Healthy,
						LastChange: timestamppb.New(s.Metadata().Updated()),
					},
				}
			}),
		},
	}

	return res, nil
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
