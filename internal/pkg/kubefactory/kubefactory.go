// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package kubefactory implement some minimal kubernetes runtime.
package kubefactory

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/kubernetes/cmd/kube-apiserver/app"
	"k8s.io/kubernetes/cmd/kube-apiserver/app/options"

	"github.com/siderolabs/talemu/internal/pkg/machine/network"
)

// Kubernetes is a mini fake kubelet.
type Kubernetes struct {
	etcd   *Etcd
	logger *zap.Logger

	dataDir string
	mu      sync.Mutex
}

// New creates a kubernetes simulator.
func New(ctx context.Context, dataDir string, logger *zap.Logger) (*Kubernetes, error) {
	etcd, err := NewEmbeddedEtcd(ctx, filepath.Join(dataDir, "etcd.db"), logger)
	if err != nil {
		return nil, err
	}

	return &Kubernetes{
		dataDir: dataDir,
		etcd:    etcd,
		logger:  logger,
	}, nil
}

// DeleteEtcdState removes all keys related to the cluster with the specified id.
func (k *Kubernetes) DeleteEtcdState(ctx context.Context, clusterID string) error {
	client := k.etcd.Client()

	resp, err := client.Delete(ctx, clusterPrefix(clusterID), clientv3.WithPrefix())
	if err != nil {
		return err
	}

	k.logger.Info("removed etcd state", zap.String("cluster", clusterID), zap.Int64("keys_deleted", resp.Deleted))

	return nil
}

// RunAPIService spawns an api service on the specified address and using etcd state for the cluster ID.
func (k *Kubernetes) RunAPIService(ctx context.Context, address, iface, machineID, clusterID string) error {
	certsDir := filepath.Join("_out/state/machines", machineID, "certs")

	s := options.NewServerRunOptions()

	s.ServiceAccountSigningKeyFile = filepath.Join(certsDir, "service-account.key")

	var lc net.ListenConfig

	lc.Control = network.BindToInterface(iface)

	lis, err := lc.Listen(ctx, "tcp", net.JoinHostPort(address, "6443"))
	if err != nil {
		return err
	}

	defer lis.Close() //nolint:errcheck

	s.SecureServing.ServerCert.CertKey.CertFile = filepath.Join(certsDir, "apiserver.crt")
	s.SecureServing.ServerCert.CertKey.KeyFile = filepath.Join(certsDir, "apiserver.key")
	s.SecureServing.Listener = lis
	s.SecureServing.ExternalAddress = net.ParseIP(address)

	k.mu.Lock()
	server.SetHostnameFuncForTests(machineID)

	s.ServiceClusterIPRanges = address + "/108"

	s.Authentication.Anonymous.Allow = false
	s.Authentication.ClientCert.ClientCA = filepath.Join(certsDir, "ca.crt")
	s.Authentication.ServiceAccounts.KeyFiles = []string{
		filepath.Join(certsDir, "service-account.pub"),
	}
	s.Authentication.ServiceAccounts.Issuers = []string{"api"}

	s.Etcd.StorageConfig.Transport.ServerList = k.etcd.Client().Endpoints()
	s.Etcd.StorageConfig.Prefix = clusterPrefix(clusterID)

	// set default options
	completedOptions, err := s.Complete(ctx)

	k.mu.Unlock()

	if err != nil {
		return err
	}

	// validate options
	if errs := completedOptions.Validate(); len(errs) != 0 {
		return errors.NewAggregate(errs)
	}

	return app.Run(ctx, completedOptions)
}

func clusterPrefix(clusterID string) string {
	return fmt.Sprintf("/%s/registry", clusterID)
}
