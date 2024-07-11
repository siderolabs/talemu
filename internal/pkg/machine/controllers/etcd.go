// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"slices"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/etcd"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// EtcdController generates node identity.
type EtcdController struct {
	GlobalState state.State
	MachineID   string
}

// Name implements controller.Controller interface.
func (ctrl *EtcdController) Name() string {
	return "runtime.EtcdController"
}

// Inputs implements controller.Controller interface.
func (ctrl *EtcdController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.V1Alpha1ID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *EtcdController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: v1alpha1.ServiceType,
			Kind: controller.OutputShared,
		},
		{
			Type: etcd.MemberType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *EtcdController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	watchEvents := make(chan state.Event)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := ctrl.GlobalState.WatchKind(ctx, emu.NewClusterStatus(emu.NamespaceName, "").Metadata(), watchEvents); err != nil {
		return err
	}

outer:
	for {
		var clusterID string

		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		case event := <-watchEvents:
			switch event.Type {
			case state.Errored:
				return event.Error
			case state.Bootstrapped:
				continue outer
			case state.Destroyed, state.Created, state.Updated:
				clusterID = event.Resource.Metadata().ID()
			}

			config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, r, config.V1Alpha1ID)
			if err != nil && !state.IsNotFoundError(err) {
				return err
			}

			if clusterID != "" && config != nil && clusterID != config.Provider().Cluster().ID() {
				continue
			}

			if config == nil || !config.Provider().Machine().Type().IsControlPlane() {
				if err = ctrl.reconcileTeardown(ctx, r, logger); err != nil {
					return err
				}

				continue
			}

			if err = ctrl.reconcileRunning(ctx, r, config, logger); err != nil {
				return err
			}
		}
	}
}

func (ctrl *EtcdController) reconcileTeardown(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	if err := ctrl.resetETCDMember(ctx, logger); err != nil {
		return err
	}

	err := r.Destroy(ctx, etcd.NewMember(etcd.NamespaceName, etcd.LocalMemberID).Metadata())
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	err = r.Destroy(ctx, v1alpha1.NewService(constants.ETCDService).Metadata())
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	return nil
}

func (ctrl *EtcdController) reconcileRunning(ctx context.Context, r controller.Runtime, config *config.MachineConfig, logger *zap.Logger) error {
	service := v1alpha1.NewService(constants.ETCDService)

	var err error

	var (
		healthy bool
		running bool
	)

	defer func() {
		err = safe.WriterModify(ctx, r, service, func(res *v1alpha1.Service) error {
			res.TypedSpec().Healthy = healthy
			res.TypedSpec().Running = running

			return nil
		})
	}()

	clusterStatus, err := safe.ReaderGetByID[*emu.ClusterStatus](ctx, ctrl.GlobalState, config.Provider().Cluster().ID())
	if err != nil {
		return err
	}

	if !clusterStatus.TypedSpec().Value.Bootstrapped {
		logger.Info("waiting for etcd bootstrap")

		return nil
	}

	running = true

	member, err := safe.WriterModifyWithResult(ctx, r, etcd.NewMember(etcd.NamespaceName, etcd.LocalMemberID), func(res *etcd.Member) error {
		if res.TypedSpec().MemberID != "" {
			return nil
		}

		logger.Info("generated etcd member")

		if res.TypedSpec().MemberID, err = ctrl.genETCDMember(ctx); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	if slices.Contains(clusterStatus.TypedSpec().Value.DenyEtcdMembers, member.TypedSpec().MemberID) {
		return nil
	}

	healthy = true

	return nil
}

func (ctrl *EtcdController) genETCDMember(ctx context.Context) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	member := etcd.FormatMemberID(binary.LittleEndian.Uint64(buf))

	if _, err := safe.StateUpdateWithConflicts(ctx, ctrl.GlobalState, emu.NewMachineStatus(emu.NamespaceName, ctrl.MachineID).Metadata(), func(res *emu.MachineStatus) error {
		res.TypedSpec().Value.EtcdMemberId = member

		return nil
	}); err != nil {
		return "", err
	}

	return member, nil
}

func (ctrl *EtcdController) resetETCDMember(ctx context.Context, logger *zap.Logger) error {
	if _, err := safe.StateUpdateWithConflicts(ctx, ctrl.GlobalState, emu.NewMachineStatus(emu.NamespaceName, ctrl.MachineID).Metadata(), func(res *emu.MachineStatus) error {
		if res.TypedSpec().Value.EtcdMemberId != "" {
			logger.Info("reset etcd member")
		}

		res.TypedSpec().Value.EtcdMemberId = ""

		return nil
	}); err != nil {
		return err
	}

	return nil
}
