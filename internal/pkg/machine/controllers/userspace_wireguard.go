// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/siderolink/pkg/wgtunnel"
	"github.com/siderolabs/siderolink/pkg/wgtunnel/wgbind"
	"github.com/siderolabs/siderolink/pkg/wgtunnel/wggrpc"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/siderolink"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// relayRetryTimeout is how long the relay keeps retrying a failed connection internally, same value
// Talos registers its controller with.
const relayRetryTimeout = 10 * time.Second

// UserspaceWireguardController implements a controller that manages a WireGuard over gRPC tunnel in userspace.
//
// This is a port of the same-named controller in Talos: it consumes the Tunnel resource written by the
// ManagerController in tunnel mode, creates the TUN-backed userspace WireGuard device for the machine's
// SideroLink interface, and relays its traffic over the SideroLink provisioning gRPC endpoint.
type UserspaceWireguardController struct{}

// Name implements controller.Controller interface.
func (ctrl *UserspaceWireguardController) Name() string {
	return "siderolink.UserspaceWireguardController"
}

// Inputs implements controller.Controller interface.
func (ctrl *UserspaceWireguardController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      siderolink.TunnelType,
			ID:        optional.Some(siderolink.TunnelID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *UserspaceWireguardController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: network.LinkRefreshType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocyclo
func (ctrl *UserspaceWireguardController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	eg, ctx := errgroup.WithContext(ctx)

	var (
		relayRetryTimer resettableTimer
		tunnelDevice    *wgtunnel.TunnelDevice
		tunnelRelay     tunnelProps
	)

	defer func() {
		tunnelRelay.relay.Close()
		tunnelDevice.Close()
	}()

	const (
		// maxPendingServerMessages is the maximum number of messages that can be pending in the queue before blocking.
		maxPendingServerMessages = 100
		// maxPendingClientMessages is the maximum number of messages that can be pending in the ring before being overwritten.
		maxPendingClientMessages = 100
	)

	qp := wgbind.NewQueuePair(maxPendingServerMessages, maxPendingClientMessages)

	for {
		select {
		case <-ctx.Done():
			// distinguish an ordinary shutdown from a failed tunnel goroutine: the latter must be
			// returned, so the runtime restarts this controller and the device gets rebuilt
			return errCause(ctx)
		case <-r.EventCh():
		case <-relayRetryTimer.C():
			relayRetryTimer.Clear()
		}

		res, err := safe.ReaderGetByID[*siderolink.Tunnel](ctx, r, siderolink.TunnelID)
		if err != nil {
			if state.IsNotFoundError(err) {
				tunnelRelay.relay.Close()
				tunnelDevice.Close()

				continue
			}

			return fmt.Errorf("failed to read tunnel spec: %w", err)
		}

		if tunnelDevice.IsClosed() {
			tunnelDevice.Close()

			dev, devErr := wgtunnel.NewTunnelDevice(res.TypedSpec().LinkName, res.TypedSpec().MTU, qp, tunnelLogger(logger))
			if devErr != nil {
				return fmt.Errorf("failed to create tunnel device: %w", devErr)
			}

			// Store in outer scope because modifying the same variable will lead to the data race below
			tunnelDevice = dev

			logger.Info("wg over grpc tunnel device created", zap.String("link_name", res.TypedSpec().LinkName))

			// The interface appeared outside the link spec controller's own sync, and the emulator has no
			// netlink watcher: bump the link refresh so the link status and the addresses get reconciled.
			// The resource ID is this controller's own, the one named after the WireGuard kind belongs to
			// the link spec controller.
			if err = safe.WriterModify(ctx, r, network.NewLinkRefresh(network.NamespaceName, "tun"), func(res *network.LinkRefresh) error {
				res.TypedSpec().Bump()

				return nil
			}); err != nil {
				return fmt.Errorf("error bumping link refresh: %w", err)
			}

			eg.Go(func() error {
				logger.Debug("tunnel device running")
				defer logger.Debug("tunnel device exited")

				return dev.Run()
			})
		}

		dstHost, insecureConn, err := parseAPIEndpoint(res.TypedSpec().APIEndpoint)
		if err != nil {
			return fmt.Errorf("failed to parse siderolink API endpoint: %w", err)
		}

		ourAddrPort := res.TypedSpec().NodeAddress

		if tunnelRelay.relay.IsClosed() || tunnelRelay.dstHost != dstHost || tunnelRelay.ourAddrPort != ourAddrPort {
			// Reset timer because we are going to start tunnel anyway
			relayRetryTimer.Reset(0)

			tunnelRelay.relay.Close()

			logger.Info(
				"updating tunnel relay",
				zap.String("old_endpoint", tunnelRelay.dstHost),
				zap.Stringer("old_node_address", tunnelRelay.ourAddrPort),
				zap.String("new_endpoint", dstHost),
				zap.Stringer("new_node_address", ourAddrPort),
			)

			relay, relayErr := wggrpc.NewRelayToHost(dstHost, relayRetryTimeout, qp, ourAddrPort, withTransportCredentials(insecureConn))
			if relayErr != nil {
				return fmt.Errorf("failed to create tunnel relay: %w", relayErr)
			}

			// Store in outer scope because modifying the same variable will lead to the data race below
			tunnelRelay = tunnelProps{relay: relay, dstHost: dstHost, ourAddrPort: ourAddrPort}

			eg.Go(func() error {
				logger.Debug("running tunnel relay")

				relayRunErr := relay.Run(ctx, tunnelLogger(logger))
				if relayRunErr == nil {
					logger.Debug(
						"tunnel relay exited gracefully",
						zap.String("endpoint", dstHost),
						zap.Stringer("node_address", ourAddrPort),
					)

					return nil
				}

				// Relay returned an error, close the relay and print the error, device should be kept running.
				relay.Close()

				const retryIn = 5 * time.Second

				logger.Error(
					"tunnel relay failed, retrying",
					zap.Duration("timeout", retryIn),
					zap.String("endpoint", dstHost),
					zap.Stringer("node_address", ourAddrPort),
					zap.Error(relayRunErr),
				)

				relayRetryTimer.Reset(retryIn)

				return nil
			})
		}
	}
}

var tunnelURLSchemeMatcher = regexp.MustCompile(`[a-zA-z]+://`)

// parseAPIEndpoint extracts the host and connection security from the SideroLink API endpoint URL.
func parseAPIEndpoint(apiEndpoint string) (host string, insecureConn bool, err error) {
	if !tunnelURLSchemeMatcher.MatchString(apiEndpoint) {
		apiEndpoint = "grpc://" + apiEndpoint
	}

	u, err := url.Parse(apiEndpoint)
	if err != nil {
		return "", false, err
	}

	host = u.Host

	if u.Port() == "" && u.Scheme == "https" {
		host += ":443"
	}

	return host, u.Scheme == "grpc", nil
}

// tunnelLogger keeps the relay and device from spamming the per-machine logs: their data-path messages
// are only interesting when debugging the tunnel itself.
func tunnelLogger(logger *zap.Logger) *zap.Logger {
	return logger.WithOptions(zap.IncreaseLevel(zap.InfoLevel))
}

// errCause returns the cause of the context error when it differs from the plain context error, which
// is the case when a goroutine of the error group failed rather than the parent context being canceled.
//
// The identity comparison is deliberate: a failed goroutine returning a wrapped cancellation error must
// still be reported, only the exact plain cancellation is not an error.
func errCause(ctx context.Context) error {
	if cause := context.Cause(ctx); cause != ctx.Err() { //nolint:errorlint
		return cause
	}

	return nil
}

type tunnelProps struct {
	relay       *wggrpc.Relay
	dstHost     string
	ourAddrPort netip.AddrPort
}

// resettableTimer wraps time.Timer to allow resetting the timer to any duration.
type resettableTimer struct {
	timer *time.Timer
	mx    sync.Mutex
}

// Reset resets the timer to the given duration.
//
// If the duration is zero, the timer is removed (and stopped as needed).
// If the duration is non-zero, the timer is created if it doesn't exist, or reset if it does.
func (rt *resettableTimer) Reset(delay time.Duration) {
	rt.mx.Lock()
	defer rt.mx.Unlock()

	if delay == 0 {
		if rt.timer != nil {
			if !rt.timer.Stop() {
				<-rt.timer.C
			}

			rt.timer = nil
		}
	} else {
		if rt.timer == nil {
			rt.timer = time.NewTimer(delay)
		} else {
			if !rt.timer.Stop() {
				<-rt.timer.C
			}

			rt.timer.Reset(delay)
		}
	}
}

// Clear should be called after receiving from the timer channel.
func (rt *resettableTimer) Clear() {
	rt.mx.Lock()
	defer rt.mx.Unlock()

	rt.timer = nil
}

// C returns the timer channel.
//
// If the timer was not reset to a non-zero duration, nil is returned.
func (rt *resettableTimer) C() <-chan time.Time {
	rt.mx.Lock()
	defer rt.mx.Unlock()

	if rt.timer == nil {
		return nil
	}

	return rt.timer.C
}
