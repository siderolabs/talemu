// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package director provides proxy call routing facility
package director

import (
	"context"
	"regexp"
	"slices"

	"github.com/siderolabs/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Router wraps grpc-proxy StreamDirector.
type Router struct {
	localBackend     proxy.Backend
	streamedMatchers []*regexp.Regexp
}

// NewRouter builds new Router.
func NewRouter(localBackend proxy.Backend) *Router {
	return &Router{
		localBackend: localBackend,
	}
}

// Register is no-op to implement factory.Registrator interface.
//
// Actual proxy handler is installed via grpc.UnknownServiceHandler option.
func (r *Router) Register(*grpc.Server) {}

// Director implements proxy.StreamDirector function.
//
// All requests are routed to the local backend. Request forwarding via node/nodes
// headers is not supported and returns an error.
func (r *Router) Director(ctx context.Context, _ string) (proxy.Mode, []proxy.Backend, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if _, exists := md["nodes"]; exists {
			return proxy.One2One, nil, status.Error(codes.Unimplemented, "request forwarding via 'nodes' header is not supported")
		}

		if _, exists := md["node"]; exists {
			return proxy.One2One, nil, status.Error(codes.Unimplemented, "request forwarding via 'node' header is not supported")
		}
	}

	return proxy.One2One, []proxy.Backend{r.localBackend}, nil
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
