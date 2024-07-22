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

// NewRebootStatus creates new RebootStatus resource.
func NewRebootStatus(ns, id string) *RebootStatus {
	return typed.NewResource[RebootStatusSpec, RebootStatusExtension](
		resource.NewMetadata(ns, RebootStatusType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.RebootStatusSpec{}),
	)
}

const (
	// RebootStatusType is the type of RebootStatus resource.
	RebootStatusType = resource.Type("RebootStatuses.talemu.sidero.dev")

	// RebootID is the ID of the singleton represeting rebooting state.
	RebootID = resource.ID("current")
)

// RebootStatus is used to simulate reboots.
type RebootStatus = typed.Resource[RebootStatusSpec, RebootStatusExtension]

// RebootStatusSpec wraps specs.RebootStatusSpec.
type RebootStatusSpec = protobuf.ResourceSpec[specs.RebootStatusSpec, *specs.RebootStatusSpec]

// RebootStatusExtension providers auxiliary methods for RebootStatus resource.
type RebootStatusExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (RebootStatusExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             RebootStatusType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
