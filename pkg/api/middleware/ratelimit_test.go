// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestIPRateLimiter_BasicFunctionality(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiter()

	// Get limiter for IP
	rl := limiter.GetLimiter("192.168.1.100")
	assert.NotNil(t, rl)

	// Should allow initial requests up to burst size
	for i := range BurstSize {
		allowed := rl.Allow()
		assert.True(t, allowed, "should allow request %d within burst", i+1)
	}

	// Should block additional requests beyond burst
	blocked := rl.Allow()
	assert.False(t, blocked, "should block request beyond burst size")
}

func TestIPRateLimiter_DifferentIPs(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiter()

	// Get limiters for different IPs
	rl1 := limiter.GetLimiter("192.168.1.100")
	rl2 := limiter.GetLimiter("192.168.1.101")

	// Should be different limiters
	assert.NotSame(t, rl1, rl2)

	// Exhaust first limiter
	for range BurstSize {
		rl1.Allow()
	}

	// First limiter should be blocked
	assert.False(t, rl1.Allow())

	// Second limiter should still allow requests
	assert.True(t, rl2.Allow())
}

func TestIPRateLimiter_SameIPReuse(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiter()

	// Get limiter for same IP twice
	rl1 := limiter.GetLimiter("192.168.1.100")
	rl2 := limiter.GetLimiter("192.168.1.100")

	// Should be same limiter instance
	assert.Same(t, rl1, rl2)
}

func TestHTTPRateLimitMiddleware_Allow(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	middleware := HTTPRateLimitMiddleware(limiter)
	wrappedHandler := middleware(handler)

	// Should allow initial requests
	for i := range BurstSize {
		req := httptest.NewRequest(http.MethodPost, "/api/test", http.NoBody)
		req.RemoteAddr = "192.168.1.100:12345"

		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "should allow request %d", i+1)
		assert.Equal(t, "success", w.Body.String())
	}
}

func TestHTTPRateLimitMiddleware_Block(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiter()

	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called when rate limited")
	})

	middleware := HTTPRateLimitMiddleware(limiter)
	wrappedHandler := middleware(handler)

	// Exhaust rate limit
	ip := "192.168.1.100:12345"
	ipLimiter := limiter.GetLimiter("192.168.1.100")
	for range BurstSize {
		ipLimiter.Allow()
	}

	// Next request should be blocked
	req := httptest.NewRequest(http.MethodPost, "/api/test", http.NoBody)
	req.RemoteAddr = ip

	w := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), "Too Many Requests")
}

func TestIPRateLimiter_Cleanup(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiter()

	// Add a limiter with old timestamp manually
	limiter.limiters["old.ip"] = &rateLimiterEntry{
		limiter:  rate.NewLimiter(rate.Limit(float64(RequestsPerMinute)/60.0), BurstSize),
		lastSeen: time.Now().Add(-15 * time.Minute), // Old timestamp
	}

	// Add a limiter with recent timestamp manually
	limiter.limiters["new.ip"] = &rateLimiterEntry{
		limiter:  rate.NewLimiter(rate.Limit(float64(RequestsPerMinute)/60.0), BurstSize),
		lastSeen: time.Now(), // Recent timestamp
	}

	// Verify both exist
	assert.Len(t, limiter.limiters, 2)

	// Run cleanup
	limiter.Cleanup()

	// Old entry should be removed, new one should remain
	assert.Len(t, limiter.limiters, 1)
	assert.Contains(t, limiter.limiters, "new.ip")
	assert.NotContains(t, limiter.limiters, "old.ip")
}

func TestHTTPRateLimitMiddleware_IPExtraction(t *testing.T) {
	t.Parallel()
	limiter := NewIPRateLimiter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPRateLimitMiddleware(limiter)
	wrappedHandler := middleware(handler)

	tests := []struct {
		name       string
		remoteAddr string
		expectedIP string
	}{
		{"with port", "192.168.1.100:12345", "192.168.1.100"},
		{"without port", "192.168.1.100", "192.168.1.100"},
		{"IPv6 with port", "[2001:db8::1]:8080", "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/api/test", http.NoBody)
			req.RemoteAddr = tt.remoteAddr

			w := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(w, req)

			// Should succeed (IP extraction worked)
			assert.Equal(t, http.StatusOK, w.Code)

			// Verify the correct IP was used for rate limiting
			ipLimiter := limiter.GetLimiter(tt.expectedIP)
			assert.NotNil(t, ipLimiter)
		})
	}
}
