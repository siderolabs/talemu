// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package schematic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	schematicsvc "github.com/siderolabs/talemu/internal/pkg/schematic"
)

const (
	expectedID        = "dc1c492eafbbbdf85e25f11b67a4296f55163752491ae31d0a83d8b6f20973ee"
	expectedSchematic = `customization:
    extraKernelArgs:
        - foo=bar
        - bar=baz
    systemExtensions:
        officialExtensions:
            - siderolabs/hello-world-service
            - siderolabs/qemu-guest-agent`
)

func TestNewService(t *testing.T) {
	t.Parallel()

	service, err := schematicsvc.NewService(t.TempDir(), "http://factory.local:8080/base", "user", "password", nil)
	require.NoError(t, err)
	require.Equal(t, "http://factory.local:8080/base", service.ImageFactoryBaseURL())
	require.Equal(t, "factory.local:8080", service.ImageFactoryHost())

	_, err = schematicsvc.NewService(t.TempDir(), "factory.local", "", "", nil)
	require.Error(t, err)
}

func TestGetByID(t *testing.T) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	cacheDir := t.TempDir()

	logger.Info("test dir", zap.String("dir", cacheDir))

	schematicService, err := schematicsvc.NewService(cacheDir, "https://factory.talos.dev", "", "", logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	before := time.Now()

	sch, err := schematicService.GetByID(ctx, expectedID, "")
	require.NoError(t, err)

	after := time.Now()

	marshaled1, err := sch.Marshal()
	require.NoError(t, err)

	logger.Info("schematic", zap.ByteString("data", marshaled1), zap.Duration("duration", after.Sub(before)))

	before = time.Now()

	sch, err = schematicService.GetByID(ctx, expectedID, "")
	require.NoError(t, err)

	after = time.Now()

	marshaled2, err := sch.Marshal()
	require.NoError(t, err)

	expected, err := schematic.Unmarshal([]byte(expectedSchematic))
	require.NoError(t, err)

	expectedMarshaled, err := expected.Marshal()
	require.NoError(t, err)

	assert.Equal(t, expectedMarshaled, marshaled1)
	assert.Equal(t, marshaled1, marshaled2)

	logger.Info("schematic", zap.ByteString("data", marshaled2), zap.Duration("duration", after.Sub(before)))
}

// TestGetByIDPerFactory asserts that a schematic is read from the factory that
// holds it, rather than always from the configured one.
//
// It deliberately does NOT assert that the same identifier can mean different
// things at different factories: an identifier is the hash of the schematic's
// canonical form, so that cannot happen, and the cache is keyed by identifier
// alone for exactly that reason.
func TestGetByIDPerFactory(t *testing.T) {
	t.Parallel()

	var configuredHits, otherHits int

	body := "customization:\n    extraKernelArgs:\n        - foo=bar\n"

	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		configuredHits++

		w.Write([]byte(body)) //nolint:errcheck
	}))
	t.Cleanup(configured.Close)

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHits++

		w.Write([]byte(body)) //nolint:errcheck
	}))
	t.Cleanup(other.Close)

	const (
		configuredID = "1111111111111111111111111111111111111111111111111111111111111111"
		otherID      = "2222222222222222222222222222222222222222222222222222222222222222"
	)

	service, err := schematicsvc.NewService(t.TempDir(), configured.URL, "", "", zaptest.NewLogger(t))
	require.NoError(t, err)

	// The configured factory, reached by empty string, by its own URL, and by a
	// trailing-slash spelling of it, which must not be mistaken for another factory.
	for _, factory := range []string{"", configured.URL, configured.URL + "/"} {
		_, getErr := service.GetByID(t.Context(), configuredID, factory)
		require.NoError(t, getErr)
	}

	assert.Equal(t, 1, configuredHits, "cached after the first read, and every spelling is the same factory")
	assert.Equal(t, 0, otherHits)

	// An identifier the configured factory does not hold is read from the factory
	// that does.
	_, err = service.GetByID(t.Context(), otherID, other.URL)
	require.NoError(t, err)

	assert.Equal(t, 1, otherHits, "the other factory should have been asked")
	assert.Equal(t, 1, configuredHits, "and the configured one should not have been asked again")

	// Cached content is reused whichever factory is named, since the identifier
	// determines the content.
	_, err = service.GetByID(t.Context(), otherID, configured.URL)
	require.NoError(t, err)

	assert.Equal(t, 1, otherHits)
	assert.Equal(t, 1, configuredHits)
}

// TestGetByIDConcurrentFactories asserts that concurrent reads of the same
// identifier from different factories are answered by their own factory. Were
// the reads coalesced by identifier alone, one factory's "not found" would be
// handed to a caller that asked a factory which holds the schematic.
func TestGetByIDConcurrentFactories(t *testing.T) {
	t.Parallel()

	const id = "3333333333333333333333333333333333333333333333333333333333333333"

	var (
		notFoundEntered  = make(chan struct{})
		notFoundContinue = make(chan struct{})
	)

	// Released in cleanup as well, so that a failure between the block and the
	// release does not leave the handler stuck and the server unable to close.
	releaseNotFound := sync.OnceFunc(func() { close(notFoundContinue) })

	notFoundFactory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(notFoundEntered)
		<-notFoundContinue

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(notFoundFactory.Close)
	t.Cleanup(releaseNotFound)

	holdingFactory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("customization:\n    extraKernelArgs:\n        - foo=bar\n")) //nolint:errcheck
	}))
	t.Cleanup(holdingFactory.Close)

	service, err := schematicsvc.NewService(t.TempDir(), notFoundFactory.URL, "", "", zaptest.NewLogger(t))
	require.NoError(t, err)

	notFoundResult := make(chan error, 1)

	go func() {
		_, getErr := service.GetByID(t.Context(), id, "")
		notFoundResult <- getErr
	}()

	// The first read is now blocked inside the factory that does not hold the
	// schematic. A read from the factory that does must not be joined onto it.
	// Bounded so that a regression fails the test instead of deadlocking it.
	<-notFoundEntered

	boundedCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	_, err = service.GetByID(boundedCtx, id, holdingFactory.URL)
	require.NoError(t, err)

	releaseNotFound()

	require.Error(t, <-notFoundResult, "the factory that does not hold the schematic still reports the miss")
}
