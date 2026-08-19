// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"context"
	"fmt"

	"github.com/siderolabs/omni/client/pkg/imagefactory"
)

// factoryLocation is where one image factory is: the host an image reference from it carries, and the base URL
// a request to it goes to.
type factoryLocation struct {
	host    string
	baseURL string
}

// factoryDirectory answers where the image factories Omni is configured with are, and nothing else.
//
// It deliberately hands out locations rather than clients. imagefactory.Clients holds clients that can call a
// factory, but they carry only the credentials readable from Omni's state, which for the role the emulator runs
// as is none at all: a request through one would reach an enterprise factory unauthenticated and be refused.
// Keeping the clients unreachable from here is what stops that distinction from being lost later.
type factoryDirectory struct {
	clients *imagefactory.Clients
}

// hosts lists the factory hosts Omni is configured with, the primary first.
func (d factoryDirectory) hosts() []string {
	hosts := []string{d.primaryHost()}

	if secondary, ok := d.clients.Secondary(); ok {
		hosts = append(hosts, secondary.Host())
	}

	return hosts
}

// primaryHost is the factory Omni serves a Talos version from unless that version says otherwise.
func (d factoryDirectory) primaryHost() string {
	return d.clients.Primary().Host()
}

// probeURLFor builds the URL to reach the factory at the given host, empty when the image came from no factory.
//
// A factory Omni is configured with is reached by the base URL Omni gave, so a plain-HTTP one keeps its scheme.
// Any other host is only known by name, and an image reference carries no scheme, so HTTPS is assumed.
func (d factoryDirectory) probeURLFor(factoryHost string) string {
	if factoryHost == "" {
		return ""
	}

	if configured := d.clients.ForHost(factoryHost); configured != nil {
		return configured.URL()
	}

	return "https://" + factoryHost
}

// forTalosVersion locates the factory Omni serves the given Talos version from.
func (d factoryDirectory) forTalosVersion(ctx context.Context, talosVersion string) (factoryLocation, error) {
	factory, err := d.clients.ForTalosVersion(ctx, talosVersion)
	if err != nil {
		return factoryLocation{}, fmt.Errorf("failed to resolve the image factory of Talos version %q: %w", talosVersion, err)
	}

	return factoryLocation{host: factory.Host(), baseURL: factory.URL()}, nil
}
