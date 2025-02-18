// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !linux

package network

import "errors"

const unixRtmgrpLink uint32 = 0

func newEthToolIoctlClient() (EthToolIoctlClient, error) {
	return nil, errors.New("ethtool is not supported on this platform")
}
