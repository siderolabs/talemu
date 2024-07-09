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

// NewVersion creates new Version resource.
func NewVersion(ns, id string) *Version {
	return typed.NewResource[VersionSpec, VersionExtension](
		resource.NewMetadata(ns, VersionType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.VersionSpec{}),
	)
}

const (
	// VersionType is the type of Version resource.
	VersionType = resource.Type("Versions.talemu.sidero.dev")

	// VersionID is the single id of the Talos version of the emulated machine.
	VersionID = "current"
)

// Version resource keeps the current Talos version of the machine.
type Version = typed.Resource[VersionSpec, VersionExtension]

// VersionSpec wraps specs.VersionSpec.
type VersionSpec = protobuf.ResourceSpec[specs.VersionSpec, *specs.VersionSpec]

// VersionExtension providers auxiliary methods for Version resource.
type VersionExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (VersionExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             VersionType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
