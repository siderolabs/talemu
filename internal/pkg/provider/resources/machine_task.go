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

// NewMachineTask creates new MachineTask.
func NewMachineTask(ns, id string) *MachineTask {
	return typed.NewResource[MachineTaskSpec, MachineTaskExtension](
		resource.NewMetadata(ns, MachineTaskType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.MachineTaskSpec{}),
	)
}

// MachineTaskType is the type of MachineTask resource.
var MachineTaskType = "MachineTask.talemu.sidero.dev"

// MachineTask describes fake machine configuration.
type MachineTask = typed.Resource[MachineTaskSpec, MachineTaskExtension]

// MachineTaskSpec wraps specs.MachineTaskSpec.
type MachineTaskSpec = protobuf.ResourceSpec[specs.MachineTaskSpec, *specs.MachineTaskSpec]

// MachineTaskExtension providers auxiliary methods for MachineTask resource.
type MachineTaskExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (MachineTaskExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             MachineTaskType,
		Aliases:          []resource.Type{},
		DefaultNamespace: emu.NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
