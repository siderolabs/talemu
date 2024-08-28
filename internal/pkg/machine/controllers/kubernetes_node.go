// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

const (
	machineIDLabel    = "talemu.dev/machine"
	inputVersionLabel = "talemu.dev/inputversion"
	osLinux           = "linux"
)

// KubernetesNodeController registers machine in the kubernetes state.
type KubernetesNodeController struct {
	GlobalState state.State

	client *kubernetes.Clientset

	MachineID string
	config    []byte
}

// Name implements controller.Controller interface.
func (ctrl *KubernetesNodeController) Name() string {
	return "network.KubernetesNodeController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KubernetesNodeController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.V1Alpha1ID),
			Kind:      controller.InputStrong,
		},
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.KubernetesType,
			ID:        optional.Some(secrets.KubernetesID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: k8s.NamespaceName,
			Type:      k8s.NodenameType,
			ID:        optional.Some(k8s.NodenameID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: v1alpha1.NamespaceName,
			Type:      v1alpha1.ServiceType,
			ID:        optional.Some(constants.ETCDService),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: network.NamespaceName,
			Type:      network.HostnameStatusType,
			ID:        optional.Some(network.HostnameID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: talos.NamespaceName,
			Type:      talos.VersionType,
			ID:        optional.Some(talos.VersionID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: hardware.NamespaceName,
			Type:      hardware.SystemInformationType,
			ID:        optional.Some(hardware.SystemInformationID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: network.NamespaceName,
			Type:      network.NodeAddressType,
			ID:        optional.Some(network.NodeAddressDefaultID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: hardware.NamespaceName,
			Type:      hardware.MemoryModuleType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: hardware.NamespaceName,
			Type:      hardware.ProcessorType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *KubernetesNodeController) Outputs() []controller.Output {
	return nil
}

// Run implements controller.Controller interface.
//
//nolint:gocognit,gocyclo,cyclop
func (ctrl *KubernetesNodeController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, r, config.V1Alpha1ID)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		nodename, err := safe.ReaderGetByID[*k8s.Nodename](ctx, r, k8s.NodenameID)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		client, err := ctrl.getClient(ctx, r, config)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		if config.Metadata().Phase() == resource.PhaseTearingDown {
			// best effort node deletion
			// Omni will clean it up if the deletion fails
			if err = ctrl.removeNode(ctx, client, nodename); err != nil {
				logger.Warn("failed to destroy the node", zap.Error(err))
			}

			if err = r.RemoveFinalizer(ctx, config.Metadata(), ctrl.Name()); err != nil {
				return err
			}

			continue
		}

		if err = r.AddFinalizer(ctx, config.Metadata(), ctrl.Name()); err != nil {
			return err
		}

		spec := v1.NodeSpec{
			PodCIDR:  config.Provider().Cluster().Network().PodCIDRs()[0],
			PodCIDRs: config.Provider().Cluster().Network().PodCIDRs(),
		}

		hostname, err := safe.ReaderGetByID[*network.HostnameStatus](ctx, r, network.HostnameID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		version, err := safe.ReaderGetByID[*talos.Version](ctx, r, talos.VersionID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		status, err := ctrl.computeNodeStatus(ctx, r, config, hostname, version)
		if err != nil {
			return err
		}

		nodenameVersion := nodename.Metadata().Version().String()

		labels := ctrl.computeNodeLabels(config, hostname, version)

		labels[machineIDLabel] = ctrl.MachineID
		labels[inputVersionLabel] = nodenameVersion

		node, err := client.CoreV1().Nodes().Get(ctx, nodename.TypedSpec().Nodename, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

		if node.Name == "" {
			node = &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodename.TypedSpec().Nodename,
					Labels: labels,
				},
				Spec:   spec,
				Status: *status,
			}

			if _, err = client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{}); err != nil {
				return err
			}

			logger.Info("created node", zap.String("node", nodename.TypedSpec().Nodename))
		} else {
			node.Status = *status

			if _, err = client.CoreV1().Nodes().UpdateStatus(ctx, node, metav1.UpdateOptions{}); err != nil {
				return err
			}

			node.Labels = labels

			if _, err = client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}

		query := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s!=%s,%s=%s",
				inputVersionLabel, nodenameVersion,
				machineIDLabel, ctrl.MachineID,
			),
		}

		// cleanup other nodes registrations if any
		err = client.CoreV1().Nodes().DeleteCollection(ctx, metav1.DeleteOptions{}, query)
		if err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}

func (ctrl *KubernetesNodeController) computeNodeStatus(ctx context.Context, r controller.Runtime, config *config.MachineConfig,
	hostname *network.HostnameStatus, version *talos.Version,
) (*v1.NodeStatus, error) {
	var (
		conditions []v1.NodeCondition
		nodeInfo   v1.NodeSystemInfo
		addresses  []v1.NodeAddress
	)

	conditions = append(conditions, v1.NodeCondition{
		Type:    v1.NodeReady,
		Reason:  "KubeletReady",
		Status:  v1.ConditionTrue,
		Message: "kubelet is posting ready status",
	})

	systemInformation, err := safe.ReaderGetByID[*hardware.SystemInformation](ctx, r, hardware.SystemInformationID)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

	if systemInformation != nil {
		nodeInfo.SystemUUID = systemInformation.TypedSpec().UUID
	}

	nodeInfo.ContainerRuntimeVersion = "containerd://1.7.13"
	nodeInfo.KernelVersion = "6.1.82-talos"
	nodeInfo.OperatingSystem = osLinux

	if version != nil {
		nodeInfo.OSImage = fmt.Sprintf("Talos (%s)", version.TypedSpec().Value.Value)
	}

	nodeInfo.KubeletVersion = getImageVersion(config.Provider().Machine().Kubelet().Image())
	nodeInfo.KubeProxyVersion = getImageVersion(config.Provider().Cluster().Proxy().Image()) //nolint:staticcheck

	address, err := safe.ReaderGetByID[*network.NodeAddress](ctx, r, network.NodeAddressDefaultID)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

	if address != nil {
		for _, addr := range address.TypedSpec().Addresses {
			addresses = append(addresses, v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: addr.String(),
			})
		}
	}

	if hostname != nil {
		addresses = append(addresses, v1.NodeAddress{
			Type:    v1.NodeHostName,
			Address: hostname.TypedSpec().Hostname,
		})
	}

	status := &v1.NodeStatus{
		Conditions: conditions,
		NodeInfo:   nodeInfo,
		Addresses:  addresses,
	}

	var (
		memCapacity int64
		cpuCapacity int64
	)

	memory, err := safe.ReaderListAll[*hardware.MemoryModule](ctx, r)
	if err != nil {
		return nil, err
	}

	if err = memory.ForEachErr(func(r *hardware.MemoryModule) error {
		memCapacity += int64(r.TypedSpec().Size) * (1024 * 1024)

		return nil
	}); err != nil {
		return nil, err
	}

	cpu, err := safe.ReaderListAll[*hardware.Processor](ctx, r)
	if err != nil {
		return nil, err
	}

	if err = cpu.ForEachErr(func(r *hardware.Processor) error {
		cpuCapacity += int64(r.TypedSpec().CoreCount)

		return nil
	}); err != nil {
		return nil, err
	}

	status.Capacity = v1.ResourceList{}
	status.Capacity[v1.ResourceMemory] = *kresource.NewQuantity(memCapacity, kresource.DecimalSI)
	status.Capacity[v1.ResourceCPU] = *kresource.NewQuantity(cpuCapacity, kresource.DecimalSI)
	status.Capacity[v1.ResourceEphemeralStorage] = *kresource.NewQuantity(5e9, kresource.DecimalSI)
	status.Capacity[v1.ResourcePods] = *kresource.NewQuantity(110, kresource.DecimalSI)

	return status, nil
}

func (ctrl *KubernetesNodeController) computeNodeLabels(config *config.MachineConfig, hostname *network.HostnameStatus, version *talos.Version) map[string]string {
	labels := map[string]string{}

	if hostname != nil {
		labels["kubernetes.io/hostname"] = hostname.TypedSpec().Hostname
	}

	if version != nil {
		labels["kubernetes.io/arch"] = version.TypedSpec().Value.Architecture
		labels["beta.kubernetes.io/arch"] = version.TypedSpec().Value.Architecture
	}

	labels["kubernetes.io/os"] = osLinux
	labels["beta.kubernetes.io/os"] = osLinux

	for key, value := range config.Config().Machine().NodeLabels() {
		labels[key] = value
	}

	if config.Config().Machine().Type().IsControlPlane() {
		labels["node-role.kubernetes.io/control-plane"] = ""
	}

	kubeletArgs := config.Config().Machine().Kubelet().ExtraArgs()
	if kubeleteLabels, ok := kubeletArgs["node-labels"]; ok {
		for _, pair := range strings.Split(kubeleteLabels, ",") {
			key, value, found := strings.Cut(pair, "=")
			if !found {
				continue
			}

			labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	return labels
}

func (ctrl *KubernetesNodeController) removeNode(ctx context.Context, client *kubernetes.Clientset, nodename *k8s.Nodename) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)

	defer cancel()

	if err := client.CoreV1().Nodes().Delete(ctx, nodename.TypedSpec().Nodename, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func (ctrl *KubernetesNodeController) getClient(ctx context.Context, r controller.Runtime, machineConfig *config.MachineConfig) (*kubernetes.Clientset, error) {
	secrets, err := safe.ReaderGetByID[*secrets.Kubernetes](ctx, r, secrets.KubernetesID)
	if err != nil && !state.IsNotFoundError(err) {
		return nil, err
	}

	var config []byte

	if secrets != nil {
		config = []byte(secrets.TypedSpec().LocalhostAdminKubeconfig)
	}

	if config == nil {
		var cluster *emu.ClusterStatus

		cluster, err = safe.ReaderGetByID[*emu.ClusterStatus](ctx, ctrl.GlobalState, machineConfig.Provider().Cluster().ID())
		if err != nil {
			return nil, err
		}

		config = cluster.TypedSpec().Value.Kubeconfig

		if config == nil {
			return nil, fmt.Errorf("the kubeconfig is not present in the cluster yet")
		}
	}

	if bytes.Equal(ctrl.config, config) && ctrl.client != nil {
		return ctrl.client, nil
	}

	cfg, err := clientcmd.NewClientConfigFromBytes(config)
	if err != nil {
		return nil, err
	}

	clientCfg, err := cfg.ClientConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(clientCfg)
	if err != nil {
		return nil, err
	}

	ctrl.client = client
	ctrl.config = config

	return client, err
}

func getImageVersion(image string) string {
	_, version, _ := strings.Cut(image, ":")

	return version
}
