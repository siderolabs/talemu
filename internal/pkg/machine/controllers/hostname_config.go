// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/martinlindhe/base36"
	"github.com/siderolabs/gen/optional"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
)

// HostnameConfigController manages network.HostnameSpec based on machine configuration, kernel cmdline.
type HostnameConfigController struct {
	MachineID string
}

// Name implements controller.Controller interface.
func (ctrl *HostnameConfigController) Name() string {
	return "network.HostnameConfigController"
}

// Inputs implements controller.Controller interface.
func (ctrl *HostnameConfigController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.ActiveID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: network.NamespaceName,
			Type:      network.NodeAddressType,
			ID:        optional.Some(network.NodeAddressDefaultID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: cluster.NamespaceName,
			Type:      cluster.IdentityType,
			ID:        optional.Some(cluster.LocalIdentity),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *HostnameConfigController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: network.HostnameSpecType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocognit
func (ctrl *HostnameConfigController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		touchedIDs := make(map[resource.ID]struct{})

		var cfgProvider talosconfig.Config

		cfg, err := machineconfig.GetComplete(ctx, r)
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting config: %w", err)
			}
		} else {
			cfgProvider = cfg.Config()
		}

		var specs []network.HostnameSpecSpec

		// defaults
		var defaultAddr *network.NodeAddress

		addrs, err := r.Get(ctx, resource.NewMetadata(network.NamespaceName, network.NodeAddressType, network.NodeAddressDefaultID, resource.VersionUndefined))
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting config: %w", err)
			}
		} else {
			defaultAddr = addrs.(*network.NodeAddress) //nolint:errcheck,forcetypeassert
		}

		// parse machine configuration for specs
		if cfgProvider != nil {
			configHostname := ctrl.parseMachineConfiguration(logger, cfgProvider)

			if configHostname.Hostname != "" {
				specs = append(specs, configHostname)
			}

			if cfgProvider.Machine().Features().StableHostnameEnabled() {
				var identity *cluster.Identity

				identity, err = safe.ReaderGetByID[*cluster.Identity](ctx, r, cluster.LocalIdentity)
				if err != nil {
					if !state.IsNotFoundError(err) {
						return fmt.Errorf("error getting local identity: %w", err)
					}

					continue
				}

				nodeID := identity.TypedSpec().NodeID

				stableHostname := ctrl.getStableDefault(nodeID)
				specs = append(specs, *stableHostname)
			} else {
				specs = append(specs, ctrl.getDefault(defaultAddr))
			}
		} else {
			specs = append(specs, network.HostnameSpecSpec{
				Hostname:    ctrl.MachineID,
				ConfigLayer: network.ConfigDefault,
			})
		}

		var ids []string

		ids, err = ctrl.apply(ctx, r, specs)
		if err != nil {
			return fmt.Errorf("error applying specs: %w", err)
		}

		for _, id := range ids {
			touchedIDs[id] = struct{}{}
		}

		// list specs for cleanup
		list, err := r.List(ctx, resource.NewMetadata(network.ConfigNamespaceName, network.HostnameSpecType, "", resource.VersionUndefined))
		if err != nil {
			return fmt.Errorf("error listing resources: %w", err)
		}

		for _, res := range list.Items {
			if res.Metadata().Owner() != ctrl.Name() {
				// skip specs created by other controllers
				continue
			}

			if _, ok := touchedIDs[res.Metadata().ID()]; !ok {
				if err = r.Destroy(ctx, res.Metadata()); err != nil {
					return fmt.Errorf("error cleaning up specs: %w", err)
				}
			}
		}

		r.ResetRestartBackoff()
	}
}

func (ctrl *HostnameConfigController) apply(ctx context.Context, r controller.Runtime, specs []network.HostnameSpecSpec) ([]resource.ID, error) {
	ids := make([]string, 0, len(specs))

	for _, spec := range specs {
		id := network.LayeredID(spec.ConfigLayer, network.HostnameID)

		if err := safe.WriterModify(
			ctx,
			r,
			network.NewHostnameSpec(network.ConfigNamespaceName, id),
			func(r *network.HostnameSpec) error {
				*r.TypedSpec() = spec

				return nil
			},
		); err != nil {
			return ids, err
		}

		ids = append(ids, id)
	}

	return ids, nil
}

func (ctrl *HostnameConfigController) getStableDefault(nodeID string) *network.HostnameSpecSpec {
	hashBytes := sha256.Sum256([]byte(nodeID))
	b36 := strings.ToLower(base36.EncodeBytes(hashBytes[:8]))

	hostname := fmt.Sprintf("talos-%s-%s", b36[1:4], b36[4:7])

	return &network.HostnameSpecSpec{
		Hostname:    hostname,
		ConfigLayer: network.ConfigDefault,
	}
}

func (ctrl *HostnameConfigController) getDefault(defaultAddr *network.NodeAddress) (spec network.HostnameSpecSpec) {
	if defaultAddr == nil || len(defaultAddr.TypedSpec().Addresses) != 1 {
		return
	}

	spec.Hostname = fmt.Sprintf("talos-%s", strings.ReplaceAll(strings.ReplaceAll(defaultAddr.TypedSpec().Addresses[0].Addr().String(), ":", ""), ".", "-"))
	spec.ConfigLayer = network.ConfigDefault

	return spec
}

func (ctrl *HostnameConfigController) parseMachineConfiguration(logger *zap.Logger, cfgProvider talosconfig.Config) (spec network.HostnameSpecSpec) {
	hostname := cfgProvider.Machine().Network().Hostname()

	if hostname == "" {
		return
	}

	if err := spec.ParseFQDN(hostname); err != nil {
		logger.Warn("ignoring error", zap.Error(err))

		return network.HostnameSpecSpec{}
	}

	spec.ConfigLayer = network.ConfigMachineConfiguration

	return spec
}
