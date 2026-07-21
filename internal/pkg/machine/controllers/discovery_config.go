// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"encoding/base64"
	"net"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/controller/generic/transform"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"go.uber.org/zap"
)

// ConfigController watches v1alpha1.Config, updates discovery config.
type ConfigController = transform.Controller[*config.MachineConfig, *cluster.Config]

// NewClusterConfigController instanciates the config controller.
func NewClusterConfigController() *ConfigController {
	return transform.NewController(
		transform.Settings[*config.MachineConfig, *cluster.Config]{
			Name: "cluster.ConfigController",
			MapMetadataOptionalFunc: func(cfg *config.MachineConfig) optional.Optional[*cluster.Config] {
				if cfg.Metadata().ID() != config.ActiveID {
					return optional.None[*cluster.Config]()
				}

				if cfg.Config().Cluster() == nil {
					return optional.None[*cluster.Config]()
				}

				return optional.Some(cluster.NewConfig(config.NamespaceName, cluster.ConfigID))
			},
			TransformFunc: func(_ context.Context, _ controller.Reader, _ *zap.Logger, cfg *config.MachineConfig, res *cluster.Config) error {
				c := cfg.Config()

				if c.Cluster().Discovery().Enabled() {
					res.TypedSpec().RegistryKubernetesEnabled = c.Cluster().Discovery().Registries().Kubernetes().Enabled()

					discoveryServiceConfigs := c.DiscoveryServiceConfigs()

					if len(discoveryServiceConfigs) > 0 {
						u := discoveryServiceConfigs[0].Endpoint()

						host := u.Hostname()
						port := u.Port()

						if port == "" {
							if u.Scheme == "http" {
								port = "80"
							} else {
								port = "443" // use default https port for everything else
							}
						}

						serviceEncryptionKey, err := base64.StdEncoding.DecodeString(c.Cluster().Secret())
						if err != nil {
							return err
						}

						res.TypedSpec().ServiceEndpoints = []cluster.ServiceEndpoint{
							{
								Name:     discoveryServiceConfigs[0].Name(),
								Endpoint: net.JoinHostPort(host, port),
								Insecure: u.Scheme == "http",
							},
						}
						res.TypedSpec().ServiceEncryptionKey = serviceEncryptionKey
						res.TypedSpec().ServiceClusterID = c.Cluster().ID()
					} else {
						res.TypedSpec().ServiceEndpoints = nil
						res.TypedSpec().ServiceEncryptionKey = nil
						res.TypedSpec().ServiceClusterID = ""
					}
				} else {
					res.TypedSpec().RegistryKubernetesEnabled = false
					res.TypedSpec().ServiceEndpoints = nil
				}

				return nil
			},
		},
	)
}
