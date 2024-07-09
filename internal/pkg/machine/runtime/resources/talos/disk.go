// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talos

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
)

// NewDisk creates new Disk resource.
func NewDisk(ns, id string) *Disk {
	return typed.NewResource[DiskSpec, DiskExtension](
		resource.NewMetadata(ns, DiskType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&storage.Disk{}),
	)
}

const (
	// DiskType is the type of Disk resource.
	DiskType = resource.Type("Disks.talemu.sidero.dev")
)

// Disk resource contains a single disk information.
type Disk = typed.Resource[DiskSpec, DiskExtension]

// DiskSpec wraps specs.DiskSpec.
type DiskSpec = protobuf.ResourceSpec[storage.Disk, *storage.Disk]

// DiskExtension providers auxiliary methods for Disk resource.
type DiskExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (DiskExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             DiskType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
