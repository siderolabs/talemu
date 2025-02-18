// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !linux

// Package network contains utility methods for setting up emulator network.
package network

import (
	"fmt"
	"syscall"
)

// BindToInterface binds the socket to the interface.
func BindToInterface(string) func(string, string, syscall.RawConn) error {
	return func(string, string, syscall.RawConn) error {
		return fmt.Errorf("function BindToInterface is not supported on this platform")
	}
}
