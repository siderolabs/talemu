// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

const labelLocal = "local"

// LocalAffiliateController builds Affiliate resource for the local node.
type LocalAffiliateController struct{}

// Name implements controller.Controller interface.
func (ctrl *LocalAffiliateController) Name() string {
	return "cluster.LocalAffiliateController"
}

// Inputs implements controller.Controller interface.
func (ctrl *LocalAffiliateController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      cluster.ConfigType,
			ID:        optional.Some(cluster.ConfigID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: cluster.NamespaceName,
			Type:      cluster.IdentityType,
			ID:        optional.Some(cluster.LocalIdentity),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: network.NamespaceName,
			Type:      network.HostnameStatusType,
			ID:        optional.Some(network.HostnameID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: k8s.NamespaceName,
			Type:      k8s.NodenameType,
			ID:        optional.Some(k8s.NodenameID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: network.NamespaceName,
			Type:      network.NodeAddressType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineTypeType,
			ID:        optional.Some(config.MachineTypeID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: cluster.NamespaceName,
			Type:      network.AddressStatusType,
			Kind:      controller.InputWeak,
		},
		{
			Namespace: talos.NamespaceName,
			Type:      talos.VersionType,
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *LocalAffiliateController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: cluster.AffiliateType,
			Kind: controller.OutputShared,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocyclo,cyclop,gocognit
func (ctrl *LocalAffiliateController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		// mandatory resources to be fetched
		discoveryConfig, err := safe.ReaderGetByID[*cluster.Config](ctx, r, cluster.ConfigID)
		if err != nil && !state.IsNotFoundError(err) {
			return fmt.Errorf("error getting discovery config: %w", err)
		}

		if discoveryConfig == nil {
			var items safe.List[*cluster.Affiliate]

			items, err = safe.ReaderListAll[*cluster.Affiliate](ctx, r, state.WithLabelQuery(resource.LabelExists(labelLocal)))
			if err != nil {
				return err
			}

			if err = items.ForEachErr(func(item *cluster.Affiliate) error {
				if err = r.Destroy(ctx, cluster.NewAffiliate(cluster.NamespaceName, item.Metadata().ID()).Metadata()); err != nil && !state.IsNotFoundError(err) {
					return err
				}

				return nil
			}); err != nil {
				return err
			}

			continue
		}

		identity, err := safe.ReaderGetByID[*cluster.Identity](ctx, r, cluster.LocalIdentity)
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting local identity: %w", err)
			}

			continue
		}

		hostname, err := safe.ReaderGetByID[*network.HostnameStatus](ctx, r, network.HostnameID)
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting hostname: %w", err)
			}

			continue
		}

		nodename, err := safe.ReaderGetByID[*k8s.Nodename](ctx, r, k8s.NodenameID)
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting nodename: %w", err)
			}

			continue
		}

		currentAddresses, err := safe.ReaderGetByID[*network.NodeAddress](ctx, r, network.NodeAddressDefaultID)
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting addresses: %w", err)
			}

			continue
		}

		version, err := safe.ReaderGetByID[*talos.Version](ctx, r, talos.VersionID)
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting version: %w", err)
			}

			continue
		}

		machineType, err := safe.ReaderGetByID[*config.MachineType](ctx, r, config.MachineTypeID)
		if err != nil {
			if !state.IsNotFoundError(err) {
				return fmt.Errorf("error getting machine type: %w", err)
			}

			continue
		}

		touchedIDs := map[resource.ID]struct{}{}

		localID := identity.TypedSpec().NodeID

		if discoveryConfig.TypedSpec().DiscoveryEnabled {
			if err = safe.WriterModify(ctx, r, cluster.NewAffiliate(cluster.NamespaceName, localID), func(res *cluster.Affiliate) error {
				res.Metadata().Labels().Set(labelLocal, "")

				spec := res.TypedSpec()

				spec.NodeID = localID
				spec.Hostname = hostname.TypedSpec().FQDN()
				spec.Nodename = nodename.TypedSpec().Nodename
				spec.MachineType = machineType.MachineType()
				spec.OperatingSystem = fmt.Sprintf("Talos (%s)", version.TypedSpec().Value.Value)

				routedNodeIPs := currentAddresses.TypedSpec().IPs()

				spec.Addresses = routedNodeIPs

				spec.KubeSpan = cluster.KubeSpanAffiliateSpec{}

				return nil
			}); err != nil {
				return err
			}

			touchedIDs[localID] = struct{}{}
		}

		// list keys for cleanup
		list, err := safe.ReaderListAll[*cluster.Affiliate](ctx, r)
		if err != nil {
			return fmt.Errorf("error listing resources: %w", err)
		}

		for res := range list.All() {
			if res.Metadata().Owner() != ctrl.Name() {
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
