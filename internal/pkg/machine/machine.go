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
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
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
)

// Machine is a single Talos machine.
type Machine struct {
	globalState state.State
	runtime     *truntime.Runtime
	logger      *zap.Logger
	shutdown    chan struct{}
	uuid        string
}

// NewMachine creates a Machine.
func NewMachine(uuid string, logger *zap.Logger, globalState state.State) (*Machine, error) {
	return &Machine{
		uuid:        uuid,
		logger:      logger,
		globalState: globalState,
		shutdown:    make(chan struct{}, 1),
	}, nil
}

// Run starts the machine.
func (m *Machine) Run(ctx context.Context, siderolinkParams *SideroLinkParams, slot int, kubernetes *kubefactory.Kubernetes, options ...Option) error {
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

	rt, err := truntime.NewRuntime(ctx, m.logger, slot, m.uuid, m.globalState, kubernetes, opts.nc, logSink)
	if err != nil {
		return fmt.Errorf("COSI runtime creation failed: %w", err)
	}

	m.runtime = rt

	resources := make([]resource.Resource, 0, 12)

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

	uniqueMachineToken := runtime.NewUniqueMachineToken()
	uniqueMachineToken.TypedSpec().Token = m.uuid

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
	securityState.TypedSpec().SecureBoot = false

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

	disk := talos.NewDisk(talos.NamespaceName, "/dev/vda")
	disk.TypedSpec().Value = &storage.Disk{
		Size:       50 * 1024 * 1024 * 1024,
		DeviceName: "/dev/vda",
		Model:      "CM5514",
		Type:       storage.Disk_HDD,
		BusPath:    "/pci0000:00/0000:00:05.0/0000:01:01.0/virtio2/host2/target2:0:0/2:0:0:0/",
	}

	resources = append(resources,
		hardwareInformation,
		siderolinkConfig,
		uniqueMachineToken,
		platformMetadata,
		processorInfo,
		securityState,
		trustdEndpoint,
		eventSinkConfig,
		disk,
		defaultRoute,
		memory,
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
