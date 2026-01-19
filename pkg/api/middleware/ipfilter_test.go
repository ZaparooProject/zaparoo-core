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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewIPFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		allowedIPs        []string
		expectedNets      int
		expectedAddrs     int
		expectWarningLogs bool
	}{
		{
			name:          "empty list",
			allowedIPs:    []string{},
			expectedNets:  0,
			expectedAddrs: 0,
		},
		{
			name:          "single IP",
			allowedIPs:    []string{"192.168.1.1"},
			expectedNets:  0,
			expectedAddrs: 1,
		},
		{
			name:          "single CIDR",
			allowedIPs:    []string{"192.168.1.0/24"},
			expectedNets:  1,
			expectedAddrs: 0,
		},
		{
			name:          "mixed IPs and CIDRs",
			allowedIPs:    []string{"192.168.1.1", "10.0.0.0/8", "172.16.0.5"},
			expectedNets:  1,
			expectedAddrs: 2,
		},
		{
			name:              "invalid IP",
			allowedIPs:        []string{"invalid"},
			expectedNets:      0,
			expectedAddrs:     0,
			expectWarningLogs: true,
		},
		{
			name:          "IPv6 address",
			allowedIPs:    []string{"::1", "2001:db8::/32"},
			expectedNets:  1,
			expectedAddrs: 1,
		},
		{
			name:          "localhost variations",
			allowedIPs:    []string{"127.0.0.1", "::1"},
			expectedNets:  0,
			expectedAddrs: 2,
		},
		{
			name:          "IP with port (port stripped)",
			allowedIPs:    []string{"192.168.1.1:7497"},
			expectedNets:  0,
			expectedAddrs: 1,
		},
		{
			name:          "IPv6 with port (port stripped)",
			allowedIPs:    []string{"[::1]:8080"},
			expectedNets:  0,
			expectedAddrs: 1,
		},
		{
			name:          "mixed IPs with and without ports",
			allowedIPs:    []string{"192.168.1.1:7497", "10.0.0.5", "172.16.0.1:9000"},
			expectedNets:  0,
			expectedAddrs: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filter := NewIPFilter(tt.allowedIPs)

			assert.NotNil(t, filter)
			assert.Len(t, filter.allowedIPs, len(tt.allowedIPs))
			assert.Len(t, filter.allowedNets, tt.expectedNets,
				"expected %d networks, got %d", tt.expectedNets, len(filter.allowedNets))
			assert.Len(t, filter.allowedAddrs, tt.expectedAddrs,
				"expected %d addresses, got %d", tt.expectedAddrs, len(filter.allowedAddrs))
		})
	}
}

func TestIPFilter_IsAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		allowedIPs []string
		expected   bool
	}{
		{
			name:       "empty allowlist allows all",
			allowedIPs: []string{},
			remoteAddr: "192.168.1.1:12345",
			expected:   true,
		},
		{
			name:       "exact IP match",
			allowedIPs: []string{"192.168.1.1"},
			remoteAddr: "192.168.1.1:12345",
			expected:   true,
		},
		{
			name:       "IP not in allowlist",
			allowedIPs: []string{"192.168.1.1"},
			remoteAddr: "192.168.1.2:12345",
			expected:   false,
		},
		{
			name:       "IP in CIDR range",
			allowedIPs: []string{"192.168.1.0/24"},
			remoteAddr: "192.168.1.100:12345",
			expected:   true,
		},
		{
			name:       "IP not in CIDR range",
			allowedIPs: []string{"192.168.1.0/24"},
			remoteAddr: "192.168.2.1:12345",
			expected:   false,
		},
		{
			name:       "multiple IPs, first matches",
			allowedIPs: []string{"192.168.1.1", "10.0.0.1"},
			remoteAddr: "192.168.1.1:12345",
			expected:   true,
		},
		{
			name:       "multiple IPs, second matches",
			allowedIPs: []string{"192.168.1.1", "10.0.0.1"},
			remoteAddr: "10.0.0.1:12345",
			expected:   true,
		},
		{
			name:       "multiple IPs, none match",
			allowedIPs: []string{"192.168.1.1", "10.0.0.1"},
			remoteAddr: "172.16.0.1:12345",
			expected:   false,
		},
		{
			name:       "mixed IPs and CIDRs, IP matches",
			allowedIPs: []string{"192.168.1.1", "10.0.0.0/8"},
			remoteAddr: "192.168.1.1:12345",
			expected:   true,
		},
		{
			name:       "mixed IPs and CIDRs, CIDR matches",
			allowedIPs: []string{"192.168.1.1", "10.0.0.0/8"},
			remoteAddr: "10.5.6.7:12345",
			expected:   true,
		},
		{
			name:       "localhost IPv4",
			allowedIPs: []string{"127.0.0.1"},
			remoteAddr: "127.0.0.1:8080",
			expected:   true,
		},
		{
			name:       "localhost IPv6",
			allowedIPs: []string{"::1"},
			remoteAddr: "[::1]:8080",
			expected:   true,
		},
		{
			name:       "remote addr without port",
			allowedIPs: []string{"192.168.1.1"},
			remoteAddr: "192.168.1.1",
			expected:   true,
		},
		{
			name:       "invalid remote addr",
			allowedIPs: []string{"192.168.1.1"},
			remoteAddr: "invalid",
			expected:   false,
		},
		{
			name:       "config has IP with port, connection allowed",
			allowedIPs: []string{"192.168.1.1:7497"},
			remoteAddr: "192.168.1.1:12345",
			expected:   true,
		},
		{
			name:       "config has IPv6 with port, connection allowed",
			allowedIPs: []string{"[::1]:8080"},
			remoteAddr: "[::1]:9999",
			expected:   true,
		},
		{
			name:       "config has IP with port, different IP blocked",
			allowedIPs: []string{"192.168.1.1:7497"},
			remoteAddr: "192.168.1.2:12345",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filter := NewIPFilter(tt.allowedIPs)
			result := filter.IsAllowed(tt.remoteAddr)

			assert.Equal(t, tt.expected, result,
				"IsAllowed(%q) with allowlist %v: expected %v, got %v",
				tt.remoteAddr, tt.allowedIPs, tt.expected, result)
		})
	}
}

func TestHTTPIPFilterMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		remoteAddr     string
		expectedBody   string
		allowedIPs     []string
		expectedStatus int
	}{
		{
			name:           "empty allowlist allows request",
			allowedIPs:     []string{},
			remoteAddr:     "192.168.1.1:12345",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "allowed IP passes through",
			allowedIPs:     []string{"192.168.1.1"},
			remoteAddr:     "192.168.1.1:12345",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "blocked IP returns forbidden",
			allowedIPs:     []string{"192.168.1.1"},
			remoteAddr:     "192.168.1.2:12345",
			expectedStatus: http.StatusForbidden,
			expectedBody:   "Forbidden\n",
		},
		{
			name:           "IP in CIDR range allowed",
			allowedIPs:     []string{"192.168.1.0/24"},
			remoteAddr:     "192.168.1.50:12345",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "IP outside CIDR range blocked",
			allowedIPs:     []string{"192.168.1.0/24"},
			remoteAddr:     "192.168.2.1:12345",
			expectedStatus: http.StatusForbidden,
			expectedBody:   "Forbidden\n",
		},
		{
			name:           "localhost allowed",
			allowedIPs:     []string{"127.0.0.1"},
			remoteAddr:     "127.0.0.1:8080",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filter := NewIPFilter(tt.allowedIPs)
			middleware := HTTPIPFilterMiddleware(filter)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})

			wrapped := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			req.RemoteAddr = tt.remoteAddr

			recorder := httptest.NewRecorder()
			wrapped.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code,
				"expected status %d, got %d", tt.expectedStatus, recorder.Code)
			assert.Equal(t, tt.expectedBody, recorder.Body.String(),
				"expected body %q, got %q", tt.expectedBody, recorder.Body.String())
		})
	}
}

func TestHTTPIPFilterMiddleware_Integration(t *testing.T) {
	t.Parallel()

	// Test that middleware works correctly in a chain with multiple middlewares
	filter := NewIPFilter([]string{"192.168.1.0/24"})
	ipFilterMiddleware := HTTPIPFilterMiddleware(filter)

	callCount := 0
	testMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			next.ServeHTTP(w, r)
		})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Chain: ipFilter -> testMiddleware -> handler
	wrapped := ipFilterMiddleware(testMiddleware(handler))

	// Test allowed IP - should reach all middlewares and handler
	callCount = 0
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.100:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 1, callCount, "testMiddleware should be called once")

	// Test blocked IP - should not reach subsequent middlewares or handler
	callCount = 0
	req = httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "10.0.0.1:12345"
	recorder = httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Equal(t, 0, callCount, "testMiddleware should not be called for blocked IP")
}

func TestParseRemoteIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		expected   string
		expectNil  bool
	}{
		{
			name:       "IPv4 with port",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "IPv6 with port",
			remoteAddr: "[::1]:12345",
			expected:   "::1",
		},
		{
			name:       "IPv4 without port",
			remoteAddr: "10.0.0.1",
			expected:   "10.0.0.1",
		},
		{
			name:       "invalid address",
			remoteAddr: "not-an-ip",
			expectNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ip := ParseRemoteIP(tt.remoteAddr)
			if tt.expectNil {
				assert.Nil(t, ip)
			} else {
				assert.NotNil(t, ip)
				assert.Equal(t, tt.expected, ip.String())
			}
		})
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{
			name:       "IPv4 loopback with port",
			remoteAddr: "127.0.0.1:12345",
			expected:   true,
		},
		{
			name:       "IPv4 loopback range",
			remoteAddr: "127.0.0.100:8080",
			expected:   true,
		},
		{
			name:       "IPv6 loopback with port",
			remoteAddr: "[::1]:12345",
			expected:   true,
		},
		{
			name:       "private IPv4",
			remoteAddr: "192.168.1.1:12345",
			expected:   false,
		},
		{
			name:       "public IPv4",
			remoteAddr: "8.8.8.8:53",
			expected:   false,
		},
		{
			name:       "invalid address",
			remoteAddr: "not-an-ip",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := IsLoopbackAddr(tt.remoteAddr)
			assert.Equal(t, tt.expected, result)
		})
	}
}
