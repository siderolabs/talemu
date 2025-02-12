// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/controller/generic/qtransform"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/gen/xerrors"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"
)

// UniqueMachineTokenController simulates node reboots by creating and removing reboot status resource.
type UniqueMachineTokenController = qtransform.QController[*runtime.MetaKey, *runtime.UniqueMachineToken]

// NewUniqueMachineTokenController creates new controller.
func NewUniqueMachineTokenController() *UniqueMachineTokenController {
	return qtransform.NewQController(
		qtransform.Settings[*runtime.MetaKey, *runtime.UniqueMachineToken]{
			Name: "runtime.UniqueMachineToken",
			MapMetadataOptionalFunc: func(r *runtime.MetaKey) optional.Optional[*runtime.UniqueMachineToken] {
				if r.Metadata().ID() != runtime.MetaKeyTagToID(16) {
					return optional.None[*runtime.UniqueMachineToken]()
				}

				return optional.Some(runtime.NewUniqueMachineToken())
			},
			UnmapMetadataFunc: func(*runtime.UniqueMachineToken) *runtime.MetaKey {
				return runtime.NewMetaKey(runtime.NamespaceName, runtime.MetaKeyTagToID(16))
			},
			TransformFunc: func(_ context.Context, _ controller.Reader, _ *zap.Logger, metaKey *runtime.MetaKey, output *runtime.UniqueMachineToken) error {
				if metaKey.Metadata().ID() != runtime.MetaKeyTagToID(16) {
					return xerrors.NewTaggedf[qtransform.DestroyOutputTag]("not the unique token key")
				}

				output.TypedSpec().Token = metaKey.TypedSpec().Value

				return nil
			},
		},
	)
}
