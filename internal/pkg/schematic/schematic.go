// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package schematic provides a service to translate schematic IDs to schematics.
package schematic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/siderolabs/image-factory/pkg/client"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
)

type Service struct {
	sf       singleflight.Group
	logger   *zap.Logger
	cacheDir string
}

func NewService(cacheDir string, logger *zap.Logger) (*Service, error) {
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
		cacheDir: cacheDir,
		logger:   logger,
	}, nil
}

func (svc *Service) GetByID(ctx context.Context, id string) (*schematic.Schematic, error) {
	ch := svc.sf.DoChan(id, func() (any, error) {
		return svc.getByID(ctx, id)
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

func (svc *Service) getByID(ctx context.Context, id string) (*schematic.Schematic, error) {
	baseURL := "https://" + emuconst.ImageFactoryHost

	svc.logger.Info("get schematic", zap.String("id", id))

	filePath := filepath.Join(svc.cacheDir, id+".yaml")

	fileBytes, readErr := os.ReadFile(filePath)
	if readErr == nil {
		svc.logger.Info("cache hit, return cached schematic", zap.String("id", id), zap.String("path", filePath))

		return schematic.Unmarshal(fileBytes)
	}

	if !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read schematic file %q: %w", filePath, readErr)
	}

	svc.logger.Info("cache miss, download schematic", zap.String("id", id))

	// doesn't exist, get schematic

	factoryClient, err := client.New(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	schematic, err := factoryClient.SchematicGet(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get schematic from factory: %w", err)
	}

	rawSchematic, err := schematic.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schematic: %w", err)
	}

	destFile := filepath.Join(svc.cacheDir, id+".yaml.tmp")

	if err = os.WriteFile(destFile, rawSchematic, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write schematic file %q: %w", destFile, err)
	}

	if err = os.Rename(destFile, filePath); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to rename schematic file %q to %q: %w", destFile, filePath, err)
	}

	return schematic, nil
}
