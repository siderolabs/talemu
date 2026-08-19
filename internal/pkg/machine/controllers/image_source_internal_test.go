// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/container"
	configv1alpha1 "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

func TestCurrentImage(t *testing.T) {
	t.Parallel()

	// Both of the factories Omni may be configured with, so that a reference from either keeps its
	// schematic ID.
	factoryHosts := []string{"factory.talos.dev", "factory-enterprise.staging.talos.dev"}

	image := func(host, schematic, version string) *talos.Image {
		res := talos.NewImage(talos.NamespaceName, talos.ImageID)
		res.TypedSpec().Value.Host = host
		res.TypedSpec().Value.Schematic = schematic
		res.TypedSpec().Value.Version = version

		return res
	}

	machineConfig := func(t *testing.T, installImage string) *config.MachineConfig {
		t.Helper()

		provider, err := container.New(&configv1alpha1.Config{
			ConfigVersion: "v1alpha1",
			MachineConfig: &configv1alpha1.MachineConfig{
				MachineInstall: &configv1alpha1.InstallConfig{InstallImage: installImage},
			},
			ClusterConfig: &configv1alpha1.ClusterConfig{},
		})
		require.NoError(t, err)

		return config.NewMachineConfigWithID(provider, config.ActiveID)
	}

	for _, test := range []struct { //nolint:govet
		name         string
		image        *talos.Image
		installImage string
		hasConfig    bool
		expected     talos.ImageRef
	}{
		{
			name:     "no image and no config",
			expected: talos.ImageRef{},
		},
		{
			name:     "the installed image is taken as it is recorded",
			image:    image("factory.talos.dev", "abcd1234", "v1.14.0"),
			expected: talos.ImageRef{Host: "factory.talos.dev", Schematic: "abcd1234", Version: "v1.14.0"},
		},
		{
			name:         "the installed image wins over the config install image",
			image:        image("factory.talos.dev", "abcd1234", "v1.14.0"),
			hasConfig:    true,
			installImage: "ghcr.io/siderolabs/installer:v1.13.6",
			expected:     talos.ImageRef{Host: "factory.talos.dev", Schematic: "abcd1234", Version: "v1.14.0"},
		},
		{
			name:         "the config install image decides when nothing is installed",
			hasConfig:    true,
			installImage: "factory.talos.dev/metal-installer/abcd1234:v1.14.0",
			expected:     talos.ImageRef{Host: "factory.talos.dev", Schematic: "abcd1234", Version: "v1.14.0"},
		},
		{
			name:         "an install image from the secondary factory keeps its schematic",
			hasConfig:    true,
			installImage: "factory-enterprise.staging.talos.dev/metal-installer/abcd1234:v1.14.0",
			expected:     talos.ImageRef{Host: "factory-enterprise.staging.talos.dev", Schematic: "abcd1234", Version: "v1.14.0"},
		},
		{
			name:         "an install image from no factory carries no schematic",
			hasConfig:    true,
			installImage: "ghcr.io/siderolabs/installer:v1.13.6",
			expected:     talos.ImageRef{Host: "ghcr.io", Version: "v1.13.6"},
		},
		{
			name:         "an empty install image",
			hasConfig:    true,
			installImage: "",
			expected:     talos.ImageRef{},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var cfg *config.MachineConfig

			if test.hasConfig {
				cfg = machineConfig(t, test.installImage)
			}

			ref, err := currentImage(test.image, cfg, factoryHosts)
			require.NoError(t, err)
			assert.Equal(t, test.expected, ref)
		})
	}
}
