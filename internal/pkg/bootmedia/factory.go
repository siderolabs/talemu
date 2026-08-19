// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"context"
	"fmt"
	"net/url"

	factoryclient "github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"go.uber.org/zap"
)

// FactorySource is a single image factory the emulator was pointed at directly.
//
// This is the source for running without Omni, and the only one that takes credentials of its own. There is
// nobody else to ask for them here.
type FactorySource struct {
	client *factoryclient.Client
	reader *schematicReader
	probe  *enterpriseProbe

	baseURL string
	host    string
}

// NewFactorySource creates a source reading from the image factory at the given base URL. The credentials are
// optional and sent as basic auth when set, which an enterprise image factory requires for schematic reads.
func NewFactorySource(cacheDir, baseURL, username, password string, logger *zap.Logger) (*FactorySource, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid image factory URL %q: %w", baseURL, err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid image factory URL %q: scheme and host are required", baseURL)
	}

	var opts []factoryclient.Option

	if username != "" || password != "" {
		if username == "" || password == "" {
			return nil, fmt.Errorf("both username and password are required when using image factory basic auth")
		}

		opts = append(opts, factoryclient.WithBasicAuth(username, password))
	}

	client, err := factoryclient.New(baseURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory client: %w", err)
	}

	source := &FactorySource{
		client:  client,
		baseURL: baseURL,
		host:    parsed.Host,
		probe:   newEnterpriseProbe(),
	}

	if source.reader, err = newSchematicReader(cacheDir, source.clientFor, nil, logger); err != nil {
		return nil, err
	}

	return source, nil
}

// BaseURL returns the base URL of the configured image factory.
func (f *FactorySource) BaseURL() string {
	return f.baseURL
}

// GetSchematicByID implements Source.
func (f *FactorySource) GetSchematicByID(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error) {
	return f.reader.read(ctx, id, talosVersion, factoryHost)
}

// FactoryHosts implements Source.
func (f *FactorySource) FactoryHosts() []string {
	return []string{f.host}
}

// FactoryHostForVersion implements Source. There is only one factory here, and it serves every version.
func (f *FactorySource) FactoryHostForVersion(context.Context, string) (string, error) {
	return f.host, nil
}

// IsEnterprise implements Source. Nothing here holds a record of the factory, so the factory is asked directly.
func (f *FactorySource) IsEnterprise(ctx context.Context, _, factoryHost string) (bool, error) {
	return f.probe.isEnterprise(ctx, f.probeURLFor(factoryHost))
}

// probeURLFor builds the URL to probe for the given image host, empty when the image came from no factory.
//
// Image references carry no scheme, so the URL defaults to HTTPS, except when the host is the configured
// factory: then its base URL is used as-is, keeping a plain-HTTP factory working.
func (f *FactorySource) probeURLFor(factoryHost string) string {
	switch factoryHost {
	case "":
		return ""
	case f.host:
		return f.baseURL
	default:
		return "https://" + factoryHost
	}
}

// clientFor returns the client to read a schematic with.
//
// Credentials belong to the configured factory and are only ever sent there. A different factory gets an
// unauthenticated client, since there is no reason to believe one factory's credentials are valid at
// another, and every reason not to send them somewhere they were not issued for.
func (f *FactorySource) clientFor(_ context.Context, _, _, factoryHost string) (*factoryclient.Client, error) {
	if factoryHost == "" || factoryHost == f.host {
		return f.client, nil
	}

	client, err := factoryclient.New("https://" + factoryHost)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory client for %q: %w", factoryHost, err)
	}

	return client, nil
}
