// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package director provides proxy call routing facility
package director

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/siderolabs/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Router wraps grpc-proxy StreamDirector.
type Router struct {
	localBackend         proxy.Backend
	remoteBackendFactory RemoteBackendFactory
	localAddressProvider LocalAddressProvider
	streamedMatchers     []*regexp.Regexp
	nodeProxyingDisabled bool
}

// RemoteBackendFactory provides backend generation by address (target).
type RemoteBackendFactory func(target string) (proxy.Backend, error)

// NewRouter builds new Router.
func NewRouter(backendFactory RemoteBackendFactory, localBackend proxy.Backend, localAddressProvider LocalAddressProvider, nodeProxyingDisabled bool) *Router {
	return &Router{
		localBackend:         localBackend,
		remoteBackendFactory: backendFactory,
		localAddressProvider: localAddressProvider,
		nodeProxyingDisabled: nodeProxyingDisabled,
	}
}

// Register is no-op to implement factory.Registrator interface.
//
// Actual proxy handler is installed via grpc.UnknownServiceHandler option.
func (r *Router) Register(*grpc.Server) {}

// Director implements proxy.StreamDirector function.
//
// When node proxying is disabled (nodeProxyingDisabled=true), Omni connects
// directly to each node via SideroLink, so the "node" header should never
// arrive here — it is rejected. When "nodes" has a single entry, it must be
// this node itself (Omni routed directly and preserved the header).
// All "nodes" entries (single or multiple) go through One2Many fan-out via
// remote backends so that AppendInfo sets Metadata.Hostname in the response.
//
// When node proxying is enabled (nodeProxyingDisabled=false, default), requests
// with "node" are forwarded to a single remote node, and "nodes" triggers
// fan-out across all listed targets (original Talos apid behavior).
func (r *Router) Director(ctx context.Context, fullMethodName string) (proxy.Mode, []proxy.Backend, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return proxy.One2One, []proxy.Backend{r.localBackend}, nil
	}

	if _, exists := md["proxyfrom"]; exists {
		return proxy.One2One, []proxy.Backend{r.localBackend}, nil
	}

	if r.nodeProxyingDisabled {
		return r.directAccessDirector(md, fullMethodName)
	}

	return r.proxyDirector(md, fullMethodName)
}

// directAccessDirector handles the case where node proxying is disabled.
// Omni talks to each node directly via SideroLink.
func (r *Router) directAccessDirector(md metadata.MD, fullMethodName string) (proxy.Mode, []proxy.Backend, error) {
	if _, exists := md["node"]; exists {
		return proxy.One2One, nil, status.Error(codes.Unimplemented, "single-node forwarding via 'node' header is not supported")
	}

	nodes := md["nodes"]
	if len(nodes) == 0 {
		return proxy.One2One, []proxy.Backend{r.localBackend}, nil
	}

	// Single-entry "nodes" means Omni routed directly to this node and preserved
	// the header so apid sets Metadata.Hostname in the response. Verify it's actually us.
	if len(nodes) == 1 {
		if !r.localAddressProvider.IsLocalTarget(nodes[0]) {
			return proxy.One2One, nil, status.Errorf(codes.InvalidArgument, "single-entry 'nodes' target %q is not this node", nodes[0])
		}
	}

	// COSI methods do not support one-2-many proxying.
	if strings.HasPrefix(fullMethodName, "/cosi.") {
		return proxy.One2One, nil, status.Error(codes.InvalidArgument, "one-2-many proxying is not supported for COSI methods")
	}

	// Fan-out through remote backends. Even for single-entry "nodes" pointing to self,
	// we go through the fan-out path so that the remote backend's AppendInfo sets
	// Metadata.Hostname in the response (matching real Talos apid behavior).
	backends := make([]proxy.Backend, len(nodes))

	for i, target := range nodes {
		var err error

		backends[i], err = r.remoteBackendFactory(target)
		if err != nil {
			return proxy.One2Many, nil, status.Error(codes.Internal, err.Error())
		}
	}

	return proxy.One2Many, backends, nil
}

// proxyDirector handles the case where node proxying is enabled (original Talos apid behavior).
func (r *Router) proxyDirector(md metadata.MD, fullMethodName string) (proxy.Mode, []proxy.Backend, error) {
	nodes, okNodes := md["nodes"]
	node, okNode := md["node"]

	if okNode && len(node) != 1 {
		return proxy.One2One, nil, status.Error(codes.InvalidArgument, "node metadata must be single-valued")
	}

	switch {
	case okNodes:
		// COSI methods do not support one-2-many proxying.
		if strings.HasPrefix(fullMethodName, "/cosi.") {
			return proxy.One2One, nil, status.Error(codes.InvalidArgument, "one-2-many proxying is not supported for COSI methods")
		}

		return r.aggregateDirector(nodes)
	case okNode:
		return r.singleDirector(node[0])
	default:
		return proxy.One2One, []proxy.Backend{r.localBackend}, nil
	}
}

// singleDirector sends request to a single instance in one-2-one mode.
func (r *Router) singleDirector(target string) (proxy.Mode, []proxy.Backend, error) {
	backend, err := r.remoteBackendFactory(target)
	if err != nil {
		return proxy.One2One, nil, status.Error(codes.Internal, err.Error())
	}

	return proxy.One2One, []proxy.Backend{backend}, nil
}

// aggregateDirector sends request across set of remote instances and aggregates results.
func (r *Router) aggregateDirector(targets []string) (proxy.Mode, []proxy.Backend, error) {
	var err error

	backends := make([]proxy.Backend, len(targets))

	for i, target := range targets {
		backends[i], err = r.remoteBackendFactory(target)
		if err != nil {
			return proxy.One2Many, nil, status.Error(codes.Internal, err.Error())
		}
	}

	return proxy.One2Many, backends, nil
}

// StreamedDetector implements proxy.StreamedDetector.
func (r *Router) StreamedDetector(fullMethodName string) bool {
	return slices.ContainsFunc(r.streamedMatchers, func(regex *regexp.Regexp) bool { return regex.MatchString(fullMethodName) })
}

// RegisterStreamedRegex register regex for streamed method.
//
// This could be exact literal match: /^\/serviceName\/methodName$/ or any
// suffix/prefix match.
func (r *Router) RegisterStreamedRegex(regex string) {
	r.streamedMatchers = append(r.streamedMatchers, regexp.MustCompile(regex))
}
