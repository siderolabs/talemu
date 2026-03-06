// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package director_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/siderolabs/grpc-proxy/proxy"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/siderolabs/talemu/internal/pkg/machine/services/apid/pkg/director"
)

type DirectorSuite struct {
	suite.Suite

	localBackend *mockBackend
	router       *director.Router
}

func (suite *DirectorSuite) SetupSuite() {
	suite.localBackend = &mockBackend{target: "local"}
	suite.router = director.NewRouter(
		mockBackendFactory,
		suite.localBackend,
		&mockLocalAddressProvider{
			local: map[string]struct{}{
				"10.0.0.1": {},
			},
		},
		true, // nodeProxyingDisabled
	)
}

func (suite *DirectorSuite) TestStreamedDetector() {
	suite.Assert().False(suite.router.StreamedDetector("/service.Service/someMethod"))

	suite.router.RegisterStreamedRegex("^" + regexp.QuoteMeta("/service.Service/someMethod") + "$")

	suite.Assert().True(suite.router.StreamedDetector("/service.Service/someMethod"))
	suite.Assert().False(suite.router.StreamedDetector("/service.Service/someMethod2"))
	suite.Assert().False(suite.router.StreamedDetector("/servicexService/someMethod"))

	suite.router.RegisterStreamedRegex("Stream$")

	suite.Assert().True(suite.router.StreamedDetector("/service.Service/getStream"))
	suite.Assert().False(suite.router.StreamedDetector("/service.Service/getStreamItem"))
}

func (suite *DirectorSuite) TestDirectorLocal() {
	ctx := context.Background()

	md := metadata.New(nil)
	mode, backends, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Equal(proxy.One2One, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().Equal(suite.localBackend, backends[0])
	suite.Assert().NoError(err)
}

func (suite *DirectorSuite) TestDirectorNoMetadata() {
	mode, backends, err := suite.router.Director(context.Background(), "/service.Service/method")
	suite.Assert().Equal(proxy.One2One, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().Equal(suite.localBackend, backends[0])
	suite.Assert().NoError(err)
}

func (suite *DirectorSuite) TestDirectorProxyFrom() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("proxyfrom", "some-node")
	md.Set("nodes", "10.0.0.1", "10.0.0.2")
	mode, backends, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Equal(proxy.One2One, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().Equal(suite.localBackend, backends[0])
	suite.Assert().NoError(err)
}

func (suite *DirectorSuite) TestDirectorNodeHeaderRejected() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("node", "10.0.0.1")
	_, _, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.Unimplemented, status.Code(err))
}

func (suite *DirectorSuite) TestDirectorSingleNodeInNodesLocal() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.1")
	mode, backends, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Equal(proxy.One2Many, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().NoError(err)
}

func (suite *DirectorSuite) TestDirectorSingleNodeInNodesNotLocal() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.99")
	_, _, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.InvalidArgument, status.Code(err))
}

func (suite *DirectorSuite) TestDirectorMultipleNodes() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.1", "10.0.0.2")
	mode, backends, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Equal(proxy.One2Many, mode)
	suite.Assert().Len(backends, 2)
	suite.Assert().NoError(err)
}

func (suite *DirectorSuite) TestDirectorSingleNodeCOSIRejected() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.1")
	_, _, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/cosi.resource.State/List")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.InvalidArgument, status.Code(err))
}

func (suite *DirectorSuite) TestDirectorMultipleNodesCOSIRejected() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.1", "10.0.0.2")
	_, _, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/cosi.resource.State/List")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.InvalidArgument, status.Code(err))
}

func TestDirectorSuite(t *testing.T) {
	suite.Run(t, new(DirectorSuite))
}

// ProxyDirectorSuite tests the proxy-enabled mode (original Talos apid behavior).
type ProxyDirectorSuite struct {
	suite.Suite

	localBackend *mockBackend
	router       *director.Router
}

func (suite *ProxyDirectorSuite) SetupSuite() {
	suite.localBackend = &mockBackend{target: "local"}
	suite.router = director.NewRouter(
		mockBackendFactory,
		suite.localBackend,
		&mockLocalAddressProvider{
			local: map[string]struct{}{
				"10.0.0.1": {},
			},
		},
		false, // nodeProxyingDisabled
	)
}

func (suite *ProxyDirectorSuite) TestNoMetadata() {
	mode, backends, err := suite.router.Director(context.Background(), "/service.Service/method")
	suite.Assert().Equal(proxy.One2One, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().Equal(suite.localBackend, backends[0])
	suite.Assert().NoError(err)
}

func (suite *ProxyDirectorSuite) TestEmptyMetadata() {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(nil))
	mode, backends, err := suite.router.Director(ctx, "/service.Service/method")
	suite.Assert().Equal(proxy.One2One, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().Equal(suite.localBackend, backends[0])
	suite.Assert().NoError(err)
}

func (suite *ProxyDirectorSuite) TestProxyFrom() {
	md := metadata.New(nil)
	md.Set("proxyfrom", "some-node")
	md.Set("nodes", "10.0.0.1", "10.0.0.2")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	mode, backends, err := suite.router.Director(ctx, "/service.Service/method")
	suite.Assert().Equal(proxy.One2One, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().Equal(suite.localBackend, backends[0])
	suite.Assert().NoError(err)
}

func (suite *ProxyDirectorSuite) TestNodeHeaderForwardsToRemote() {
	md := metadata.New(nil)
	md.Set("node", "10.0.0.2")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	mode, backends, err := suite.router.Director(ctx, "/service.Service/method")
	suite.Assert().Equal(proxy.One2One, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().NoError(err)
	// backend should be the remote one, not local
	suite.Assert().NotEqual(suite.localBackend, backends[0])
}

func (suite *ProxyDirectorSuite) TestNodeHeaderMultipleValuesRejected() {
	md := metadata.New(nil)
	md.Append("node", "10.0.0.1")
	md.Append("node", "10.0.0.2")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, _, err := suite.router.Director(ctx, "/service.Service/method")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.InvalidArgument, status.Code(err))
}

func (suite *ProxyDirectorSuite) TestSingleNodeInNodesForwardsToRemote() {
	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.2")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	mode, backends, err := suite.router.Director(ctx, "/service.Service/method")
	// in proxy mode, single-entry "nodes" still goes through fan-out, no local address check
	suite.Assert().Equal(proxy.One2Many, mode)
	suite.Assert().Len(backends, 1)
	suite.Assert().NoError(err)
}

func (suite *ProxyDirectorSuite) TestMultipleNodes() {
	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.1", "10.0.0.2")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	mode, backends, err := suite.router.Director(ctx, "/service.Service/method")
	suite.Assert().Equal(proxy.One2Many, mode)
	suite.Assert().Len(backends, 2)
	suite.Assert().NoError(err)
}

func (suite *ProxyDirectorSuite) TestMultipleNodesCOSIRejected() {
	md := metadata.New(nil)
	md.Set("nodes", "10.0.0.1", "10.0.0.2")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, _, err := suite.router.Director(ctx, "/cosi.resource.State/List")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.InvalidArgument, status.Code(err))
}

func TestProxyDirectorSuite(t *testing.T) {
	suite.Run(t, new(ProxyDirectorSuite))
}
