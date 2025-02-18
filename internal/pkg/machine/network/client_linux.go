// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build linux

package network

import (
	ethtoolioctl "github.com/safchain/ethtool"
	"golang.org/x/sys/unix"
)

const unixRtmgrpLink uint32 = unix.RTMGRP_LINK

func newEthToolIoctlClient() (EthToolIoctlClient, error) {
	ethTool, err := ethtoolioctl.NewEthtool()
	if err != nil {
		return nil, err
	}

	return &ethToolWrapper{
		ethTool: ethTool,
	}, nil
}

type ethToolWrapper struct {
	ethTool *ethtoolioctl.Ethtool
}

func (e *ethToolWrapper) Close() {
	e.ethTool.Close()
}

func (e *ethToolWrapper) DriverInfo(iface string) (DriverInfo, error) {
	driverInfo, err := e.ethTool.DriverInfo(iface)
	if err != nil {
		return DriverInfo{}, err
	}

	return DriverInfo{
		Driver:    driverInfo.Driver,
		Version:   driverInfo.Version,
		FwVersion: driverInfo.FwVersion,
		BusInfo:   driverInfo.BusInfo,
	}, nil
}

func (e *ethToolWrapper) PermAddr(iface string) (string, error) {
	return e.ethTool.PermAddr(iface)
}
