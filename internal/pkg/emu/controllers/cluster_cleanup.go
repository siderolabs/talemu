// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// ClusterCleanupController removes empty clusters and cleans up their data.
type ClusterCleanupController struct {
	Kubernetes *kubefactory.Kubernetes
}

// Name implements controller.Controller interface.
func (ctrl *ClusterCleanupController) Name() string {
	return "k8s.ClusterStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *ClusterCleanupController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: emu.NamespaceName,
			Type:      emu.ClusterStatusType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *ClusterCleanupController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: emu.ClusterStatusType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *ClusterCleanupController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		list, err := safe.ReaderListAll[*emu.ClusterStatus](ctx, r)
		if err != nil {
			return fmt.Errorf("error listing clusters: %w", err)
		}

		err = list.ForEachErr(func(res *emu.ClusterStatus) error {
			if res.Metadata().Phase() == resource.PhaseRunning {
				return nil
			}

			if err = ctrl.Kubernetes.DeleteEtcdState(ctx, res.Metadata().ID()); err != nil {
				return err
			}

			if err = r.Destroy(ctx, res.Metadata(), controller.WithOwner("")); err != nil && !state.IsNotFoundError(err) {
				return err
			}

			return nil
		})
		if err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}
