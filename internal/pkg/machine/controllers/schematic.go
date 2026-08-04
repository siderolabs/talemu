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

// readCurrentSchematic returns the schematic of the machine's current Talos image
// together with the base URL of the factory that holds it.
//
// The two travel together on purpose. A schematic only exists in the factory that
// built it, and installing or upgrading replaces both the schematic and the
// factory it came from, so resolving the current schematic against the factory the
// machine originally booted from would ask the wrong one. The precedence over
// installed image, config install image and boot media is shared with
// resolveImageSourceURL.
func readCurrentSchematic(ctx context.Context, r controller.Runtime, imageFactoryHost, bootFactoryURL string) (schematicID, factoryURL string, err error) {
	schematicContent, err := machineconfig.GetComplete(ctx, r)
	if err != nil && !state.IsNotFoundError(err) {
		return "", "", err
	}

	image, err := safe.ReaderGetByID[*talos.Image](ctx, r, talos.ImageID)
	if err != nil && !state.IsNotFoundError(err) {
		return "", "", err
	}

	if image == nil && schematicContent == nil {
		return "", "", nil
	}

	factoryURL = resolveImageSourceURL(image, schematicContent, imageFactoryHost, bootFactoryURL)

	if image != nil {
		return image.TypedSpec().Value.Schematic, factoryURL, nil
	}

	installImage := schematicContent.Container().RawV1Alpha1().Machine().Install().Image()
	if installImage == "" {
		return "", "", nil
	}

	parsed, err := talos.ParseImageRef(imageFactoryHost, installImage)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse schematic id from the install image: %w", err)
	}

	return parsed.Schematic, factoryURL, nil
}
