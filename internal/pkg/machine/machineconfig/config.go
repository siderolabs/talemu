// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package machineconfig provides a utility method to retrieve complete machine config.
package machineconfig

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
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
