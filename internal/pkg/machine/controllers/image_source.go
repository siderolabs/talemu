// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"net/url"

	"github.com/siderolabs/talos/pkg/machinery/resources/config"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// EnterpriseChecker detects whether an image factory is an enterprise instance.
type EnterpriseChecker interface {
	IsEnterprise(ctx context.Context, baseURL string) (bool, error)
}

// resolveImageSourceURL returns the base URL of the image factory the machine's current Talos
// image comes from, or an empty string when the image is not a factory one (a community build).
//
// The precedence follows the resource state, not value emptiness: the installed image decides if
// it exists, then the config install image, and only a machine with neither (maintenance mode,
// nothing installed) takes the boot media identity. An image ref without a host, as well as an
// unparseable one, resolves to community.
func resolveImageSourceURL(image *talos.Image, cfg *config.MachineConfig, imageFactoryHost, bootFactoryURL string) string {
	switch {
	case image != nil:
		return factoryURLForHost(image.TypedSpec().Value.Host, bootFactoryURL)
	case cfg != nil:
		installImage := cfg.Container().RawV1Alpha1().Machine().Install().Image()
		if installImage == "" {
			return ""
		}

		parsed, err := talos.ParseImageRef(imageFactoryHost, installImage)
		if err != nil {
			return ""
		}

		return factoryURLForHost(parsed.Host, bootFactoryURL)
	default:
		return bootFactoryURL
	}
}

// factoryURLForHost builds the base URL to probe for the given image host. Image refs carry no
// scheme, so the URL defaults to HTTPS, except when the host is the boot factory itself: then its
// configured base URL is used as-is, keeping a plain-HTTP boot factory working.
func factoryURLForHost(host, bootFactoryURL string) string {
	if host == "" {
		return ""
	}

	if parsed, err := url.Parse(bootFactoryURL); err == nil && parsed.Host == host {
		return bootFactoryURL
	}

	return "https://" + host
}
