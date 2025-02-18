// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
)

// NodenameController renders manifests based on templates and config/secrets.
type NodenameController struct{}

// Name implements controller.Controller interface.
func (ctrl *NodenameController) Name() string {
	return "k8s.NodenameController"
}

// Inputs implements controller.Controller interface.
func (ctrl *NodenameController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.V1Alpha1ID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: network.NamespaceName,
			Type:      network.HostnameStatusType,
			ID:        optional.Some(network.HostnameID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *NodenameController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: k8s.NodenameType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *NodenameController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		cfg, err := machineconfig.GetComplete(ctx, r)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return fmt.Errorf("error getting config: %w", err)
		}

		hostnameStatus, err := safe.ReaderGetByID[*network.HostnameStatus](ctx, r, network.HostnameID)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		if err = safe.WriterModify(
			ctx,
			r,
			k8s.NewNodename(k8s.NamespaceName, k8s.NodenameID),
			func(res *k8s.Nodename) error {
				var hostname string

				if cfg.Config().Machine().Kubelet().RegisterWithFQDN() {
					hostname = hostnameStatus.TypedSpec().FQDN()
				} else {
					hostname = hostnameStatus.TypedSpec().Hostname
				}

				res.TypedSpec().Nodename, err = FromHostname(hostname)
				if err != nil {
					return err
				}

				res.TypedSpec().HostnameVersion = hostnameStatus.Metadata().Version().String()
				res.TypedSpec().SkipNodeRegistration = cfg.Config().Machine().Kubelet().SkipNodeRegistration()

				return nil
			},
		); err != nil {
			return fmt.Errorf("error modifying nodename resource: %w", err)
		}

		r.ResetRestartBackoff()
	}
}

// FromHostname converts a hostname to Kubernetes Node name.
//
// UNIX hostname has almost no restrictions, but Kubernetes Node name has
// to be RFC 1123 compliant. This function converts a hostname to a valid
// Kubernetes Node name (if possible).
//
// The allowed format is:
//
//	[a-z0-9]([-a-z0-9]*[a-z0-9])?
func FromHostname(hostname string) (string, error) {
	nodename := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			// allow lowercase
			return r
		case r >= 'A' && r <= 'Z':
			// lowercase uppercase letters
			return r - 'A' + 'a'
		case r >= '0' && r <= '9':
			// allow digits
			return r
		case r == '-' || r == '_':
			// allow dash, convert underscore to dash
			return '-'
		case r == '.':
			// allow dot
			return '.'
		default:
			// drop anything else
			return -1
		}
	}, hostname)

	// now drop any dashes/dots at the beginning or end
	nodename = strings.Trim(nodename, "-.")

	if len(nodename) == 0 {
		return "", fmt.Errorf("could not convert hostname %q to a valid Kubernetes Node name", hostname)
	}

	return nodename, nil
}
