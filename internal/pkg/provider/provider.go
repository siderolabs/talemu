// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider implements emulator infra provider for Omni.
package provider

import (
	"github.com/cosi-project/runtime/pkg/controller"

	"github.com/siderolabs/talemu/internal/pkg/emu"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/provider/controllers"
	"github.com/siderolabs/talemu/internal/pkg/schematic"
)

// RegisterControllers registers additional controllers required for the infra provider.
func RegisterControllers(runtime *emu.Runtime, kubernetes *kubefactory.Kubernetes, nc *network.Client, schematicService *schematic.Service) error {
	controllers := []controller.Controller{
		controllers.NewMachineController(runtime.State(), kubernetes, nc, schematicService),
	}

	for _, ctrl := range controllers {
		if err := runtime.RegisterController(ctrl); err != nil {
			return err
		}
	}

	return nil
}
