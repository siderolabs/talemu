// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package factory_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/siderolabs/talemu/internal/pkg/factory"
)

func TestEnterpriseChecker(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	var enterpriseRequests, communityRequests, notFoundRequests atomic.Int32

	enterprise := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		enterpriseRequests.Add(1)

		w.Header().Set("Server", "Image Factory Enterprise v1.3.3")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(enterprise.Close)

	community := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		communityRequests.Add(1)

		w.Header().Set("Server", "Image Factory v1.3.3")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(community.Close)

	// a plain registry answering 404 without the enterprise marker
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		notFoundRequests.Add(1)

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(notFound.Close)

	checker := factory.NewEnterpriseChecker()

	// empty base URL means no factory, no request
	isEnterprise, err := checker.IsEnterprise(ctx, "")
	require.NoError(t, err)
	assert.False(t, isEnterprise)

	for range 3 {
		isEnterprise, err = checker.IsEnterprise(ctx, enterprise.URL)
		require.NoError(t, err)
		assert.True(t, isEnterprise)

		isEnterprise, err = checker.IsEnterprise(ctx, community.URL)
		require.NoError(t, err)
		assert.False(t, isEnterprise)

		isEnterprise, err = checker.IsEnterprise(ctx, notFound.URL)
		require.NoError(t, err)
		assert.False(t, isEnterprise)
	}

	// every answer is cached, whatever the status code
	assert.EqualValues(t, 1, enterpriseRequests.Load())
	assert.EqualValues(t, 1, communityRequests.Load())
	assert.EqualValues(t, 1, notFoundRequests.Load())

	// transport errors are returned and not cached
	broken := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	broken.Close()

	_, err = checker.IsEnterprise(ctx, broken.URL)
	require.Error(t, err)

	_, err = checker.IsEnterprise(ctx, broken.URL)
	require.Error(t, err)
}
