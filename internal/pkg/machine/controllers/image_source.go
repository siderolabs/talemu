// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"

	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// BootMediaSource answers what the emulated machine booted from.
type BootMediaSource interface {
	GetSchematicByID(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error)
	FactoryHosts() []string
	IsEnterprise(ctx context.Context, talosVersion, factoryHost string) (bool, error)
}

// currentImage returns the reference of the Talos image the machine runs.
//
// The precedence follows the resource state, not value emptiness: the installed image decides if it exists,
// then the config install image, and a machine with neither (maintenance mode, nothing installed) has no
// image at all. The boot media needs no case of its own, since it is seeded as the installed image.
func currentImage(image *talos.Image, cfg *config.MachineConfig, factoryHosts []string) (talos.ImageRef, error) {
	switch {
	case image != nil:
		return talos.ImageRef{
			Host:      image.TypedSpec().Value.Host,
			Schematic: image.TypedSpec().Value.Schematic,
			Version:   image.TypedSpec().Value.Version,
		}, nil
	case cfg != nil:
		installImage := cfg.Container().RawV1Alpha1().Machine().Install().Image()
		if installImage == "" {
			return talos.ImageRef{}, nil
		}

		return talos.ParseImageRef(factoryHosts, installImage)
	default:
		return talos.ImageRef{}, nil
	}
}

// currentImageOrNone wraps currentImage for the controllers that have to keep reconciling: a reference it
// cannot parse is reported as no image at all, which is what a machine running a non-factory build looks like.
func currentImageOrNone(image *talos.Image, cfg *config.MachineConfig, factoryHosts []string, logger *zap.Logger) talos.ImageRef {
	ref, err := currentImage(image, cfg, factoryHosts)
	if err != nil {
		logger.Warn("failed to parse the current image, reporting the machine as running no factory image", zap.Error(err))

		return talos.ImageRef{}
	}

	return ref
}
