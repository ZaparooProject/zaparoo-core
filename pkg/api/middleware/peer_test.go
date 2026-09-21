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

package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

type peerTestConn struct{ net.Conn }

func (peerTestConn) LocalAddr() net.Addr { return &net.UnixAddr{Name: "private.sock", Net: "unix"} }

func unixRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
	r.RemoteAddr = "@"
	return r.WithContext(PeerContext(r.Context(), peerTestConn{}))
}

func TestUnixAuthFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, configured, supplied string
		want                       int
	}{
		{"missing_configuration", "", "", http.StatusUnauthorized},
		{"missing_key", "test-key", "", http.StatusUnauthorized},
		{"wrong_key", "test-key", "wrong-key", http.StatusUnauthorized},
		{"valid_key", "test-key", "test-key", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			auth := NewAuthConfig(func() []string { return []string{tc.configured} })
			r := unixRequest(t, "/api")
			// Even an address rewritten to look like TCP loopback cannot exempt Unix auth.
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set("Authorization", "Bearer "+tc.supplied)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.True(t, APIKeyAuthenticated(r))
				w.WriteHeader(http.StatusNoContent)
			})
			response := httptest.NewRecorder()
			UnixAuthMiddleware(auth)(next).ServeHTTP(response, r)
			assert.Equal(t, tc.want, response.Code)
			assert.Equal(t, tc.want == http.StatusNoContent, WebSocketAuthHandler(auth, r))
		})
	}
}

func TestUnixAuthenticationCoversBootstrapAndDebug(t *testing.T) {
	t.Parallel()
	auth := NewAuthConfig(func() []string { return nil })
	handler := UnixAuthMiddleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/api/pair/start", "/debug/pprof/", "/app/", "/api", "/health"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, unixRequest(t, path))
		want := http.StatusUnauthorized
		if path == "/health" {
			want = http.StatusNoContent
		}
		assert.Equal(t, want, response.Code, path)
	}
}

func TestUnixLocalityRequiresTransportEvidence(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
	r.RemoteAddr = "@"
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	r.Header.Set("X-Local-Transport", "unix")
	assert.False(t, IsLocalRequest(r))
	assert.True(t, IsLocalRequest(unixRequest(t, "/api")))
}

func TestUnixPassesLocalFiltersAndRatePolicy(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiterWithLimits(rate.Limit(0), 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := NonWSIPFilterMiddleware(func() []string { return nil })(HTTPRateLimitMiddleware(limiter)(next))
	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, unixRequest(t, "/api"))
		assert.Equal(t, http.StatusNoContent, response.Code)
	}
}
