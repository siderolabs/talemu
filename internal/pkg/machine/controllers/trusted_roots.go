// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"crypto/x509"
	"fmt"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// trustedRoots returns the CAs the machine trusts, like Talos: the system ones plus the ones in its active machine
// configuration.
//
// It reads the state directly rather than through a controller input, the way Talos reads its trust bundle when it
// dials, so a configuration change does not make the SideroLink connection provision again.
func trustedRoots(ctx context.Context, st state.State) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("failed to load the system CAs: %w", err)
	}

	cfg, err := safe.StateGetByID[*config.MachineConfig](ctx, st, config.ActiveID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return pool, nil
		}

		return nil, fmt.Errorf("failed to get the machine config: %w", err)
	}

	if roots := cfg.Config().TrustedRoots(); roots != nil {
		for _, certificates := range roots.ExtraTrustedRootCertificates() {
			pool.AppendCertsFromPEM([]byte(certificates))
		}
	}

	return pool, nil
}
