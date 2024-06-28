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

// NewClusterStatus creates new ClusterStatus state.
func NewClusterStatus(ns, id string) *ClusterStatus {
	return typed.NewResource[ClusterStatusSpec, ClusterStatusExtension](
		resource.NewMetadata(ns, ClusterStatusType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.ClusterStatusSpec{}),
	)
}

// ClusterStatusType is the type of ClusterStatus resource.
//
// tsgen:ClusterStatusType
const ClusterStatusType = resource.Type("ClusterStatuses.omni.sidero.dev")

// ClusterStatus resource contains current information about the Machine bootstrap status.
type ClusterStatus = typed.Resource[ClusterStatusSpec, ClusterStatusExtension]

// ClusterStatusSpec wraps specs.ClusterStatusSpec.
type ClusterStatusSpec = protobuf.ResourceSpec[specs.ClusterStatusSpec, *specs.ClusterStatusSpec]

// ClusterStatusExtension providers auxiliary methods for ClusterStatus resource.
type ClusterStatusExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (ClusterStatusExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             ClusterStatusType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
