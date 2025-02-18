// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/go-pointer"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
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
	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
)

// StaticPodController renders fake static pod states.
type StaticPodController struct {
	client *kubernetes.Clientset

	MachineID string
	config    []byte
}

// Name implements controller.Controller interface.
func (ctrl *StaticPodController) Name() string {
	return "network.StaticPodController"
}

// Inputs implements controller.Controller interface.
func (ctrl *StaticPodController) Inputs() []controller.Input {
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
			Namespace: network.NamespaceName,
			Type:      network.NodeAddressType,
			ID:        optional.Some(network.NodeAddressDefaultID),
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
			ID:        optional.Some(constants.KubeletService),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *StaticPodController) Outputs() []controller.Output {
	return nil
}

// Run implements controller.Controller interface.
func (ctrl *StaticPodController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		if err := ctrl.reconcile(ctx, r, logger); err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}

//nolint:gocognit,cyclop,gocyclo,maintidx
func (ctrl *StaticPodController) reconcile(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	cfg, err := machineconfig.GetComplete(ctx, r)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

		return err
	}

	address, err := safe.ReaderGetByID[*network.NodeAddress](ctx, r, network.NodeAddressDefaultID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

		return err
	}

	nodename, err := safe.ReaderGetByID[*k8s.Nodename](ctx, r, k8s.NodenameID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

		return err
	}

	kubelet, err := safe.ReaderGetByID[*v1alpha1.Service](ctx, r, constants.KubeletService)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

		return err
	}

	if !kubelet.TypedSpec().Healthy {
		return nil
	}

	client, err := ctrl.getClient(ctx, r)
	if err != nil {
		if state.IsNotFoundError(err) {
			return nil
		}

		return err
	}

	ns := "kube-system"

	if cfg.Metadata().Phase() == resource.PhaseTearingDown {
		query := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s",
				machineIDLabel, ctrl.MachineID,
			),
		}

		err = client.CoreV1().Pods(ns).DeleteCollection(ctx, metav1.DeleteOptions{
			GracePeriodSeconds: pointer.To[int64](0),
		}, query)
		if err != nil {
			return err
		}

		return r.RemoveFinalizer(ctx, cfg.Metadata(), ctrl.Name())
	}

	if err = r.AddFinalizer(ctx, cfg.Metadata(), ctrl.Name()); err != nil {
		return err
	}

	serviceAccount := v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: ns,
		},
	}

	_, err = client.CoreV1().ServiceAccounts(serviceAccount.Namespace).Get(ctx, serviceAccount.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		_, err = client.CoreV1().ServiceAccounts(serviceAccount.Namespace).Create(ctx, &serviceAccount, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	nodenameVersion := nodename.Metadata().Version().String()

	for _, cb := range []func(config *config.MachineConfig, nodename *k8s.Nodename) (*v1.Pod, error){
		ctrl.renderAPIServer,
		ctrl.renderControllerManager,
		ctrl.renderScheduler,
	} {
		var pod *v1.Pod

		if pod, err = cb(cfg, nodename); err != nil {
			return err
		}

		pod.Status.HostIP = address.TypedSpec().Addresses[0].String()
		pod.Labels[machineIDLabel] = ctrl.MachineID
		pod.Labels[inputVersionLabel] = nodenameVersion

		pod.Spec.SchedulerName = "default-scheduler"
		pod.Spec.NodeName = nodename.TypedSpec().Nodename
		pod.Spec.HostNetwork = true

		pod.Status.Phase = v1.PodRunning
		pod.Status.Conditions = []v1.PodCondition{
			{
				Type:   v1.PodReadyToStartContainers,
				Status: v1.ConditionTrue,
			},
			{
				Type:   v1.PodInitialized,
				Status: v1.ConditionTrue,
			},
			{
				Type:   v1.PodReady,
				Status: v1.ConditionTrue,
			},
			{
				Type:   v1.ContainersReady,
				Status: v1.ConditionTrue,
			},
			{
				Type:   v1.PodScheduled,
				Status: v1.ConditionTrue,
			},
		}

		pod.Status.ContainerStatuses = make([]v1.ContainerStatus, 0, len(pod.Spec.Containers))

		for _, container := range pod.Spec.Containers {
			pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses,
				v1.ContainerStatus{
					Name:    container.Name,
					Image:   container.Image,
					Started: pointer.To(true),
					Ready:   true,
					State: v1.ContainerState{
						Running: &v1.ContainerStateRunning{},
					},
				},
			)
		}

		existing, err := client.CoreV1().Pods(ns).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}

		if existing.Name != "" {
			if _, err = client.CoreV1().Pods(ns).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
				return err
			}

			for index, container := range pod.Spec.Containers {
				existing.Spec.Containers[index].Image = container.Image
			}

			existing.Labels = pod.Labels

			if _, err = client.CoreV1().Pods(ns).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
				return err
			}

			logger.Info("updated static pod", zap.String("name", pod.Name))

			continue
		}

		if _, err = client.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			return err
		}

		if _, err = client.CoreV1().Pods(ns).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
			return err
		}

		logger.Info("created static pod", zap.String("name", pod.Name))

		query := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s!=%s,%s=%s",
				inputVersionLabel, nodenameVersion,
				machineIDLabel, ctrl.MachineID,
			),
		}

		err = client.CoreV1().Pods(ns).DeleteCollection(ctx, metav1.DeleteOptions{
			GracePeriodSeconds: pointer.To[int64](0),
		}, query)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ctrl *StaticPodController) renderAPIServer(machineConfig *config.MachineConfig, nodename *k8s.Nodename) (*v1.Pod, error) {
	var pod v1.Pod

	pod.Name = fmt.Sprintf("kube-apiserver-%s", nodename.TypedSpec().Nodename)
	pod.Labels = map[string]string{
		"k8s-app": "kube-apiserver",
		"tier":    "control-plane",
	}

	pod.Spec.Containers = []v1.Container{
		{
			Name:  "kube-apiserver",
			Image: machineConfig.Config().Cluster().APIServer().Image(),
			Resources: v1.ResourceRequirements{
				Limits: v1.ResourceList{
					v1.ResourceCPU:    kresource.MustParse("50m"),
					v1.ResourceMemory: kresource.MustParse("256Mi"),
				},
			},
		},
	}

	return &pod, nil
}

func (ctrl *StaticPodController) renderScheduler(machineConfig *config.MachineConfig, nodename *k8s.Nodename) (*v1.Pod, error) {
	var pod v1.Pod

	pod.Name = fmt.Sprintf("kube-scheduler-%s", nodename.TypedSpec().Nodename)
	pod.Labels = map[string]string{
		"k8s-app": "kube-scheduler",
		"tier":    "control-plane",
	}

	pod.Spec.Containers = []v1.Container{
		{
			Name:  "kube-scheduler",
			Image: machineConfig.Config().Cluster().Scheduler().Image(),
			Resources: v1.ResourceRequirements{
				Limits: v1.ResourceList{
					v1.ResourceCPU:    kresource.MustParse("50m"),
					v1.ResourceMemory: kresource.MustParse("256Mi"),
				},
			},
		},
	}

	return &pod, nil
}

func (ctrl *StaticPodController) renderControllerManager(machineConfig *config.MachineConfig, nodename *k8s.Nodename) (*v1.Pod, error) {
	var pod v1.Pod

	pod.Name = fmt.Sprintf("kube-controller-manager-%s", nodename.TypedSpec().Nodename)
	pod.Labels = map[string]string{
		"k8s-app": "kube-controller-manager",
		"tier":    "control-plane",
	}

	pod.Spec.Containers = []v1.Container{
		{
			Name:  "kube-controller-manager",
			Image: machineConfig.Config().Cluster().ControllerManager().Image(),
			Resources: v1.ResourceRequirements{
				Limits: v1.ResourceList{
					v1.ResourceCPU:    kresource.MustParse("50m"),
					v1.ResourceMemory: kresource.MustParse("256Mi"),
				},
			},
		},
	}

	return &pod, nil
}

func (ctrl *StaticPodController) getClient(ctx context.Context, r controller.Runtime) (*kubernetes.Clientset, error) {
	secrets, err := safe.ReaderGetByID[*secrets.Kubernetes](ctx, r, secrets.KubernetesID)
	if err != nil {
		return nil, err
	}

	var config []byte

	if secrets != nil {
		config = []byte(secrets.TypedSpec().LocalhostAdminKubeconfig)
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
