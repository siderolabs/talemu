// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package clientconfig

import (
	"context"
	"time"

	"github.com/siderolabs/go-api-signature/pkg/client/auth"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"google.golang.org/grpc/metadata"
)

func registerKey(ctx context.Context, cli *auth.Client, key *pgp.Key, email string, opts ...auth.RegisterPGPPublicKeyOption) error {
	armoredPublicKey, err := key.ArmorPublic()
	if err != nil {
		return err
	}

	_, err = cli.RegisterPGPPublicKey(ctx, email, []byte(armoredPublicKey), opts...)
	if err != nil {
		return err
	}

	debugCtx := metadata.AppendToOutgoingContext(ctx, "x-sidero-debug-verified-email", email)

	err = cli.ConfirmPublicKey(debugCtx, key.Fingerprint())
	if err != nil {
		return err
	}

	timeoutCtx, timeoutCtxCancel := context.WithTimeout(ctx, 10*time.Second)
	defer timeoutCtxCancel()

	return cli.AwaitPublicKeyConfirmation(timeoutCtx, key.Fingerprint())
}
