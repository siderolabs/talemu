// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/cosi-project/runtime/pkg/state/impl/store"
	"github.com/cosi-project/runtime/pkg/state/impl/store/bolt"
	"github.com/siderolabs/gen/xslices"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"
)

// NamespacedState defines additional namespaced state to pass to the namespaced.NewState.
type NamespacedState struct {
	State     state.State
	Namespace string
}

// NewState creates State.
func NewState(path string, logger *zap.Logger, namespacedStates ...NamespacedState) (state.State, io.Closer, error) {
	builder, backingStore, err := newBoltStateBuilder(path, &bbolt.Options{}, true, logger)
	if err != nil {
		return nil, nil, err
	}

	nsStates := xslices.ToMap(namespacedStates, func(nst NamespacedState) (string, state.State) {
		return nst.Namespace, nst.State
	})

	state := state.WrapCore(namespaced.NewState(func(n resource.Namespace) state.CoreState {
		if n == meta.NamespaceName {
			return inmem.NewState(n)
		}

		if st, ok := nsStates[n]; ok {
			return st
		}

		return builder(n)
	}))

	return state, backingStore, nil
}

func newBoltStateBuilder(path string, options *bbolt.Options, compact bool, logger *zap.Logger) (
	builder namespaced.StateBuilder,
	backingStore io.Closer,
	stateErr error,
) {
	boltStore, err := bolt.NewBackingStore(
		func() (*bbolt.DB, error) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, fmt.Errorf("failed to create BoltDB directory: %w", err)
			}

			_, err := os.Stat(path)
			if err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to stat BoltDB path: %w", err)
			}

			var (
				runCompaction = compact && err == nil // if compaction is requested & there's an existing db file
				oldDBPath     string
				oldDB         *bbolt.DB
			)

			if runCompaction {
				oldDBPath = path + ".old"

				if err = os.Rename(path, oldDBPath); err != nil {
					return nil, err
				}

				logger.Info("moved existing boltdb file", zap.String("src", path), zap.String("dst", oldDBPath))

				if oldDB, err = bbolt.Open(oldDBPath, 0o600, nil); err != nil {
					return nil, fmt.Errorf("failed to open BoltDB: %w", err)
				}

				defer oldDB.Close() //nolint:errcheck
			}

			// open the actual db file
			db, err := bbolt.Open(path, 0o600, options)
			if err != nil {
				return nil, fmt.Errorf("failed to open BoltDB: %w", err)
			}

			// compaction: compact the old db file into the new one
			if runCompaction {
				if err = bbolt.Compact(db, oldDB, 65536); err != nil {
					return nil, fmt.Errorf("failed to compact BoltDB: %w", err)
				}

				logger.Info("compacted BoltDB", zap.String("src", oldDBPath), zap.String("dst", path))

				if err = oldDB.Close(); err != nil {
					return nil, fmt.Errorf("failed to close old BoltDB: %w", err)
				}

				if err = os.Remove(oldDBPath); err != nil {
					return nil, fmt.Errorf("failed to remove old BoltDB file: %w", err)
				}

				logger.Info("removed old BoltDB file after compaction", zap.String("path", oldDBPath))
			}

			return db, nil
		},
		store.ProtobufMarshaler{},
	)
	if err != nil {
		return nil, nil, err
	}

	return func(ns resource.Namespace) state.CoreState {
		return inmem.NewStateWithOptions(inmem.WithBackingStore(boltStore.WithNamespace(ns)),
			inmem.WithHistoryGap(20),
			inmem.WithHistoryMaxCapacity(5000),
		)(ns)
	}, boltStore, nil
}

// GetStateDir constructs state directory for the machine.
func GetStateDir(id string) string {
	return filepath.Join("_out/state/machines", id)
}
