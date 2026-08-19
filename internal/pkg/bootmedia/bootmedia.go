// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package bootmedia answers what the emulated machines booted from.
//
// Real Talos never asks an image factory anything: it runs the media, so it already knows its own kernel
// args, its extensions and whether it is an enterprise build. The emulator runs no image, so it has to
// recover those facts from whoever produced the media.
package bootmedia

import (
	"context"

	"github.com/siderolabs/image-factory/pkg/schematic"
)

// Source is the origin of the boot media the emulated machines pretend to have booted from. Everything the
// emulator would otherwise have to ask an image factory is asked here instead.
type Source interface {
	// GetSchematicByID recovers the content behind a schematic ID.
	GetSchematicByID(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error)

	// FactoryHosts lists the image factory hosts whose image references carry a schematic ID. Media that no
	// factory built still lists them, since an install or an upgrade can point the machine at a factory image
	// whose schematic has to be read.
	FactoryHosts() []string

	// IsEnterprise reports whether the image identified by the version and the factory host is an
	// enterprise build. An empty host means the image came from no factory, which is never enterprise.
	IsEnterprise(ctx context.Context, talosVersion, factoryHost string) (bool, error)
}

var (
	_ Source = (*FactorySource)(nil)
	_ Source = (*OmniSource)(nil)
)
