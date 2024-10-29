// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machine

import (
	"strings"

	"github.com/siderolabs/talemu/internal/pkg/machine/network"
)

// Options is the extra machine options.
type Options struct {
	nc           *network.Client
	talosVersion string
	schematic    string
	secureBoot   bool
}

// Option represents a single extra machine option.
type Option func(*Options)

// WithTalosVersion creates the machine with the initial talos version.
func WithTalosVersion(version string) Option {
	return func(o *Options) {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}

		o.talosVersion = version
	}
}

// WithSchematic creates the machine with the initial schematic.
func WithSchematic(schematic string) Option {
	return func(o *Options) {
		o.schematic = schematic
	}
}

// WithNetworkClient explicitly sets the network client to use in the machine controllers.
func WithNetworkClient(nc *network.Client) Option {
	return func(o *Options) {
		o.nc = nc
	}
}

// WithSecureBoot simulates secure boot mode for the machine.
func WithSecureBoot(value bool) Option {
	return func(o *Options) {
		o.secureBoot = value
	}
}
