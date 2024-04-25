// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"context"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"google.golang.org/protobuf/types/known/emptypb"
)

type machineService struct {
	machine.UnimplementedMachineServiceServer
	storage.UnimplementedStorageServiceServer

	state state.State
}

// Disks implements storage.StorageServiceServer.
func (c *machineService) Disks(context.Context, *emptypb.Empty) (*storage.DisksResponse, error) {
	return &storage.DisksResponse{
		Messages: []*storage.Disks{
			{
				Metadata: &common.Metadata{},
				Disks: []*storage.Disk{
					{
						Size:       50 * 1024 * 1024 * 1024,
						DeviceName: "/dev/sda",
						Model:      "CM5514",
						Type:       storage.Disk_HDD,
						BusPath:    "/pci0000:00/0000:00:05.0/0000:01:01.0/virtio2/host2/target2:0:0/2:0:0:0/",
					},
				},
			},
		},
	}, nil
}
