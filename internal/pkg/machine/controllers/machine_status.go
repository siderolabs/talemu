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
	"github.com/siderolabs/go-blockdevice/blockdevice/util/disk"
	"github.com/siderolabs/image-factory/pkg/constants"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// MachineStatusController computes machine state from the existing resources.
// Updates machine status resource.
type MachineStatusController struct {
	State state.State
}

// Name implements controller.Controller interface.
func (ctrl *MachineStatusController) Name() string {
	return "runtime.MachineStatusController"
}

// Inputs implements controller.Controller interface.
func (ctrl *MachineStatusController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: runtime.NamespaceName,
			Type:      runtime.ExtensionStatusType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: talos.NamespaceName,
			Type:      talos.VersionType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *MachineStatusController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: runtime.MachineStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *MachineStatusController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, r, config.V1Alpha1ID)
		if err != nil {
			if state.IsNotFoundError(err) {
				if err = safe.WriterModify(ctx, r, runtime.NewMachineStatus(), func(res *runtime.MachineStatus) error {
					res.TypedSpec().Stage = runtime.MachineStageMaintenance
					res.TypedSpec().Status.Ready = true

					return nil
				}); err != nil {
					return err
				}

				continue
			}

			return err
		}

		if err = ctrl.reconcile(ctx, r, config); err != nil {
			return err
		}

		ready := true
		stage := runtime.MachineStageRunning
		unmetConditions := []runtime.UnmetCondition{}

		expectResources := []resource.Resource{
			talos.NewVersion(talos.NamespaceName, talos.VersionID),
			runtime.NewExtensionStatus(runtime.NamespaceName, constants.SchematicIDExtensionName),
		}

		for _, expect := range expectResources {
			_, err = r.Get(ctx, expect.Metadata())
			if err != nil {
				if state.IsNotFoundError(err) {
					ready = false
					stage = runtime.MachineStageBooting

					unmetConditions = append(
						unmetConditions,
						runtime.UnmetCondition{
							Name:   "resourceNotReady",
							Reason: fmt.Sprintf("%s doesn't exist yet", expect.Metadata()),
						},
					)

					continue
				}

				return fmt.Errorf("failed to query %w", err)
			}
		}

		if err := safe.WriterModify(ctx, r, runtime.NewMachineStatus(), func(res *runtime.MachineStatus) error {
			res.TypedSpec().Stage = stage
			res.TypedSpec().Status.Ready = ready
			res.TypedSpec().Status.UnmetConditions = unmetConditions

			return nil
		}); err != nil {
			return err
		}
	}
}

func (ctrl *MachineStatusController) reconcile(ctx context.Context, r controller.Runtime, cfg *config.MachineConfig) error {
	disks, err := safe.ReaderListAll[*talos.Disk](ctx, ctrl.State)
	if err != nil {
		return err
	}

	installConfig, ok := cfg.Config().Machine().Install().(*v1alpha1.InstallConfig)
	if !ok {
		return fmt.Errorf("failed to read install config, only v1alpha1.InstallConfig is supported")
	}

	var (
		installed   bool
		installDisk *talos.Disk
	)

	disks.ForEach(func(r *talos.Disk) {
		if r.TypedSpec().Value.SystemDisk {
			installed = true
		}

		switch {
		case installConfig.InstallDisk != "" && installConfig.InstallDisk == r.TypedSpec().Value.DeviceName:
			installDisk = r
		case installDisk == nil && installConfig.InstallDiskSelector != nil:
			matchers := installConfig.DiskMatchers()

			if disk.Match(&disk.Disk{
				Size:       r.TypedSpec().Value.Size,
				Model:      r.TypedSpec().Value.Model,
				BusPath:    r.TypedSpec().Value.BusPath,
				DeviceName: r.TypedSpec().Value.DeviceName,
				Serial:     r.TypedSpec().Value.Serial,
				Name:       r.TypedSpec().Value.Name,
				WWID:       r.TypedSpec().Value.Wwid,
				UUID:       r.TypedSpec().Value.Uuid,
				Type:       disk.Type(r.TypedSpec().Value.Type),
				SubSystem:  r.TypedSpec().Value.Subsystem,
				ReadOnly:   r.TypedSpec().Value.Readonly,
				Modalias:   r.TypedSpec().Value.Modalias,
			}, matchers...) {
				installDisk = r
			}
		}
	})

	if installed {
		return nil
	}

	if installDisk == nil {
		return status.Errorf(codes.InvalidArgument, "the install disk %s doesn't exist", installConfig.InstallDisk)
	}

	if err = safe.WriterModify(ctx, r, runtime.NewMachineStatus(), func(res *runtime.MachineStatus) error {
		res.TypedSpec().Stage = runtime.MachineStageInstalling
		res.TypedSpec().Status.Ready = false

		return nil
	}); err != nil {
		return err
	}

	_, err = safe.StateUpdateWithConflicts(ctx, ctrl.State, installDisk.Metadata(), func(d *talos.Disk) error {
		d.TypedSpec().Value.SystemDisk = true

		return nil
	})

	return err
}
