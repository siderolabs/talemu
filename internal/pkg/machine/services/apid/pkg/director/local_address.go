// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package director

import (
	"context"
	"sync"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// LocalAddressProvider provides local address information.
type LocalAddressProvider interface {
	IsLocalTarget(string) bool
}

// NewLocalAddressProvider initializes watches and returns a new LocalAddrProvider.
//
// Call Run to start processing events.
func NewLocalAddressProvider(st state.State) (*LocalAddrProvider, error) {
	return &LocalAddrProvider{state: st}, nil
}

// LocalAddrProvider watches and keeps track of the local node addresses.
type LocalAddrProvider struct {
	state          state.State
	localAddresses map[string]struct{}
	localHostnames map[string]struct{}
	mu             sync.Mutex
}

// Run processes watch events until the context is canceled.
func (p *LocalAddrProvider) Run(ctx context.Context) error {
	eventCh := make(chan state.Event)

	if err := p.state.Watch(ctx, resource.NewMetadata(network.NamespaceName, network.NodeAddressType, network.NodeAddressCurrentID, resource.VersionUndefined), eventCh); err != nil {
		return err
	}

	if err := p.state.Watch(ctx, resource.NewMetadata(network.NamespaceName, network.HostnameStatusType, network.HostnameID, resource.VersionUndefined), eventCh); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-eventCh:
			p.handleEvent(ev)
		}
	}
}

func (p *LocalAddrProvider) handleEvent(ev state.Event) {
	switch ev.Type {
	case state.Created, state.Updated, state.Noop:
		// expected
	case state.Destroyed, state.Bootstrapped, state.Errored:
		return
	}

	switch r := ev.Resource.(type) {
	case *network.NodeAddress:
		p.mu.Lock()

		p.localAddresses = make(map[string]struct{}, len(r.TypedSpec().Addresses))

		for _, addr := range r.TypedSpec().Addresses {
			p.localAddresses[addr.Addr().String()] = struct{}{}
		}

		p.mu.Unlock()
	case *network.HostnameStatus:
		p.mu.Lock()

		p.localHostnames = make(map[string]struct{}, 2)

		p.localHostnames[r.TypedSpec().Hostname] = struct{}{}
		p.localHostnames[r.TypedSpec().FQDN()] = struct{}{}

		p.mu.Unlock()
	}
}

// IsLocalTarget returns true if the address (hostname) is local.
func (p *LocalAddrProvider) IsLocalTarget(target string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, ok1 := p.localAddresses[target]
	_, ok2 := p.localHostnames[target]

	return ok1 || ok2
}
