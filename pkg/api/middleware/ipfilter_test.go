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

// ipsProvider wraps a slice as an IPsProvider for testing.
func ipsProvider(ips []string) IPsProvider {
	return func() []string { return ips }
}

func TestParseAllowedIPs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		allowedIPs    []string
		expectedNets  int
		expectedAddrs int
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
			name:          "invalid IP filtered out",
			allowedIPs:    []string{"invalid"},
			expectedNets:  0,
			expectedAddrs: 0,
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

			nets, addrs := parseAllowedIPs(tt.allowedIPs)

			assert.Len(t, nets, tt.expectedNets,
				"expected %d networks, got %d", tt.expectedNets, len(nets))
			assert.Len(t, addrs, tt.expectedAddrs,
				"expected %d addresses, got %d", tt.expectedAddrs, len(addrs))
		})
	}
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

func TestNonWSIPFilterMiddleware_LoopbackAlwaysAllowed(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := NonWSIPFilterMiddleware(ipsProvider(nil))
	wrapped := mw(next)

	tests := []string{"127.0.0.1:12345", "[::1]:12345"}
	for _, addr := range tests {
		called = false
		//nolint:noctx // test helper
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
		req.RemoteAddr = addr
		recorder := httptest.NewRecorder()
		wrapped.ServeHTTP(recorder, req)

		assert.True(t, called, "next should be called for %s", addr)
		assert.Equal(t, http.StatusOK, recorder.Code, "loopback %s should be allowed", addr)
	}
}

func TestNonWSIPFilterMiddleware_RemoteEmptyAllowlistDenied(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := NonWSIPFilterMiddleware(ipsProvider(nil))
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.50:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.False(t, called, "next must not be called when remote with empty allowlist")
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestNonWSIPFilterMiddleware_RemoteAllowlistedIP(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := NonWSIPFilterMiddleware(ipsProvider([]string{"192.168.1.50"}))
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.50:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.True(t, called, "allowlisted IP must be admitted")
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestNonWSIPFilterMiddleware_RemoteAllowlistedCIDR(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := NonWSIPFilterMiddleware(ipsProvider([]string{"10.0.0.0/8"}))
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "10.0.0.107:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.True(t, called, "IP within allowlisted CIDR must be admitted")
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestNonWSIPFilterMiddleware_RemoteNotInAllowlist(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := NonWSIPFilterMiddleware(ipsProvider([]string{"192.168.1.50", "10.0.0.0/8"}))
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "172.16.0.5:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.False(t, called, "IP not in allowlist must be denied")
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestNonWSIPFilterMiddleware_MalformedRemoteAddr(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := NonWSIPFilterMiddleware(ipsProvider([]string{"192.168.1.0/24"}))
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "bad-addr"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.False(t, called, "next must not be called when ParseRemoteIP returns nil")
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestNonWSIPFilterMiddleware_HotReload(t *testing.T) {
	t.Parallel()

	allowedIPs := []string{}
	provider := func() []string { return allowedIPs }
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := NonWSIPFilterMiddleware(provider)
	wrapped := mw(next)

	// Initially empty allowlist, remote IP denied
	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.50:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)
	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, recorder.Code)

	// Add the IP to the allowlist; next request must be admitted
	allowedIPs = []string{"192.168.1.50"}
	//nolint:noctx // test helper
	req = httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.50:12345"
	recorder = httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestRunIPFilterMiddleware_LoopbackBypass(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := RunIPFilterMiddleware(ipsProvider(nil), func() bool { return false })
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/run/test", http.NoBody)
	req.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.True(t, called, "loopback must bypass all checks")
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestRunIPFilterMiddleware_AllowRunBypass(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := RunIPFilterMiddleware(ipsProvider(nil), func() bool { return true })
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/run/test", http.NoBody)
	req.RemoteAddr = "192.168.1.50:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.True(t, called, "remote must be admitted when hasAllowRun returns true")
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestRunIPFilterMiddleware_AllowedIPsFallback(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := RunIPFilterMiddleware(ipsProvider([]string{"192.168.1.50"}), func() bool { return false })
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/run/test", http.NoBody)
	req.RemoteAddr = "192.168.1.50:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.True(t, called, "IP in AllowedIPs must be admitted via fallback")
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestRunIPFilterMiddleware_RemoteDenied(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := RunIPFilterMiddleware(ipsProvider(nil), func() bool { return false })
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/run/test", http.NoBody)
	req.RemoteAddr = "192.168.1.50:12345"
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.False(t, called, "remote must be denied when hasAllowRun is false and IP not allowed")
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestMatchAllowedIPs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ip      string
		allowed []string
		want    bool
	}{
		{name: "single IP match", ip: "192.168.1.50", allowed: []string{"192.168.1.50"}, want: true},
		{name: "single IP no match", ip: "192.168.1.50", allowed: []string{"192.168.1.51"}, want: false},
		{name: "CIDR match", ip: "10.0.0.107", allowed: []string{"10.0.0.0/8"}, want: true},
		{name: "CIDR no match", ip: "192.168.1.50", allowed: []string{"10.0.0.0/8"}, want: false},
		{name: "mixed match by IP", ip: "192.168.1.50", allowed: []string{"192.168.1.50", "10.0.0.0/8"}, want: true},
		{name: "mixed match by CIDR", ip: "10.0.0.107", allowed: []string{"192.168.1.50", "10.0.0.0/8"}, want: true},
		{name: "empty allowlist", ip: "192.168.1.50", allowed: []string{}, want: false},
		{name: "invalid entries skipped", ip: "192.168.1.50", allowed: []string{"garbage", "192.168.1.50"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ip := ParseRemoteIP(tt.ip)
			assert.Equal(t, tt.want, matchAllowedIPs(ip, tt.allowed))
		})
	}
}
