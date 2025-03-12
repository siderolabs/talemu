// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package talos contains Talos specific resources.
package talos

import (
	"context"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/registry"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/cri"
	"github.com/siderolabs/talos/pkg/machinery/resources/etcd"
	"github.com/siderolabs/talos/pkg/machinery/resources/files"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubeaccess"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/perf"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"github.com/siderolabs/talos/pkg/machinery/resources/siderolink"
	"github.com/siderolabs/talos/pkg/machinery/resources/time"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
)

// Register Talos resource group in the state.
func Register(ctx context.Context, state state.State) error {
	namespaceRegistry := registry.NewNamespaceRegistry(state)
	resourceRegistry := registry.NewResourceRegistry(state)

	if err := namespaceRegistry.RegisterDefault(ctx); err != nil {
		return err
	}

	if err := resourceRegistry.RegisterDefault(ctx); err != nil {
		return err
	}

	// register Talos namespaces
	for _, ns := range []struct {
		name        string
		description string
	}{
		{v1alpha1.NamespaceName, "Talos v1alpha1 subsystems glue resources."},
		{cluster.NamespaceName, "Cluster configuration and discovery resources."},
		{cluster.RawNamespaceName, "Cluster unmerged raw resources."},
		{config.NamespaceName, "Talos node configuration."},
		{etcd.NamespaceName, "etcd resources."},
		{files.NamespaceName, "Files and file-like resources."},
		{hardware.NamespaceName, "Hardware resources."},
		{k8s.NamespaceName, "Kubernetes all node types resources."},
		{k8s.ControlPlaneNamespaceName, "Kubernetes control plane resources."},
		{kubespan.NamespaceName, "KubeSpan resources."},
		{network.NamespaceName, "Networking resources."},
		{network.ConfigNamespaceName, "Networking configuration resources."},
		{cri.NamespaceName, "CRI Seccomp resources."},
		{secrets.NamespaceName, "Resources with secret material."},
		{perf.NamespaceName, "Stats resources."},

		{NamespaceName, "Emulator resource."},
	} {
		if err := namespaceRegistry.Register(ctx, ns.name, ns.description); err != nil {
			return err
		}
	}

	// register Talos resources
	for _, r := range []meta.ResourceWithRD{
		&cluster.Affiliate{},
		&cluster.Config{},
		&cluster.Identity{},
		&cluster.Info{},
		&cluster.Member{},
		&config.MachineConfig{},
		&config.MachineType{},
		&cri.SeccompProfile{},
		&etcd.Config{},
		&etcd.PKIStatus{},
		&etcd.Spec{},
		&etcd.Member{},
		&files.EtcFileSpec{},
		&files.EtcFileStatus{},
		&hardware.Processor{},
		&hardware.MemoryModule{},
		&hardware.SystemInformation{},
		&k8s.AdmissionControlConfig{},
		&k8s.AuditPolicyConfig{},
		&k8s.APIServerConfig{},
		&k8s.KubePrismEndpoints{},
		&k8s.ConfigStatus{},
		&k8s.ControllerManagerConfig{},
		&k8s.Endpoint{},
		&k8s.ExtraManifestsConfig{},
		&k8s.KubeletConfig{},
		&k8s.KubeletLifecycle{},
		&k8s.KubeletSpec{},
		&k8s.KubePrismConfig{},
		&k8s.KubePrismStatuses{},
		&k8s.Manifest{},
		&k8s.ManifestStatus{},
		&k8s.BootstrapManifestsConfig{},
		&k8s.NodeCordonedSpec{},
		&k8s.NodeIP{},
		&k8s.NodeIPConfig{},
		&k8s.NodeLabelSpec{},
		&k8s.Nodename{},
		&k8s.NodeStatus{},
		&k8s.NodeTaintSpec{},
		&k8s.SchedulerConfig{},
		&k8s.StaticPod{},
		&k8s.StaticPodServerStatus{},
		&k8s.StaticPodStatus{},
		&k8s.SecretsStatus{},
		&block.Disk{},
		&block.SystemDisk{},
		&block.Device{},
		&block.DiscoveredVolume{},
		&block.DiscoveryRefreshRequest{},
		&block.DiscoveryRefreshStatus{},
		&kubeaccess.Config{},
		&kubespan.Config{},
		&kubespan.Endpoint{},
		&kubespan.Identity{},
		&kubespan.PeerSpec{},
		&kubespan.PeerStatus{},
		&network.AddressStatus{},
		&network.AddressSpec{},
		&network.DeviceConfigSpec{},
		&network.HardwareAddr{},
		&network.HostnameStatus{},
		&network.HostnameSpec{},
		&network.LinkRefresh{},
		&network.LinkStatus{},
		&network.LinkSpec{},
		&network.NodeAddress{},
		&network.NodeAddressFilter{},
		&network.OperatorSpec{},
		&network.ProbeSpec{},
		&network.ProbeStatus{},
		&network.ResolverStatus{},
		&network.ResolverSpec{},
		&network.RouteStatus{},
		&network.RouteSpec{},
		&network.Status{},
		&network.TimeServerStatus{},
		&network.TimeServerSpec{},
		&perf.CPU{},
		&perf.Memory{},
		&runtime.DevicesStatus{},
		&runtime.EventSinkConfig{},
		&runtime.ExtensionStatus{},
		&runtime.KernelModuleSpec{},
		&runtime.KernelParamSpec{},
		&runtime.KernelParamDefaultSpec{},
		&runtime.KernelParamStatus{},
		&runtime.KmsgLogConfig{},
		&runtime.MaintenanceServiceConfig{},
		&runtime.MaintenanceServiceRequest{},
		&runtime.MachineStatus{},
		&runtime.MetaKey{},
		&runtime.MountStatus{},
		&runtime.PlatformMetadata{},
		&runtime.SecurityState{},
		&secrets.API{},
		&secrets.CertSAN{},
		&secrets.Etcd{},
		&secrets.EtcdRoot{},
		&secrets.Kubelet{},
		&secrets.Kubernetes{},
		&secrets.KubernetesDynamicCerts{},
		&secrets.KubernetesRoot{},
		&secrets.MaintenanceServiceCerts{},
		&secrets.MaintenanceRoot{},
		&secrets.OSRoot{},
		&secrets.Trustd{},
		&siderolink.Config{},
		&time.Status{},
		&v1alpha1.AcquireConfigSpec{},
		&v1alpha1.AcquireConfigStatus{},
		&v1alpha1.Service{},

		&EventSinkState{},
		&Image{},
		&Version{},
		&Reboot{},
		&RebootStatus{},
	} {
		if err := resourceRegistry.Register(ctx, r); err != nil {
			return err
		}
	}

	return nil
}
