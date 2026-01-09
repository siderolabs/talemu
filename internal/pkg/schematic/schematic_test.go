// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package schematic_test

import (
	"context"
	"testing"
	"time"

	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	schematicsvc "github.com/siderolabs/talemu/internal/pkg/schematic"
)

const (
	expectedID        = "dc1c492eafbbbdf85e25f11b67a4296f55163752491ae31d0a83d8b6f20973ee"
	expectedSchematic = `customization:
    extraKernelArgs:
        - foo=bar
        - bar=baz
    systemExtensions:
        officialExtensions:
            - siderolabs/hello-world-service
            - siderolabs/qemu-guest-agent`
)

func TestGetByID(t *testing.T) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	cacheDir := t.TempDir()

	logger.Info("test dir", zap.String("dir", cacheDir))

	schematicService, err := schematicsvc.NewService(cacheDir, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	before := time.Now()

	sch, err := schematicService.GetByID(ctx, expectedID)
	require.NoError(t, err)

	after := time.Now()

	marshaled1, err := sch.Marshal()
	require.NoError(t, err)

	logger.Info("schematic", zap.ByteString("data", marshaled1), zap.Duration("duration", after.Sub(before)))

	before = time.Now()

	sch, err = schematicService.GetByID(ctx, expectedID)
	require.NoError(t, err)

	after = time.Now()

	marshaled2, err := sch.Marshal()
	require.NoError(t, err)

	expected, err := schematic.Unmarshal([]byte(expectedSchematic))
	require.NoError(t, err)

	expectedMarshaled, err := expected.Marshal()
	require.NoError(t, err)

	assert.Equal(t, expectedMarshaled, marshaled1)
	assert.Equal(t, marshaled1, marshaled2)

	logger.Info("schematic", zap.ByteString("data", marshaled2), zap.Duration("duration", after.Sub(before)))
}
