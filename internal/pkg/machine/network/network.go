// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package network contains utility methods for setting up emulator network.
package network

import (
	"syscall"
)

// BindToInterface binds the socket to the interface.
func BindToInterface(iface string) func(_, _ string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var err error

		fn := func(fd uintptr) {
			err = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
		}

		if err = c.Control(fn); err != nil {
			return err
		}

		if err != nil {
			return err
		}

		return nil
	}
}
