// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package machine implements emulation code for a single Talos machine.
package machine

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"

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
	"github.com/siderolabs/talemu/internal/pkg/machine/blocklayout"
	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
	"github.com/siderolabs/talemu/internal/pkg/machine/events"
	"github.com/siderolabs/talemu/internal/pkg/machine/logging"
	machinenetwork "github.com/siderolabs/talemu/internal/pkg/machine/network"
	truntime "github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
	"github.com/siderolabs/talemu/internal/pkg/schematic"
)

const (
	diskDevName = "vda"
	diskDevPath = "/dev/vda"

	diskSize = blocklayout.DiskSize
)

// MaxExtraDisks is the number of additional disks a machine can be given, capped
// so the device names stay in the single-letter vdb..vdz range.
const MaxExtraDisks = 25

// extraDisks builds count additional empty disks, named vdb, vdc and so on.
//
// They carry no partitions, so the machine has somewhere to install to that is
// not the disk it booted from.
//
// Callers are expected to keep count within 0..MaxExtraDisks, which the command
// line enforces, since the naming scheme only has single letters to work with.
func extraDisks(count int) []resource.Resource {
	resources := make([]resource.Resource, 0, count*2)

	for i := range count {
		name := fmt.Sprintf("vd%c", 'b'+i)
		devPath := "/dev/" + name

		disk := block.NewDisk(block.NamespaceName, name)
		disk.TypedSpec().Size = diskSize
		disk.TypedSpec().Model = "CM5514"
		disk.TypedSpec().Transport = "virtio"
		disk.TypedSpec().Rotational = true
		disk.TypedSpec().BusPath = fmt.Sprintf("/pci0000:00/0000:00:05.0/0000:01:01.0/virtio2/host2/target2:0:0/2:0:%d:0/", i+1)

		discovered := block.NewDiscoveredVolume(block.NamespaceName, name)
		discovered.TypedSpec().Type = "disk"
		discovered.TypedSpec().DevPath = devPath
		discovered.TypedSpec().DevicePath = devPath
		discovered.TypedSpec().Name = name
		discovered.TypedSpec().SetSize(diskSize)
		discovered.TypedSpec().SectorSize = 512
		discovered.TypedSpec().IOSize = 512

		resources = append(resources, disk, discovered)
	}

	return resources
}

// seedBootMedia records the boot media of a machine that has not installed yet.
//
// State survives restarts, so a machine started again with different media would
// otherwise keep reporting the old one forever. Until an install happens, Talos is
// running from ISO or PXE with nothing on disk, and booting such a machine from
// other media genuinely does change what it is running, so the seeded values take
// precedence. After an install the disk decides, and the installed image is left
// alone.
func seedBootMedia(ctx context.Context, st state.State, image *talos.Image) error {
	_, err := safe.StateGetByID[*block.SystemDisk](ctx, st, block.SystemDiskID)

	switch {
	case err == nil:
		// installed: the disk holds what the machine runs, not the boot media
		return nil
	case !state.IsNotFoundError(err):
		return fmt.Errorf("failed to read system disk: %w", err)
	}

	return st.Modify(ctx, talos.NewImage(talos.NamespaceName, talos.ImageID), func(res resource.Resource) error {
		res.(*talos.Image).TypedSpec().Value = image.TypedSpec().Value //nolint:forcetypeassert,errcheck

		return nil
	})
}

// Machine is a single Talos machine.
type Machine struct {
	globalState       state.State
	runtime           *truntime.Runtime
	logger            *zap.Logger
	shutdown          chan struct{}
	schematicService  *schematic.Service
	enterpriseChecker controllers.EnterpriseChecker
	uuid              string
}

// NewMachine creates a Machine.
func NewMachine(uuid string, logger *zap.Logger, globalState state.State, schematicService *schematic.Service,
	enterpriseChecker controllers.EnterpriseChecker,
) (*Machine, error) {
	return &Machine{
		uuid:              uuid,
		logger:            logger,
		globalState:       globalState,
		schematicService:  schematicService,
		enterpriseChecker: enterpriseChecker,
		shutdown:          make(chan struct{}, 1),
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

	// The internal machine identifier is derived from the unique slot rather than the UUID:
	// UUIDs can be duplicated (e.g. when forcing UUID conflicts), and using them for per-machine
	// state would make machines clobber each other's state directory and resources.
	machineID := truntime.MachineID(slot)

	m.logger = zap.New(core).With(zap.String("machine", machineID), zap.String("uuid", m.uuid))

	// the configured base URL is used as-is, so a plain-HTTP factory keeps working
	bootFactoryURL := opts.bootFactoryURL
	if bootFactoryURL == "" {
		bootFactoryURL = m.schematicService.ImageFactoryBaseURL()
	}

	// The raw args are whatever the caller knows that the schematic does not carry.
	// The provider passes connection args here because it conveys them separately from
	// the boot media, while static mode reads everything from the schematic and passes
	// nothing, so the two never double-count.
	rt, err := truntime.NewRuntime(
		ctx, m.logger, slot, machineID, m.globalState,
		kubernetes, opts.nc, logSink, siderolinkParams.RawKernelArgs, m.schematicService,
		m.enterpriseChecker, m.schematicService.ImageFactoryHost(), bootFactoryURL, opts.nodeProxyingDisabled,
		opts.extensions,
	)
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

	// SideroLink is optional: bare Talos does not join anything, and boot media
	// without SideroLink kernel args emulates exactly that. Only seed the config
	// when there is an endpoint to join, the connection controller treats a
	// missing config as "nothing to do" and the machine simply runs standalone.
	if siderolinkParams.APIEndpoint != "" {
		siderolinkConfig := siderolink.NewConfig(config.NamespaceName, siderolink.ConfigID)
		siderolinkConfig.TypedSpec().APIEndpoint = siderolinkParams.APIEndpoint
		siderolinkConfig.TypedSpec().JoinToken = siderolinkParams.JoinToken
		siderolinkConfig.TypedSpec().Host = siderolinkParams.Host
		siderolinkConfig.TypedSpec().Insecure = siderolinkParams.Insecure
		siderolinkConfig.TypedSpec().Tunnel = siderolinkParams.TunnelMode

		resources = append(resources, siderolinkConfig)
	}

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

	resources = append(
		resources,
		hardwareInformation,
		platformMetadata,
		processorInfo,
		securityState,
		trustdEndpoint,
		eventSinkConfig,
		disk,
		defaultRoute,
		memory,
		discoveredDisk,
		pciNet,
		pciDisk,
	)

	// The machines are seeded with the partition layout on the boot disk. An install
	// can target any disk, at which point the layout moves there.
	resources = append(resources, blocklayout.Build(diskDevName)...)
	resources = append(resources, extraDisks(opts.extraDisks)...)

	var bootMedia *talos.Image

	if opts.schematic != "" || opts.talosVersion != "" {
		image := talos.NewImage(talos.NamespaceName, talos.ImageID)

		image.TypedSpec().Value.Schematic = opts.schematic
		image.TypedSpec().Value.Version = opts.talosVersion

		// the seeded image is the boot media, so it carries the boot factory host, making the
		// machine identity (enterprise-ness, FIPS state) follow the boot media until an
		// install/upgrade overwrites the image
		var bootFactoryHost string

		bootFactoryHost, err = hostOfURL(bootFactoryURL)
		if err != nil {
			return err
		}

		image.TypedSpec().Value.Host = bootFactoryHost

		bootMedia = image
	}

	for _, r := range resources {
		if err = rt.State().Create(ctx, r); err != nil {
			if state.IsConflictError(err) {
				continue
			}

			return fmt.Errorf("failed to create resource %s: %w", r.Metadata(), err)
		}
	}

	if bootMedia != nil {
		if err = seedBootMedia(ctx, rt.State(), bootMedia); err != nil {
			return err
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

// hostOfURL extracts the host part of a base URL.
func hostOfURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %q: %w", rawURL, err)
	}

	return parsed.Host, nil
}
