// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// APIError is an error response from a Zaparoo Online endpoint. Code is the
// machine-readable error code when the response carried one. A code that
// backup handles itself also matches that sentinel through errors.Is, and
// reads as it always has.
type APIError struct {
	sentinel error
	// Fields names the request fields a validation error refused, as the
	// server rendered them. Empty for errors that name none.
	Fields  map[string]string
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	if e.sentinel != nil {
		return e.sentinel.Error()
	}
	return fmt.Sprintf("remote backup server returned status %d: %s", e.Status, e.Message)
}

func (e *APIError) Unwrap() error {
	return e.sentinel
}

// AsAPIError returns the API error response err carries, if any.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// IsRateLimitedError reports a request the server kept refusing with a rate
// limit after every retry.
func IsRateLimitedError(err error) bool {
	return errors.Is(err, errRemoteRateLimited)
}

// OnlineClient sends authenticated device requests to a Zaparoo Online
// endpoint for features other than backup. It shares the backup client's
// credential lookup, device headers, per-request timeouts and rate-limit
// handling, so every Online feature behaves the same on the wire.
type OnlineClient struct {
	client *remoteClient
}

// NewOnlineClient returns a client for the endpoint at baseURL. It fails with
// an error matching IsRemoteUnlinkedError when the device holds no credential
// for that endpoint, and a 401 response fails the same way.
func (m *Manager) NewOnlineClient(baseURL string) (*OnlineClient, error) {
	client, err := m.newAuthenticatedRemoteClient(baseURL, nil)
	if err != nil {
		return nil, err
	}
	return &OnlineClient{client: client}, nil
}

// WithRateLimitWaits overrides how long rate-limited requests wait before a
// retry, for tests that exercise the retry path.
func (m *Manager) WithRateLimitWaits(minWait, defaultWait, maxWait time.Duration) *Manager {
	m.rateLimitWaits = &rateLimitWaits{minWait: minWait, defaultWait: defaultWait, maxWait: maxWait}
	return m
}

// BaseURL returns the endpoint the client talks to, without a trailing slash.
func (o *OnlineClient) BaseURL() string {
	return o.client.baseURL
}

// CredentialTag returns a short digest of the device credential, so sync
// bookkeeping can tell that the device was linked again since it last synced
// without keeping the credential itself.
func (o *OnlineClient) CredentialTag() string {
	sum := sha256.Sum256([]byte(o.client.bearer))
	return hex.EncodeToString(sum[:8])
}

// DoJSON sends body encoded as JSON, or no body when it is nil, and decodes a
// successful JSON response into out unless out is nil. path may end in a
// query string.
func (o *OnlineClient) DoJSON(ctx context.Context, method, path string, body, out any) error {
	return o.client.doJSON(ctx, method, path, body, out)
}

// DoBytes sends body as application/octet-stream with the extra headers, and
// decodes a successful JSON response into out unless out is nil.
func (o *OnlineClient) DoBytes(
	ctx context.Context,
	method, path string,
	body []byte,
	headers http.Header,
	out any,
) error {
	return o.client.doBytes(ctx, method, path, body, headers, out)
}

// RetryRateLimited runs op, waiting out and retrying rate-limited responses.
// The wait honours the server's Retry-After within the client's bounds.
func (o *OnlineClient) RetryRateLimited(ctx context.Context, op func() error) error {
	return o.client.retryRateLimited(ctx, op)
}
