// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !linux

package watch

import "errors"

// NewEthtool returns an error on non-linux systems.
func NewEthtool(Trigger) (Watcher, error) {
	return nil, errors.New("ethtool watch is not supported on non-linux systems")
}
