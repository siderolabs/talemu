// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

func TestResolveImageSourceURL(t *testing.T) {
	t.Parallel()

	const factoryHost = "factory.talos.dev"

	imageWithHost := func(host string) *talos.Image {
		image := talos.NewImage(talos.NamespaceName, talos.ImageID)
		image.TypedSpec().Value.Host = host

		return image
	}

	for _, test := range []struct { //nolint:govet
		name           string
		image          *talos.Image
		bootFactoryURL string
		expected       string
	}{
		{
			name:           "no image, no config: boot factory as-is",
			bootFactoryURL: "http://factory.local:8080",
			expected:       "http://factory.local:8080",
		},
		{
			name:           "image from a foreign factory",
			image:          imageWithHost("factory-enterprise.staging.talos.dev"),
			bootFactoryURL: "https://factory.talos.dev",
			expected:       "https://factory-enterprise.staging.talos.dev",
		},
		{
			name:           "image host matching the boot factory keeps its scheme",
			image:          imageWithHost("factory.local:8080"),
			bootFactoryURL: "http://factory.local:8080",
			expected:       "http://factory.local:8080",
		},
		{
			name:           "image without a host is community",
			image:          imageWithHost(""),
			bootFactoryURL: "https://factory.talos.dev",
			expected:       "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, resolveImageSourceURL(test.image, nil, factoryHost, test.bootFactoryURL))
		})
	}
}
