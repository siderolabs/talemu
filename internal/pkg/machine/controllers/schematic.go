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

func readCurrentSchematicID(ctx context.Context, r controller.Runtime, imageFactoryHost string) (string, error) {
	schematicContent, err := machineconfig.GetComplete(ctx, r)
	if err != nil && !state.IsNotFoundError(err) {
		return "", err
	}

	image, err := safe.ReaderGetByID[*talos.Image](ctx, r, talos.ImageID)
	if err != nil && !state.IsNotFoundError(err) {
		return "", err
	}

	if image == nil && schematicContent == nil {
		return "", nil
	}

	if image != nil {
		return image.TypedSpec().Value.Schematic, nil
	}

	installImage := schematicContent.Container().RawV1Alpha1().Machine().Install().Image()
	if installImage == "" {
		return "", nil
	}

	parsed, err := talos.ParseImageRef(imageFactoryHost, installImage)
	if err != nil {
		return "", fmt.Errorf("failed to parse schematic id from the install image: %w", err)
	}

	return parsed.Schematic, nil
}
