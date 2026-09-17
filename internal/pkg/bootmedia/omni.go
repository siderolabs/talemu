// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"context"
	"fmt"

	factoryclient "github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/imagefactory"
	"go.uber.org/zap"
)

// OmniSource is boot media resolved through the Omni instance the emulator is connected to.
//
// Omni already knows which image factory serves which Talos version, so this source configures no factory.
// It reads the endpoints from resources any signed client may read.
//
// What it cannot get from Omni is a credential to read a schematic with. Omni hands an infra provider only
// what a provider needs: a download token inside an installation media URL, which the factory accepts for
// images and PXE scripts and nothing else. So the credentials are given explicitly and sent to the factory
// Omni serves the Talos version from, the way the standalone emulator sends them to its configured factory.
type OmniSource struct {
	factories factoryDirectory
	reader    *schematicReader
	logger    *zap.Logger

	// clientOptions authenticate a client to the factory Omni serves a Talos version from. Empty when no
	// credentials were given, which reads anonymously and is all a community factory needs.
	clientOptions []factoryclient.Option

	// probe asks a factory whether it is an enterprise build, which is the only thing here that the factory
	// itself is the authority on.
	probe *enterpriseProbe

	hosts []string
}

// NewOmniSource creates a source backed by Omni.
//
// The credentials are optional, but an enterprise factory answers a schematic read to nothing else, so
// running against one without them is warned about here rather than discovered one machine at a time.
func NewOmniSource(ctx context.Context, cacheDir string, c *client.Client, creds Credentials, logger *zap.Logger) (*OmniSource, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	clientOptions, err := creds.clientOptions()
	if err != nil {
		return nil, err
	}

	clients, err := imagefactory.NewClientsFromState(ctx, c.Omni().State())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the image factories Omni is configured with: %w", err)
	}

	factories := factoryDirectory{clients: clients}

	hosts := factories.hosts()

	source := &OmniSource{
		factories:     factories,
		logger:        logger,
		clientOptions: clientOptions,
		probe:         newEnterpriseProbe(),
		hosts:         hosts,
	}

	if source.reader, err = newSchematicReader(cacheDir, source.clientFor, logger); err != nil {
		return nil, err
	}

	logger.Info("resolved the image factories Omni is configured with",
		zap.Strings("hosts", hosts), zap.Bool("credentials", len(clientOptions) > 0))

	if len(clientOptions) == 0 {
		source.warnIfEnterprise(ctx)
	}

	return source, nil
}

// warnIfEnterprise logs a warning for every configured factory that is an enterprise build, since without
// credentials every schematic read from it is going to be refused.
//
// A factory that cannot be probed is left alone: the reads themselves report what is wrong with it.
func (o *OmniSource) warnIfEnterprise(ctx context.Context) {
	for _, host := range o.hosts {
		enterprise, err := o.probe.isEnterprise(ctx, o.factories.probeURLFor(host))
		if err != nil {
			o.logger.Debug("failed to probe the image factory for its edition", zap.String("host", host), zap.Error(err))

			continue
		}

		if enterprise {
			o.logger.Warn("no image factory credentials are configured, and this factory is an enterprise build "+
				"that refuses schematic reads without them: the machines will not be able to report their extensions and kernel args",
				zap.String("host", host))
		}
	}
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

// clientFor returns a client for the factory the image came from.
//
// Routed by the host on the image reference, which is where the schematic actually is. Routing by Talos version
// instead would follow Omni to whichever factory serves that version today, and a machine whose image was built
// before Omni moved the version would be asked of a factory that never had its schematic.
//
// The credentials belong to the factory Omni serves this version from, so that is the only one they are sent
// to. Any other factory gets an anonymous client, which is also all the old code ever had, and a schematic ID is
// a content hash so any factory holding it holds the same content.
func (o *OmniSource) clientFor(ctx context.Context, _, talosVersion, factoryHost string) (*factoryclient.Client, error) {
	serving, err := o.factories.forTalosVersion(ctx, talosVersion)
	if err != nil {
		return nil, err
	}

	if factoryHost == "" || factoryHost == serving.host {
		return factoryclient.New(serving.baseURL, o.clientOptions...)
	}

	baseURL := o.factories.probeURLFor(factoryHost)

	o.logger.Debug("reading a schematic from the factory the image came from, which is not the one serving this Talos version",
		zap.String("talos_version", talosVersion),
		zap.String("image_host", factoryHost),
		zap.String("serving_host", serving.host))

	return factoryclient.New(baseURL)
}
