// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"bytes"
	"context"
	stdlibtls "crypto/tls"
	stdlibx509 "crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	cosiv1alpha1 "github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/protobuf/server"
	"github.com/siderolabs/crypto/tls"
	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/gen/xslices"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/siderolabs/talemu/internal/pkg/machine/network"
)

// APID is the emulated APId Talos service.
type APID struct {
	state       state.State
	globalState state.State
	shutdown    chan struct{}
	eg          *errgroup.Group
	machineID   string
}

// NewAPID creates new APID.
func NewAPID(machineID string, state state.State, globalState state.State) *APID {
	return &APID{
		machineID:   machineID,
		state:       state,
		globalState: globalState,
	}
}

// Run creates COSI runtime, generates certs, registers gRPC services.
// Only maintenance mode is supported right now.
func (apid *APID) Run(ctx context.Context, endpoint netip.Prefix, logger *zap.Logger, apiCerts *secrets.API, iface string) error {
	if err := apid.Stop(); err != nil {
		return err
	}

	apid.shutdown = make(chan struct{}, 1)

	resourceState := server.NewState(apid.state)

	logger.Info("starting APID", zap.String("endpoint", endpoint.Addr().String()), zap.String("interface", iface))

	var lc net.ListenConfig

	lc.Control = network.BindToInterface(iface)

	lis, err := lc.Listen(ctx, "tcp", net.JoinHostPort(endpoint.Addr().String(), strconv.FormatInt(constants.ApidPort, 10)))
	if err != nil {
		return err
	}

	tlsType := tls.ServerOnly

	if apiCerts != nil {
		tlsType = tls.Mutual
	}

	provider := NewTLSProvider()
	if err = provider.Update(net.IP(endpoint.Addr().AsSlice()), apiCerts); err != nil {
		return err
	}

	cfg, err := tls.New(
		tls.WithClientAuthType(tlsType),
		tls.WithServerCertificateProvider(provider),
		tls.WithDynamicClientCA(provider),
	)
	if err != nil {
		return err
	}

	tlsCredentials := credentials.NewTLS(cfg)

	serverOptions := []grpc.ServerOption{
		grpc.Creds(tlsCredentials),
		grpc.SharedWriteBuffer(true),
	}

	s := grpc.NewServer(
		serverOptions...,
	)

	machineSrv := &machineService{
		state:       apid.state,
		globalState: apid.globalState,
		machineID:   apid.machineID,
	}

	machine.RegisterMachineServiceServer(s, machineSrv)
	storage.RegisterStorageServiceServer(s, machineSrv)
	cosiv1alpha1.RegisterStateServer(s, resourceState)

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		err := s.Serve(lis)
		if errors.Is(err, context.Canceled) {
			return nil
		}

		return err
	})

	eg.Go(func() error {
		select {
		case <-ctx.Done():
		case <-apid.shutdown:
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		ServerGracefulStop(s, shutdownCtx) //nolint:contextcheck

		return nil
	})

	eg.Go(func() error {
		return s.Serve(lis)
	})

	apid.eg = eg

	return nil
}

// Stop shuts down the runtime and gRPC services.
func (apid *APID) Stop() error {
	if apid.shutdown == nil || apid.eg == nil {
		return nil
	}

	defer func() {
		apid.shutdown = nil
		apid.eg = nil
	}()

	apid.shutdown <- struct{}{}

	return apid.eg.Wait()
}

// NewTLSProvider creates a new TLS provider for maintenance service.
//
// The provider expects that the certificates are pushed to it.
func NewTLSProvider() *TLSProvider {
	return &TLSProvider{}
}

// TLSProvider provides TLS configuration for maintenance service.
type TLSProvider struct {
	caCertPool             *stdlibx509.CertPool
	clientCert, serverCert *stdlibtls.Certificate

	ca []byte
	mu sync.Mutex
}

// Update the certificate in the provider.
func (provider *TLSProvider) Update(endpoint net.IP, apiCerts *secrets.API) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	var (
		serverCert *stdlibtls.Certificate
		clientCert *stdlibtls.Certificate
		ca         []byte
		caCertPool *stdlibx509.CertPool
		err        error
	)

	if apiCerts == nil {
		serverCert, err = getMaintenanceCert(endpoint)
		if err != nil {
			return err
		}
	} else {
		serverCrt, err := stdlibtls.X509KeyPair(apiCerts.TypedSpec().Server.Crt, apiCerts.TypedSpec().Server.Key)
		if err != nil {
			return fmt.Errorf("failed to parse server cert and key into a TLS Certificate: %w", err)
		}

		serverCert = &serverCrt

		ca = bytes.Join(
			xslices.Map(
				apiCerts.TypedSpec().AcceptedCAs,
				func(cert *x509.PEMEncodedCertificate) []byte {
					return cert.Crt
				},
			),
			nil,
		)

		caCertPool = stdlibx509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(ca) {
			return fmt.Errorf("failed to parse CA certs into a CertPool")
		}

		if apiCerts.TypedSpec().Client != nil {
			var clientCrt stdlibtls.Certificate

			clientCrt, err = stdlibtls.X509KeyPair(apiCerts.TypedSpec().Client.Crt, apiCerts.TypedSpec().Client.Key)
			if err != nil {
				return fmt.Errorf("failed to parse client cert and key into a TLS Certificate: %w", err)
			}

			clientCert = &clientCrt
		} else {
			clientCert = nil
		}
	}

	provider.ca = ca
	provider.caCertPool = caCertPool
	provider.clientCert = clientCert
	provider.serverCert = serverCert

	return nil
}

func getMaintenanceCert(endpoint net.IP) (*stdlibtls.Certificate, error) {
	ca, err := x509.NewSelfSignedCertificateAuthority()
	if err != nil {
		return nil, fmt.Errorf("failed to generate self-signed CA: %w", err)
	}

	server, err := x509.NewKeyPair(ca,
		x509.IPAddresses([]net.IP{endpoint}),
		x509.NotAfter(time.Now().Add(x509.DefaultCertificateValidityDuration)),
		x509.KeyUsage(stdlibx509.KeyUsageDigitalSignature),
		x509.ExtKeyUsage([]stdlibx509.ExtKeyUsage{
			stdlibx509.ExtKeyUsageServerAuth,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate maintenance server cert: %w", err)
	}

	cert, err := stdlibtls.X509KeyPair(server.CrtPEM, server.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server cert and key into a TLS Certificate: %w", err)
	}

	return &cert, nil
}

// GetCA implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetCA() ([]byte, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return provider.ca, nil
}

// GetCACertPool implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetCACertPool() (*stdlibx509.CertPool, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return provider.caCertPool, nil
}

// GetCertificate implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetCertificate(*stdlibtls.ClientHelloInfo) (*stdlibtls.Certificate, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return provider.serverCert, nil
}

// GetClientCertificate implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetClientCertificate(*stdlibtls.CertificateRequestInfo) (*stdlibtls.Certificate, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	return provider.clientCert, nil
}

// ServerGracefulStop the server with a timeout.
//
// Core gRPC doesn't support timeouts.
func ServerGracefulStop(server *grpc.Server, shutdownCtx context.Context) { //nolint:revive
	stopped := make(chan struct{})

	go func() {
		server.GracefulStop()
		close(stopped)
	}()

	select {
	case <-shutdownCtx.Done():
		server.Stop()
	case <-stopped:
		server.Stop()
	}
}
