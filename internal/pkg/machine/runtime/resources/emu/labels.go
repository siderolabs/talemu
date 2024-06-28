// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package emu

// SystemLabelPrefix is the prefix of all internal talemu labels.
const SystemLabelPrefix = "talemu.sidero.dev/"

// LabelCluster is the cluster identity.
const LabelCluster = SystemLabelPrefix + "cluster"

// LabelControlPlaneRole the machine is a contol plane.
const LabelControlPlaneRole = SystemLabelPrefix + "role-control-plane"

// LabelWorkerRole the machine is a worker.
const LabelWorkerRole = SystemLabelPrefix + "role-worker"
