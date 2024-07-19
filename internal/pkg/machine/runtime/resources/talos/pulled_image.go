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

// NewCachedImage creates new CachedImage resource.
func NewCachedImage(ns, id string) *CachedImage {
	return typed.NewResource[CachedImageSpec, CachedImageExtension](
		resource.NewMetadata(ns, CachedImageType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.CachedImageSpec{}),
	)
}

const (
	// CachedImageType is the type of CachedImage resource.
	CachedImageType = resource.Type("CachedImages.talemu.sidero.dev")
)

// CachedImage is each image that was pulled by the image pre-pull.
type CachedImage = typed.Resource[CachedImageSpec, CachedImageExtension]

// CachedImageSpec wraps specs.CachedImageSpec.
type CachedImageSpec = protobuf.ResourceSpec[specs.CachedImageSpec, *specs.CachedImageSpec]

// CachedImageExtension providers auxiliary methods for CachedImage resource.
type CachedImageExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (CachedImageExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             CachedImageType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
