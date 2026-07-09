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
	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
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
			ID:        optional.Some(config.ActiveID),
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

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
			// The machine's own config can arrive after the cluster has already bootstrapped (e.g. a control
			// plane joining an existing cluster). Reconcile here so etcd still comes up in that ordering, not
			// only when the global cluster status changes.
			if err := ctrl.reconcile(ctx, r, logger); err != nil {
				return err
			}
		case event := <-watchEvents:
			switch event.Type {
			case state.Errored:
				return event.Error
			case state.Bootstrapped, state.Noop:
				continue
			case state.Destroyed, state.Created, state.Updated:
			}

			if err := ctrl.reconcile(ctx, r, logger); err != nil {
				return err
			}
		}
	}
}

// reconcile ensures the etcd service and member are present for a control-plane machine.
func (ctrl *EtcdController) reconcile(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	config, err := machineconfig.GetComplete(ctx, r)
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	if config == nil || !config.Provider().Machine().Type().IsControlPlane() {
		return nil
	}

	return ctrl.reconcileRunning(ctx, r, config, logger)
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
		if state.IsNotFoundError(err) {
			// Config landed before the cluster status was published. Leave etcd not-ready and wait: the
			// cluster status watch will trigger another reconcile once it appears.
			return nil
		}

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

		memberID, genErr := genMemberID()
		if genErr != nil {
			return genErr
		}

		res.TypedSpec().MemberID = memberID

		return nil
	})
	if err != nil {
		return err
	}

	// EtcdMemberList/EtcdStatus read the member from the shared global status and filter it by the cluster
	// and control-plane labels. Those labels are otherwise written once by ApplyConfiguration, separately
	// from the member, so a machine could end up with a member but no labels and drop out of the member
	// list. Write both together here, every reconcile, so a control plane that has a member is always
	// counted for its cluster.
	if err = ctrl.syncGlobalMember(ctx, config.Provider().Cluster().ID(), member.TypedSpec().MemberID); err != nil {
		return err
	}

	if slices.Contains(clusterStatus.TypedSpec().Value.DenyEtcdMembers, member.TypedSpec().MemberID) {
		return nil
	}

	healthy = true

	return nil
}

func genMemberID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return etcd.FormatMemberID(binary.LittleEndian.Uint64(buf)), nil
}

func (ctrl *EtcdController) syncGlobalMember(ctx context.Context, clusterID, memberID string) error {
	_, err := safe.StateUpdateWithConflicts(ctx, ctrl.GlobalState, emu.NewMachineStatus(emu.NamespaceName, ctrl.MachineID).Metadata(), func(res *emu.MachineStatus) error {
		res.TypedSpec().Value.EtcdMemberId = memberID

		res.Metadata().Labels().Set(emu.LabelCluster, clusterID)
		res.Metadata().Labels().Set(emu.LabelControlPlaneRole, "")

		return nil
	})

	return err
}
