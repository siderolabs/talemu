// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package runtime starts COSI state and runtime.
package runtime

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/state"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
	"github.com/siderolabs/talemu/internal/pkg/machine/services"
)

// Runtime handles COSI state setup and lifecycle.
type Runtime struct {
	state   state.State
	runtime *runtime.Runtime
}

// NewRuntime creates new runtime.
func NewRuntime(ctx context.Context, logger *zap.Logger, machineIndex int) (*Runtime, error) {
	state, err := newState(ctx)
	if err != nil {
		return nil, err
	}

	controllers := []controller.Controller{
		&controllers.ManagerController{
			MachineIndex: machineIndex,
		},
		&controllers.LinkSpecController{},
		&controllers.APIDController{
			APID: services.NewAPID(state),
		},
		&controllers.AddressSpecController{},
	}

	runtime, err := runtime.NewRuntime(state, logger)
	if err != nil {
		return nil, err
	}

	for _, ctrl := range controllers {
		if err = runtime.RegisterController(ctrl); err != nil {
			return nil, err
		}
	}

	return &Runtime{
		state:   state,
		runtime: runtime,
	}, nil
}

// Run starts COSI runtime.
func (r *Runtime) Run(ctx context.Context) error {
	return r.runtime.Run(ctx)
}

// State returns COSI state.
func (r *Runtime) State() state.State {
	return r.state
}
