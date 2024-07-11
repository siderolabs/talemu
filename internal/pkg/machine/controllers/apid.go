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
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
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
			addresses, err := safe.ReaderListAll[*network.AddressStatus](ctx, r)
			if err != nil {
				return err
			}

			siderolink, found := addresses.Find(func(address *network.AddressStatus) bool {
				return strings.HasPrefix(address.TypedSpec().LinkName, constants.SideroLinkName)
			})
			if !found {
				logger.Info("apid is waiting for siderolink interface to be up")

				continue
			}

			address := siderolink.TypedSpec().Address

			apiCerts, err := safe.ReaderGetByID[*secrets.API](ctx, r, secrets.APIID)
			if err != nil && !state.IsNotFoundError(err) {
				return err
			}

			insecure := (apiCerts == nil)

			if ctrl.address == address && ctrl.insecure == insecure {
				continue
			}

			service := v1alpha1.NewService(emuconst.APIDService)

			err = safe.WriterModify(ctx, r, service, func(res *v1alpha1.Service) error {
				res.TypedSpec().Healthy = false
				res.TypedSpec().Running = false

				return nil
			})
			if err != nil {
				return err
			}

			if err = ctrl.APID.Run(ctx, address, logger, apiCerts, siderolink.TypedSpec().LinkName); err != nil {
				return err
			}

			err = safe.WriterModify(ctx, r, service, func(res *v1alpha1.Service) error {
				res.TypedSpec().Healthy = true
				res.TypedSpec().Running = true

				return nil
			})
			if err != nil {
				return err
			}

			ctrl.address = address
			ctrl.insecure = insecure
		}
	}
}
