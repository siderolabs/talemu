// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package schematic provides a service to translate schematic IDs to schematics.
package schematic

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

type Service struct {
	sf            singleflight.Group
	logger        *zap.Logger
	factoryClient *client.Client

	cacheDir         string
	imageFactoryHost string
}

// NewService creates a schematic service. The credentials are optional and used as basic auth
// against the image factory when set, as enterprise image factories require authentication for
// schematic reads.
func NewService(cacheDir, imageFactoryBaseURL, username, password string, logger *zap.Logger) (*Service, error) {
	factoryURL, err := url.Parse(imageFactoryBaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid image factory URL %q: %w", imageFactoryBaseURL, err)
	}

	if factoryURL.Scheme == "" || factoryURL.Host == "" {
		return nil, fmt.Errorf("invalid image factory URL %q: scheme and host are required", imageFactoryBaseURL)
	}

	var clientOpts []client.Option

	if username != "" {
		clientOpts = append(clientOpts, client.WithBasicAuth(username, password))
	}

	factoryClient, err := client.New(imageFactoryBaseURL, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory client: %w", err)
	}

	if cacheDir == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user cache dir: %w", err)
		}

		cacheDir = filepath.Join(userCacheDir, "talemu-schematics")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		cacheDir:         cacheDir,
		factoryClient:    factoryClient,
		imageFactoryHost: factoryURL.Host,
		logger:           logger,
	}, nil
}

// ImageFactoryBaseURL returns the base URL of the configured image factory client.
func (svc *Service) ImageFactoryBaseURL() string {
	return svc.factoryClient.BaseURL()
}

// ImageFactoryHost returns the configured image factory host.
func (svc *Service) ImageFactoryHost() string {
	return svc.imageFactoryHost
}

// normalizeFactoryURL puts a factory base URL in a comparable form, so that two
// spellings of the same factory are recognized as one.
//
// The two URLs being compared reach the emulator from different places, the
// command line of this process and the provider data of a machine, so a trailing
// slash or a differently cased host is likely. Treating those as different
// factories would silently drop the credentials belonging to the configured one.
func normalizeFactoryURL(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")

	return parsed.String()
}

// clientFor returns the factory client for a base URL, falling back to the
// configured factory when the URL is empty or is that same factory.
//
// Credentials belong to the configured factory and are only ever sent there. A
// different factory gets an unauthenticated client, since there is no reason to
// believe one factory's credentials are valid at another, and every reason not to
// send them somewhere they were not issued for.
func (svc *Service) clientFor(baseURL string) (*client.Client, error) {
	normalized := normalizeFactoryURL(baseURL)

	if normalized == "" || normalized == normalizeFactoryURL(svc.factoryClient.BaseURL()) {
		return svc.factoryClient, nil
	}

	c, err := client.New(normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory client for %q: %w", baseURL, err)
	}

	return c, nil
}

// GetByID reads a schematic from the factory that built the boot media it
// describes. An empty factoryBaseURL means the configured factory.
func (svc *Service) GetByID(ctx context.Context, id, factoryBaseURL string) (*schematic.Schematic, error) {
	factoryClient, err := svc.clientFor(factoryBaseURL)
	if err != nil {
		return nil, err
	}

	// Coalesced per factory and ID, never by ID alone: on a miss the flight's result
	// is that one factory's answer, and handing its 404 to a caller asking a
	// different factory would fail a read that factory could have served. The disk
	// cache below stays keyed by ID alone, since a schematic ID is the hash of the
	// schematic's canonical form, so any factory holding it holds the same content.
	ch := svc.sf.DoChan(factoryClient.BaseURL()+" "+id, func() (any, error) {
		return svc.getByID(ctx, id, factoryClient)
	})

	var res singleflight.Result

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res = <-ch:
	}

	if res.Err != nil {
		return nil, res.Err
	}

	sch, ok := res.Val.(*schematic.Schematic)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T; want *schematic.Schematic", res.Val)
	}

	return sch, nil
}

func (svc *Service) getByID(ctx context.Context, id string, factoryClient *client.Client) (*schematic.Schematic, error) {
	// Schematics can carry a join token, so the cache is kept readable only by the
	// user running the emulator.
	if err := os.MkdirAll(svc.cacheDir, 0o700); err != nil {
		return nil, err
	}

	svc.logger.Info("get schematic", zap.String("id", id), zap.String("factory", factoryClient.BaseURL()))

	filePath := filepath.Join(svc.cacheDir, id+".yaml")

	fileBytes, readErr := os.ReadFile(filePath)
	if readErr == nil {
		svc.logger.Info("cache hit, return cached schematic", zap.String("id", id), zap.String("path", filePath))

		return schematic.Unmarshal(fileBytes)
	}

	if !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read schematic file %q: %w", filePath, readErr)
	}

	svc.logger.Info("cache miss, download schematic", zap.String("id", id), zap.String("factory", factoryClient.BaseURL()))

	// doesn't exist, get schematic

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	schematic, err := factoryClient.SchematicGet(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get schematic from factory %s: %w", factoryClient.BaseURL(), err)
	}

	rawSchematic, err := schematic.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schematic: %w", err)
	}

	destFile := filepath.Join(svc.cacheDir, id+".yaml.tmp")

	if err = os.WriteFile(destFile, rawSchematic, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write schematic file %q: %w", destFile, err)
	}

	if err = os.Rename(destFile, filePath); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to rename schematic file %q to %q: %w", destFile, filePath, err)
	}

	return schematic, nil
}
