// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewFactorySourceRejectsRelativeURL(t *testing.T) {
	t.Parallel()

	source, err := NewFactorySource(t.TempDir(), "http://factory.local:8080/base", "user", "password", zaptest.NewLogger(t))
	require.NoError(t, err)
	assert.Equal(t, "http://factory.local:8080/base", source.BaseURL())
	assert.Equal(t, []string{"factory.local:8080"}, source.FactoryHosts())

	_, err = NewFactorySource(t.TempDir(), "factory.local", "", "", zaptest.NewLogger(t))
	require.Error(t, err, "a URL without a scheme would only fail later, at the request")
}

// TestNewFactorySourceRejectsHalfCredentials asserts that half a credential pair is refused rather than sent as
// no credentials at all, which would surface as an unexplained 401 from the factory.
func TestNewFactorySourceRejectsHalfCredentials(t *testing.T) {
	t.Parallel()

	_, err := NewFactorySource(t.TempDir(), "http://factory.local:8080", "user", "", zaptest.NewLogger(t))
	require.Error(t, err)

	_, err = NewFactorySource(t.TempDir(), "http://factory.local:8080", "", "password", zaptest.NewLogger(t))
	require.Error(t, err)
}

// TestFactoryEnterprise asserts how the emulator decides that boot media is an enterprise build without an
// Omni to ask: the factory says so through the Server response header on every response, including the ones it
// refuses, which is also how Omni detects it.
//
// Both halves are real behavior, checked against factory-enterprise.staging.talos.dev: /versions answers 200
// with "Image Factory Enterprise" to an unauthenticated request, and an auth-required route answers 401 while
// still sending that header.
func TestFactoryEnterprise(t *testing.T) {
	t.Parallel()

	var enterpriseHits, communityHits, refusedHits atomic.Int64

	enterprise := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		enterpriseHits.Add(1)

		w.Header().Set("Server", "Image Factory Enterprise v1.4.0")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(enterprise.Close)

	community := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		communityHits.Add(1)

		w.Header().Set("Server", "Image Factory v1.4.0")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(community.Close)

	// an enterprise factory refusing an unauthenticated probe still identifies itself
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refusedHits.Add(1)

		w.Header().Set("Server", "Image Factory Enterprise v1.4.0")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(refused.Close)

	hostOf := func(t *testing.T, rawURL string) string {
		t.Helper()

		parsed, err := url.Parse(rawURL)
		require.NoError(t, err)

		return parsed.Host
	}

	// The configured factory is reached by its base URL, so a plain-HTTP one keeps working. Any other host is
	// only known by name, so it is assumed to be HTTPS and is not reachable in this test.
	source, err := NewFactorySource(t.TempDir(), enterprise.URL, "", "", zaptest.NewLogger(t))
	require.NoError(t, err)

	// no factory host means the image came from no factory, so there is nothing to probe
	isEnterprise, err := source.IsEnterprise(t.Context(), "v1.14.0", "")
	require.NoError(t, err)
	assert.False(t, isEnterprise)
	assert.Zero(t, enterpriseHits.Load())

	for range 3 {
		isEnterprise, err = source.IsEnterprise(t.Context(), "v1.14.0", hostOf(t, enterprise.URL))
		require.NoError(t, err)
		assert.True(t, isEnterprise)
	}

	assert.Equal(t, int64(1), enterpriseHits.Load(), "a factory does not change its identity, so one probe is enough")

	// A community factory and one that refuses the probe, each reached as the configured factory of their own
	// source so that the plain-HTTP base URL is used.
	communitySource, err := NewFactorySource(t.TempDir(), community.URL, "", "", zaptest.NewLogger(t))
	require.NoError(t, err)

	isEnterprise, err = communitySource.IsEnterprise(t.Context(), "v1.14.0", hostOf(t, community.URL))
	require.NoError(t, err)
	assert.False(t, isEnterprise)

	refusedSource, err := NewFactorySource(t.TempDir(), refused.URL, "", "", zaptest.NewLogger(t))
	require.NoError(t, err)

	isEnterprise, err = refusedSource.IsEnterprise(t.Context(), "v1.14.0", hostOf(t, refused.URL))
	require.NoError(t, err)
	assert.True(t, isEnterprise, "the identifying header is present even on a refusal")
}

func TestFactoryProbeURLFor(t *testing.T) {
	t.Parallel()

	// A plain-HTTP configured factory, to check it is used as it was given rather than assumed to be HTTPS.
	source, err := NewFactorySource(t.TempDir(), "http://factory.local:8080", "", "", zaptest.NewLogger(t))
	require.NoError(t, err)

	assert.Empty(t, source.probeURLFor(""), "no host means no factory")
	assert.Equal(t, "http://factory.local:8080", source.probeURLFor("factory.local:8080"))
	assert.Equal(t, "https://factory.example.com", source.probeURLFor("factory.example.com"),
		"an image reference carries no scheme, so another factory is assumed to be HTTPS")
}
