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
	suite.localBackend = &mockBackend{}
	suite.router = director.NewRouter(suite.localBackend)
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

func (suite *DirectorSuite) TestDirectorNodeHeaderRejected() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("node", "127.0.0.1")
	_, _, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.Unimplemented, status.Code(err))
}

func (suite *DirectorSuite) TestDirectorNodesHeaderRejected() {
	ctx := context.Background()

	md := metadata.New(nil)
	md.Set("nodes", "127.0.0.1", "127.0.0.2")
	_, _, err := suite.router.Director(metadata.NewIncomingContext(ctx, md), "/service.Service/method")
	suite.Assert().Error(err)
	suite.Assert().Equal(codes.Unimplemented, status.Code(err))
}

func TestDirectorSuite(t *testing.T) {
	suite.Run(t, new(DirectorSuite))
}
