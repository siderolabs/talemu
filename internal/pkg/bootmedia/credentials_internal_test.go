// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"testing"

	"github.com/stretchr/testify/require"

	emuconstants "github.com/siderolabs/talemu/internal/pkg/constants"
)

func TestCredentialsClientOptions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		creds   Credentials
		options int
		wantErr bool
	}{
		{name: "none", creds: Credentials{}},
		{name: "token", creds: Credentials{Token: "omni-token"}, options: 1},
		{name: "basic auth", creds: Credentials{Username: "user", Password: "hunter2"}, options: 1},
		{name: "username only", creds: Credentials{Username: "user"}, wantErr: true},
		{name: "password only", creds: Credentials{Password: "hunter2"}, wantErr: true},
		{name: "token and basic auth", creds: Credentials{Username: "user", Password: "hunter2", Token: "omni-token"}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := tt.creds.clientOptions()
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.Len(t, opts, tt.options)
			require.Equal(t, tt.options == 0, tt.creds.IsZero())
		})
	}
}

func TestCredentialsFromEnv(t *testing.T) { //nolint:paralleltest // sets the environment
	require.True(t, CredentialsFromEnv().IsZero())

	t.Setenv(emuconstants.ImageFactoryTokenEnv, "omni-token")

	require.Equal(t, Credentials{Token: "omni-token"}, CredentialsFromEnv())

	t.Setenv(emuconstants.ImageFactoryUsernameEnv, "user")
	t.Setenv(emuconstants.ImageFactoryPasswordEnv, "hunter2")

	require.Equal(t, Credentials{Username: "user", Password: "hunter2", Token: "omni-token"}, CredentialsFromEnv())
}
