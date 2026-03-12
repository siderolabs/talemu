// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package machine implements emulation code for a single Talos machine.
package machine

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/jsimonetti/rtnetlink"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/siderolabs/talos/pkg/machinery/resources/siderolink"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
	"github.com/siderolabs/talemu/internal/pkg/machine/events"
	"github.com/siderolabs/talemu/internal/pkg/machine/logging"
	machinenetwork "github.com/siderolabs/talemu/internal/pkg/machine/network"
	truntime "github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
	"github.com/siderolabs/talemu/internal/pkg/schematic"
)

const (
	diskDevName      = "vda"
	diskDevPath      = "/dev/vda"
	diskPartType     = "part"
	stateDevPath     = "/dev/vda4"
	ephemeralDevPath = "/dev/vda5"

	diskSize        = 50 * 1024 * 1024 * 1024
	ephemeralOffset = 304 * 1024 * 1024
	ephemeralSize   = diskSize - ephemeralOffset
)

// Machine is a single Talos machine.
type Machine struct {
	globalState      state.State
	runtime          *truntime.Runtime
	logger           *zap.Logger
	shutdown         chan struct{}
	schematicService *schematic.Service
	uuid             string
}

// NewMachine creates a Machine.
func NewMachine(uuid string, logger *zap.Logger, globalState state.State, schematicService *schematic.Service) (*Machine, error) {
	return &Machine{
		uuid:             uuid,
		logger:           logger,
		globalState:      globalState,
		schematicService: schematicService,
		shutdown:         make(chan struct{}, 1),
	}, nil
}

// Run starts the machine.
func (m *Machine) Run(ctx context.Context, siderolinkParams *SideroLinkParams, slot int, kubernetes *kubefactory.Kubernetes, options ...Option) error { //nolint:maintidx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var opts Options

	for _, o := range options {
		o(&opts)
	}

	if opts.nc == nil {
		opts.nc = machinenetwork.NewClient()

		if err := opts.nc.Run(ctx); err != nil {
			return fmt.Errorf("netclient creation failed: %w", err)
		}

		defer opts.nc.Close() //nolint:errcheck
	}

	logSink, err := logging.NewZapCore(siderolinkParams.LogsEndpoint)
	if err != nil {
		return fmt.Errorf("log sink creation failed: %w", err)
	}

	core := zapcore.NewTee(m.logger.Core(), logSink)

	defer logSink.Close(ctx) //nolint:errcheck

	m.logger = zap.New(core).With(zap.String("machine", m.uuid))

	rt, err := truntime.NewRuntime(ctx, m.logger, slot, m.uuid, m.globalState, kubernetes, opts.nc, logSink, siderolinkParams.RawKernelArgs, m.schematicService)
	if err != nil {
		return fmt.Errorf("COSI runtime creation failed: %w", err)
	}

	m.runtime = rt

	resources := make([]resource.Resource, 0, 11)

	// populate the initial machine state
	hardwareInformation := hardware.NewSystemInformation(hardware.SystemInformationID)
	hardwareInformation.TypedSpec().UUID = m.uuid
	hardwareInformation.TypedSpec().ProductName = "Talos Emulator"
	hardwareInformation.TypedSpec().Manufacturer = "qemu"

	siderolinkConfig := siderolink.NewConfig(config.NamespaceName, siderolink.ConfigID)
	siderolinkConfig.TypedSpec().APIEndpoint = siderolinkParams.APIEndpoint
	siderolinkConfig.TypedSpec().JoinToken = siderolinkParams.JoinToken
	siderolinkConfig.TypedSpec().Host = siderolinkParams.Host
	siderolinkConfig.TypedSpec().Insecure = siderolinkParams.Insecure
	siderolinkConfig.TypedSpec().Tunnel = siderolinkParams.TunnelMode

	platformMetadata := runtime.NewPlatformMetadataSpec(runtime.NamespaceName, runtime.PlatformMetadataID)
	platformMetadata.TypedSpec().Platform = "metal"
	platformMetadata.TypedSpec().Hostname = m.uuid

	processorInfo := hardware.NewProcessorInfo("1")
	processorInfo.TypedSpec().Manufacturer = "qemu"
	processorInfo.TypedSpec().CoreCount = 64
	processorInfo.TypedSpec().MaxSpeed = 4000
	processorInfo.TypedSpec().ProductName = "Fake CPU"
	processorInfo.TypedSpec().ThreadCount = 2

	securityState := runtime.NewSecurityStateSpec(runtime.NamespaceName)
	securityState.TypedSpec().SecureBoot = opts.secureBoot

	// if the machine is using secure boot, we know that it is booted with UKI
	// todo: we can pass this as a boolean flag to talemu for non-secureboot UKI testing if we need it in the future.
	securityState.TypedSpec().BootedWithUKI = opts.secureBoot

	trustdEndpoint := k8s.NewEndpoint(k8s.ControlPlaneNamespaceName, "omniTrustd")

	trustdEndpoint.TypedSpec().Addresses = []netip.Addr{
		netip.MustParseAddr(constants.OmniEndpoint),
	}

	eventSinkConfig := runtime.NewEventSinkConfig()
	eventSinkConfig.TypedSpec().Endpoint = siderolinkParams.EventsEndpoint

	defaultRoute := network.NewRouteStatus(network.NamespaceName, "inet4/192.168.0.1//1024")
	defaultRoute.TypedSpec().Family = nethelpers.FamilyInet4
	defaultRoute.TypedSpec().Source = netip.MustParseAddr("192.168.0.1")
	defaultRoute.TypedSpec().Gateway = netip.MustParseAddr("192.168.0.1")
	defaultRoute.TypedSpec().Table = nethelpers.TableMain
	defaultRoute.TypedSpec().Priority = 1024
	defaultRoute.TypedSpec().Scope = nethelpers.ScopeGlobal
	defaultRoute.TypedSpec().Type = nethelpers.TypeAnycast
	defaultRoute.TypedSpec().Protocol = nethelpers.ProtocolBoot

	memory := hardware.NewMemoryModuleInfo("1")
	memory.TypedSpec().Size = 64 * 1024
	memory.TypedSpec().Manufacturer = "SideroLabs UltraMem"

	disk := block.NewDisk(block.NamespaceName, diskDevName)
	disk.TypedSpec().Size = diskSize
	disk.TypedSpec().Model = "CM5514"
	disk.TypedSpec().Transport = "virtio"
	disk.TypedSpec().Rotational = true
	disk.TypedSpec().BusPath = "/pci0000:00/0000:00:05.0/0000:01:01.0/virtio2/host2/target2:0:0/2:0:0:0/"

	discoveredDisk := block.NewDiscoveredVolume(block.NamespaceName, diskDevName)
	discoveredDisk.TypedSpec().Type = "disk"
	discoveredDisk.TypedSpec().DevPath = diskDevPath
	discoveredDisk.TypedSpec().DevicePath = diskDevPath
	discoveredDisk.TypedSpec().Name = diskDevName
	discoveredDisk.TypedSpec().SetSize(diskSize)
	discoveredDisk.TypedSpec().SectorSize = 512
	discoveredDisk.TypedSpec().IOSize = 512

	discoveredEfi := block.NewDiscoveredVolume(block.NamespaceName, "vda1")
	discoveredEfi.TypedSpec().Type = diskPartType
	discoveredEfi.TypedSpec().DevPath = "/dev/vda1"
	discoveredEfi.TypedSpec().DevicePath = "/dev/vda1"
	discoveredEfi.TypedSpec().Parent = diskDevName
	discoveredEfi.TypedSpec().ParentDevPath = diskDevPath
	discoveredEfi.TypedSpec().Name = "vda1"
	discoveredEfi.TypedSpec().PartitionLabel = "EFI"
	discoveredEfi.TypedSpec().PartitionIndex = 1
	discoveredEfi.TypedSpec().Offset = 1024 * 1024
	discoveredEfi.TypedSpec().SetSize(100 * 1024 * 1024)

	discoveredBoot := block.NewDiscoveredVolume(block.NamespaceName, "vda2")
	discoveredBoot.TypedSpec().Type = diskPartType
	discoveredBoot.TypedSpec().DevPath = "/dev/vda2"
	discoveredBoot.TypedSpec().DevicePath = "/dev/vda2"
	discoveredBoot.TypedSpec().Parent = diskDevName
	discoveredBoot.TypedSpec().ParentDevPath = diskDevPath
	discoveredBoot.TypedSpec().Name = "vda2"
	discoveredBoot.TypedSpec().PartitionLabel = "BOOT"
	discoveredBoot.TypedSpec().PartitionIndex = 2
	discoveredBoot.TypedSpec().Offset = 101 * 1024 * 1024
	discoveredBoot.TypedSpec().SetSize(100 * 1024 * 1024)

	discoveredMeta := block.NewDiscoveredVolume(block.NamespaceName, "vda3")
	discoveredMeta.TypedSpec().Type = diskPartType
	discoveredMeta.TypedSpec().DevPath = "/dev/vda3"
	discoveredMeta.TypedSpec().DevicePath = "/dev/vda3"
	discoveredMeta.TypedSpec().Parent = diskDevName
	discoveredMeta.TypedSpec().ParentDevPath = diskDevPath
	discoveredMeta.TypedSpec().Name = "vda3"
	discoveredMeta.TypedSpec().PartitionLabel = "META"
	discoveredMeta.TypedSpec().PartitionIndex = 3
	discoveredMeta.TypedSpec().Offset = 202 * 1024 * 1024
	discoveredMeta.TypedSpec().SetSize(2 * 1024 * 1024)

	discoveredState := block.NewDiscoveredVolume(block.NamespaceName, "vda4")
	discoveredState.TypedSpec().Type = diskPartType
	discoveredState.TypedSpec().DevPath = stateDevPath
	discoveredState.TypedSpec().DevicePath = stateDevPath
	discoveredState.TypedSpec().Parent = diskDevName
	discoveredState.TypedSpec().ParentDevPath = diskDevPath
	discoveredState.TypedSpec().Name = "vda4"
	discoveredState.TypedSpec().PartitionLabel = "STATE"
	discoveredState.TypedSpec().PartitionIndex = 4
	discoveredState.TypedSpec().Offset = 204 * 1024 * 1024
	discoveredState.TypedSpec().SetSize(100 * 1024 * 1024)

	discoveredEphemeral := block.NewDiscoveredVolume(block.NamespaceName, "vda5")
	discoveredEphemeral.TypedSpec().Type = diskPartType
	discoveredEphemeral.TypedSpec().DevPath = ephemeralDevPath
	discoveredEphemeral.TypedSpec().DevicePath = ephemeralDevPath
	discoveredEphemeral.TypedSpec().Parent = diskDevName
	discoveredEphemeral.TypedSpec().ParentDevPath = diskDevPath
	discoveredEphemeral.TypedSpec().Name = "vda5"
	discoveredEphemeral.TypedSpec().PartitionLabel = "EPHEMERAL"
	discoveredEphemeral.TypedSpec().PartitionIndex = 5
	discoveredEphemeral.TypedSpec().Offset = ephemeralOffset
	discoveredEphemeral.TypedSpec().SetSize(ephemeralSize)

	volumeState := block.NewVolumeStatus(block.NamespaceName, "STATE")
	volumeState.TypedSpec().Phase = block.VolumePhaseReady
	volumeState.TypedSpec().Type = block.VolumeTypePartition
	volumeState.TypedSpec().Location = stateDevPath
	volumeState.TypedSpec().MountLocation = "/system/state"
	volumeState.TypedSpec().ParentLocation = diskDevPath
	volumeState.TypedSpec().PartitionIndex = 4
	volumeState.TypedSpec().Filesystem = block.FilesystemTypeXFS
	volumeState.TypedSpec().SetSize(100 * 1024 * 1024)

	volumeEphemeral := block.NewVolumeStatus(block.NamespaceName, "EPHEMERAL")
	volumeEphemeral.TypedSpec().Phase = block.VolumePhaseReady
	volumeEphemeral.TypedSpec().Type = block.VolumeTypePartition
	volumeEphemeral.TypedSpec().Location = ephemeralDevPath
	volumeEphemeral.TypedSpec().MountLocation = "/var"
	volumeEphemeral.TypedSpec().ParentLocation = diskDevPath
	volumeEphemeral.TypedSpec().PartitionIndex = 5
	volumeEphemeral.TypedSpec().Filesystem = block.FilesystemTypeXFS
	volumeEphemeral.TypedSpec().SetSize(ephemeralSize)

	pciNet := hardware.NewPCIDeviceInfo("0000:00:01.0")
	pciNet.TypedSpec().Class = "Network controller"
	pciNet.TypedSpec().Subclass = "Ethernet controller"
	pciNet.TypedSpec().Vendor = "Red Hat, Inc."
	pciNet.TypedSpec().Product = "Virtio network device"
	pciNet.TypedSpec().ClassID = "0x02"
	pciNet.TypedSpec().SubclassID = "0x00"
	pciNet.TypedSpec().VendorID = "0x1af4"
	pciNet.TypedSpec().ProductID = "0x1000"
	pciNet.TypedSpec().Driver = "virtio-pci"

	pciDisk := hardware.NewPCIDeviceInfo("0000:00:05.0")
	pciDisk.TypedSpec().Class = "Mass storage controller"
	pciDisk.TypedSpec().Subclass = "SCSI storage controller"
	pciDisk.TypedSpec().Vendor = "Red Hat, Inc."
	pciDisk.TypedSpec().Product = "Virtio block device"
	pciDisk.TypedSpec().ClassID = "0x01"
	pciDisk.TypedSpec().SubclassID = "0x00"
	pciDisk.TypedSpec().VendorID = "0x1af4"
	pciDisk.TypedSpec().ProductID = "0x1001"
	pciDisk.TypedSpec().Driver = "virtio-pci"

	resources = append(resources,
		hardwareInformation,
		siderolinkConfig,
		platformMetadata,
		processorInfo,
		securityState,
		trustdEndpoint,
		eventSinkConfig,
		disk,
		defaultRoute,
		memory,
		discoveredDisk,
		discoveredEfi,
		discoveredBoot,
		discoveredMeta,
		discoveredState,
		discoveredEphemeral,
		volumeState,
		volumeEphemeral,
		pciNet,
		pciDisk,
	)

	if opts.schematic != "" || opts.talosVersion != "" {
		image := talos.NewImage(talos.NamespaceName, talos.ImageID)

		image.TypedSpec().Value.Schematic = opts.schematic
		image.TypedSpec().Value.Version = opts.talosVersion

		resources = append(resources, image)
	}

	for _, r := range resources {
		if err = rt.State().Create(ctx, r); err != nil {
			if state.IsConflictError(err) {
				continue
			}

			return fmt.Errorf("failed to create resource %s: %w", r.Metadata(), err)
		}
	}

	sink, err := events.NewHandler(rt.State())
	if err != nil {
		return err
	}

	var eg errgroup.Group

	eg.Go(func() error {
		select {
		case <-ctx.Done():
		case <-m.shutdown:
			cancel()
		}

		return nil
	})

	eg.Go(func() error {
		return rt.Run(ctx)
	})

	eg.Go(func() error {
		return sink.Run(ctx, m.logger)
	})

	return eg.Wait()
}

// Cleanup removes created network interfaces.
func (m *Machine) Cleanup(ctx context.Context) error {
	if m.runtime == nil {
		return nil
	}

	select {
	case m.shutdown <- struct{}{}:
	default:
	}

	// remove all created interfaces
	links, err := safe.ReaderListAll[*network.LinkSpec](ctx, m.runtime.State())
	if err != nil {
		return err
	}

	conn, err := rtnetlink.Dial(nil)
	if err != nil {
		return fmt.Errorf("error dialing rtnetlink socket: %w", err)
	}

	defer conn.Close() //nolint:errcheck

	// list rtnetlink links (interfaces)
	rtnetlinks, err := conn.Link.List()
	if err != nil {
		return fmt.Errorf("error listing links: %w", err)
	}

	return links.ForEachErr(func(link *network.LinkSpec) error {
		existing := controllers.FindLink(rtnetlinks, link.TypedSpec().Name)
		if existing == nil {
			return nil
		}

		m.logger.Info("teardown interface", zap.String("interface", link.TypedSpec().Name))

		return conn.Link.Delete(existing.Index)
	})
}
