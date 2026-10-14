// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package machineconfig provides utility methods to read and store the machine config.
package machineconfig

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
)

type notFoundError struct{}

// Error implements error interface.
func (e notFoundError) Error() string {
	return "config is partial"
}

// NotFoundError implements state.ErrNotFound interface.
func (e notFoundError) NotFoundError() {}

// GetComplete returns the complete (non-partial) MachineConfig. If the config does not exist or is partial, it will return state.ErrNotFound.
func GetComplete(ctx context.Context, st controller.Reader) (*config.MachineConfig, error) {
	conf, err := safe.ReaderGetByID[*config.MachineConfig](ctx, st, config.ActiveID)
	if err != nil {
		return nil, err
	}

	if conf.Config().Machine() == nil {
		return nil, notFoundError{}
	}

	return conf, nil
}

// Store creates or updates the given machine config.
func Store(ctx context.Context, st state.State, cfg *config.MachineConfig) error {
	err := st.Create(ctx, cfg)
	if err == nil || !state.IsConflictError(err) {
		return err
	}

	existing, err := st.Get(ctx, cfg.Metadata())
	if err != nil {
		return err
	}

	cfg.Metadata().SetVersion(existing.Metadata().Version())

	return st.Update(ctx, cfg)
}

// RestorePersistent replaces the active config with the persistent one and destroys the persistent one.
//
// The persistent config only exists while a config applied in try mode is not confirmed yet.
func RestorePersistent(ctx context.Context, st state.State) (bool, error) {
	persistent, err := safe.StateGetByID[*config.MachineConfig](ctx, st, config.PersistentID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return false, nil
		}

		return false, err
	}

	if err = Store(ctx, st, config.NewMachineConfig(persistent.Provider())); err != nil {
		return false, err
	}

	if err = DestroyPersistent(ctx, st); err != nil {
		return false, err
	}

	return true, nil
}

// DestroyPersistent destroys the persistent config, if there is one.
func DestroyPersistent(ctx context.Context, st state.State) error {
	md := resource.NewMetadata(config.NamespaceName, config.MachineConfigType, config.PersistentID, resource.VersionUndefined)

	if err := st.Destroy(ctx, md); err != nil && !state.IsNotFoundError(err) {
		return err
	}

	return nil
}

// ClusterID returns the cluster ID of the given machine config.
//
// It reads the identity document, which also surfaces the legacy .cluster.id field of the v1alpha1
// config for configs generated for Talos older than 1.14. Returns an empty string when the config
// carries no cluster identity.
func ClusterID(cfg talosconfig.Config) string {
	identity := cfg.DiscoveryIdentityConfig()
	if identity == nil {
		return ""
	}

	return identity.ClusterID()
}
