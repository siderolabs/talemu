// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LifecycleService is a GRPC service emulating the behavior of the Talos lifecycle service.
type LifecycleService struct {
	machine.UnimplementedLifecycleServiceServer

	state              state.State
	logger             *zap.Logger
	sharedMachineState *machineState
	imageFactoryHost   string
}

// NewLifecycleService creates a new LifecycleService.
func NewLifecycleService(st state.State, imageFactoryHost string, logger *zap.Logger, sharedMachineState *machineState) *LifecycleService {
	if sharedMachineState == nil {
		sharedMachineState = newMachineState()
	}

	return &LifecycleService{state: st, imageFactoryHost: imageFactoryHost, logger: logger, sharedMachineState: sharedMachineState}
}

// Install implements machine.LifecycleServiceServer.
func (s *LifecycleService) Install(req *machine.LifecycleServiceInstallRequest, srv machine.LifecycleService_InstallServer) error {
	ctx := srv.Context()

	if !s.sharedMachineState.lifecycleMu.TryLock() {
		return errLifecycleInProgress
	}
	defer s.sharedMachineState.lifecycleMu.Unlock()

	image := req.GetSource().GetImageName()
	if image == "" {
		return status.Error(codes.InvalidArgument, "install image name is required")
	}

	disk := req.GetDestination().GetDisk()
	if disk == "" {
		return status.Error(codes.InvalidArgument, "install destination disk is required")
	}

	if err := validateInstaller(ctx, s.state, image); err != nil {
		return err
	}

	installed, err := machineInstalled(ctx, s.state)
	if err != nil {
		return err
	}

	if installed {
		return status.Error(codes.AlreadyExists, "Talos is already installed on disk")
	}

	if _, err = setImage(ctx, s.state, s.imageFactoryHost, image); err != nil {
		return err
	}

	if err = s.state.Modify(ctx, block.NewSystemDisk(block.NamespaceName, block.SystemDiskID), func(res resource.Resource) error {
		typedRes := res.(*block.SystemDisk) //nolint:forcetypeassert,errcheck
		typedRes.TypedSpec().DiskID = filepath.Base(disk)
		typedRes.TypedSpec().DevPath = disk

		return nil
	}, state.WithUpdateOwner("runtime.MachineStatusController")); err != nil {
		return err
	}

	return streamInstallerProgress(
		func(p *machine.LifecycleServiceInstallProgress) error {
			return srv.Send(&machine.LifecycleServiceInstallResponse{Progress: p})
		},
		fmt.Sprintf("[talemu] installing %s to %s", image, disk),
		"[talemu] writing system partitions",
		"[talemu] installation complete",
	)
}

// Upgrade implements machine.LifecycleServiceServer.
func (s *LifecycleService) Upgrade(req *machine.LifecycleServiceUpgradeRequest, srv machine.LifecycleService_UpgradeServer) error {
	ctx := srv.Context()

	if !s.sharedMachineState.lifecycleMu.TryLock() {
		return errLifecycleInProgress
	}
	defer s.sharedMachineState.lifecycleMu.Unlock()

	image := req.GetSource().GetImageName()
	if image == "" {
		return status.Error(codes.InvalidArgument, "upgrade image name is required")
	}

	if err := validateInstaller(ctx, s.state, image); err != nil {
		return err
	}

	installed, err := machineInstalled(ctx, s.state)
	if err != nil {
		return err
	}

	if !installed {
		return status.Error(codes.FailedPrecondition, "Talos is not installed on disk")
	}

	if _, err = setImage(ctx, s.state, s.imageFactoryHost, image); err != nil {
		return err
	}

	return streamInstallerProgress(
		func(p *machine.LifecycleServiceInstallProgress) error {
			return srv.Send(&machine.LifecycleServiceUpgradeResponse{Progress: p})
		},
		fmt.Sprintf("[talemu] upgrading to %s", image),
		"[talemu] writing new system image",
		"[talemu] upgrade complete",
	)
}

// machineInstalled reports whether Talos is installed on disk, modeled by the presence of the SystemDisk resource.
func machineInstalled(ctx context.Context, st state.State) (bool, error) {
	_, err := safe.ReaderGetByID[*block.SystemDisk](ctx, st, block.SystemDiskID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func streamInstallerProgress(send func(*machine.LifecycleServiceInstallProgress) error, messages ...string) error {
	for _, msg := range messages {
		if err := send(installProgressMessage(msg)); err != nil {
			return err
		}
	}

	return send(installProgressExitCode(0))
}

func installProgressMessage(msg string) *machine.LifecycleServiceInstallProgress {
	return &machine.LifecycleServiceInstallProgress{
		Response: &machine.LifecycleServiceInstallProgress_Message{Message: msg},
	}
}

func installProgressExitCode(code int32) *machine.LifecycleServiceInstallProgress {
	return &machine.LifecycleServiceInstallProgress{
		Response: &machine.LifecycleServiceInstallProgress_ExitCode{ExitCode: code},
	}
}
