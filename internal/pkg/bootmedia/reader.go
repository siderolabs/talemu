// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	factoryclient "github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// readTimeout caps a single schematic read, resolving the client included.
const readTimeout = 30 * time.Second

// clientFunc returns a client able to read the given schematic.
type clientFunc func(ctx context.Context, id, talosVersion, factoryHost string) (*factoryclient.Client, error)

// staleCredentialsFunc drops whatever credentials a source has cached, so that the next client it builds
// fetches them again. Nil for a source whose credentials cannot go stale under it.
type staleCredentialsFunc func()

// schematicReader reads schematics from an image factory, coalescing concurrent reads of the same one and
// caching what it reads on disk.
type schematicReader struct {
	clientFor        clientFunc
	staleCredentials staleCredentialsFunc
	logger           *zap.Logger
	sf               singleflight.Group
	cacheDir         string
}

func newSchematicReader(cacheDir string, clientFor clientFunc, staleCredentials staleCredentialsFunc, logger *zap.Logger) (*schematicReader, error) {
	if cacheDir == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user cache dir: %w", err)
		}

		cacheDir = filepath.Join(userCacheDir, "talemu-schematics")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &schematicReader{
		cacheDir:         cacheDir,
		clientFor:        clientFor,
		staleCredentials: staleCredentials,
		logger:           logger,
	}, nil
}

// read returns the schematic with the given ID.
func (r *schematicReader) read(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error) {
	if !validSchematicID(id) {
		return nil, fmt.Errorf("%q is not a schematic ID, which is the hex SHA-256 of a schematic", id)
	}

	// Coalesced per factory and ID, never by ID alone: on a miss the flight's result is one factory's
	// answer, and handing its 404 to a caller asking a different factory would fail a read that factory
	// could have served. The disk cache below stays keyed by ID alone, since a schematic ID is the hash of
	// the schematic's canonical form, so any factory holding it holds the same content.
	ch := r.sf.DoChan(factoryHost+" "+talosVersion+" "+id, func() (any, error) {
		return r.readUncoalesced(ctx, id, talosVersion, factoryHost)
	})

	var res singleflight.Result

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res = <-ch:
	}

	if res.Err != nil {
		return nil, res.Err
	}

	sch, ok := res.Val.(*schematic.Schematic)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T, want *schematic.Schematic", res.Val)
	}

	return sch, nil
}

func (r *schematicReader) readUncoalesced(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error) {
	switch cached, err := r.fromCache(id); {
	case err == nil:
		r.logger.Debug("cache hit, return cached schematic", zap.String("id", id))

		return cached, nil
	case !errors.Is(err, errNotCached):
		return nil, err
	}

	r.logger.Info("cache miss, download schematic",
		zap.String("id", id), zap.String("talos_version", talosVersion), zap.String("factory_host", factoryHost))

	sch, err := r.get(ctx, id, talosVersion, factoryHost)
	if err != nil {
		return nil, err
	}

	if err = r.store(id, sch); err != nil {
		return nil, err
	}

	return sch, nil
}

// get reads the schematic from the factory, retrying once with fresh credentials when it does not accept them.
func (r *schematicReader) get(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error) {
	sch, err := r.getOnce(ctx, id, talosVersion, factoryHost)
	if err == nil || r.staleCredentials == nil || !isUnauthenticated(err) {
		return sch, err
	}

	r.logger.Info("image factory rejected the credentials, fetching them again",
		zap.String("id", id), zap.String("factory_host", factoryHost))

	r.staleCredentials()

	return r.getOnce(ctx, id, talosVersion, factoryHost)
}

func (r *schematicReader) getOnce(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error) {
	// Resolving the client counts against the same cap as the read: a source may go to Omni to work out which
	// factory holds the schematic and to ask for the credentials to reach it, so the factory is not the only
	// thing here that can stop responding.
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	client, err := r.clientFor(ctx, id, talosVersion, factoryHost)
	if err != nil {
		return nil, err
	}

	sch, err := client.SchematicGet(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get schematic %q from factory %s: %w", id, client.BaseURL(), err)
	}

	return sch, nil
}

// isUnauthenticated reports whether the factory did not accept the credentials, as opposed to accepting them
// and refusing the request anyway.
//
// Deliberately not 403. A rotated password answers 401, which a fresh set fixes. A 403 means the credentials
// were accepted and may not do this, which is what a token scoped to image downloads would answer for a
// schematic read, and refetching the same scope would be refused the same way on every reconcile of every
// machine. The enterprise image factory already offers Bearer alongside Basic.
func isUnauthenticated(err error) bool {
	return factoryclient.IsHTTPErrorCode(err, http.StatusUnauthorized)
}

// schematicIDLength is how long a schematic ID is: the image factory derives one as the hex encoding of the
// SHA-256 of the schematic's canonical form, so 32 bytes spelled out.
const schematicIDLength = sha256.Size * 2

// validSchematicID reports whether the ID could have come from an image factory.
//
// Checked because an ID reaches a path under the cache directory, and not every route here constrains it: an
// infra provider takes the ID from what Omni answered, and an image reference can carry a last segment that is
// no ID at all. Checking at the single point all of them pass through beats trusting each of them.
func validSchematicID(id string) bool {
	if len(id) != schematicIDLength {
		return false
	}

	// Lower case only, which is what hex.EncodeToString produces and what a factory therefore serves.
	return !strings.ContainsFunc(id, func(r rune) bool {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
			return false
		default:
			return true
		}
	})
}

// errNotCached means the schematic has to be read from a factory.
var errNotCached = errors.New("schematic is not cached")

// fromCache returns the cached schematic, or errNotCached when it is not cached.
func (r *schematicReader) fromCache(id string) (*schematic.Schematic, error) {
	contents, err := os.ReadFile(r.cachePath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errNotCached
		}

		return nil, fmt.Errorf("failed to read schematic file for %q: %w", id, err)
	}

	sch, err := schematic.Unmarshal(contents)
	if err != nil {
		// Reported as an error instead, a single unreadable file would break this schematic until someone
		// deleted it by hand. Treated as a miss, it is replaced by the read below.
		r.logger.Warn("discarding an unreadable cached schematic", zap.String("id", id), zap.Error(err))

		return nil, errNotCached
	}

	return sch, nil
}

// store caches the schematic on disk.
//
// A schematic can carry a join token and an embedded machine config, so the cache is kept readable only by
// the user running the emulator.
func (r *schematicReader) store(id string, sch *schematic.Schematic) error {
	raw, err := sch.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal schematic %q: %w", id, err)
	}

	if err = os.MkdirAll(r.cacheDir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(r.cacheDir, id+".*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("failed to create a temporary schematic file for %q: %w", id, err)
	}

	tmpPath := tmp.Name()

	defer os.Remove(tmpPath) //nolint:errcheck

	// os.CreateTemp opens it 0600 already, which is the mode this cache needs.
	if _, err = tmp.Write(raw); err != nil {
		tmp.Close() //nolint:errcheck

		return fmt.Errorf("failed to write schematic file %q: %w", tmpPath, err)
	}

	if err = tmp.Close(); err != nil {
		return fmt.Errorf("failed to close schematic file %q: %w", tmpPath, err)
	}

	// Another writer publishing it first is no failure: an ID is the hash of the schematic's canonical form,
	// so whatever is already there is the same content.
	if err = os.Rename(tmpPath, r.cachePath(id)); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("failed to rename schematic file %q: %w", tmpPath, err)
	}

	return nil
}

func (r *schematicReader) cachePath(id string) string {
	return filepath.Join(r.cacheDir, id+".yaml")
}
