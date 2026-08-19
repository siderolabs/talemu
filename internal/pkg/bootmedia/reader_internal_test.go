// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	factoryclient "github.com/siderolabs/image-factory/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const schematicBody = "customization:\n    extraKernelArgs:\n        - foo=bar\n"

// clientForServer returns a clientFunc reaching whichever test server the host maps to.
func clientForServer(byHost map[string]string) clientFunc {
	return func(_ context.Context, _, _, factoryHost string) (*factoryclient.Client, error) {
		return factoryclient.New(byHost[factoryHost])
	}
}

func TestReaderCachesByID(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		w.Write([]byte(schematicBody)) //nolint:errcheck
	}))
	t.Cleanup(factory.Close)

	reader, err := newSchematicReader(t.TempDir(), clientForServer(map[string]string{"one": factory.URL}), nil, zaptest.NewLogger(t))
	require.NoError(t, err)

	const id = "1111111111111111111111111111111111111111111111111111111111111111"

	first, err := reader.read(t.Context(), id, "v1.14.0", "one")
	require.NoError(t, err)
	assert.Equal(t, []string{"foo=bar"}, first.Customization.ExtraKernelArgs)

	second, err := reader.read(t.Context(), id, "v1.14.0", "one")
	require.NoError(t, err)
	assert.Equal(t, first, second)

	assert.Equal(t, int64(1), hits.Load(), "the second read should come from the cache")
}

// TestReaderCoalescesPerFactory asserts that concurrent reads of the same identifier from different factories
// are answered by their own factory. Were the reads coalesced by identifier alone, one factory's "not found"
// would be handed to a caller that asked a factory which holds the schematic.
//
// It deliberately does not assert that the same identifier can mean different things at different factories:
// an identifier is the hash of the schematic's canonical form, so that cannot happen, and the disk cache is
// keyed by identifier alone for exactly that reason.
func TestReaderCoalescesPerFactory(t *testing.T) {
	t.Parallel()

	const id = "3333333333333333333333333333333333333333333333333333333333333333"

	notFoundEntered := make(chan struct{})
	notFoundContinue := make(chan struct{})

	// Released in cleanup as well, so that a failure between the block and the release does not leave the
	// handler stuck and the server unable to close.
	releaseNotFound := sync.OnceFunc(func() { close(notFoundContinue) })

	notFoundFactory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(notFoundEntered)
		<-notFoundContinue

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(notFoundFactory.Close)
	t.Cleanup(releaseNotFound)

	holdingFactory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(schematicBody)) //nolint:errcheck
	}))
	t.Cleanup(holdingFactory.Close)

	reader, err := newSchematicReader(t.TempDir(), clientForServer(map[string]string{
		"missing": notFoundFactory.URL,
		"holding": holdingFactory.URL,
	}), nil, zaptest.NewLogger(t))
	require.NoError(t, err)

	missingResult := make(chan error, 1)

	go func() {
		_, getErr := reader.read(t.Context(), id, "v1.14.0", "missing")
		missingResult <- getErr
	}()

	// The first read is now blocked inside the factory that does not hold the schematic. A read from the
	// factory that does must not be joined onto it. Bounded so that a regression fails the test instead of
	// deadlocking it.
	<-notFoundEntered

	boundedCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	_, err = reader.read(boundedCtx, id, "v1.14.0", "holding")
	require.NoError(t, err)

	releaseNotFound()

	require.Error(t, <-missingResult, "the factory that does not hold the schematic still reports the miss")
}

// TestReaderRefreshesStaleCredentials asserts that a factory turning the read away for its credentials makes
// the reader drop them and try once more, which is what a rotated image factory password looks like.
func TestReaderRefreshesStaleCredentials(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		if r.Header.Get("Authorization") != "Basic fresh" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		w.Write([]byte(schematicBody)) //nolint:errcheck
	}))
	t.Cleanup(factory.Close)

	var (
		credentials atomic.Value
		staleCalls  atomic.Int64
	)

	credentials.Store("Basic stale")

	clientFor := func(_ context.Context, _, _, _ string) (*factoryclient.Client, error) {
		headers := http.Header{}
		headers.Set("Authorization", credentials.Load().(string)) //nolint:forcetypeassert,errcheck

		return factoryclient.New(factory.URL, withHeaders(headers))
	}

	staleCredentials := func() {
		staleCalls.Add(1)
		credentials.Store("Basic fresh")
	}

	reader, err := newSchematicReader(t.TempDir(), clientFor, staleCredentials, zaptest.NewLogger(t))
	require.NoError(t, err)

	got, err := reader.read(t.Context(), "4444444444444444444444444444444444444444444444444444444444444444", "v1.14.0", "one")
	require.NoError(t, err)
	assert.Equal(t, []string{"foo=bar"}, got.Customization.ExtraKernelArgs)

	assert.Equal(t, int64(1), staleCalls.Load(), "the credentials should have been dropped exactly once")
	assert.Equal(t, int64(2), hits.Load(), "the rejected read and the retry")
}

// TestReaderGivesUpAfterOneRetry asserts that a factory rejecting the fresh credentials too is reported
// rather than retried forever.
func TestReaderGivesUpAfterOneRetry(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(factory.Close)

	var staleCalls atomic.Int64

	reader, err := newSchematicReader(t.TempDir(),
		clientForServer(map[string]string{"one": factory.URL}),
		func() { staleCalls.Add(1) },
		zaptest.NewLogger(t))
	require.NoError(t, err)

	_, err = reader.read(t.Context(), "5555555555555555555555555555555555555555555555555555555555555555", "v1.14.0", "one")
	require.Error(t, err)

	assert.Equal(t, int64(1), staleCalls.Load())
	assert.Equal(t, int64(2), hits.Load(), "one retry, not a loop")
}

// TestReaderTimesOutResolvingTheClient asserts the read cap is already in place when the client is resolved.
// Resolving one can go to Omni, both for the factory holding the schematic and for the credentials to reach
// it, and neither call has a timeout of its own.
func TestReaderTimesOutResolvingTheClient(t *testing.T) {
	t.Parallel()

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(schematicBody)) //nolint:errcheck
	}))
	t.Cleanup(factory.Close)

	type resolved struct {
		deadline time.Time
		capped   bool
	}

	// Reported out rather than asserted in place, as the flight runs on its own goroutine.
	resolvedCh := make(chan resolved, 1)

	clientFor := func(ctx context.Context, _, _, _ string) (*factoryclient.Client, error) {
		deadline, capped := ctx.Deadline()

		resolvedCh <- resolved{deadline: deadline, capped: capped}

		return factoryclient.New(factory.URL)
	}

	reader, err := newSchematicReader(t.TempDir(), clientFor, nil, zaptest.NewLogger(t))
	require.NoError(t, err)

	_, err = reader.read(t.Context(), "6666666666666666666666666666666666666666666666666666666666666666", "v1.14.0", "one")
	require.NoError(t, err)

	got := <-resolvedCh
	require.True(t, got.capped, "the client is resolved before the read cap is applied, so an unresponsive Omni is unbounded")
	assert.LessOrEqual(t, time.Until(got.deadline), readTimeout, "resolving the client and the read share one cap")
}

// TestReaderReplacesAnUnreadableCacheEntry asserts that a cache file which cannot be parsed counts as a miss.
// Reported as an error instead, a single truncated file would break its schematic permanently, since nothing
// else ever rewrites one.
func TestReaderReplacesAnUnreadableCacheEntry(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		w.Write([]byte(schematicBody)) //nolint:errcheck
	}))
	t.Cleanup(factory.Close)

	cacheDir := t.TempDir()

	reader, err := newSchematicReader(cacheDir, clientForServer(map[string]string{"one": factory.URL}), nil, zaptest.NewLogger(t))
	require.NoError(t, err)

	const id = "7777777777777777777777777777777777777777777777777777777777777777"

	// what a writer interrupted partway through leaves behind
	require.NoError(t, os.WriteFile(reader.cachePath(id), []byte(schematicBody[:len(schematicBody)/2]), 0o600))

	got, err := reader.read(t.Context(), id, "v1.14.0", "one")
	require.NoError(t, err)
	assert.Equal(t, []string{"foo=bar"}, got.Customization.ExtraKernelArgs)
	assert.Equal(t, int64(1), hits.Load())

	second, err := reader.read(t.Context(), id, "v1.14.0", "one")
	require.NoError(t, err)
	assert.Equal(t, got, second)
	assert.Equal(t, int64(1), hits.Load(), "the entry is replaced, so the next read comes off the disk again")

	// the temporary file each write goes through is not left behind
	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	assert.Equal(t, []string{id + ".yaml"}, names)
}

// TestReaderRejectsUnusableSchematicID asserts an ID that cannot be a cache file name is refused before
// anything is requested or written. Not every route here constrains the ID: an infra provider takes it from
// what Omni answered rather than from an image reference, which cannot carry a path separator.
func TestReaderRejectsUnusableSchematicID(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		w.Write([]byte(schematicBody)) //nolint:errcheck
	}))
	t.Cleanup(factory.Close)

	cacheDir := t.TempDir()

	reader, err := newSchematicReader(cacheDir, clientForServer(map[string]string{"one": factory.URL}), nil, zaptest.NewLogger(t))
	require.NoError(t, err)

	// what an image factory produces: the hex SHA-256 of the schematic's canonical form
	const validID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	for _, id := range []string{
		"",
		".",
		"..",
		"../../etc/passwd",
		"abcd/1234",
		"abcd 1234",
		"abcd\x00",
		// A last segment an image reference can carry that is no schematic ID.
		"metal-installer",
		// The right shape, but a factory spells a digest in lowercase.
		strings.ToUpper(validID),
		// One character short, and one too long.
		validID[:len(validID)-1],
		validID + "0",
	} {
		_, err = reader.read(t.Context(), id, "v1.14.0", "one")
		require.Error(t, err, "the ID %q cannot identify a schematic, so it has to be refused before anything is read", id)
	}

	assert.Zero(t, hits.Load(), "a refused ID must not reach a factory")

	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "and must leave nothing behind in the cache")

	// The ordinary case is a hex digest, which the rule has to keep accepting.
	got, err := reader.read(t.Context(), validID, "v1.14.0", "one")
	require.NoError(t, err)
	assert.Equal(t, []string{"foo=bar"}, got.Customization.ExtraKernelArgs)
}

// TestReaderDoesNotRefetchOnForbidden asserts a 403 fails at once instead of dropping the credentials and
// asking for them again. A 403 means the factory accepted them and will not allow this, so the same set
// refetched is refused the same way, on every reconcile of every machine. A token scoped to image downloads
// answers exactly that for a schematic read.
func TestReaderDoesNotRefetchOnForbidden(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(factory.Close)

	var staleCalls atomic.Int64

	reader, err := newSchematicReader(t.TempDir(),
		clientForServer(map[string]string{"one": factory.URL}),
		func() { staleCalls.Add(1) },
		zaptest.NewLogger(t))
	require.NoError(t, err)

	_, err = reader.read(t.Context(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "v1.14.0", "one")
	require.Error(t, err)

	assert.Zero(t, staleCalls.Load(), "the credentials were accepted, so there is nothing to refresh")
	assert.Equal(t, int64(1), hits.Load(), "and nothing to ask a second time")
}
