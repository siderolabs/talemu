// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package events is Talos machine events.
package events

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/rs/xid"
	"github.com/siderolabs/gen/xslices"
	"github.com/siderolabs/siderolink/api/events"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	emunet "github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// Handler watches machine status resource and turns each resource change into an event.
type Handler struct {
	state state.State
}

// NewHandler creates new events handler.
func NewHandler(st state.State) (*Handler, error) {
	return &Handler{
		state: st,
	}, nil
}

// Run starts the events handler.
func (h *Handler) Run(ctx context.Context, logger *zap.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	config, err := safe.ReaderGetByID[*runtime.EventSinkConfig](ctx, h.state, runtime.EventSinkConfigID)
	if err != nil {
		return err
	}

	endpoint := config.TypedSpec().Endpoint

	// The siderolink address can change at runtime: when Omni resolves a UUID conflict it assigns a
	// new UUID and the machine re-provisions, getting a brand new address. Events must originate from
	// the address Omni currently associates with this machine, so watch the address and rebuild the
	// event sink connection whenever it changes (the same way APIDController restarts its listener).
	addressCh := make(chan state.Event)
	if err = h.state.WatchKind(ctx, network.NewAddressStatus(network.NamespaceName, "").Metadata(), addressCh); err != nil {
		return fmt.Errorf("failed to watch addresses: %w", err)
	}

	var (
		boundAddr  netip.Addr
		sinkEg     *errgroup.Group
		stopSink   context.CancelFunc
		haveRunner bool
	)

	teardownSink := func() {
		if !haveRunner {
			return
		}

		stopSink()
		sinkEg.Wait() //nolint:errcheck

		haveRunner = false
	}

	defer teardownSink()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-addressCh:
			if ev.Type == state.Errored {
				return fmt.Errorf("address watch failed: %w", ev.Error)
			}
		}

		addr, linkName, ok, err := h.siderolinkAddress(ctx)
		if err != nil {
			return err
		}

		if !ok || addr == boundAddr {
			continue
		}

		teardownSink()

		boundAddr = addr

		sinkCtx, cancelSink := context.WithCancel(ctx)

		sinkEg, err = h.startEventSink(sinkCtx, logger, endpoint, addr, linkName)
		if err != nil {
			cancelSink()

			return err
		}

		stopSink = cancelSink
		haveRunner = true
	}
}

// siderolinkAddress returns the current siderolink interface address, if any.
func (h *Handler) siderolinkAddress(ctx context.Context) (netip.Addr, string, bool, error) {
	list, err := safe.ReaderListAll[*network.AddressStatus](ctx, h.state)
	if err != nil {
		return netip.Addr{}, "", false, err
	}

	addr, ok := list.Find(func(r *network.AddressStatus) bool {
		return strings.HasPrefix(r.TypedSpec().LinkName, constants.SideroLinkName)
	})
	if !ok {
		return netip.Addr{}, "", false, nil
	}

	return addr.TypedSpec().Address.Addr(), addr.TypedSpec().LinkName, true, nil
}

// startEventSink dials the event sink bound to the given address and starts publishing events.
// The returned group winds down (closing the connection) once ctx is canceled.
func (h *Handler) startEventSink(ctx context.Context, logger *zap.Logger, endpoint string, addr netip.Addr, linkName string) (*errgroup.Group, error) {
	bindAddress := net.TCPAddrFromAddrPort(netip.AddrPortFrom(addr, 0))

	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithSharedWriteBuffer(true),
		grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
			var dialer net.Dialer

			dialer.LocalAddr = bindAddress
			dialer.Control = emunet.BindToInterface(linkName)

			return dialer.DialContext(ctx, "tcp", address)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("error establishing connection to event sink: %w", err)
	}

	eg, ctx := errgroup.WithContext(ctx)

	// close the connection once the context is canceled (address change or shutdown).
	eg.Go(func() error {
		<-ctx.Done()

		return conn.Close()
	})

	client := events.NewEventSinkServiceClient(conn)

	for _, id := range []string{emuconst.APIDService, emuconst.ETCDService, emuconst.KubeletService} {
		eg.Go(func() error {
			return h.runWithRetries(ctx, logger, func() error {
				return generateEvents(ctx, h, v1alpha1.NewService(id), client, func(res *v1alpha1.Service) (*events.EventRequest, error) {
					id := xid.NewWithTime(res.Metadata().Updated())

					state := "Stopped"

					switch {
					case res.TypedSpec().Running && res.TypedSpec().Healthy:
						state = "Running"
					case res.TypedSpec().Running && !res.TypedSpec().Healthy:
						state = "Starting"
					}

					payload := &machine.ServiceEvent{
						State: state,
						Ts:    timestamppb.Now(),
					}

					data, err := anypb.New(payload)
					if err != nil {
						return nil, err
					}

					return &events.EventRequest{
						Id:   id.String(),
						Data: data,
					}, nil
				}, logger)
			})
		})
	}

	eg.Go(func() error {
		return h.runWithRetries(ctx, logger, func() error {
			return generateEvents(ctx, h, runtime.NewMachineStatus(), client, func(res *runtime.MachineStatus) (*events.EventRequest, error) {
				id := xid.NewWithTime(res.Metadata().Updated())

				payload := &machine.MachineStatusEvent{
					Stage: machine.MachineStatusEvent_MachineStage(res.TypedSpec().Stage),
					Status: &machine.MachineStatusEvent_MachineStatus{
						Ready: res.TypedSpec().Status.Ready,
						UnmetConditions: xslices.Map(res.TypedSpec().Status.UnmetConditions, func(cond runtime.UnmetCondition) *machine.MachineStatusEvent_MachineStatus_UnmetCondition {
							return &machine.MachineStatusEvent_MachineStatus_UnmetCondition{
								Name:   cond.Name,
								Reason: cond.Reason,
							}
						}),
					},
				}

				logger.Debug("machine status event", zap.Reflect("payload", payload))

				data, err := anypb.New(payload)
				if err != nil {
					return nil, err
				}

				return &events.EventRequest{
					Id:   id.String(),
					Data: data,
				}, nil
			}, logger)
		})
	})

	return eg, nil
}

func (h *Handler) runWithRetries(ctx context.Context, logger *zap.Logger, cb func() error) error {
	backoff := time.Second

	for {
		err := cb()
		if err != nil {
			logger.WithOptions(zap.AddStacktrace(zap.PanicLevel)).Warn("event sink connector crashed", zap.Error(err))

			time.Sleep(backoff)

			select {
			case <-ctx.Done():
				return nil
			default:
			}

			backoff = min(backoff*2+time.Second*time.Duration(rand.Intn(10)), time.Second*30)

			continue
		}

		return nil
	}
}

//nolint:gocognit,gocyclo,cyclop
func generateEvents[T resource.Resource](ctx context.Context, h *Handler, res T, client events.EventSinkServiceClient, callback func(res T) (*events.EventRequest, error), logger *zap.Logger) error {
	latest, err := h.state.Get(ctx, res.Metadata())
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	lastVersions, err := safe.ReaderGetByID[*talos.EventSinkState](ctx, h.state, talos.EventSinkStateID)
	if err != nil {
		if !state.IsNotFoundError(err) {
			return err
		}

		lastVersions = talos.NewEventSinkState(talos.NamespaceName, talos.EventSinkStateID)
		lastVersions.TypedSpec().Value.Versions = map[string]uint64{}

		if err = h.state.Create(ctx, lastVersions); err != nil && !state.IsConflictError(err) {
			return err
		}
	}

	var backlog int

	if latest != nil && lastVersions != nil {
		lastVersion := latest.Metadata().Version().Value()

		backlog = int(lastVersion - lastVersions.TypedSpec().Value.Versions[res.Metadata().Type()])
	}

	var opts []state.WatchOption

	if backlog != 0 {
		opts = append(opts, state.WithTailEvents(backlog))
	}

	eventCh := make(chan safe.WrappedStateEvent[T])

	err = safe.StateWatch(ctx, h.state, res.Metadata(), eventCh, opts...)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-eventCh:
			switch event.Type() {
			case state.Errored:
				return event.Error()
			case state.Bootstrapped, state.Destroyed, state.Noop:
			case state.Created, state.Updated:
				if _, err = safe.StateUpdateWithConflicts(
					ctx, h.state, talos.NewEventSinkState(talos.NamespaceName, talos.EventSinkStateID).Metadata(),
					func(st *talos.EventSinkState) error {
						var res T

						res, err = event.Resource()
						if err != nil {
							return err
						}

						version := res.Metadata().Version().Value()

						if v, ok := st.TypedSpec().Value.Versions[res.Metadata().Type()]; ok && version <= v {
							return nil
						}

						var event *events.EventRequest

						event, err = callback(res)
						if err != nil {
							return err
						}

						if _, err = client.Publish(ctx, event); err != nil {
							return err
						}

						if st.TypedSpec().Value.Versions == nil {
							st.TypedSpec().Value.Versions = map[string]uint64{}
						}

						st.TypedSpec().Value.Versions[res.Metadata().Type()] = version

						logger.Debug("sent event", zap.Reflect("event", event), zap.String("resource", res.Metadata().String()))

						return nil
					},
				); err != nil {
					return err
				}
			}
		}
	}
}
