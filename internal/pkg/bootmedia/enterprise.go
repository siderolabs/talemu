// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bootmedia

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// enterpriseProbeTimeout caps the enterprise probe.
const enterpriseProbeTimeout = 15 * time.Second

// enterpriseProbe detects whether an image factory is an enterprise build.
//
// An enterprise image factory identifies itself through the Server response header on every response,
// including the unauthenticated ones, which is also how Omni detects it. Asking the factory itself is the
// only option for one that no record describes.
type enterpriseProbe struct {
	client *http.Client
	cache  map[string]bool

	mu sync.Mutex
}

func newEnterpriseProbe() *enterpriseProbe {
	return &enterpriseProbe{
		client: &http.Client{Timeout: enterpriseProbeTimeout},
		cache:  map[string]bool{},
	}
}

// isEnterprise reports whether the image factory at the given base URL is an enterprise build. An empty
// base URL means no factory and answers false without a request.
func (p *enterpriseProbe) isEnterprise(ctx context.Context, baseURL string) (bool, error) {
	if baseURL == "" {
		return false, nil
	}

	p.mu.Lock()
	enterprise, ok := p.cache[baseURL]
	p.mu.Unlock()

	if ok {
		return enterprise, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/versions", nil)
	if err != nil {
		return false, fmt.Errorf("failed to build image factory request for %q: %w", baseURL, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to probe image factory %q: %w", baseURL, err)
	}

	defer resp.Body.Close() //nolint:errcheck

	enterprise = strings.Contains(resp.Header.Get("Server"), "Enterprise")

	p.mu.Lock()
	p.cache[baseURL] = enterprise
	p.mu.Unlock()

	return enterprise, nil
}
