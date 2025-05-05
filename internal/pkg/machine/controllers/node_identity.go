// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"crypto/rand"
	"io"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/jxskiss/base62"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
)

// NodeIdentityController generates node identity.
type NodeIdentityController struct{}

// Name implements controller.Controller interface.
func (ctrl *NodeIdentityController) Name() string {
	return "cluster.NodeIdentityController"
}

// Inputs implements controller.Controller interface.
func (ctrl *NodeIdentityController) Inputs() []controller.Input {
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
func (ctrl *NodeIdentityController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: cluster.IdentityType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *NodeIdentityController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		_, err := machineconfig.GetComplete(ctx, r)
		if err != nil {
			if state.IsNotFoundError(err) {
				err = r.Destroy(ctx, cluster.NewIdentity(cluster.NamespaceName, cluster.LocalIdentity).Metadata())
				if err != nil && !state.IsNotFoundError(err) {
					return err
				}

				continue
			}

			return err
		}

		if err = safe.WriterModify(ctx, r, cluster.NewIdentity(cluster.NamespaceName, cluster.LocalIdentity), func(res *cluster.Identity) error {
			if res.TypedSpec().NodeID == "" {
				res.TypedSpec().NodeID, err = generate()

				return err
			}

			return nil
		}); err != nil {
			return err
		}
	}
}

func generate() (string, error) {
	buf := make([]byte, constants.DefaultNodeIdentitySize)

	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}

	return base62.EncodeToString(buf), nil
}
