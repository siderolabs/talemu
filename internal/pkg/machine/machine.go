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
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/siderolabs/talos/pkg/machinery/resources/siderolink"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
	"github.com/siderolabs/talemu/internal/pkg/machine/events"
	truntime "github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// Machine is a single Talos machine.
type Machine struct {
	globalState state.State
	runtime     *truntime.Runtime
	logger      *zap.Logger
	uuid        string
}

// NewMachine creates a Machine.
func NewMachine(uuid string, logger *zap.Logger, globalState state.State) (*Machine, error) {
	return &Machine{
		uuid:        uuid,
		logger:      logger.With(zap.String("machine", uuid)),
		globalState: globalState,
	}, nil
}

// SideroLinkParams is the siderolink params needed to join Omni instance.
type SideroLinkParams struct {
	APIEndpoint    string
	JoinToken      string
	LogsEndpoint   string
	EventsEndpoint string
	Host           string
	Insecure       bool
	TunnelMode     bool
}

// Run starts the machine.
func (m *Machine) Run(ctx context.Context, siderolinkParams *SideroLinkParams, machineIndex int) error {
	rt, err := truntime.NewRuntime(ctx, m.logger, machineIndex, m.globalState)
	if err != nil {
		return err
	}

	m.runtime = rt

	resources := make([]resource.Resource, 0, 10)

	// populate the inputs for the siderolink controller
	hardwareInformation := hardware.NewSystemInformation(hardware.SystemInformationID)
	hardwareInformation.TypedSpec().UUID = m.uuid
	hardwareInformation.TypedSpec().ProductName = "Talos Emulator"
	hardwareInformation.TypedSpec().Manufacturer = "SideroLabs"

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
	processorInfo.TypedSpec().Manufacturer = "SideroLabs"
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

	helloWorldExtension := runtime.NewExtensionStatus(runtime.NamespaceName, "hello-world-service")
	helloWorldExtension.TypedSpec().Metadata.Name = "hello-world-service"
	helloWorldExtension.TypedSpec().Metadata.Version = "v1.0.0"

	disk := talos.NewDisk(talos.NamespaceName, "/dev/vda")
	disk.TypedSpec().Value = &storage.Disk{
		Size:       50 * 1024 * 1024 * 1024,
		DeviceName: "/dev/sda",
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
		helloWorldExtension,
	)

	for _, r := range resources {
		if err = rt.State().Create(ctx, r); err != nil {
			if state.IsConflictError(err) {
				continue
			}

			return err
		}
	}

	sink, err := events.NewHandler(ctx, rt.State(), m.uuid)
	if err != nil {
		return err
	}

	var eg errgroup.Group

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
