// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talos

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"

	"github.com/siderolabs/talemu/api/specs"
)

// NewReboot creates new Reboot resource.
func NewReboot(ns, id string) *Reboot {
	return typed.NewResource[RebootSpec, RebootExtension](
		resource.NewMetadata(ns, RebootType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.RebootSpec{}),
	)
}

const (
	// RebootType is the type of Reboot resource.
	RebootType = resource.Type("Reboots.talemu.sidero.dev")
)

// Reboot is used to simulate reboots.
type Reboot = typed.Resource[RebootSpec, RebootExtension]

// RebootSpec wraps specs.RebootSpec.
type RebootSpec = protobuf.ResourceSpec[specs.RebootSpec, *specs.RebootSpec]

// RebootExtension providers auxiliary methods for Reboot resource.
type RebootExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (RebootExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             RebootType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
