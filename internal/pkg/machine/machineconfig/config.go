// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package machineconfig provides a utility method to retrieve complete machine config.
package machineconfig

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
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
