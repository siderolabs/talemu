// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package initramfs

import (
	"archive/tar"
	"bytes"
	"context"
	"debug/pe"
	"fmt"
	"io"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/authn/github"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/version"
	"go.uber.org/zap"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
)

// ImageSource is an initramfs source that extracts the initramfs from the vmlinuz.efi in the installer image, for example,
// factory.talos.dev/metal-installer/<schematic-id>:v1.11.2.
//
// It first tries to find the image in the local Docker daemon, and if it fails, pulls it from the remote registry and tries to write it to the local daemon.
type ImageSource struct {
	logger *zap.Logger
}

// NewImageSource creates a new ImageSource.
func NewImageSource(logger *zap.Logger) *ImageSource {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &ImageSource{
		logger: logger,
	}
}

func (svc *ImageSource) Get(ctx context.Context, schematicID string) (io.ReadCloser, error) {
	targetFilePath := strings.TrimLeft(fmt.Sprintf(constants.UKIAssetPath, "amd64"), "/")

	img, err := svc.getImage(ctx, schematicID)
	if err != nil {
		return nil, fmt.Errorf("failed to get installer image: %w", err)
	}

	efiFileBytes, err := svc.findFileInImage(img, targetFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to find file in image: %w", err)
	}

	if efiFileBytes == nil {
		return nil, fmt.Errorf("file %q not found in any layer of the image", targetFilePath)
	}

	initramfsBytes, err := svc.extractInitramfsFromEFI(efiFileBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to extract initramfs from EFI file: %w", err)
	}

	return io.NopCloser(bytes.NewReader(initramfsBytes)), nil
}

func (svc *ImageSource) getImage(ctx context.Context, schematicID string) (v1.Image, error) {
	imageRefStr := fmt.Sprintf("%s/%s-installer/%s:%s", emuconst.ImageFactoryHost, constants.PlatformMetal, schematicID, version.Tag)
	platform := v1.Platform{
		Architecture: "amd64",
		OS:           "linux",
	}

	ref, err := name.ParseReference(imageRefStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", imageRefStr, err)
	}

	img, err := daemon.Image(ref, daemon.WithContext(ctx))
	if err != nil {
		svc.logger.Info("image could not be read from the local daemon, pull from remote", zap.String("image", imageRefStr), zap.Error(err))

		img, err = remote.Image(ref, remote.WithPlatform(platform), remote.WithContext(ctx), remote.WithAuthFromKeychain(
			authn.NewMultiKeychain(
				authn.DefaultKeychain,
				github.Keychain,
				google.Keychain,
			),
		))
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %q: %w", imageRefStr, err)
		}

		if err = svc.writeImageToLocalDaemon(ctx, img, imageRefStr); err != nil {
			svc.logger.Warn("failed to write image to local daemon", zap.String("image", imageRefStr), zap.Error(err))
		} else {
			svc.logger.Info("image written to local daemon", zap.String("image", imageRefStr))
		}
	} else {
		svc.logger.Info("image found in local daemon", zap.String("image", imageRefStr))
	}

	return img, nil
}

func (svc *ImageSource) writeImageToLocalDaemon(ctx context.Context, img v1.Image, imageRefStr string) error {
	tag, tagErr := name.NewTag(imageRefStr)
	if tagErr != nil {
		return fmt.Errorf("failed to parse image tag %q: %w", imageRefStr, tagErr)
	}

	if _, err := daemon.Write(tag, img, daemon.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to write image to local daemon: %w", err)
	}

	return nil
}

func (svc *ImageSource) findFileInImage(img v1.Image, filePath string) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not get image layers: %w", err)
	}

	for i := len(layers) - 1; i >= 0; i-- {
		file, found, fileErr := svc.findFileInLayer(layers[i], filePath)
		if fileErr != nil {
			return nil, fmt.Errorf("error searching for file %q in layer %d: %w", filePath, i, fileErr)
		}

		if found {
			return file, nil
		}
	}

	return nil, fmt.Errorf("could not find file %q in any layer", filePath)
}

func (svc *ImageSource) findFileInLayer(layer v1.Layer, filePath string) (data []byte, found bool, err error) {
	rc, err := layer.Uncompressed()
	if err != nil {
		return nil, false, fmt.Errorf("could not uncompress layer: %w", err)
	}

	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			svc.logger.Error("failed to close layer reader", zap.Error(closeErr))
		}
	}()

	tarReader := tar.NewReader(rc)

	for {
		header, headerErr := tarReader.Next()
		if headerErr == io.EOF {
			break
		}

		if headerErr != nil {
			return nil, false, fmt.Errorf("error reading tar header: %w", headerErr)
		}

		if header.Name == filePath {
			fileBytes, readErr := io.ReadAll(tarReader)
			if readErr != nil {
				return nil, false, fmt.Errorf("could not read file %q from layer: %w", filePath, readErr)
			}

			return fileBytes, true, nil
		}
	}

	return nil, false, nil
}

// extractInitramfsFromEFI parses a PE (Portable Executable) file, which is the format
// for EFI binaries, to find and extract the contents of a specific section.
func (svc *ImageSource) extractInitramfsFromEFI(efiBytes []byte) ([]byte, error) {
	peFile, err := pe.NewFile(bytes.NewReader(efiBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to parse PE file: %w", err)
	}

	defer func() {
		if closeErr := peFile.Close(); closeErr != nil {
			svc.logger.Error("failed to close PE file", zap.Error(closeErr))
		}
	}()

	const sectionName = ".initrd"

	for _, section := range peFile.Sections {
		if section.Name == sectionName {
			data, dataErr := section.Data()
			if dataErr != nil {
				return nil, fmt.Errorf("could not read data from section %q: %w", sectionName, dataErr)
			}

			return data, nil
		}
	}

	return nil, fmt.Errorf("section %q not found in EFI file", sectionName)
}
