// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/types/security"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
)

func TestWithTransportCredentials(t *testing.T) {
	t.Parallel()

	ca, err := x509.NewSelfSignedCertificateAuthority(x509.ECDSA(true))
	require.NoError(t, err)

	serverCert, err := x509.NewKeyPair(ca, x509.IPAddresses([]net.IP{net.ParseIP("127.0.0.1")}), x509.ECDSA(true))
	require.NoError(t, err)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer(grpc.Creds(credentials.NewServerTLSFromCert(serverCert.Certificate)))
	healthpb.RegisterHealthServer(server, health.NewServer())

	go server.Serve(listener) //nolint:errcheck

	t.Cleanup(server.Stop)

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	dial := func() error {
		transportCredentials, credentialsErr := controllers.WithTransportCredentials(t.Context(), st, false)
		require.NoError(t, credentialsErr)

		conn, connErr := grpc.NewClient(listener.Addr().String(), transportCredentials)
		require.NoError(t, connErr)

		defer conn.Close() //nolint:errcheck

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		_, checkErr := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})

		return checkErr
	}

	require.ErrorContains(t, dial(), "certificate signed by unknown authority", "a machine without the CA in its config does not trust the server")

	roots := security.NewTrustedRootsConfigV1Alpha1()
	roots.MetaName = "omni-trusted-roots"
	roots.Certificates = string(ca.CrtPEM)

	cfg, err := container.New(roots)
	require.NoError(t, err)

	require.NoError(t, st.Create(t.Context(), config.NewMachineConfig(cfg)))

	require.NoError(t, dial(), "a machine with the CA in its config trusts the server")
}
