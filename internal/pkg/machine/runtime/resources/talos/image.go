// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talos

import (
	"fmt"
	"strings"

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

// ImageRef is the parsed form of an installer image reference.
type ImageRef struct {
	// Host is the registry host of the reference. Real install images are pulled via containerd,
	// which requires fully qualified references, so the first path component is the host. It is
	// only empty for a single-component reference, which cannot reach a real machine.
	Host string

	// Schematic is the image factory schematic ID. It is only recoverable when the reference
	// points at the image factory host, since only those references carry it as the repository
	// path segment.
	Schematic string

	// Version is the image tag.
	Version string
}

// ParseImageRef splits an installer image reference into its registry host, schematic id and
// Talos version.
func ParseImageRef(imageFactoryHost, imageRef string) (ImageRef, error) {
	ref := imageRef

	if at := strings.IndexByte(ref, '@'); at != -1 {
		ref = ref[:at]
	}

	parts := strings.Split(ref, "/")

	schematicCandidate, version, found := strings.Cut(parts[len(parts)-1], ":")
	if !found {
		return ImageRef{}, fmt.Errorf("failed to parse the image %q", imageRef)
	}

	parsed := ImageRef{
		Version: version,
	}

	if len(parts) > 1 {
		parsed.Host = parts[0]
	}

	if parsed.Host != "" && parsed.Host == imageFactoryHost {
		parsed.Schematic = schematicCandidate
	}

	return parsed, nil
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
