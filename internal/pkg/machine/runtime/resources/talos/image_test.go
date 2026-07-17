// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talos_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

func TestParseImageRef(t *testing.T) {
	t.Parallel()

	const factoryHost = "factory.talos.dev"

	for _, test := range []struct { //nolint:govet
		name     string
		ref      string
		expected talos.ImageRef
		wantErr  bool
	}{
		{
			name: "factory installer",
			ref:  "factory.talos.dev/metal-installer/abcd1234:v1.13.6",
			expected: talos.ImageRef{
				Host:      "factory.talos.dev",
				Schematic: "abcd1234",
				Version:   "v1.13.6",
			},
		},
		{
			name: "factory installer with digest",
			ref:  "factory.talos.dev/metal-installer/abcd1234:v1.13.6@sha256:deadbeef",
			expected: talos.ImageRef{
				Host:      "factory.talos.dev",
				Schematic: "abcd1234",
				Version:   "v1.13.6",
			},
		},
		{
			name: "foreign factory keeps host, drops schematic",
			ref:  "factory-enterprise.staging.talos.dev/metal-installer/abcd1234:v1.13.6",
			expected: talos.ImageRef{
				Host:    "factory-enterprise.staging.talos.dev",
				Version: "v1.13.6",
			},
		},
		{
			name: "plain registry image",
			ref:  "ghcr.io/siderolabs/installer:v1.13.6",
			expected: talos.ImageRef{
				Host:    "ghcr.io",
				Version: "v1.13.6",
			},
		},
		{
			name: "registry with port",
			ref:  "localhost:5000/siderolabs/installer:v1.13.6",
			expected: talos.ImageRef{
				Host:    "localhost:5000",
				Version: "v1.13.6",
			},
		},
		{
			name: "single component ref has no host",
			ref:  "installer:v1.13.6",
			expected: talos.ImageRef{
				Version: "v1.13.6",
			},
		},
		{
			name:    "no tag",
			ref:     "ghcr.io/siderolabs/installer",
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := talos.ParseImageRef(factoryHost, test.ref)
			if test.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.expected, parsed)
		})
	}
}
