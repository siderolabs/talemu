// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talos

import (
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller/generic"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
)

// NamespaceName sets the default namespace name for the emulator resources.
const NamespaceName = "emulator"

func init() {
	mustRegisterResource(CachedImageType, &CachedImage{})
	mustRegisterResource(DiskType, &Disk{})
	mustRegisterResource(EventSinkStateType, &EventSinkState{})
	mustRegisterResource(ImageType, &Image{})
	mustRegisterResource(VersionType, &Version{})
	mustRegisterResource(RebootType, &Reboot{})
	mustRegisterResource(RebootStatusType, &RebootStatus{})
}

var resources []generic.ResourceWithRD

// mustRegisterResource adds resource to the registry, registers it's protobuf decoders/encoders.
func mustRegisterResource[T any, R interface {
	protobuf.Res[T]
	meta.ResourceDefinitionProvider
}](
	resourceType resource.Type,
	r R,
) {
	resources = append(resources, r)

	err := protobuf.RegisterResource(resourceType, r)
	if err != nil {
		panic(fmt.Errorf("failed to register resource %T: %w", r, err))
	}
}
