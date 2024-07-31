// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machine

// Options is the extra machine options.
type Options struct {
	talosVersion string
	schematic    string
}

// Option represents a single extra machine option.
type Option func(*Options)

// WithTalosVersion creates the machine with the initial talos version.
func WithTalosVersion(version string) Option {
	return func(o *Options) {
		o.talosVersion = version
	}
}

// WithSchematic creates the machine with the initial schematic.
func WithSchematic(schematic string) Option {
	return func(o *Options) {
		o.schematic = schematic
	}
}
