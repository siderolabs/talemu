// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package events is Talos machine events.
package events

import (
	"context"
	"fmt"
	"net"
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
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// Handler watches machine status resource and turns each resource change into an event.
type Handler struct {
	state  state.State
	client events.EventSinkServiceClient
}

// NewHandler creates new events handler.
func NewHandler(ctx context.Context, st state.State, uuid string) (*Handler, error) {
	config, err := safe.ReaderGetByID[*runtime.EventSinkConfig](ctx, st, runtime.EventSinkConfigID)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		config.TypedSpec().Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithSharedWriteBuffer(true),
		grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) {
			var dialer net.Dialer

			_, id, _ := strings.Cut(uuid, "-")

			dialer.Control = network.BindToInterface(constants.SideroLinkName + id)

			return dialer.DialContext(ctx, "tcp", address)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("error establishing connection to event sink: %w", err)
	}

	return &Handler{
		state:  st,
		client: events.NewEventSinkServiceClient(conn),
	}, nil
}

// Run starts the events handler.
func (h *Handler) Run(ctx context.Context, logger *zap.Logger) error {
	var eg errgroup.Group

	eg.Go(func() error {
		return h.runWithRetries(ctx, logger, func() error {
			logger = logger.With(zap.String("resource", "MachineStatus"))

			return generateEvents(ctx, h, runtime.NewMachineStatus(), func(res *runtime.MachineStatus) (*events.EventRequest, error) {
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

				data, err := anypb.New(payload)
				if err != nil {
					return nil, err
				}

				return &events.EventRequest{
					Id:   id.String(),
					Data: data,
				}, nil
			})
		})
	})

	return eg.Wait()
}

func (h *Handler) runWithRetries(ctx context.Context, logger *zap.Logger, cb func() error) error {
	for {
		err := cb()
		if err != nil {
			logger.Error("event sink connector crashed", zap.Error(err))

			time.Sleep(time.Second)

			select {
			case <-ctx.Done():
				return nil
			default:
			}

			continue
		}

		return nil
	}
}

//nolint:gocognit,gocyclo,cyclop
func generateEvents[T resource.Resource](ctx context.Context, h *Handler, res T, callback func(res T) (*events.EventRequest, error)) error {
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
				return fmt.Errorf(event.Error().Error())
			case state.Bootstrapped, state.Destroyed:
			case state.Created, state.Updated:
				if _, err = safe.StateUpdateWithConflicts(ctx, h.state, talos.NewEventSinkState(talos.NamespaceName, talos.EventSinkStateID).Metadata(),
					func(st *talos.EventSinkState) error {
						var res T

						res, err = event.Resource()
						if err != nil {
							return err
						}

						var event *events.EventRequest

						event, err = callback(res)
						if err != nil {
							return err
						}

						if _, err = h.client.Publish(ctx, event); err != nil {
							return err
						}

						st.TypedSpec().Value.Versions[res.Metadata().Type()] = res.Metadata().Version().Value()

						return nil
					},
				); err != nil {
					return err
				}
			}
		}
	}
}
