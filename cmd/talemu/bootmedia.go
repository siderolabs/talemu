// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/bootmedia"
	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
)

// bootMedia is the media the emulated machines pretend to have booted from, resolved once at startup.
//
// Real Talos would need none of this: it runs the media and so knows its own kernel args and extensions.
// The emulator does not run it, so it has to be told, or told where to look.
type bootMedia struct {
	source bootmedia.Source

	// factoryHost is the factory that built this media, empty when none did.
	factoryHost string

	schematicID string
	kernelArgs  string
}

// resolveBootMedia works out which kind of media the flags describe and prepares it.
//
// A schematic identifies media an image factory built. Without one it was built locally, which carries no
// schematic, so the kernel args and the extensions the caller passed describe it instead.
func resolveBootMedia(ctx context.Context, logger *zap.Logger) (*bootMedia, error) {
	if cfg.schematicID != "" {
		return factoryBootMedia(ctx, logger)
	}

	return imagerBootMedia(logger)
}

// imagerBootMedia is media built locally, which carries no schematic, so the kernel args and the extensions
// are whatever the caller passed.
//
// The configured factory is passed along even though nothing is read from it here. An install or an upgrade
// can point a machine at a factory image, and reporting the extensions and kernel args of one means reading
// its schematic.
func imagerBootMedia(logger *zap.Logger) (*bootMedia, error) {
	source, err := bootmedia.NewFactorySource(
		cfg.schematicCacheDir, cfg.imageFactoryBaseURL,
		os.Getenv(emuconst.ImageFactoryUsernameEnv), os.Getenv(emuconst.ImageFactoryPasswordEnv),
		logger.With(zap.String("component", "boot_media")),
	)
	if err != nil {
		return nil, err
	}

	logger.Info("emulating boot media that no image factory built",
		zap.Strings("extensions", cfg.extensions), zap.String("factory", source.BaseURL()))

	// No factory host: whatever the configured factory is for reading later schematics, none of it built this.
	return &bootMedia{
		source:     source,
		kernelArgs: cfg.kernelArgs,
	}, nil
}

// factoryBootMedia is media an image factory built, identified by its schematic.
//
// The factory is asked for the schematic up front: it is the source of truth for the kernel args and the
// extensions, and a missing one is a mistake worth failing on now rather than leaving every machine to
// rediscover it forever. Machines booted from media nobody has would never come up either.
func factoryBootMedia(ctx context.Context, logger *zap.Logger) (*bootMedia, error) {
	source, err := bootmedia.NewFactorySource(
		cfg.schematicCacheDir, cfg.imageFactoryBaseURL,
		os.Getenv(emuconst.ImageFactoryUsernameEnv), os.Getenv(emuconst.ImageFactoryPasswordEnv),
		logger.With(zap.String("component", "boot_media")),
	)
	if err != nil {
		return nil, err
	}

	host, err := source.FactoryHostForVersion(ctx, cfg.talosVersion)
	if err != nil {
		return nil, err
	}

	sch, err := source.GetSchematicByID(ctx, cfg.schematicID, cfg.talosVersion, host)
	if err != nil {
		return nil, fmt.Errorf("schematic %q could not be read from %s: %w", cfg.schematicID, source.BaseURL(), err)
	}

	logger.Info("emulating boot media from the configured image factory",
		zap.String("factory", source.BaseURL()), zap.String("schematic", cfg.schematicID))

	return &bootMedia{
		source:      source,
		factoryHost: host,
		schematicID: cfg.schematicID,
		kernelArgs:  strings.Join(sch.Customization.ExtraKernelArgs, " "),
	}, nil
}
