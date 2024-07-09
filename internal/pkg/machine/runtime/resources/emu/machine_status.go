// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package emu

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"

	"github.com/siderolabs/talemu/api/specs"
)

// NewMachineStatus creates new MachineStatus.
func NewMachineStatus(ns, id string) *MachineStatus {
	return typed.NewResource[MachineStatusSpec, MachineStatusExtension](
		resource.NewMetadata(ns, MachineStatusType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.MachineStatusSpec{}),
	)
}

// MachineStatusType is the type of MachineStatus resource.
//
// tsgen:MachineStatusType
const MachineStatusType = resource.Type("MachineStatuses.talemu.sidero.dev")

// MachineStatus describes virtual machine status.
type MachineStatus = typed.Resource[MachineStatusSpec, MachineStatusExtension]

// MachineStatusSpec wraps specs.MachineStatusSpec.
type MachineStatusSpec = protobuf.ResourceSpec[specs.MachineStatusSpec, *specs.MachineStatusSpec]

// MachineStatusExtension providers auxiliary methods for MachineStatus resource.
type MachineStatusExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (MachineStatusExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             MachineStatusType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
