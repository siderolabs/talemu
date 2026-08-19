// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnterpriseProbeNormalizesBaseURL asserts that a base URL spelled with a trailing slash is probed at the
// same path as one without. The two spellings reach the emulator from different places, a command line and
// the provider data of a machine, so both have to work.
func TestEnterpriseProbeNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		probed string
	)

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		probed = r.URL.Path
		mu.Unlock()

		w.Header().Set("Server", "Image Factory Enterprise v1.4.0")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(factory.Close)

	probe := newEnterpriseProbe()

	isEnterprise, err := probe.isEnterprise(t.Context(), factory.URL+"/")
	require.NoError(t, err)
	assert.True(t, isEnterprise)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, "/versions", probed, "a trailing slash must not double up in the probed path")
}

// TestEnterpriseProbeCaches asserts a factory is asked once: it cannot change its identity, and every machine
// asks about the same handful of factories.
func TestEnterpriseProbeCaches(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		hits int
	)

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()

		w.Header().Set("Server", "Image Factory v1.4.0")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(factory.Close)

	probe := newEnterpriseProbe()

	for range 3 {
		isEnterprise, err := probe.isEnterprise(t.Context(), factory.URL)
		require.NoError(t, err)
		assert.False(t, isEnterprise)
	}

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, 1, hits)

	// an empty base URL means the image came from no factory, so there is nothing to ask
	isEnterprise, err := probe.isEnterprise(t.Context(), "")
	require.NoError(t, err)
	assert.False(t, isEnterprise)
}

// TestEnterpriseProbeIgnoresAHostThatSaysNothing asserts that a host serving no factory reports community. An
// image reference can carry a plain registry host, and https://ghcr.io/versions answers 404 with no Server
// header at all.
func TestEnterpriseProbeIgnoresAHostThatSaysNothing(t *testing.T) {
	t.Parallel()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(registry.Close)

	isEnterprise, err := newEnterpriseProbe().isEnterprise(t.Context(), registry.URL)
	require.NoError(t, err)
	assert.False(t, isEnterprise)
}
