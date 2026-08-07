// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAPIEndpoint(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		endpoint     string
		expectedHost string
		insecureConn bool
	}{
		{
			name:         "plain grpc",
			endpoint:     "grpc://192.168.0.1:8090",
			expectedHost: "192.168.0.1:8090",
			insecureConn: true,
		},
		{
			name:         "grpc with query params",
			endpoint:     "grpc://omni.example.org:8090?jointoken=secret&grpc_tunnel=true",
			expectedHost: "omni.example.org:8090",
			insecureConn: true,
		},
		{
			name:         "https",
			endpoint:     "https://omni.example.org:8091?jointoken=secret",
			expectedHost: "omni.example.org:8091",
			insecureConn: false,
		},
		{
			name:         "https without port defaults to 443",
			endpoint:     "https://omni.example.org?jointoken=secret&grpc_tunnel=true",
			expectedHost: "omni.example.org:443",
			insecureConn: false,
		},
		{
			name:         "no scheme defaults to insecure grpc",
			endpoint:     "192.168.0.1:8090",
			expectedHost: "192.168.0.1:8090",
			insecureConn: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			host, insecureConn, err := parseAPIEndpoint(test.endpoint)
			require.NoError(t, err)

			assert.Equal(t, test.expectedHost, host)
			assert.Equal(t, test.insecureConn, insecureConn)
		})
	}
}

func TestErrCause(t *testing.T) {
	t.Parallel()

	t.Run("plain cancellation is not an error", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		assert.NoError(t, errCause(ctx))
	})

	t.Run("cancellation with a cause is returned", func(t *testing.T) {
		t.Parallel()

		errTunnelDied := errors.New("tunnel device failed")

		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(errTunnelDied)

		assert.ErrorIs(t, errCause(ctx), errTunnelDied)
	})
}
