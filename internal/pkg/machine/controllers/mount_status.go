// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
)

// MountStatusController generates node fake mounts.
type MountStatusController struct{}

// Name implements controller.Controller interface.
func (ctrl *MountStatusController) Name() string {
	return "runtime.MountStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *MountStatusController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.V1Alpha1ID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: block.NamespaceName,
			Type:      block.DiskType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: block.NamespaceName,
			Type:      block.SystemDiskType,
			Kind:      controller.InputWeak,
			ID:        optional.Some(block.SystemDiskID),
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *MountStatusController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: runtime.MountStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *MountStatusController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		statuses := []struct {
			label  string
			target string
			index  int
		}{
			{
				label:  constants.EphemeralPartitionLabel,
				index:  6,
				target: "/var",
			},
			{
				label:  constants.StatePartitionLabel,
				index:  5,
				target: "/system/state",
			},
		}

		config, err := machineconfig.GetComplete(ctx, r)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		systemDisk, err := safe.ReaderGetByID[*block.SystemDisk](ctx, r, block.SystemDiskID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		for _, status := range statuses {
			if config == nil || systemDisk == nil {
				err = r.Destroy(ctx, runtime.NewMountStatus(runtime.NamespaceName, status.label).Metadata())
				if err != nil && !state.IsNotFoundError(err) {
					return err
				}

				continue
			}

			if err = safe.WriterModify(ctx, r, runtime.NewMountStatus(runtime.NamespaceName, status.label), func(res *runtime.MountStatus) error {
				encryption := config.Provider().Machine().SystemDiskEncryption().Get(status.label)

				res.TypedSpec().Encrypted = encryption != nil
				res.TypedSpec().Source = fmt.Sprintf("%s%d", systemDisk.TypedSpec().DevPath, status.index)
				res.TypedSpec().Target = status.target
				res.TypedSpec().FilesystemType = "xfs"

				return nil
			}); err != nil {
				return err
			}
		}
	}
}
