// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/controller/generic"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/omni/client/pkg/constants"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/etcd"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// MachineService is a GRPC service emulating the behavior of the Talos machine service.
type MachineService struct {
	machine.UnimplementedMachineServiceServer
	storage.UnimplementedStorageServiceServer

	state       state.State
	globalState state.State

	logger *zap.Logger

	machineID string
}

// NewMachineService creates a new MachineService.
func NewMachineService(machineID string, state, globalState state.State, logger *zap.Logger) *MachineService {
	return &MachineService{
		state:       state,
		globalState: globalState,
		logger:      logger,
		machineID:   machineID,
	}
}

// ApplyConfiguration implements machine.MachineServiceServer.
func (c *MachineService) ApplyConfiguration(ctx context.Context, request *machine.ApplyConfigurationRequest) (*machine.ApplyConfigurationResponse, error) {
	cfgProvider, err := configloader.NewFromBytes(request.GetData())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	cfg := config.NewMachineConfig(cfgProvider)
	isPartialConfig := cfg.Config().Machine() == nil

	if !isPartialConfig {
		if err = c.validateInstaller(ctx, cfg.Config().Machine().Install().Image()); err != nil {
			return nil, err
		}
	}

	existingCompleteConfig, err := machineconfig.GetComplete(ctx, c.state)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

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

	allocated := existingCompleteConfig == nil && !isPartialConfig
	if !allocated {
		return &machine.ApplyConfigurationResponse{
			Messages: []*machine.ApplyConfiguration{{Mode: machine.ApplyConfigurationRequest_NO_REBOOT}},
		}, nil
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
func (c *MachineService) Bootstrap(ctx context.Context, _ *machine.BootstrapRequest) (*machine.BootstrapResponse, error) {
	config, err := machineconfig.GetComplete(ctx, c.state)
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
func (c *MachineService) Reset(ctx context.Context, request *machine.ResetRequest) (*machine.ResetResponse, error) {
	if slices.IndexFunc(request.SystemPartitionsToWipe, func(s *machine.ResetPartitionSpec) bool {
		return s.Label == "STATE"
	}) == -1 {
		return nil, status.Errorf(codes.Unimplemented, "this reset mode is not supported")
	}

	cfg, err := machineconfig.GetComplete(ctx, c.state)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine is not configured")
		}

		return nil, err
	}

	if err = destroyResourceByID[*config.MachineConfig](ctx, c.state, cfg.Metadata().ID()); err != nil {
		return nil, err
	}

	id := cfg.Provider().Cluster().ID()

	clusterStatus, err := safe.ReaderGetByID[*emu.ClusterStatus](ctx, c.globalState, id)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.Internal, "the emulator doesn't have the cluster with id %q", id)
		}

		return nil, err
	}

	clusterStatus, err = safe.StateUpdateWithConflicts(ctx, c.globalState, clusterStatus.Metadata(), func(r *emu.ClusterStatus) error {
		if cfg.Provider().Machine().Type().IsControlPlane() {
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
		if _, err = c.globalState.Teardown(ctx, clusterStatus.Metadata()); err != nil {
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
func (c *MachineService) Version(ctx context.Context, _ *emptypb.Empty) (*machine.VersionResponse, error) {
	version := fmt.Sprintf("v%s", constants.DefaultTalosVersion)

	res, err := safe.ReaderGetByID[*talos.Version](ctx, c.state, talos.VersionID)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

	architecture := ""

	if res != nil {
		version = res.TypedSpec().Value.Value
		architecture = res.TypedSpec().Value.Architecture
	}

	return &machine.VersionResponse{
		Messages: []*machine.Version{
			{
				Version: &machine.VersionInfo{
					Tag:  version,
					Arch: architecture,
				},
			},
		},
	}, nil
}

// Upgrade implements machine.MachineServiceServer.
func (c *MachineService) Upgrade(ctx context.Context, req *machine.UpgradeRequest) (*machine.UpgradeResponse, error) {
	if err := c.validateInstaller(ctx, req.Image); err != nil {
		return nil, err
	}

	parts := strings.Split(req.Image, "/")

	var schematic string

	s, version, found := strings.Cut(parts[len(parts)-1], ":")
	if !found {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse the image")
	}

	if parts[0] == emuconst.ImageFactoryHost {
		schematic = s
	}

	image := talos.NewImage(talos.NamespaceName, talos.ImageID)
	image.TypedSpec().Value.Schematic = schematic
	image.TypedSpec().Value.Version = version

	changed := true

	if err := c.state.Create(ctx, image); err != nil {
		if !state.IsConflictError(err) {
			return nil, err
		}

		if _, err = safe.StateUpdateWithConflicts(ctx, c.state, image.Metadata(), func(res *talos.Image) error {
			changed = !res.TypedSpec().Value.EqualVT(image.TypedSpec().Value)

			res.TypedSpec().Value = image.TypedSpec().Value

			return nil
		}); err != nil {
			return nil, err
		}
	}

	if changed {
		if _, err := c.Reboot(ctx, &machine.RebootRequest{}); err != nil {
			return nil, err
		}
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

// Reboot implements machine.MachineServiceServer.
func (c *MachineService) Reboot(ctx context.Context, _ *machine.RebootRequest) (*machine.RebootResponse, error) {
	reboot := talos.NewReboot(talos.NamespaceName, talos.RebootID)
	reboot.TypedSpec().Value.Downtime = durationpb.New(time.Second * 2)

	err := destroyResourceByID[*talos.Reboot](ctx, c.state, talos.RebootID)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

	if err = c.state.Create(ctx, reboot); err != nil {
		return nil, err
	}

	return &machine.RebootResponse{
		Messages: []*machine.Reboot{
			{},
		},
	}, nil
}

// EtcdMemberList implements machine.MachineServiceServer.
func (c *MachineService) EtcdMemberList(ctx context.Context, _ *machine.EtcdMemberListRequest) (*machine.EtcdMemberListResponse, error) {
	config, err := machineconfig.GetComplete(ctx, c.state)
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
func (c *MachineService) EtcdRemoveMemberByID(ctx context.Context, req *machine.EtcdRemoveMemberByIDRequest) (*machine.EtcdRemoveMemberByIDResponse, error) {
	config, err := machineconfig.GetComplete(ctx, c.state)
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
func (c *MachineService) EtcdLeaveCluster(ctx context.Context, _ *machine.EtcdLeaveClusterRequest) (*machine.EtcdLeaveClusterResponse, error) {
	config, err := machineconfig.GetComplete(ctx, c.state)
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

// EtcdForfeitLeadership implements machine.MachineServiceServer.
func (c *MachineService) EtcdForfeitLeadership(context.Context, *machine.EtcdForfeitLeadershipRequest) (*machine.EtcdForfeitLeadershipResponse, error) {
	return &machine.EtcdForfeitLeadershipResponse{
		Messages: []*machine.EtcdForfeitLeadership{
			{},
		},
	}, nil
}

func (c *MachineService) EtcdStatus(ctx context.Context, _ *emptypb.Empty) (*machine.EtcdStatusResponse, error) {
	member, err := safe.ReaderGetByID[*etcd.Member](ctx, c.state, etcd.LocalMemberID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil, status.Errorf(codes.InvalidArgument, "the machine doesn't have etcd member")
		}

		return nil, fmt.Errorf("failed to get etcd member %w", err)
	}

	id, err := etcd.ParseMemberID(member.TypedSpec().MemberID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to parse etcd member id %s", err.Error())
	}

	return &machine.EtcdStatusResponse{
		Messages: []*machine.EtcdStatus{
			{
				MemberStatus: &machine.EtcdMemberStatus{
					MemberId: id,
				},
			},
		},
	}, nil
}

// List implements machine.MachineServiceServer.
func (c *MachineService) List(req *machine.ListRequest, serv machine.MachineService_ListServer) error {
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
func (c *MachineService) ServiceList(ctx context.Context, _ *emptypb.Empty) (*machine.ServiceListResponse, error) {
	res := &machine.ServiceListResponse{}

	services, err := safe.ReaderListAll[*v1alpha1.Service](ctx, c.state)
	if err != nil {
		return nil, err
	}

	list := &machine.ServiceList{
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
	}

	list.Services = append(list.Services, &machine.ServiceInfo{
		Id:     "machined",
		State:  "Running",
		Events: &machine.ServiceEvents{},
		Health: &machine.ServiceHealth{
			Healthy: true,
		},
	})

	res.Messages = []*machine.ServiceList{
		list,
	}

	return res, nil
}

// Disks implements storage.StorageServiceServer.
func (c *MachineService) Disks(ctx context.Context, _ *emptypb.Empty) (*storage.DisksResponse, error) {
	systemDisk, err := safe.StateGetByID[*block.SystemDisk](ctx, c.state, block.SystemDiskID)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

	disks, err := safe.StateListAll[*block.Disk](ctx, c.state)
	if err != nil {
		return nil, err
	}

	diskConv := func(d *block.Disk) *storage.Disk {
		var diskType storage.Disk_DiskType

		switch {
		case d.TypedSpec().CDROM:
			diskType = storage.Disk_CD
		case d.TypedSpec().Transport == "nvme":
			diskType = storage.Disk_NVME
		case d.TypedSpec().Transport == "mmc":
			diskType = storage.Disk_SD
		case d.TypedSpec().Rotational:
			diskType = storage.Disk_HDD
		case d.TypedSpec().Transport != "":
			diskType = storage.Disk_SSD
		}

		return &storage.Disk{
			DeviceName: filepath.Join("/dev", d.Metadata().ID()),
			Model:      d.TypedSpec().Model,
			Size:       d.TypedSpec().Size,
			Serial:     d.TypedSpec().Serial,
			Modalias:   d.TypedSpec().Modalias,
			Wwid:       d.TypedSpec().WWID,
			Uuid:       d.TypedSpec().UUID,
			Type:       diskType,
			BusPath:    d.TypedSpec().BusPath,
			SystemDisk: systemDisk != nil && d.Metadata().ID() == systemDisk.TypedSpec().DiskID,
			Subsystem:  d.TypedSpec().SubSystem,
			Readonly:   d.TypedSpec().Readonly,
		}
	}

	reply := &storage.DisksResponse{
		Messages: []*storage.Disks{
			{
				Disks: safe.ToSlice(disks, diskConv),
			},
		},
	}

	return reply, nil
}

// Hostname implements machine.MachineServiceServer.
func (c *MachineService) Hostname(ctx context.Context, _ *emptypb.Empty) (*machine.HostnameResponse, error) {
	hostname, err := safe.ReaderGetByID[*network.HostnameStatus](ctx, c.state, network.HostnameID)
	if err != nil {
		return nil, err
	}

	return &machine.HostnameResponse{
		Messages: []*machine.Hostname{
			{
				Hostname: hostname.TypedSpec().Hostname,
			},
		},
	}, nil
}

// ImageList implements machine.MachineServiceServer.
func (c *MachineService) ImageList(_ *machine.ImageListRequest, serv machine.MachineService_ImageListServer) error {
	images, err := safe.ReaderListAll[*talos.CachedImage](serv.Context(), c.state)
	if err != nil {
		return err
	}

	return images.ForEachErr(func(r *talos.CachedImage) error {
		return serv.Send(&machine.ImageListResponse{
			Name:      r.Metadata().ID(),
			Digest:    r.TypedSpec().Value.Digest,
			Size:      r.TypedSpec().Value.Size,
			CreatedAt: timestamppb.New(r.Metadata().Created()),
		})
	})
}

// ImagePull implements machine.MachineServiceServer.
func (c *MachineService) ImagePull(ctx context.Context, req *machine.ImagePullRequest) (*machine.ImagePullResponse, error) {
	if strings.HasSuffix(req.Reference, "-bad") {
		return nil, fmt.Errorf("emulator is set to fail on images ending with -bad suffix")
	}

	image := talos.NewCachedImage(talos.NamespaceName, req.Reference)
	image.TypedSpec().Value.Digest = "aaaa"
	image.TypedSpec().Value.Size = 1024

	if err := c.state.Create(ctx, image); err != nil && !state.IsConflictError(err) {
		return nil, err
	}

	return &machine.ImagePullResponse{
		Messages: []*machine.ImagePull{},
	}, nil
}

// Dmesg implements machine.MachineServiceServer.
func (c *MachineService) Dmesg(_ *machine.DmesgRequest, serv machine.MachineService_DmesgServer) error {
	return serv.Send(&common.Data{
		Bytes: []byte(
			"I wish I was a real Talos...",
		),
	})
}

// Logs implements machine.MachineServiceServer.
func (c *MachineService) Logs(req *machine.LogsRequest, serv machine.MachineService_LogsServer) error {
	return serv.Send(&common.Data{
		Bytes: fmt.Appendf(nil, "I will pretend I know something about the service %q", req.Id),
	})
}

// Containers implements machine.MachineServiceServer.
func (c *MachineService) Containers(context.Context, *machine.ContainersRequest) (*machine.ContainersResponse, error) {
	return &machine.ContainersResponse{}, nil
}

// MetaWrite implements machine.MachineServiceServer.
func (c *MachineService) MetaWrite(ctx context.Context, req *machine.MetaWriteRequest) (*machine.MetaWriteResponse, error) {
	metaKey := runtime.NewMetaKey(runtime.NamespaceName, runtime.MetaKeyTagToID(uint8(req.Key)))

	metaKey.TypedSpec().Value = string(req.Value)

	if req.Key == 16 {
		if err := c.createOrUpdateUniqueToken(ctx, req); err != nil {
			return nil, err
		}
	}

	if err := c.state.Create(ctx, metaKey); err != nil {
		if !state.IsConflictError(err) {
			return nil, err
		}

		_, err = safe.StateUpdateWithConflicts(ctx, c.state, metaKey.Metadata(), func(r *runtime.MetaKey) error {
			r.TypedSpec().Value = string(req.Value)

			return nil
		})
		if err != nil {
			return nil, err
		}

		c.logger.Info("updated meta", zap.Uint32("key", req.Key), zap.Binary("value", req.Value))

		return &machine.MetaWriteResponse{}, nil
	}

	c.logger.Info("created meta", zap.Uint32("key", req.Key), zap.Binary("value", req.Value))

	return &machine.MetaWriteResponse{}, nil
}

// MetaDelete implements machine.MachineServiceServer.
func (c *MachineService) MetaDelete(ctx context.Context, req *machine.MetaDeleteRequest) (*machine.MetaDeleteResponse, error) {
	if err := destroyResourceByID[*runtime.MetaKey](ctx, c.state, runtime.MetaKeyTagToID(uint8(req.Key))); err != nil {
		if !state.IsNotFoundError(err) {
			return nil, err
		}

		c.logger.Info("meta not found", zap.Uint32("key", req.Key))

		return nil, status.Error(codes.NotFound, "meta not found")
	}

	c.logger.Info("deleted meta", zap.Uint32("key", req.Key))

	return &machine.MetaDeleteResponse{}, nil
}

func (c *MachineService) validateInstaller(ctx context.Context, image string) error {
	securityState, err := safe.ReaderGetByID[*runtime.SecurityState](ctx, c.state, runtime.SecurityStateID)
	if err != nil {
		return err
	}

	if !securityState.TypedSpec().SecureBoot {
		return nil
	}

	if !strings.Contains(image, "-secureboot") {
		return status.Errorf(codes.InvalidArgument, "tried to use non-secureboot image for the secureboot mode")
	}

	return nil
}

func destroyResourceByID[T generic.ResourceWithRD](ctx context.Context, st state.State, id resource.ID) error {
	var res T

	md := resource.NewMetadata(res.ResourceDefinition().DefaultNamespace, res.ResourceDefinition().Type, id, resource.VersionUndefined)

	_, err := st.Teardown(ctx, md)
	if err != nil {
		return err
	}

	_, err = st.WatchFor(ctx, md, state.WithFinalizerEmpty())
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	if err = st.Destroy(ctx, md); err != nil {
		return err
	}

	return nil
}

func (c *MachineService) createOrUpdateUniqueToken(ctx context.Context, req *machine.MetaWriteRequest) error {
	uniqueMachineToken := runtime.NewUniqueMachineToken()
	uniqueMachineToken.TypedSpec().Token = string(req.Value)

	if _, err := c.state.Get(ctx, uniqueMachineToken.Metadata()); err != nil {
		if !state.IsNotFoundError(err) {
			return err
		}

		return c.state.Create(ctx, uniqueMachineToken)
	}

	_, err := safe.StateUpdateWithConflicts(ctx, c.state, uniqueMachineToken.Metadata(), func(r *runtime.UniqueMachineToken) error {
		r.TypedSpec().Token = string(req.Value)

		return nil
	})

	return err
}
