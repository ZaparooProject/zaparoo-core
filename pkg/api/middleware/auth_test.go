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

func TestNewAuthConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		keys        []string
		wantEnabled bool
	}{
		{
			name:        "no keys - auth disabled",
			keys:        nil,
			wantEnabled: false,
		},
		{
			name:        "empty slice - auth disabled",
			keys:        []string{},
			wantEnabled: false,
		},
		{
			name:        "with keys - auth enabled",
			keys:        []string{"key1", "key2"},
			wantEnabled: true,
		},
		{
			name:        "empty strings filtered out",
			keys:        []string{"", "key1", ""},
			wantEnabled: true,
		},
		{
			name:        "all empty strings - auth disabled",
			keys:        []string{"", "", ""},
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewAuthConfig(tt.keys)

			assert.NotNil(t, cfg)
			assert.Equal(t, tt.wantEnabled, cfg.Enabled())
		})
	}
}

func TestAuthConfig_IsValidKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		checkKey string
		keys     []string
		expected bool
	}{
		{
			name:     "valid key",
			keys:     []string{"secret-key-123"},
			checkKey: "secret-key-123",
			expected: true,
		},
		{
			name:     "invalid key",
			keys:     []string{"secret-key-123"},
			checkKey: "wrong-key",
			expected: false,
		},
		{
			name:     "empty key rejected",
			keys:     []string{"secret-key-123"},
			checkKey: "",
			expected: false,
		},
		{
			name:     "multiple keys, first matches",
			keys:     []string{"key1", "key2", "key3"},
			checkKey: "key1",
			expected: true,
		},
		{
			name:     "multiple keys, last matches",
			keys:     []string{"key1", "key2", "key3"},
			checkKey: "key3",
			expected: true,
		},
		{
			name:     "multiple keys, none match",
			keys:     []string{"key1", "key2", "key3"},
			checkKey: "key4",
			expected: false,
		},
		{
			name:     "no keys configured",
			keys:     []string{},
			checkKey: "any-key",
			expected: false,
		},
		{
			name:     "key with special characters",
			keys:     []string{"key!@#$%^&*()_+-="},
			checkKey: "key!@#$%^&*()_+-=",
			expected: true,
		},
		{
			name:     "case sensitive",
			keys:     []string{"SecretKey"},
			checkKey: "secretkey",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewAuthConfig(tt.keys)
			result := cfg.IsValidKey(tt.checkKey)

			assert.Equal(t, tt.expected, result,
				"IsValidKey(%q) with keys %v: expected %v, got %v",
				tt.checkKey, tt.keys, tt.expected, result)
		})
	}
}

func TestHTTPAuthMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		authHeader     string
		queryParam     string
		expectedBody   string
		keys           []string
		expectedStatus int
	}{
		{
			name:           "no keys configured - pass through",
			keys:           []string{},
			authHeader:     "",
			queryParam:     "",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "keys configured, no key provided",
			keys:           []string{"secret"},
			authHeader:     "",
			queryParam:     "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Unauthorized: API key required\n",
		},
		{
			name:           "keys configured, invalid key via header",
			keys:           []string{"secret"},
			authHeader:     "Bearer wrong-key",
			queryParam:     "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Unauthorized: Invalid API key\n",
		},
		{
			name:           "keys configured, valid key via header",
			keys:           []string{"secret"},
			authHeader:     "Bearer secret",
			queryParam:     "",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "keys configured, valid key via query param",
			keys:           []string{"secret"},
			authHeader:     "",
			queryParam:     "secret",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "keys configured, invalid key via query param",
			keys:           []string{"secret"},
			authHeader:     "",
			queryParam:     "wrong",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Unauthorized: Invalid API key\n",
		},
		{
			name:           "header takes precedence over query param",
			keys:           []string{"secret"},
			authHeader:     "Bearer secret",
			queryParam:     "wrong",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "wrong auth scheme - falls back to query param",
			keys:           []string{"secret"},
			authHeader:     "Basic secret",
			queryParam:     "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Unauthorized: API key required\n",
		},
		{
			name:           "multiple keys, second key valid",
			keys:           []string{"key1", "key2", "key3"},
			authHeader:     "Bearer key2",
			queryParam:     "",
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewAuthConfig(tt.keys)
			middleware := HTTPAuthMiddleware(cfg)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})

			wrapped := middleware(handler)

			url := "/test"
			if tt.queryParam != "" {
				url += "?key=" + tt.queryParam
			}

			req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			recorder := httptest.NewRecorder()
			wrapped.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code,
				"expected status %d, got %d", tt.expectedStatus, recorder.Code)
			assert.Equal(t, tt.expectedBody, recorder.Body.String(),
				"expected body %q, got %q", tt.expectedBody, recorder.Body.String())
		})
	}
}

func TestWebSocketAuthHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authHeader string
		queryParam string
		keys       []string
		expected   bool
	}{
		{
			name:       "no keys configured - allowed",
			keys:       []string{},
			authHeader: "",
			queryParam: "",
			expected:   true,
		},
		{
			name:       "keys configured, no key provided",
			keys:       []string{"secret"},
			authHeader: "",
			queryParam: "",
			expected:   false,
		},
		{
			name:       "keys configured, valid key via header",
			keys:       []string{"secret"},
			authHeader: "Bearer secret",
			queryParam: "",
			expected:   true,
		},
		{
			name:       "keys configured, valid key via query param",
			keys:       []string{"secret"},
			authHeader: "",
			queryParam: "secret",
			expected:   true,
		},
		{
			name:       "keys configured, invalid key",
			keys:       []string{"secret"},
			authHeader: "Bearer wrong",
			queryParam: "",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewAuthConfig(tt.keys)

			url := "/ws"
			if tt.queryParam != "" {
				url += "?key=" + tt.queryParam
			}

			req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			result := WebSocketAuthHandler(cfg, req)

			assert.Equal(t, tt.expected, result,
				"WebSocketAuthHandler expected %v, got %v", tt.expected, result)
		})
	}
}

func TestHTTPAuthMiddleware_Integration(t *testing.T) {
	t.Parallel()

	cfg := NewAuthConfig([]string{"valid-key"})
	authMiddleware := HTTPAuthMiddleware(cfg)

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

	// Chain: auth -> testMiddleware -> handler
	wrapped := authMiddleware(testMiddleware(handler))

	// Test valid key - should reach all middlewares and handler
	callCount = 0
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-key")
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 1, callCount, "testMiddleware should be called once")

	// Test invalid key - should not reach subsequent middlewares or handler
	callCount = 0
	req = httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid-key")
	recorder = httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, 0, callCount, "testMiddleware should not be called for invalid key")

	// Test no key - should not reach subsequent middlewares or handler
	callCount = 0
	req = httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	recorder = httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, 0, callCount, "testMiddleware should not be called for missing key")
}

func TestHTTPAuthMiddleware_LocalhostExempt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		remoteAddr     string
		expectedStatus int
	}{
		{
			name:           "IPv4 loopback allowed without key",
			remoteAddr:     "127.0.0.1:12345",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv6 loopback allowed without key",
			remoteAddr:     "[::1]:12345",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-loopback requires key",
			remoteAddr:     "192.168.1.100:12345",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "public IP requires key",
			remoteAddr:     "8.8.8.8:12345",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewAuthConfig([]string{"secret-key"})
			middleware := HTTPAuthMiddleware(cfg)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})

			wrapped := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			req.RemoteAddr = tt.remoteAddr

			recorder := httptest.NewRecorder()
			wrapped.ServeHTTP(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code)
		})
	}
}

func TestWebSocketAuthHandler_LocalhostExempt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{
			name:       "IPv4 loopback allowed without key",
			remoteAddr: "127.0.0.1:12345",
			expected:   true,
		},
		{
			name:       "IPv6 loopback allowed without key",
			remoteAddr: "[::1]:12345",
			expected:   true,
		},
		{
			name:       "non-loopback requires key",
			remoteAddr: "192.168.1.100:12345",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewAuthConfig([]string{"secret-key"})

			req := httptest.NewRequest(http.MethodGet, "/ws", http.NoBody)
			req.RemoteAddr = tt.remoteAddr

			result := WebSocketAuthHandler(cfg, req)

			assert.Equal(t, tt.expected, result)
		})
	}
}
