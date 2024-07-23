// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/controller/generic/qtransform"
	"github.com/siderolabs/gen/xerrors"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// RebootStatusController simulates node reboots by creating and removing reboot status resource.
type RebootStatusController = qtransform.QController[*talos.Reboot, *talos.RebootStatus]

// NewRebootStatusController creates new controller.
func NewRebootStatusController() *RebootStatusController {
	return qtransform.NewQController(
		qtransform.Settings[*talos.Reboot, *talos.RebootStatus]{
			Name: "talos.RebootStatus",
			MapMetadataFunc: func(r *talos.Reboot) *talos.RebootStatus {
				return talos.NewRebootStatus(r.Metadata().Namespace(), r.Metadata().ID())
			},
			UnmapMetadataFunc: func(r *talos.RebootStatus) *talos.Reboot {
				return talos.NewReboot(r.Metadata().Namespace(), r.Metadata().ID())
			},
			TransformFunc: func(_ context.Context, _ controller.Reader, _ *zap.Logger, reboot *talos.Reboot, _ *talos.RebootStatus) error {
				rebootEndTime := reboot.Metadata().Updated().Add(reboot.TypedSpec().Value.Downtime.AsDuration())
				if time.Now().Before(rebootEndTime) {
					return controller.NewRequeueInterval(time.Until(rebootEndTime))
				}

				return xerrors.NewTaggedf[qtransform.DestroyOutputTag]("reboot done")
			},
		},
	)
}
