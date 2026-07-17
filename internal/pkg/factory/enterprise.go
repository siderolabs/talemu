// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package factory implements image factory introspection helpers.
package factory

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EnterpriseChecker detects whether an image factory is an enterprise instance.
//
// An enterprise image factory identifies itself via the Server response header on every response,
// including unauthenticated ones, which is also how Omni detects it.
type EnterpriseChecker struct {
	client *http.Client
	cache  map[string]bool

	mu sync.Mutex
}

// NewEnterpriseChecker creates a new EnterpriseChecker.
func NewEnterpriseChecker() *EnterpriseChecker {
	return &EnterpriseChecker{
		client: &http.Client{Timeout: 15 * time.Second},
		cache:  map[string]bool{},
	}
}

// IsEnterprise reports whether the image factory at the given base URL is an enterprise instance.
//
// An empty base URL means no factory and reports false without any request. Any received response
// is cached per base URL whatever its status code, as a factory does not change its identity, and
// even an error response carries the identifying header. Transport errors are returned and not
// cached, so a transient network failure does not stick.
//
// The lock only guards the cache, never the probe itself, so one unresponsive factory cannot
// stall lookups of the others. Concurrent probes of the same base URL are possible and harmless.
func (checker *EnterpriseChecker) IsEnterprise(ctx context.Context, baseURL string) (bool, error) {
	if baseURL == "" {
		return false, nil
	}

	checker.mu.Lock()
	enterprise, ok := checker.cache[baseURL]
	checker.mu.Unlock()

	if ok {
		return enterprise, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/versions", nil)
	if err != nil {
		return false, fmt.Errorf("failed to build image factory request for %q: %w", baseURL, err)
	}

	resp, err := checker.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to probe image factory %q: %w", baseURL, err)
	}

	defer resp.Body.Close() //nolint:errcheck

	enterprise = strings.Contains(resp.Header.Get("Server"), "Enterprise")

	checker.mu.Lock()
	checker.cache[baseURL] = enterprise
	checker.mu.Unlock()

	return enterprise, nil
}
