// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	v1alpha1resource "github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
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
		{
			Namespace: v1alpha1resource.NamespaceName,
			Type:      v1alpha1resource.ServiceType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: talos.NamespaceName,
			ID:        optional.Some(talos.RebootID),
			Type:      talos.RebootStatusType,
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
		{
			Type: block.SystemDiskType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocognit
func (ctrl *MachineStatusController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		reboot, err := safe.ReaderGetByID[*talos.RebootStatus](ctx, r, talos.RebootID)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		config, err := machineconfig.GetComplete(ctx, r)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		if config == nil {
			if err = safe.WriterModify(ctx, r, runtime.NewMachineStatus(), func(res *runtime.MachineStatus) error {
				res.TypedSpec().Stage = runtime.MachineStageMaintenance
				res.TypedSpec().Status.Ready = true

				return nil
			}); err != nil {
				return err
			}

			continue
		}

		if err = ctrl.reconcile(ctx, r, config); err != nil {
			return err
		}

		stage := runtime.MachineStageRunning
		unmetConditions := []runtime.UnmetCondition{}

		expectResources := []resource.Resource{
			talos.NewVersion(talos.NamespaceName, talos.VersionID),
		}

		for _, expect := range expectResources {
			_, err = r.Get(ctx, expect.Metadata())
			if err != nil {
				if state.IsNotFoundError(err) {
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

		if reboot != nil {
			stage = runtime.MachineStageRebooting
		}

		services := []string{emuconst.APIDService}

		if config.Provider().Machine().Type().IsControlPlane() {
			services = append(services, emuconst.ETCDService)
			services = append(services, emuconst.KubeletService)
		}

		serviceConditions, err := ctrl.checkServicesReady(ctx, r, services...)
		if err != nil {
			return err
		}

		unmetConditions = append(unmetConditions, serviceConditions...)

		if err = safe.WriterModify(ctx, r, runtime.NewMachineStatus(), func(res *runtime.MachineStatus) error {
			res.TypedSpec().Stage = stage
			res.TypedSpec().Status.Ready = len(unmetConditions) == 0
			res.TypedSpec().Status.UnmetConditions = unmetConditions

			return nil
		}); err != nil {
			return err
		}
	}
}

func (ctrl *MachineStatusController) reconcile(ctx context.Context, r controller.Runtime, cfg *config.MachineConfig) error {
	disks, err := safe.ReaderListAll[*block.Disk](ctx, ctrl.State)
	if err != nil {
		return err
	}

	systemDisk, err := safe.ReaderGetByID[*block.SystemDisk](ctx, r, block.SystemDiskID)
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	installConfig, ok := cfg.Config().Machine().Install().(*v1alpha1.InstallConfig)
	if !ok {
		return fmt.Errorf("failed to read install config, only v1alpha1.InstallConfig is supported")
	}

	var (
		installed   bool
		installDisk *block.Disk
	)

	disks.ForEach(func(r *block.Disk) {
		if systemDisk != nil && systemDisk.TypedSpec().DiskID == r.Metadata().ID() {
			installed = true
		}

		if installConfig.InstallDisk != "" && installConfig.InstallDisk == filepath.Join("/dev", r.Metadata().ID()) {
			installDisk = r
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

	return safe.WriterModify(ctx, r, block.NewSystemDisk(block.NamespaceName, block.SystemDiskID), func(r *block.SystemDisk) error {
		r.TypedSpec().DiskID = installDisk.Metadata().ID()
		r.TypedSpec().DevPath = filepath.Join("/dev/", installDisk.Metadata().ID())

		return nil
	})
}

func (ctrl *MachineStatusController) checkServicesReady(ctx context.Context, r controller.Runtime, services ...string) ([]runtime.UnmetCondition, error) {
	var conditions []runtime.UnmetCondition

	name := "serviceNotReady"

	for _, s := range services {
		service, err := safe.ReaderGetByID[*v1alpha1resource.Service](ctx, r, s)
		if err != nil {
			if state.IsNotFoundError(err) {
				conditions = append(conditions, runtime.UnmetCondition{
					Name:   name,
					Reason: fmt.Sprintf("service %q is not started", s),
				})

				continue
			}

			return nil, err
		}

		if service.TypedSpec().Healthy && service.TypedSpec().Running {
			continue
		}

		if !service.TypedSpec().Healthy {
			conditions = append(conditions, runtime.UnmetCondition{
				Name:   name,
				Reason: fmt.Sprintf("service %q is not healthy", service.Metadata().ID()),
			})

			continue
		}

		if !service.TypedSpec().Running {
			conditions = append(conditions, runtime.UnmetCondition{
				Name:   name,
				Reason: fmt.Sprintf("service %q is not running", service.Metadata().ID()),
			})
		}
	}

	return conditions, nil
}
