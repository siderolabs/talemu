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
	nc                   *network.Client
	talosVersion         string
	schematic            string
	bootFactoryURL       string
	extensions           []string
	extraDisks           int
	secureBoot           bool
	nodeProxyingDisabled bool
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

// WithNodeProxyingDisabled disables node-to-node proxying in apid.
// When set, apid rejects single-node forwarding via the "node" header
// and only accepts direct connections from Omni via SideroLink.
func WithNodeProxyingDisabled(value bool) Option {
	return func(o *Options) {
		o.nodeProxyingDisabled = value
	}
}

// WithBootFactoryURL sets the base URL of the image factory the machine's boot media is
// pretended to come from. Empty means the configured image factory.
func WithBootFactoryURL(value string) Option {
	return func(o *Options) {
		o.bootFactoryURL = value
	}
}

// WithExtraDisks gives the machine additional empty disks beyond the system one,
// named vdb, vdc and so on.
//
// A machine with a single disk offers nothing to choose from, so anything that
// selects an install disk cannot be exercised against it.
func WithExtraDisks(count int) Option {
	return func(o *Options) {
		o.extraDisks = count
	}
}

// WithExtensions sets the extensions the machine reports when it has no schematic.
//
// A machine built by an image factory carries a schematic that lists its
// extensions, and that takes precedence. Media built without a factory has no
// schematic, so the extensions have to be stated directly, the same way real
// Talos reads them from local metadata instead of asking a factory.
func WithExtensions(extensions []string) Option {
	return func(o *Options) {
		o.extensions = extensions
	}
}
