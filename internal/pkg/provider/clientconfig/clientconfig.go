// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package clientconfig holds the configuration for the test client for Omni API.
package clientconfig

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/adrg/xdg"
	authcli "github.com/siderolabs/go-api-signature/pkg/client/auth"
	"github.com/siderolabs/go-api-signature/pkg/client/interceptor"
	"github.com/siderolabs/go-api-signature/pkg/message"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"github.com/siderolabs/omni/client/pkg/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	defaultEmail = "test-user@siderolabs.com"
)

// ClientConfig is a test client.
type ClientConfig struct {
	endpoint string
}

// New creates a new test client config.
func New(endpoint string) *ClientConfig {
	return &ClientConfig{
		endpoint: endpoint,
	}
}

// GetClient returns a test client for the default test email.
func (t *ClientConfig) GetClient(publicKeyOpts ...authcli.RegisterPGPPublicKeyOption) (*client.Client, error) {
	return t.GetClientForEmail(defaultEmail, publicKeyOpts...)
}

// GetClientForEmail returns a test client for the given email.
func (t *ClientConfig) GetClientForEmail(email string, publicKeyOpts ...authcli.RegisterPGPPublicKeyOption) (*client.Client, error) {
	signatureInterceptor := buildSignatureInterceptor(email, publicKeyOpts...)

	return client.New(t.endpoint,
		client.WithInsecureSkipTLSVerify(true),
		client.WithGrpcOpts(
			grpc.WithUnaryInterceptor(signatureInterceptor.Unary()),
			grpc.WithStreamInterceptor(signatureInterceptor.Stream()),
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
				InsecureSkipVerify: true,
			})),
		),
	)
}

var talosAPIKeyMutex sync.Mutex

// TalosAPIKeyPrepare prepares a public key to be used with tests interacting via Talos API client using the default test email.
func TalosAPIKeyPrepare(ctx context.Context, client *client.Client, contextName string) error {
	return TalosAPIKeyPrepareWithEmail(ctx, client, contextName, defaultEmail)
}

// TalosAPIKeyPrepareWithEmail prepares a public key to be used with tests interacting via Talos API client using the given email.
func TalosAPIKeyPrepareWithEmail(ctx context.Context, client *client.Client, contextName, email string) error {
	talosAPIKeyMutex.Lock()
	defer talosAPIKeyMutex.Unlock()

	path, err := xdg.DataFile(filepath.Join("talos", "keys", fmt.Sprintf("%s-%s.pgp", contextName, email)))
	if err != nil {
		return err
	}

	stat, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if stat != nil && time.Since(stat.ModTime()) < 2*time.Hour {
		return nil
	}

	newKey, err := pgp.GenerateKey("", "", email, 4*time.Hour)
	if err != nil {
		return err
	}

	err = registerKey(ctx, client.Auth(), newKey, email)
	if err != nil {
		return err
	}

	keyArmored, err := newKey.Armor()
	if err != nil {
		return err
	}

	return os.WriteFile(path, []byte(keyArmored), 0o600)
}

func buildSignatureInterceptor(email string, publicKeyOpts ...authcli.RegisterPGPPublicKeyOption) *interceptor.Interceptor {
	userKeyFunc := func(ctx context.Context, cc *grpc.ClientConn, _ *interceptor.Options) (message.Signer, error) {
		newKey, err := pgp.GenerateKey("", "", email, 4*time.Hour)
		if err != nil {
			return nil, err
		}

		authCli := authcli.NewClient(cc)

		err = registerKey(ctx, authCli, newKey, email, publicKeyOpts...)
		if err != nil {
			return nil, err
		}

		return newKey, nil
	}

	return interceptor.New(interceptor.Options{
		GetUserKeyFunc:   userKeyFunc,
		RenewUserKeyFunc: userKeyFunc,
		Identity:         email,
	})
}
