// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"testing"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetImage verifies that every part of the parsed image ref is recorded, and that a
// cross-factory switch at the same version and schematic shape counts as a change, so it
// triggers the emulated reboot.
func TestSetImage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	const factoryHost = "factory.talos.dev"

	changed, err := setImage(ctx, st, factoryHost, "factory.talos.dev/metal-installer/abcd1234:v1.13.6")
	require.NoError(t, err)
	assert.True(t, changed)

	// same ref again: no change
	changed, err = setImage(ctx, st, factoryHost, "factory.talos.dev/metal-installer/abcd1234:v1.13.6")
	require.NoError(t, err)
	assert.False(t, changed)

	// same version, different factory: must count as a change
	changed, err = setImage(ctx, st, factoryHost, "factory-enterprise.staging.talos.dev/metal-installer/abcd1234:v1.13.6")
	require.NoError(t, err)
	assert.True(t, changed)
}
