// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import talosconfig "github.com/siderolabs/talos/pkg/machinery/config"

// NewMachineState allocates the per-machine state with the given boot config.
func NewMachineState(bootConfig talosconfig.Provider) *machineState {
	return newMachineState(bootConfig)
}
