// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"strings"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/logging"
)

// LogSinkController configures log sink.
type LogSinkController struct {
	LogSink *logging.ZapCore
}

// Name implements controller.Controller interface.
func (ctrl *LogSinkController) Name() string {
	return "siderolink.LogSinkController"
}

// Inputs implements controller.Controller interface.
func (ctrl *LogSinkController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: network.NamespaceName,
			Type:      network.AddressStatusType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *LogSinkController) Outputs() []controller.Output {
	return nil
}

// Run implements controller.Controller interface.
func (ctrl *LogSinkController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		addresses, err := safe.ReaderListAll[*network.AddressStatus](ctx, r)
		if err != nil {
			return err
		}

		if err = addresses.ForEachErr(func(r *network.AddressStatus) error {
			if strings.HasPrefix(r.TypedSpec().LinkName, constants.SideroLinkName) {
				return ctrl.LogSink.ConfigureInterface(ctx, r)
			}

			return nil
		}); err != nil {
			return err
		}
	}
}
