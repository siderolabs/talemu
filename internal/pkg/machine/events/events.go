// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package events is Talos machine events.
package events

import (
	"context"
	"fmt"
	"net"
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
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

// Handler watches machine status resource and turns each resource change into an event.
type Handler struct {
	state  state.State
	client events.EventSinkServiceClient
}

// NewHandler creates new events handler.
func NewHandler(ctx context.Context, st state.State, machineIndex int) (*Handler, error) {
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

			dialer.Control = network.BindToInterface(fmt.Sprintf("%s%d", constants.SideroLinkName, machineIndex))

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

	for _, id := range []string{emuconst.APIDService, emuconst.ETCDService, emuconst.KubeletService} {
		eg.Go(func() error {
			return h.runWithRetries(ctx, logger, func() error {
				return generateEvents(ctx, h, v1alpha1.NewService(id), func(res *v1alpha1.Service) (*events.EventRequest, error) {
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

	return eg.Wait()
}

func (h *Handler) runWithRetries(ctx context.Context, logger *zap.Logger, cb func() error) error {
	for {
		err := cb()
		if err != nil {
			logger.WithOptions(zap.AddStacktrace(zap.PanicLevel)).Warn("event sink connector crashed", zap.Error(err))

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
func generateEvents[T resource.Resource](ctx context.Context, h *Handler, res T, callback func(res T) (*events.EventRequest, error), logger *zap.Logger) error {
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
			case state.Bootstrapped, state.Destroyed:
			case state.Created, state.Updated:
				if _, err = safe.StateUpdateWithConflicts(ctx, h.state, talos.NewEventSinkState(talos.NamespaceName, talos.EventSinkStateID).Metadata(),
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

						if _, err = h.client.Publish(ctx, event); err != nil {
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
