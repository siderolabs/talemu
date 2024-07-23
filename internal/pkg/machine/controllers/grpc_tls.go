// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	stdlibx509 "crypto/x509"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"github.com/siderolabs/talos/pkg/machinery/role"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/grpc/gen"
	emunet "github.com/siderolabs/talemu/internal/pkg/machine/network"
)

// GRPCTLSController manages secrets.API based on configuration to provide apid certificate.
type GRPCTLSController struct{}

// Name implements controller.Controller interface.
func (ctrl *GRPCTLSController) Name() string {
	return "secrets.GRPCTLSController"
}

// Inputs implements controller.Controller interface.
func (ctrl *GRPCTLSController) Inputs() []controller.Input {
	// initial set of inputs: wait for machine type to be known and network to be partially configured
	return []controller.Input{
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineTypeType,
			ID:        optional.Some(config.MachineTypeID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *GRPCTLSController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: secrets.APIType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *GRPCTLSController) Run(ctx context.Context, r controller.Runtime, logger *zap.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		// reset inputs back to what they were initially
		if err := r.UpdateInputs(ctrl.Inputs()); err != nil {
			return err
		}

		machineTypeRes, err := safe.ReaderGetByID[*config.MachineType](ctx, r, config.MachineTypeID)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return fmt.Errorf("error getting machine type: %w", err)
		}

		machineType := machineTypeRes.MachineType()

		// machine type is known and network is ready, we can now proceed to one or another reconcile loop
		switch machineType {
		case machine.TypeInit, machine.TypeControlPlane:
			if err = ctrl.reconcile(ctx, r, logger, true); err != nil {
				return err
			}
		case machine.TypeWorker:
			if err = ctrl.reconcile(ctx, r, logger, false); err != nil {
				return err
			}
		case machine.TypeUnknown:
			// machine configuration is not loaded yet, do nothing
		default:
			panic(fmt.Sprintf("unexpected machine type %v", machineType))
		}

		if err = ctrl.teardownAll(ctx, r); err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}

//nolint:gocognit,gocyclo,cyclop
func (ctrl *GRPCTLSController) reconcile(ctx context.Context, r controller.Runtime, logger *zap.Logger, isControlplane bool) error {
	inputs := []controller.Input{
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.OSRootType,
			ID:        optional.Some(secrets.OSRootID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.CertSANType,
			ID:        optional.Some(secrets.CertSANAPIID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineTypeType,
			ID:        optional.Some(config.MachineTypeID),
			Kind:      controller.InputWeak,
		},
	}

	if !isControlplane {
		// worker nodes depend on endpoint list
		inputs = append(inputs, controller.Input{
			Namespace: k8s.ControlPlaneNamespaceName,
			Type:      k8s.EndpointType,
			Kind:      controller.InputWeak,
		}, controller.Input{
			Namespace: network.NamespaceName,
			Type:      network.AddressStatusType,
			Kind:      controller.InputWeak,
		})
	}

	if err := r.UpdateInputs(inputs); err != nil {
		return fmt.Errorf("error updating inputs: %w", err)
	}

	r.QueueReconcile()

	refreshTicker := time.NewTicker(x509.DefaultCertificateValidityDuration / 2)
	defer refreshTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		case <-refreshTicker.C:
		}

		machineTypeRes, err := safe.ReaderGetByID[*config.MachineType](ctx, r, config.MachineTypeID)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return fmt.Errorf("error getting machine type: %w", err)
		}

		machineType := machineTypeRes.MachineType()

		switch machineType {
		case machine.TypeInit, machine.TypeControlPlane:
			if !isControlplane {
				logger.Info("machine type changed to controlplane")

				return nil
			}
		case machine.TypeWorker:
			if isControlplane {
				logger.Info("machine type changed to worker")

				return nil
			}
		case machine.TypeUnknown:
			logger.Info("machine type was reset")

			return nil
		}

		rootResource, err := safe.ReaderGetByID[*secrets.OSRoot](ctx, r, secrets.OSRootID)
		if err != nil {
			if state.IsNotFoundError(err) {
				if err = ctrl.teardownAll(ctx, r); err != nil {
					return fmt.Errorf("error destroying resources: %w", err)
				}

				continue
			}

			return fmt.Errorf("error getting etcd root secrets: %w", err)
		}

		rootSpec := rootResource.TypedSpec()

		certSANResource, err := safe.ReaderGetByID[*secrets.CertSAN](ctx, r, secrets.CertSANAPIID)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return fmt.Errorf("error getting certSANs: %w", err)
		}

		certSANs := certSANResource.TypedSpec()

		var endpointsStr []string

		if !isControlplane {
			endpointResources, err := safe.ReaderListAll[*k8s.Endpoint](ctx, r)
			if err != nil {
				return fmt.Errorf("error getting endpoints resources: %w", err)
			}

			var endpointAddrs k8s.EndpointList

			// merge all endpoints into a single list
			endpointResources.ForEach(func(endpoint *k8s.Endpoint) {
				endpointAddrs = endpointAddrs.Merge(endpoint)
			})

			if len(endpointAddrs) == 0 {
				continue
			}

			endpointsStr = endpointAddrs.Strings()
		}

		if isControlplane {
			if err := ctrl.generateControlPlane(ctx, r, logger, rootSpec, certSANs); err != nil {
				return err
			}
		} else {
			if err := ctrl.generateWorker(ctx, r, logger, rootSpec, endpointsStr, certSANs); err != nil {
				return err
			}
		}

		r.ResetRestartBackoff()
	}
}

func (ctrl *GRPCTLSController) generateControlPlane(ctx context.Context, r controller.Runtime, logger *zap.Logger, rootSpec *secrets.OSRootSpec, certSANs *secrets.CertSANSpec) error {
	if rootSpec.IssuingCA == nil {
		return nil
	}

	ca, err := x509.NewCertificateAuthorityFromCertificateAndKey(rootSpec.IssuingCA)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	serverCert, err := x509.NewKeyPair(ca,
		x509.IPAddresses(certSANs.StdIPs()),
		x509.DNSNames(certSANs.DNSNames),
		x509.CommonName(certSANs.FQDN),
		x509.NotAfter(time.Now().Add(x509.DefaultCertificateValidityDuration)),
		x509.KeyUsage(stdlibx509.KeyUsageDigitalSignature),
		x509.ExtKeyUsage([]stdlibx509.ExtKeyUsage{
			stdlibx509.ExtKeyUsageServerAuth,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to generate API server cert: %w", err)
	}

	clientCert, err := x509.NewKeyPair(ca,
		x509.CommonName(certSANs.FQDN),
		x509.Organization(string(role.Impersonator)),
		x509.NotAfter(time.Now().Add(x509.DefaultCertificateValidityDuration)),
		x509.KeyUsage(stdlibx509.KeyUsageDigitalSignature),
		x509.ExtKeyUsage([]stdlibx509.ExtKeyUsage{
			stdlibx509.ExtKeyUsageClientAuth,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to generate API client cert: %w", err)
	}

	if err := safe.WriterModify(ctx, r, secrets.NewAPI(),
		func(r *secrets.API) error {
			apiSecrets := r.TypedSpec()

			apiSecrets.AcceptedCAs = rootSpec.AcceptedCAs
			apiSecrets.Server = x509.NewCertificateAndKeyFromKeyPair(serverCert)
			apiSecrets.Client = x509.NewCertificateAndKeyFromKeyPair(clientCert)

			return nil
		}); err != nil {
		return fmt.Errorf("error modifying resource: %w", err)
	}

	clientFingerprint, _ := x509.SPKIFingerprintFromDER(clientCert.Certificate.Certificate[0]) //nolint:errcheck
	serverFingerprint, _ := x509.SPKIFingerprintFromDER(serverCert.Certificate.Certificate[0]) //nolint:errcheck

	logger.Debug("generated new certificates",
		zap.Stringer("client", clientFingerprint),
		zap.Stringer("server", serverFingerprint),
	)

	return nil
}

func (ctrl *GRPCTLSController) generateWorker(ctx context.Context, r controller.Runtime, logger *zap.Logger,
	rootSpec *secrets.OSRootSpec, endpointsStr []string, certSANs *secrets.CertSANSpec,
) error {
	links, err := safe.ReaderListAll[*network.AddressStatus](ctx, r)
	if err != nil {
		return err
	}

	address, found := links.Find(func(r *network.AddressStatus) bool {
		return strings.HasPrefix(r.TypedSpec().LinkName, constants.SideroLinkName)
	})
	if !found {
		return fmt.Errorf("failed to find sideroaddress address")
	}

	remoteGen, err := gen.NewRemoteGenerator(func(ctx context.Context, addr string) (net.Conn, error) {
		var dialer net.Dialer

		dialer.LocalAddr = net.TCPAddrFromAddrPort(netip.AddrPortFrom(address.TypedSpec().Address.Addr(), 0))
		dialer.Control = emunet.BindToInterface(address.TypedSpec().LinkName)

		return dialer.DialContext(ctx, "tcp", addr)
	}, rootSpec.Token, endpointsStr, rootSpec.AcceptedCAs)
	if err != nil {
		return fmt.Errorf("failed creating trustd client: %w", err)
	}

	defer remoteGen.Close() //nolint:errcheck

	serverCSR, serverCert, err := x509.NewEd25519CSRAndIdentity(
		x509.IPAddresses(certSANs.StdIPs()),
		x509.DNSNames(certSANs.DNSNames),
		x509.CommonName(certSANs.FQDN),
	)
	if err != nil {
		return fmt.Errorf("failed to generate API server CSR: %w", err)
	}

	logger.Debug("sending CSR", zap.Strings("endpoints", endpointsStr))

	var ca []byte

	// run the CSR generation in a goroutine, so we can abort the request if the inputs change
	errCh := make(chan error)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		ca, serverCert.Crt, err = remoteGen.IdentityContext(ctx, serverCSR)
		errCh <- err
	}()

	select {
	case <-r.EventCh():
		// there's an update to the inputs, terminate the attempt, and let the controller handle the retry
		cancel()

		// re-queue the reconcile event, so that controller retries with new inputs
		r.QueueReconcile()

		// wait for the goroutine to finish, ignoring the error (should be context.Canceled)
		<-errCh

		return nil
	case err = <-errCh:
	}

	if err != nil {
		return fmt.Errorf("failed to sign API server CSR: %w", err)
	}

	if err := safe.WriterModify(ctx, r, secrets.NewAPI(),
		func(r *secrets.API) error {
			apiSecrets := r.TypedSpec()

			apiSecrets.AcceptedCAs = []*x509.PEMEncodedCertificate{
				{
					Crt: ca,
				},
			}
			apiSecrets.Server = serverCert

			return nil
		}); err != nil {
		return fmt.Errorf("error modifying resource: %w", err)
	}

	serverFingerprint, _ := x509.SPKIFingerprintFromPEM(serverCert.Crt) //nolint:errcheck

	logger.Debug("generated new certificates",
		zap.Stringer("server", serverFingerprint),
	)

	return nil
}

func (ctrl *GRPCTLSController) teardownAll(ctx context.Context, r controller.Runtime) error {
	list, err := r.List(ctx, resource.NewMetadata(secrets.NamespaceName, secrets.APIType, "", resource.VersionUndefined))
	if err != nil {
		return err
	}

	for _, res := range list.Items {
		if err = r.Destroy(ctx, res.Metadata()); err != nil {
			return err
		}
	}

	return nil
}
