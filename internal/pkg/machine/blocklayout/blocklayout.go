// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package blocklayout builds the emulated partition layout of a machine's system disk,
// so that the discovered volumes, the volume statuses and the mount statuses all
// describe the same disk: the one Talos was installed to.
package blocklayout

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

// DiskSize is the size of every emulated disk.
const DiskSize = 50 * 1024 * 1024 * 1024

// Partition indexes of the layout, shared with the mount statuses which point at
// "<system disk dev path><index>".
const (
	StatePartitionIndex     = 4
	EphemeralPartitionIndex = 5
)

const (
	partType = "part"

	ephemeralOffset = 304 * 1024 * 1024
	ephemeralSize   = DiskSize - ephemeralOffset
)

// Build returns the discovered volumes and volume statuses describing a Talos
// installation on the given disk.
func Build(diskName string) []resource.Resource {
	devPath := "/dev/" + diskName

	partition := func(index int, label string, offset, size uint64) *block.DiscoveredVolume {
		name := fmt.Sprintf("%s%d", diskName, index)

		volume := block.NewDiscoveredVolume(block.NamespaceName, name)
		volume.TypedSpec().Type = partType
		volume.TypedSpec().DevPath = fmt.Sprintf("%s%d", devPath, index)
		volume.TypedSpec().DevicePath = volume.TypedSpec().DevPath
		volume.TypedSpec().Parent = diskName
		volume.TypedSpec().ParentDevPath = devPath
		volume.TypedSpec().Name = name
		volume.TypedSpec().PartitionLabel = label
		volume.TypedSpec().PartitionIndex = uint(index)
		volume.TypedSpec().Offset = offset
		volume.TypedSpec().SetSize(size)

		return volume
	}

	volumeStatus := func(index int, label, mountLocation string, size uint64) *block.VolumeStatus {
		volume := block.NewVolumeStatus(block.NamespaceName, label)
		volume.TypedSpec().Phase = block.VolumePhaseReady
		volume.TypedSpec().Type = block.VolumeTypePartition
		volume.TypedSpec().Location = fmt.Sprintf("%s%d", devPath, index)
		volume.TypedSpec().MountLocation = mountLocation
		volume.TypedSpec().ParentLocation = devPath
		volume.TypedSpec().PartitionIndex = index
		volume.TypedSpec().Filesystem = block.FilesystemTypeXFS
		volume.TypedSpec().SetSize(size)

		return volume
	}

	return []resource.Resource{
		partition(1, "EFI", 1024*1024, 100*1024*1024),
		partition(2, "BOOT", 101*1024*1024, 100*1024*1024),
		partition(3, "META", 202*1024*1024, 2*1024*1024),
		partition(StatePartitionIndex, constants.StatePartitionLabel, 204*1024*1024, 100*1024*1024),
		partition(EphemeralPartitionIndex, constants.EphemeralPartitionLabel, ephemeralOffset, ephemeralSize),
		volumeStatus(StatePartitionIndex, constants.StatePartitionLabel, "/system/state", 100*1024*1024),
		volumeStatus(EphemeralPartitionIndex, constants.EphemeralPartitionLabel, "/var", ephemeralSize),
	}
}

// Move places the emulated partition layout onto the given disk, removing it from
// whatever disk held it before. Used when installing, since the machines are seeded
// with the layout on their boot disk while the install can target any disk.
func Move(ctx context.Context, st state.State, diskName string) error {
	volumes, err := safe.StateListAll[*block.DiscoveredVolume](ctx, st)
	if err != nil {
		return err
	}

	for volume := range volumes.All() {
		if volume.TypedSpec().Type != partType {
			continue
		}

		if err = st.Destroy(ctx, volume.Metadata()); err != nil && !state.IsNotFoundError(err) {
			return err
		}
	}

	for _, label := range []string{constants.StatePartitionLabel, constants.EphemeralPartitionLabel} {
		if err = st.Destroy(ctx, block.NewVolumeStatus(block.NamespaceName, label).Metadata()); err != nil && !state.IsNotFoundError(err) {
			return err
		}
	}

	for _, res := range Build(diskName) {
		if err = st.Create(ctx, res); err != nil {
			return err
		}
	}

	return nil
}
