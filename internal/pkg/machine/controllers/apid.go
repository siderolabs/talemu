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
	"go.uber.org/zap"

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
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *APIDController) Outputs() []controller.Output {
	return nil
}

// Run implements controller.Controller interface.
//
//nolint:gocognit
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
				continue
			}

			address := siderolink.TypedSpec().Address

			apiCerts, err := safe.ReaderGetByID[*secrets.API](ctx, r, secrets.APIID)
			if err != nil && !state.IsNotFoundError(err) {
				return err
			}

			insecure := (apiCerts == nil)

			config, err := safe.ReaderGetByID[*config.MachineConfig](ctx, r, config.V1Alpha1ID)
			if err != nil && !state.IsNotFoundError(err) {
				return err
			}

			// make APID crash if the machine has config but there's no certs
			if insecure && config != nil {
				if err = ctrl.APID.Stop(); err != nil {
					return err
				}

				continue
			}

			if ctrl.address == address && ctrl.insecure == insecure {
				continue
			}

			if err = ctrl.APID.Run(ctx, address, logger, apiCerts, siderolink.TypedSpec().LinkName); err != nil {
				return err
			}

			ctrl.address = address
			ctrl.insecure = insecure
		}
	}
}
