// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// readCurrentImage returns the reference of the Talos image the machine currently runs, which carries the
// schematic to resolve along with the factory host and the Talos version that locate it.
func readCurrentImage(ctx context.Context, r controller.Runtime, factoryHosts []string) (talos.ImageRef, error) {
	config, err := machineconfig.GetComplete(ctx, r)
	if err != nil && !state.IsNotFoundError(err) {
		return talos.ImageRef{}, err
	}

	image, err := safe.ReaderGetByID[*talos.Image](ctx, r, talos.ImageID)
	if err != nil && !state.IsNotFoundError(err) {
		return talos.ImageRef{}, err
	}

	ref, err := currentImage(image, config, factoryHosts)
	if err != nil {
		return talos.ImageRef{}, fmt.Errorf("failed to parse the current image: %w", err)
	}

	return ref, nil
}
