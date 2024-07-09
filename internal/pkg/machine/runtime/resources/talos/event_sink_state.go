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

// NewEventSinkState creates new EventSinkState state.
func NewEventSinkState(ns, id string) *EventSinkState {
	return typed.NewResource[EventSinkStateSpec, EventSinkStateExtension](
		resource.NewMetadata(ns, EventSinkStateType, id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.EventSinkStateSpec{}),
	)
}

// EventSinkStateType is the type of EventSinkState resource.
const (
	EventSinkStateType = resource.Type("EventSinkStates.talemu.sidero.dev")
	EventSinkStateID   = "current"
)

// EventSinkState resource contains event sink state.
type EventSinkState = typed.Resource[EventSinkStateSpec, EventSinkStateExtension]

// EventSinkStateSpec wraps specs.EventSinkStateSpec.
type EventSinkStateSpec = protobuf.ResourceSpec[specs.EventSinkStateSpec, *specs.EventSinkStateSpec]

// EventSinkStateExtension providers auxiliary methods for EventSinkState resource.
type EventSinkStateExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (EventSinkStateExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             EventSinkStateType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}
