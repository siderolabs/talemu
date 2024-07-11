// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package backend

import (
	"context"
	"errors"
	"net"

	"github.com/akutz/memconn"
)

// NewTransport creates a new transport.
func NewTransport(address string) *Transport {
	return &Transport{address: address}
}

// Transport is transport for in-memory connection.
type Transport struct {
	address string
}

// Listener creates new listener.
func (l *Transport) Listener() (net.Listener, error) {
	if l.address == "" {
		return nil, errors.New("address is not set")
	}

	return memconn.Listen("memu", l.address)
}

// DialContext creates a new connection.
func (l *Transport) DialContext(ctx context.Context) (net.Conn, error) {
	if l.address == "" {
		return nil, errors.New("address is not set")
	}

	return memconn.DialContext(ctx, "memu", l.address)
}

// Address returns the address. Since this is a memory-based connection, the address is always "passthrough:" + address,
// because the address is not a real network address and gRPC tries to resolve it otherwise.
func (l *Transport) Address() string {
	return "passthrough:" + l.address
}
