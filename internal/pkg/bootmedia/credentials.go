// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"fmt"
	"os"

	factoryclient "github.com/siderolabs/image-factory/pkg/client"

	emuconstants "github.com/siderolabs/talemu/internal/pkg/constants"
)

// Credentials authenticate the schematic reads: an API token or a basic auth pair, both optional.
//
// An infra provider normally needs no factory credentials. The emulator is the exception because it stands
// in for Talos: it reads schematics to report the extensions and kernel args a real machine would, and an
// enterprise factory only answers a schematic read to a full credential. Neither the download token Omni
// hands out with an installation medium nor the machine token it puts into machine configs is one - the
// factory accepts those on image and registry paths only - so the emulator has to be given the same token
// Omni itself holds.
type Credentials struct {
	Username string
	Password string
	Token    string
}

// CredentialsFromEnv reads the credentials from the TALEMU_IMAGE_FACTORY_* environment variables.
func CredentialsFromEnv() Credentials {
	return Credentials{
		Username: os.Getenv(emuconstants.ImageFactoryUsernameEnv),
		Password: os.Getenv(emuconstants.ImageFactoryPasswordEnv),
		Token:    os.Getenv(emuconstants.ImageFactoryTokenEnv),
	}
}

// IsZero reports whether no credentials are set.
func (c Credentials) IsZero() bool {
	return c == Credentials{}
}

// clientOptions returns the factory client options sending these credentials, none when they are empty.
func (c Credentials) clientOptions() ([]factoryclient.Option, error) {
	switch {
	case c.Token != "" && (c.Username != "" || c.Password != ""):
		return nil, fmt.Errorf("either an image factory API token or a basic auth pair is expected, not both")
	case c.Token != "":
		return []factoryclient.Option{factoryclient.WithBearerToken(c.Token)}, nil
	case c.Username != "" || c.Password != "":
		if c.Username == "" || c.Password == "" {
			return nil, fmt.Errorf("both username and password are required when using image factory basic auth")
		}

		return []factoryclient.Option{factoryclient.WithBasicAuth(c.Username, c.Password)}, nil
	default:
		return nil, nil
	}
}
