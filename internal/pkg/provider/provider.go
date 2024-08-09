// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider implements emulator cloud provider for Omni.
package provider

import (
	"github.com/cosi-project/runtime/pkg/controller"

	"github.com/siderolabs/talemu/internal/pkg/emu"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/provider/controllers"
)

// RegisterControllers registers additional controllers required for the cloud provider.
func RegisterControllers(runtime *emu.Runtime, kubernetes *kubefactory.Kubernetes, nc *network.Client) error {
	qcontrollers := []controller.QController{
		controllers.NewMachineRequestStatusController(),
	}

	controllers := []controller.Controller{
		controllers.NewMachineController(runtime.State(), kubernetes, nc),
	}

	for _, ctrl := range qcontrollers {
		if err := runtime.RegisterQController(ctrl); err != nil {
			return err
		}
	}

	for _, ctrl := range controllers {
		if err := runtime.RegisterController(ctrl); err != nil {
			return err
		}
	}

	return nil
}
