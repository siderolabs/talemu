// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"net/netip"
	"strings"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
	"github.com/siderolabs/talemu/internal/pkg/machine/services"
)

// APIDController interacts with SideroLink API and brings up the SideroLink Wireguard interface.
type APIDController struct {
	APID     *services.APID
	address  netip.Prefix
	insecure bool
}

// Name implements controller.Controller interface.
func (ctrl *APIDController) Name() string {
	return "APIDController"
}

// Inputs implements controller.Controller interface.
func (ctrl *APIDController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: network.NamespaceName,
			Type:      network.AddressStatusType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: secrets.NamespaceName,
			ID:        optional.Some(secrets.APIID),
			Type:      secrets.APIType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: config.NamespaceName,
			ID:        optional.Some(config.V1Alpha1ID),
			Type:      config.MachineConfigType,
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
func (ctrl *APIDController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: v1alpha1.ServiceType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *APIDController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
			if err := ctrl.reconcile(ctx, r, logger); err != nil {
				return err
			}
		}
	}
}

func (ctrl *APIDController) reconcile(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	var err error

	service := v1alpha1.NewService(emuconst.APIDService)

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

	reboot, err := safe.ReaderGetByID[*talos.RebootStatus](ctx, r, talos.RebootID)
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	if reboot != nil {
		logger.Info("the machine is rebooting")

		ctrl.address = netip.Prefix{}

		return ctrl.APID.Stop()
	}

	addresses, err := safe.ReaderListAll[*network.AddressStatus](ctx, r)
	if err != nil {
		return err
	}

	siderolink, found := addresses.Find(func(address *network.AddressStatus) bool {
		return strings.HasPrefix(address.TypedSpec().LinkName, constants.SideroLinkName)
	})
	if !found {
		logger.Info("apid is waiting for siderolink interface to be up")

		return nil
	}

	address := siderolink.TypedSpec().Address

	apiCerts, err := safe.ReaderGetByID[*secrets.API](ctx, r, secrets.APIID)
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	config, err := machineconfig.GetComplete(ctx, r)
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	insecure := apiCerts == nil

	running = true
	healthy = true

	if insecure && config != nil {
		logger.Info("the machine is configured but the certs are not ready yet")

		ctrl.address = netip.Prefix{}

		return ctrl.APID.Stop()
	}

	if ctrl.address == address && ctrl.insecure == insecure {
		return nil
	}

	if err = ctrl.APID.Run(ctx, address, logger, apiCerts, siderolink.TypedSpec().LinkName); err != nil {
		return err
	}

	ctrl.address = address
	ctrl.insecure = insecure

	return nil
}
