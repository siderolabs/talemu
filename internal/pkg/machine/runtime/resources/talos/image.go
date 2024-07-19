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

// NewImage creates new Image resource.
func NewImage(ns, id string) *Image {
	return typed.NewResource[ImageSpec, ImageExtension](
		resource.NewMetadata(ns, ImageType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.ImageSpec{}),
	)
}

const (
	// ImageType is the type of Image resource.
	ImageType = resource.Type("Images.talemu.sidero.dev")

	// ImageID is the single id of the Talos image installed on the machine.
	ImageID = "current"
)

// Image resource keeps the last image used in the upgrade request.
type Image = typed.Resource[ImageSpec, ImageExtension]

// ImageSpec wraps specs.ImageSpec.
type ImageSpec = protobuf.ResourceSpec[specs.ImageSpec, *specs.ImageSpec]

// ImageExtension providers auxiliary methods for Image resource.
type ImageExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (ImageExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             ImageType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
