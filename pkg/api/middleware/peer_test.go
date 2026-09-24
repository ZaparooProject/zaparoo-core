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

// scopedUnixRequest is a Unix request accepted by the listener that carries the
// listener key scope, the way http.Server layers BaseContext under ConnContext.
func scopedUnixRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(ListenerKeyScope(t.Context()), http.MethodGet, path, http.NoBody)
	r.RemoteAddr = "@"
	return r.WithContext(PeerContext(r.Context(), peerTestConn{}))
}

func tcpRequest(t *testing.T, remoteAddr string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
	r.RemoteAddr = remoteAddr
	return r
}

func TestListenerKeysStayOnTheirListener(t *testing.T) {
	t.Parallel()
	const listenerKey, networkKey = "listener-key", "network-key"
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, tc := range []struct {
		request     func(*testing.T) *http.Request
		name        string
		key         string
		networkKeys []string
		want        int
	}{
		{
			name: "listener_key_on_its_listener", key: listenerKey, networkKeys: []string{networkKey},
			request: func(t *testing.T) *http.Request { return scopedUnixRequest(t, "/api") },
			want:    http.StatusNoContent,
		},
		{
			name: "network_key_refused_on_private_listener", key: networkKey, networkKeys: []string{networkKey},
			request: func(t *testing.T) *http.Request { return scopedUnixRequest(t, "/api") },
			want:    http.StatusUnauthorized,
		},
		{
			name: "listener_key_refused_from_network", key: listenerKey, networkKeys: []string{networkKey},
			request: func(t *testing.T) *http.Request { return tcpRequest(t, "192.168.1.50:4000") },
			want:    http.StatusUnauthorized,
		},
		{
			name: "network_key_from_network", key: networkKey, networkKeys: []string{networkKey},
			request: func(t *testing.T) *http.Request { return tcpRequest(t, "192.168.1.50:4000") },
			want:    http.StatusNoContent,
		},
		{
			name: "missing_key_from_network", key: "", networkKeys: []string{networkKey},
			request: func(t *testing.T) *http.Request { return tcpRequest(t, "192.168.1.50:4000") },
			want:    http.StatusUnauthorized,
		},
		{
			// Standalone behavior: with no configured keys this layer is open and
			// the listener key neither enables nor satisfies it.
			name: "no_network_keys_is_standalone_open", key: "", networkKeys: nil,
			request: func(t *testing.T) *http.Request { return tcpRequest(t, "192.168.1.50:4000") },
			want:    http.StatusNoContent,
		},
		{
			name: "unscoped_unix_peer_cannot_use_listener_key", key: listenerKey, networkKeys: []string{networkKey},
			request: func(t *testing.T) *http.Request { return unixRequest(t, "/api") },
			want:    http.StatusUnauthorized,
		},
		{
			name: "unscoped_unix_peer_without_network_keys_fails_closed", key: listenerKey, networkKeys: nil,
			request: func(t *testing.T) *http.Request { return unixRequest(t, "/api") },
			want:    http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			auth := NewListenerAuthConfig(
				func() []string { return tc.networkKeys },
				func() []string { return []string{listenerKey} },
			)
			r := tc.request(t)
			if tc.key != "" {
				r.Header.Set("Authorization", "Bearer "+tc.key)
			}
			response := httptest.NewRecorder()
			HTTPAuthMiddleware(auth)(next).ServeHTTP(response, r)
			assert.Equal(t, tc.want, response.Code)

			r = tc.request(t)
			if tc.key != "" {
				r.Header.Set("Authorization", "Bearer "+tc.key)
			}
			assert.Equal(t, tc.want == http.StatusNoContent, WebSocketAuthHandler(auth, r))
		})
	}
}

func TestListenerKeyScopeIsNotClientControlled(t *testing.T) {
	t.Parallel()
	auth := NewListenerAuthConfig(
		func() []string { return []string{"network-key"} },
		func() []string { return []string{"listener-key"} },
	)
	r := tcpRequest(t, "192.168.1.50:4000")
	r.Header.Set("Authorization", "Bearer listener-key")
	r.Header.Set("X-Listener-Key-Scope", "true")
	r.URL.RawQuery = "scope=listener&key=listener-key"
	assert.False(t, HasListenerKeyScope(r))
	assert.False(t, WebSocketAuthHandler(auth, r))
}

func TestAuthConfigWithoutListenerKeysIgnoresScope(t *testing.T) {
	t.Parallel()
	auth := NewAuthConfig(func() []string { return []string{"only-key"} })
	r := scopedUnixRequest(t, "/api")
	r.Header.Set("Authorization", "Bearer only-key")
	assert.True(t, WebSocketAuthHandler(auth, r))
}

// untrustedLoopbackRequest is a TCP request to a server that shares loopback
// with other apps, as http.Server builds it from an UntrustedLoopback base context.
func untrustedLoopbackRequest(t *testing.T, remoteAddr string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(UntrustedLoopback(t.Context()), http.MethodGet, "/api", http.NoBody)
	r.RemoteAddr = remoteAddr
	return r
}

func TestUntrustedLoopbackIsNeverLocal(t *testing.T) {
	t.Parallel()
	auth := NewAuthConfig(func() []string { return []string{"network-key"} })
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	noAllowedIPs := func() []string { return nil }

	for _, addr := range []string{"127.0.0.1:4000", "[::1]:4000", "127.8.9.10:4000"} {
		// Standalone: loopback is local, needs no key and is never limited.
		standalone := tcpRequest(t, addr)
		assert.True(t, IsTrustedLoopback(standalone), addr)
		assert.True(t, IsLocalRequest(standalone), addr)
		assert.True(t, WebSocketAuthHandler(auth, standalone), addr)

		r := untrustedLoopbackRequest(t, addr)
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		assert.False(t, IsTrustedLoopback(r), addr)
		assert.False(t, IsLocalRequest(r), addr)
		assert.False(t, WebSocketAuthHandler(auth, r), addr)

		response := httptest.NewRecorder()
		HTTPAuthMiddleware(auth)(next).ServeHTTP(response, untrustedLoopbackRequest(t, addr))
		assert.Equal(t, http.StatusUnauthorized, response.Code, addr)

		response = httptest.NewRecorder()
		NonWSIPFilterMiddleware(noAllowedIPs)(next).ServeHTTP(response, untrustedLoopbackRequest(t, addr))
		assert.Equal(t, http.StatusForbidden, response.Code, addr)
		response = httptest.NewRecorder()
		RunIPFilterMiddleware(noAllowedIPs, func() bool { return false })(next).
			ServeHTTP(response, untrustedLoopbackRequest(t, addr))
		assert.Equal(t, http.StatusForbidden, response.Code, addr)

		keyed := untrustedLoopbackRequest(t, addr)
		keyed.Header.Set("Authorization", "Bearer network-key")
		response = httptest.NewRecorder()
		HTTPAuthMiddleware(auth)(next).ServeHTTP(response, keyed)
		assert.Equal(t, http.StatusNoContent, response.Code, addr)
	}
}

func TestUntrustedLoopbackIsRateLimited(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	serve := func(r *http.Request) int {
		limiter := NewIPRateLimiterWithLimits(rate.Limit(0), 1)
		handler := HTTPRateLimitMiddleware(limiter)(next)
		handler.ServeHTTP(httptest.NewRecorder(), r)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response.Code
	}
	assert.Equal(t, http.StatusNoContent, serve(tcpRequest(t, "127.0.0.1:4000")), "standalone loopback is exempt")
	assert.Equal(t, http.StatusTooManyRequests, serve(untrustedLoopbackRequest(t, "127.0.0.1:4000")))
	assert.Equal(t, http.StatusTooManyRequests, serve(untrustedLoopbackRequest(t, "[::1]:4000")))
}

func TestUnixPeerStaysLocalWhenLoopbackIsUntrusted(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequestWithContext(UntrustedLoopback(t.Context()), http.MethodGet, "/api", http.NoBody)
	r.RemoteAddr = "@"
	r = r.WithContext(PeerContext(r.Context(), peerTestConn{}))
	assert.True(t, IsLocalRequest(r))
	assert.False(t, IsTrustedLoopback(r))
}
