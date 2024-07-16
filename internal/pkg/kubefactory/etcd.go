// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package kubefactory

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/siderolabs/gen/xslices"
	"github.com/siderolabs/omni/client/pkg/constants"
	"github.com/siderolabs/omni/client/pkg/panichandler"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// Etcd represents a single running embedded etcd server.
type Etcd struct {
	server *embed.Etcd
	logger *zap.Logger
	client *clientv3.Client
}

// NewEmbeddedEtcd creates new embedded etcd instance.
func NewEmbeddedEtcd(ctx context.Context, path string, logger *zap.Logger) (*Etcd, error) {
	logger = logger.WithOptions(
		// never enable debug logs for etcd, they are too chatty
		zap.IncreaseLevel(zap.ErrorLevel),
	)

	logger.Info("starting embedded etcd server", zap.String("data_dir", path))

	cfg := embed.NewConfig()
	cfg.Dir = path
	cfg.EnableGRPCGateway = false
	cfg.LogLevel = "info"
	cfg.ZapLoggerBuilder = embed.NewZapLoggerBuilder(logger)
	cfg.AuthToken = ""
	cfg.AutoCompactionMode = "periodic"
	cfg.AutoCompactionRetention = "5h"
	cfg.ExperimentalCompactHashCheckEnabled = true
	cfg.ExperimentalInitialCorruptCheck = true

	peerURL, err := url.Parse("http://localhost:0")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	cfg.ListenPeerUrls = []url.URL{*peerURL}

	embeddedServer, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to start embedded etcd: %w", err)
	}

	panichandler.Go(func() {
		for etcdErr := range embeddedServer.Err() {
			if etcdErr != nil {
				logger.Error("embedded etcd error", zap.Error(etcdErr))
			}
		}
	}, logger)

	// give etcd some time to start
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()

	select {
	case <-embeddedServer.Server.ReadyNotify():
	case <-ctx.Done():
		embeddedServer.Close()

		return nil, fmt.Errorf("etcd failed to start: %w", ctx.Err())
	case <-time.After(15 * time.Second):
		embeddedServer.Close()

		return nil, errors.New("etcd failed to start")
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   xslices.Map(embeddedServer.Clients, func(l net.Listener) string { return l.Addr().String() }),
		DialTimeout: 5 * time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(constants.GRPCMaxMessageSize)),
			grpc.WithSharedWriteBuffer(true),
		},
		Logger: logger.WithOptions(
			// never enable debug logs for etcd client, they are too chatty
			zap.IncreaseLevel(zap.InfoLevel),
		),
	})
	if err != nil {
		return nil, err
	}

	return &Etcd{
		server: embeddedServer,
		logger: logger,
		client: cli,
	}, nil
}

// Client creates a client for the server.
func (e *Etcd) Client() *clientv3.Client {
	return e.client
}

// Stop the etcd server.
func (e *Etcd) Stop() error {
	if err := e.client.Close(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("error closing client: %w", err)
	}

	e.server.Close()

	select {
	case <-e.server.Server.StopNotify():
	case <-time.After(5 * time.Second):
		return fmt.Errorf("failed to gracefully stop etcd server")
	}

	return nil
}
