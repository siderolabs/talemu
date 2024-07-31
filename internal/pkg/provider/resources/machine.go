// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package resources

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"

	"github.com/siderolabs/talemu/api/specs"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// NewMachine creates new Machine.
func NewMachine(ns, id string) *Machine {
	return typed.NewResource[MachineSpec, MachineExtension](
		resource.NewMetadata(ns, MachineType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.MachineSpec{}),
	)
}

// MachineType is the type of Machine resource.
//
// tsgen:MachineType
const MachineType = resource.Type("Machines.talemu.sidero.dev")

// Machine describes virtual machine configuration.
type Machine = typed.Resource[MachineSpec, MachineExtension]

// MachineSpec wraps specs.MachineSpec.
type MachineSpec = protobuf.ResourceSpec[specs.MachineSpec, *specs.MachineSpec]

// MachineExtension providers auxiliary methods for Machine resource.
type MachineExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (MachineExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             MachineType,
		Aliases:          []resource.Type{},
		DefaultNamespace: emu.NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
