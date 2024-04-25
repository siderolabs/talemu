// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	stdlibtls "crypto/tls"
	stdlibx509 "crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync/atomic"
	"time"

	cosiv1alpha1 "github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/protobuf/server"
	"github.com/siderolabs/crypto/tls"
	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// APID is the emulated APId Talos service.
type APID struct {
	state    state.State
	shutdown chan struct{}
	eg       *errgroup.Group
}

// NewAPID creates new APID.
func NewAPID(state state.State) *APID {
	return &APID{
		state: state,
	}
}

// Run creates COSI runtime, generates certs, registers gRPC services.
// Only maintenance mode is supported right now.
func (apid *APID) Run(ctx context.Context, endpoint netip.Prefix, logger *zap.Logger) error {
	if err := apid.Stop(); err != nil {
		return err
	}

	apid.shutdown = make(chan struct{}, 1)

	resourceState := server.NewState(apid.state)

	logger.Info("starting APID", zap.String("endpoint", endpoint.Addr().String()))

	lis, err := net.Listen("tcp", net.JoinHostPort(endpoint.Addr().String(), "50000"))
	if err != nil {
		return err
	}

	provider := NewTLSProvider()
	if err = provider.Update(net.IP(endpoint.Addr().AsSlice())); err != nil {
		return err
	}

	cfg, err := tls.New(
		tls.WithClientAuthType(tls.ServerOnly),
		tls.WithServerCertificateProvider(provider),
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
		state: apid.state,
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

		s.Stop()

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
	serverCert atomic.Pointer[stdlibtls.Certificate]
}

// Update the certificate in the provider.
func (provider *TLSProvider) Update(endpoint net.IP) error {
	ca, err := x509.NewSelfSignedCertificateAuthority()
	if err != nil {
		return fmt.Errorf("failed to generate self-signed CA: %w", err)
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
		return fmt.Errorf("failed to generate maintenance server cert: %w", err)
	}

	serverCert, err := stdlibtls.X509KeyPair(server.CrtPEM, server.KeyPEM)
	if err != nil {
		return fmt.Errorf("failed to parse server cert and key into a TLS Certificate: %w", err)
	}

	provider.serverCert.Store(&serverCert)

	return nil
}

// GetCA implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetCA() ([]byte, error) {
	return nil, nil
}

// GetCACertPool implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetCACertPool() (*stdlibx509.CertPool, error) {
	return nil, nil //nolint:nilnil
}

// GetCertificate implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetCertificate(*stdlibtls.ClientHelloInfo) (*stdlibtls.Certificate, error) {
	return provider.serverCert.Load(), nil
}

// GetClientCertificate implements tls.CertificateProvider interface.
func (provider *TLSProvider) GetClientCertificate(*stdlibtls.CertificateRequestInfo) (*stdlibtls.Certificate, error) {
	return nil, nil //nolint:nilnil
}
