// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/omni/client/pkg/panichandler"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
)

// KubernetesController interacts with SideroLink API and brings up the SideroLink Wireguard interface.
type KubernetesController struct {
	Kubernetes *kubefactory.Kubernetes
	MachineID  string
	address    string
}

// Name implements controller.Controller interface.
func (ctrl *KubernetesController) Name() string {
	return "services.KubernetesController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KubernetesController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: network.NamespaceName,
			Type:      network.AddressStatusType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.V1Alpha1ID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineTypeType,
			ID:        optional.Some(config.MachineTypeID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: k8s.ControlPlaneNamespaceName,
			ID:        optional.Some(k8s.StaticPodSecretsStaticPodID),
			Type:      k8s.SecretsStatusType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *KubernetesController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: v1alpha1.ServiceType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocognit,gocyclo,cyclop
func (ctrl *KubernetesController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	serverCtx, cancelServerCtx := context.WithCancel(ctx)
	defer cancelServerCtx()

	var stopCh chan struct{}

	stopServer := func() {
		if stopCh == nil {
			return
		}

		logger.Info("stopping kubernetes api server")

		cancelServerCtx()

		<-stopCh
		stopCh = nil

		logger.Info("kubernetes api server stopped")

		ctrl.address = ""
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
			addresses, err := safe.ReaderListAll[*network.AddressStatus](ctx, r)
			if err != nil {
				return err
			}

			machineType, err := safe.ReaderGetByID[*config.MachineType](ctx, r, config.MachineTypeID)
			if err != nil && !state.IsNotFoundError(err) {
				return err
			}

			config, err := machineconfig.GetComplete(ctx, r)
			if err != nil && !state.IsNotFoundError(err) {
				return err
			}

			var (
				address string
				iface   string
			)

			siderolink, found := addresses.Find(func(address *network.AddressStatus) bool {
				return strings.HasPrefix(address.TypedSpec().LinkName, constants.SideroLinkName)
			})
			if found {
				address = siderolink.TypedSpec().Address.Addr().String()
				iface = siderolink.TypedSpec().LinkName
			}

			certs, err := safe.ReaderGetByID[*k8s.SecretsStatus](ctx, r, k8s.StaticPodSecretsStaticPodID)
			if err != nil && !state.IsNotFoundError(err) {
				return err
			}

			err = func() error {
				var started bool

				defer func() {
					service := v1alpha1.NewService(emuconst.KubeletService)

					if !started {
						e := r.Destroy(ctx, service.Metadata())
						if e != nil && !state.IsNotFoundError(e) {
							err = e
						}

						return
					}

					err = safe.WriterModify(ctx, r, service, func(res *v1alpha1.Service) error {
						res.TypedSpec().Healthy = true
						res.TypedSpec().Running = true

						return nil
					})
				}()

				if certs == nil || address == "" || config == nil || machineType == nil || !machineType.MachineType().IsControlPlane() {
					stopServer()

					return nil
				}

				started = true

				if ctrl.address == address {
					return nil
				}

				stopServer()

				serverCtx, cancelServerCtx = context.WithCancel(ctx) //nolint:fatcontext

				logger.Info("starting kubernetes api server", zap.String("address", address))

				stopCh = make(chan struct{}, 1)

				panichandler.Go(func() {
					for {
						defer func() {
							select {
							case stopCh <- struct{}{}:
							default:
							}
						}()

						if err = ctrl.Kubernetes.RunAPIService(serverCtx, address, iface, ctrl.MachineID, config.Provider().Cluster().ID()); err != nil {
							logger.Error("kubernetes api server crashed", zap.Error(err))
						}

						time.Sleep(time.Second)

						select {
						case <-serverCtx.Done():
							return
						case <-ctx.Done():
							return
						default:
						}
					}
				}, logger)

				ctrl.address = address

				return nil
			}()
			if err != nil {
				return err
			}
		}
	}
}
