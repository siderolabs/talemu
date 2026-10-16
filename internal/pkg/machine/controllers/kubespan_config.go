// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/controller/generic/transform"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"go.uber.org/zap"
)

// KubeSpanConfigController watches v1alpha1.Config, updates KubeSpan config.
type KubeSpanConfigController = transform.Controller[*config.MachineConfig, *kubespan.Config]

// NewKubeSpanConfigController instantiates the KubeSpan config controller.
//
// It is a verbatim copy of the kubespan.ConfigController of Talos.
func NewKubeSpanConfigController() *KubeSpanConfigController {
	return transform.NewController(
		transform.Settings[*config.MachineConfig, *kubespan.Config]{
			Name: "kubespan.ConfigController",
			MapMetadataOptionalFunc: func(cfg *config.MachineConfig) optional.Optional[*kubespan.Config] {
				if cfg.Metadata().ID() != config.ActiveID {
					return optional.None[*kubespan.Config]()
				}

				if cfg.Config().Machine() == nil || cfg.Config().Cluster() == nil {
					return optional.None[*kubespan.Config]()
				}

				if cfg.Config().DiscoveryIdentityConfig() == nil {
					return optional.None[*kubespan.Config]()
				}

				return optional.Some(kubespan.NewConfig(config.NamespaceName, kubespan.ConfigID))
			},
			TransformFunc: func(_ context.Context, _ controller.Reader, _ *zap.Logger, cfg *config.MachineConfig, res *kubespan.Config) error {
				spec := res.TypedSpec()

				*spec = kubespan.ConfigSpec{}

				if cfg != nil && cfg.Config().Machine() != nil {
					c := cfg.Config()

					if c.NetworkKubeSpanConfig() != nil {
						res.TypedSpec().Enabled = c.NetworkKubeSpanConfig().Enabled()
						res.TypedSpec().ForceRouting = c.NetworkKubeSpanConfig().ForceRouting()
						res.TypedSpec().AdvertiseKubernetesNetworks = c.NetworkKubeSpanConfig().AdvertiseKubernetesNetworks()
						res.TypedSpec().HarvestExtraEndpoints = c.NetworkKubeSpanConfig().HarvestExtraEndpoints()
						res.TypedSpec().MTU = c.NetworkKubeSpanConfig().MTU()

						if c.NetworkKubeSpanConfig().Filters() != nil {
							res.TypedSpec().EndpointFilters = c.NetworkKubeSpanConfig().Filters().Endpoints()
							res.TypedSpec().PeerEndpointFilters = c.NetworkKubeSpanConfig().Filters().PeerEndpoints()
							res.TypedSpec().ExcludeAdvertisedNetworks = c.NetworkKubeSpanConfig().Filters().ExcludeAdvertisedNetworks()
						}
					}

					identity := c.DiscoveryIdentityConfig()
					res.TypedSpec().ClusterID = identity.ClusterID()
					res.TypedSpec().SharedSecret = identity.ClusterSecret()

					res.TypedSpec().ExtraEndpoints = c.KubespanConfig().ExtraAnnouncedEndpoints()
				}

				return nil
			},
		},
	)
}
