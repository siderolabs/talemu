// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package initramfs

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/siderolabs/talos/pkg/machinery/version"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/constants"
)

// HTTPSource is an initramfs source that downloads the initramfs from a HTTP endpoint, for example, from
// https://factory.talos.dev/image/<schematic-id>/v1.11.2/initramfs-amd64.xz.
type HTTPSource struct {
	logger *zap.Logger
}

// NewHTTPSource creates a new HTTPSource.
func NewHTTPSource(logger *zap.Logger) *HTTPSource {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &HTTPSource{
		logger: logger,
	}
}

// Get implements the InitramfsSource interface.
func (svc *HTTPSource) Get(ctx context.Context, schematicID string) (io.ReadCloser, error) {
	initramfsURL := fmt.Sprintf("https://%s/image/%s/%s/initramfs-amd64.xz", constants.ImageFactoryHost, schematicID, version.Tag)

	svc.logger.Info("download initramfs", zap.String("url", initramfsURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, initramfsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download initramfs: %w", err)
	}

	return resp.Body, nil
}
