// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network

import (
	"context"
	"fmt"

	"github.com/jsimonetti/rtnetlink"
	"github.com/mdlayher/ethtool"
	ethtoolioctl "github.com/safchain/ethtool"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/siderolabs/talemu/internal/pkg/machine/network/watch"
)

// Client defines a shared network client for all machines.
type Client struct {
	rtnetlinkWatcher watch.Watcher
	ethtoolWatcher   watch.Watcher
	rtnetlinkConn    *rtnetlink.Conn
	ethIoctlClient   *ethtoolioctl.Ethtool
	ethClient        *ethtool.Client
	wgClient         *wgctrl.Client

	listeners map[string]func()
}

// NewClient creates a new network client.
func NewClient() *Client {
	return &Client{
		listeners: make(map[string]func()),
	}
}

// QueueReconcile implements watch.Trigger.
func (nc *Client) QueueReconcile() {
	for _, listener := range nc.listeners {
		listener()
	}
}

// AddListener adds watches listener.
func (nc *Client) AddListener(id string, cb func()) {
	nc.listeners[id] = cb
}

// RemoveListener removes watches listener.
func (nc *Client) RemoveListener(id string) {
	delete(nc.listeners, id)
}

// Run starts the network clients.
func (nc *Client) Run(ctx context.Context) error {
	var err error

	// create watch connections to rtnetlink and ethtool via genetlink
	// these connections are used only to join multicast groups and receive notifications on changes
	// other connections are used to send requests and receive responses, as we can't mix the notifications and request/responses
	nc.rtnetlinkWatcher, err = watch.NewRtNetlink(watch.NewDefaultRateLimitedTrigger(ctx, nc), unix.RTMGRP_LINK)
	if err != nil {
		return err
	}

	nc.ethtoolWatcher, err = watch.NewEthtool(watch.NewDefaultRateLimitedTrigger(ctx, nc))
	if err != nil {
		return err
	}

	nc.rtnetlinkConn, err = rtnetlink.Dial(nil)
	if err != nil {
		return fmt.Errorf("error dialing rtnetlink socket: %w", err)
	}

	nc.ethClient, err = ethtool.New()
	if err != nil {
		return err
	}

	nc.ethIoctlClient, err = ethtoolioctl.NewEthtool()
	if err != nil {
		return err
	}

	nc.wgClient, err = wgctrl.New()
	if err != nil {
		return err
	}

	return nil
}

// Close all underlying clients.
func (nc *Client) Close() error {
	if err := nc.ethClient.Close(); err != nil {
		return err
	}

	if err := nc.wgClient.Close(); err != nil {
		return err
	}

	nc.ethIoctlClient.Close()
	nc.ethtoolWatcher.Done()
	nc.rtnetlinkWatcher.Done()

	return nc.rtnetlinkConn.Close()
}

// Conn returns rtnetlink conn.
func (nc *Client) Conn() *rtnetlink.Conn {
	return nc.rtnetlinkConn
}

// EthIoCtl client.
func (nc *Client) EthIoCtl() *ethtoolioctl.Ethtool {
	return nc.ethIoctlClient
}

// Eth client.
func (nc *Client) Eth() *ethtool.Client {
	return nc.ethClient
}

// Wg client.
func (nc *Client) Wg() *wgctrl.Client {
	return nc.wgClient
}
