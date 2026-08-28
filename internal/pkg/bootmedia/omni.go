// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	factoryclient "github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/omni/client/api/omni/management"
	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/imagefactory"
	talosconstants "github.com/siderolabs/talos/pkg/machinery/constants"
	"go.uber.org/zap"

	emuconstants "github.com/siderolabs/talemu/internal/pkg/constants"
)

// OmniSource is boot media resolved through the Omni instance the emulator is connected to.
//
// Omni already knows which image factory serves which Talos version, and it holds the credentials for it,
// so this source configures neither. It reads the endpoints from resources any signed client may read, and
// it takes the credentials from an installation media response, which is the one call that hands them to a caller
// whose role cannot read them directly.
type OmniSource struct {
	client    *client.Client
	factories factoryDirectory
	reader    *schematicReader
	logger    *zap.Logger

	// headers caches the credentials per factory base URL. They are derived from what Omni holds for that
	// factory, so currently they change only when those credentials are rotated, and re-asking per schematic read
	// would be a round trip per read.
	headers map[string]http.Header

	// probe asks a factory whether it is an enterprise build, which is the only thing here that the factory
	// itself is the authority on.
	probe *enterpriseProbe

	hosts []string

	mu sync.Mutex
}

// NewOmniSource creates a source backed by Omni.
func NewOmniSource(ctx context.Context, cacheDir string, c *client.Client, logger *zap.Logger) (*OmniSource, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	clients, err := imagefactory.NewClientsFromState(ctx, c.Omni().State())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the image factories Omni is configured with: %w", err)
	}

	factories := factoryDirectory{clients: clients}

	hosts := factories.hosts()

	source := &OmniSource{
		client:    c,
		factories: factories,
		logger:    logger,
		headers:   map[string]http.Header{},
		probe:     newEnterpriseProbe(),
		hosts:     hosts,
	}

	if source.reader, err = newSchematicReader(cacheDir, source.clientFor, source.staleCredentials, logger); err != nil {
		return nil, err
	}

	logger.Info("resolved the image factories Omni is configured with", zap.Strings("hosts", hosts))

	return source, nil
}

// GetSchematicByID implements Source.
func (o *OmniSource) GetSchematicByID(ctx context.Context, id, talosVersion, factoryHost string) (*schematic.Schematic, error) {
	return o.reader.read(ctx, id, talosVersion, factoryHost)
}

// FactoryHosts implements Source.
func (o *OmniSource) FactoryHosts() []string {
	return o.hosts
}

// IsEnterprise implements Source.
//
// The factory is asked rather than Omni. It identifies itself through the Server response header on every
// response, including the ones it refuses for want of credentials, so no credentials are needed to read it and
// the answer is about the factory the image actually came from.
//
// Omni records an equivalent flag per Talos version, but reassembling a per-factory answer out of per-version
// copies is both more code and less accurate: it cannot answer for a host Omni serves no version from, such as
// a plain registry image, and it has to pick a winner when two versions of one factory disagree.
func (o *OmniSource) IsEnterprise(ctx context.Context, _, factoryHost string) (bool, error) {
	return o.probe.isEnterprise(ctx, o.factories.probeURLFor(factoryHost))
}

// clientFor returns a client for the factory the image came from, authenticated with what Omni hands out for it.
//
// Routed by the host on the image reference, which is where the schematic actually is. Routing by Talos version
// instead would follow Omni to whichever factory serves that version today, and a machine whose image was built
// before Omni moved the version would be asked of a factory that never had its schematic.
//
// Credentials can only be requested per version though, so they are sent only to the factory Omni serves this
// version from. Any other factory gets an anonymous client, which is also all the old code ever had, and a
// schematic ID is a content hash so any factory holding it holds the same content.
func (o *OmniSource) clientFor(ctx context.Context, id, talosVersion, factoryHost string) (*factoryclient.Client, error) {
	serving, err := o.factories.forTalosVersion(ctx, talosVersion)
	if err != nil {
		return nil, err
	}

	if factoryHost == "" || factoryHost == serving.host {
		headers, headersErr := o.headersFor(ctx, serving.baseURL, id, talosVersion)
		if headersErr != nil {
			return nil, headersErr
		}

		return factoryclient.New(serving.baseURL, withHeaders(headers))
	}

	baseURL := o.factories.probeURLFor(factoryHost)

	o.logger.Debug("reading a schematic from the factory the image came from, which is not the one serving this Talos version",
		zap.String("talos_version", talosVersion),
		zap.String("image_host", factoryHost),
		zap.String("serving_host", serving.host))

	return factoryclient.New(baseURL)
}

// headersFor returns the headers to reach the factory at the given base URL with.
//
// Only ever called for the factory Omni serves this Talos version from, which is the only one it hands out
// credentials for, so what is cached under a base URL is always what authenticates a request to it.
func (o *OmniSource) headersFor(ctx context.Context, baseURL, schematicID, talosVersion string) (http.Header, error) {
	o.mu.Lock()
	cached, ok := o.headers[baseURL]
	o.mu.Unlock()

	if ok {
		return cached, nil
	}

	headers, err := o.fetchHeaders(ctx, schematicID, talosVersion)
	if err != nil {
		return nil, err
	}

	o.mu.Lock()
	o.headers[baseURL] = headers
	o.mu.Unlock()

	return headers, nil
}

// fetchHeaders asks Omni for an installation medium and keeps only the headers it answers with.
func (o *OmniSource) fetchHeaders(ctx context.Context, schematicID, talosVersion string) (http.Header, error) {
	resp, err := o.client.Management().GetInstallationMediaURL(ctx, &management.InstallationMediaURLRequest{
		TalosVersion:          talosVersion,
		SchematicId:           schematicID,
		InstallationMediaKind: management.InstallationMediaURLRequest_INSTALLATION_MEDIA_KIND_ISO,
		Platform:              talosconstants.PlatformMetal,
		Architecture:          emuconstants.EmulatedArchitecture,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get the image factory credentials from Omni: %w", err)
	}

	headers := make(http.Header, len(resp.GetHeaders()))

	for name, value := range resp.GetHeaders() {
		headers.Set(name, value)
	}

	return headers, nil
}

// withHeaders sends the given headers with every request, which is how the image factory credentials Omni
// hands out in an installation media response reach the factory.
func withHeaders(headers http.Header) factoryclient.Option {
	return func(o *factoryclient.Options) {
		if len(headers) == 0 {
			return
		}

		if o.ExtraHeaders == nil {
			o.ExtraHeaders = http.Header{}
		}

		for name, values := range headers {
			for _, value := range values {
				o.ExtraHeaders.Add(name, value)
			}
		}
	}
}

// staleCredentials drops every cached credential, so the next client built fetches them from Omni again.
//
// All of them rather than the one entry that was rejected: Omni configures at most two factories, refetching
// is a single call, and this only runs after a factory has already turned a read away.
func (o *OmniSource) staleCredentials() {
	o.mu.Lock()
	defer o.mu.Unlock()

	clear(o.headers)
}
