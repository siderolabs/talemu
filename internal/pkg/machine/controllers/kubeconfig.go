// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// KubeconfigController saves admin kubeconfig to the global emulator state in the cluster resource.
type KubeconfigController struct {
	GlobalState state.State
}

// Name implements controller.Controller interface.
func (ctrl *KubeconfigController) Name() string {
	return "k8s.KubeconfigController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KubeconfigController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.V1Alpha1ID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.KubernetesType,
			ID:        optional.Some(secrets.KubernetesID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *KubeconfigController) Outputs() []controller.Output {
	return nil
}

// Run implements controller.Controller interface.
func (ctrl *KubeconfigController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		config, err := machineconfig.GetComplete(ctx, r)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		secrets, err := safe.ReaderGetByID[*secrets.Kubernetes](ctx, r, secrets.KubernetesID)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		_, err = safe.StateUpdateWithConflicts(ctx, ctrl.GlobalState, emu.NewClusterStatus(emu.NamespaceName, config.Provider().Cluster().ID()).Metadata(),
			func(res *emu.ClusterStatus) error {
				res.TypedSpec().Value.Kubeconfig = []byte(secrets.TypedSpec().AdminKubeconfig)

				return nil
			},
		)
		if err != nil {
			return err
		}
	}
}
