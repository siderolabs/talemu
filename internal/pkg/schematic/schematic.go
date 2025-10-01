// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package schematic provides a service to translate schematic IDs to schematics.
package schematic

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/extensions"
	"github.com/u-root/u-root/pkg/cpio"
	"github.com/ulikunitz/xz"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gopkg.in/yaml.v3"

	"github.com/siderolabs/talemu/internal/pkg/schematic/initramfs"
)

type InitramfsSource interface {
	Get(ctx context.Context, schematicID string) (io.ReadCloser, error)
}

type Service struct {
	sf              singleflight.Group
	initramfsSource InitramfsSource
	logger          *zap.Logger
	cacheDir        string
}

func NewService(cacheDir string, useImageInitramfsSource bool, logger *zap.Logger) (*Service, error) {
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

	var initramfsSource InitramfsSource

	if useImageInitramfsSource {
		initramfsSource = initramfs.NewImageSource(logger)
	} else {
		initramfsSource = initramfs.NewHTTPSource(logger)
	}

	return &Service{
		cacheDir:        cacheDir,
		logger:          logger,
		initramfsSource: initramfsSource,
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
	if err := os.MkdirAll(svc.cacheDir, 0o755); err != nil {
		return nil, err
	}

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

	// doesn't exist, download initramfs

	initramfsReader, err := svc.initramfsSource.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get initramfs for schematic ID %q: %w", id, err)
	}

	defer func() {
		if closeErr := initramfsReader.Close(); closeErr != nil {
			svc.logger.Error("failed to close initramfs reader", zap.Error(closeErr))
		}
	}()

	bufferedReader := bufio.NewReader(initramfsReader)

	rawSchematic, err := extractRawSchematic(bufferedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to extract raw schematic: %w", err)
	}

	destFile := filepath.Join(svc.cacheDir, id+".yaml.tmp")

	if err = os.WriteFile(destFile, []byte(rawSchematic), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write schematic file %q: %w", destFile, err)
	}

	if err = os.Rename(destFile, filePath); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to rename schematic file %q to %q: %w", destFile, filePath, err)
	}

	parsedSchematic, err := schematic.Unmarshal([]byte(rawSchematic))
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal schematic: %w", err)
	}

	return parsedSchematic, nil
}

func extractRawSchematic(reader *bufio.Reader) (string, error) {
	var closeFuncs []func()

	defer func() {
		for _, c := range closeFuncs {
			c()
		}
	}()

	for {
		decReader, closeFunc, err := decompressingReadCloser(reader)
		if err != nil {
			return "", err
		}

		closeFuncs = append(closeFuncs, closeFunc)

		reader = bufio.NewReader(decReader)

		d := &discarder{r: reader}
		cpioReader := cpio.Newc.Reader(d)

		var rec cpio.Record

		for {
			if rec, err = cpioReader.ReadRecord(); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}

				return "", err
			}

			if rec.Name == strings.TrimLeft(constants.ExtensionsConfigFile, "/") {
				return parseRawFromExtensions(rec.ReaderAt)
			}
		}

		if err = eatPadding(reader); err != nil {
			return "", err
		}
	}
}

func decompressingReadCloser(in *bufio.Reader) (rdr io.Reader, closeFunc func(), err error) {
	magic, err := in.Peek(4)
	if err != nil {
		return nil, nil, err
	}

	switch {
	case bytes.Equal(magic, []byte{0xfd, '7', 'z', 'X'}): // xz
		var reader io.Reader

		if reader, err = xz.NewReader(in); err != nil {
			return nil, nil, err
		}

		return reader, func() {}, nil
	case bytes.Equal(magic, []byte{0x28, 0xb5, 0x2f, 0xfd}): // zstd
		var decoder *zstd.Decoder

		if decoder, err = zstd.NewReader(in); err != nil {
			return nil, nil, err
		}

		return decoder, decoder.Close, nil
	default:
		return in, func() {}, nil // return the original reader
	}
}

func parseRawFromExtensions(readerAt io.ReaderAt) (string, error) {
	sectionReader, ok := readerAt.(*io.SectionReader)
	if !ok {
		return "", fmt.Errorf("unexpected ReaderAt type %T; want *io.SectionReader", readerAt)
	}

	var extensionsConfig extensions.Config

	if err := yaml.NewDecoder(sectionReader).Decode(&extensionsConfig); err != nil {
		return "", err
	}

	if len(extensionsConfig.Layers) == 0 {
		return "", fmt.Errorf("extensions config has no layers")
	}

	last := extensionsConfig.Layers[len(extensionsConfig.Layers)-1]

	return last.Metadata.ExtraInfo, nil
}

// discarder is used to implement ReadAt from a Reader
// by reading, and discarding, data until the offset
// is reached. it can only go forward. it is designed
// for pipe-like files.
type discarder struct {
	r   io.Reader
	pos int64
}

// ReadAt implements ReadAt for a discarder.
// It is an error for the offset to be negative.
func (r *discarder) ReadAt(p []byte, off int64) (int, error) {
	if off-r.pos < 0 {
		return 0, errors.New("negative seek on discarder not allowed")
	}

	if off != r.pos {
		i, err := io.Copy(io.Discard, io.LimitReader(r.r, off-r.pos))
		if err != nil || i != off-r.pos {
			return 0, err
		}

		r.pos += i
	}

	n, err := io.ReadFull(r.r, p)
	if err != nil {
		return n, err
	}

	r.pos += int64(n)

	return n, err
}

var _ io.ReaderAt = &discarder{}

func eatPadding(in io.ByteScanner) error {
	for {
		b, err := in.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		if b != 0 {
			return in.UnreadByte()
		}
	}
}
