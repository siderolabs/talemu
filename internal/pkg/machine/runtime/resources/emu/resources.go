// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package emu contains emulator specific resources.
package emu

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller/generic"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/registry"
)

// NamespaceName sets the default namespace name for the emulator resources.
const NamespaceName = "emulator"

func init() {
	mustRegisterResource(ClusterStatusType, &ClusterStatus{})
	mustRegisterResource(MachineStatusType, &MachineStatus{})
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

// Register the emulator resource group in the state.
func Register(ctx context.Context, state state.State) error {
	namespaceRegistry := registry.NewNamespaceRegistry(state)
	resourceRegistry := registry.NewResourceRegistry(state)

	if err := namespaceRegistry.RegisterDefault(ctx); err != nil {
		return err
	}

	if err := resourceRegistry.RegisterDefault(ctx); err != nil {
		return err
	}

	// register namespaces
	for _, ns := range []struct {
		name        string
		description string
	}{
		{NamespaceName, "Emulator resources."},
	} {
		if err := namespaceRegistry.Register(ctx, ns.name, ns.description); err != nil {
			return err
		}
	}

	// register resources
	for _, r := range resources {
		if err := resourceRegistry.Register(ctx, r); err != nil {
			return err
		}
	}

	return nil
}
