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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestNewIPRateLimiter_DefaultLimits(t *testing.T) {
	t.Parallel()
	rl := NewIPRateLimiter()
	assert.InDelta(t, float64(RequestsPerMinute)/60.0, float64(rl.rate), 0.001)
	assert.Equal(t, BurstSize, rl.burst)
}

func TestNewIPRateLimiterWithLimits(t *testing.T) {
	t.Parallel()
	rl := NewIPRateLimiterWithLimits(5, 2)
	assert.InDelta(t, 5.0, float64(rl.rate), 0.001)
	assert.Equal(t, 2, rl.burst)
}

func TestGetLimiter_CreateAndReuse(t *testing.T) {
	t.Parallel()
	rl := NewIPRateLimiter()

	l1 := rl.GetLimiter("192.168.1.1")
	l2 := rl.GetLimiter("192.168.1.1")
	l3 := rl.GetLimiter("192.168.1.2")

	assert.Same(t, l1, l2, "same IP must return the same limiter")
	assert.NotSame(t, l1, l3, "different IPs must return different limiters")
}

func TestCleanup_RemovesStaleEntries(t *testing.T) {
	t.Parallel()
	rl := NewIPRateLimiter()

	// Force a stale entry by setting lastSeen far in the past.
	rl.mu.Lock()
	rl.limiters["stale"] = &rateLimiterEntry{
		limiter:  rate.NewLimiter(rl.rate, rl.burst),
		lastSeen: time.Now().Add(-20 * time.Minute),
	}
	rl.mu.Unlock()

	// Add a fresh entry via the public API.
	rl.GetLimiter("fresh")

	rl.Cleanup()

	rl.mu.RLock()
	_, staleExists := rl.limiters["stale"]
	_, freshExists := rl.limiters["fresh"]
	rl.mu.RUnlock()

	assert.False(t, staleExists, "stale entry should be removed")
	assert.True(t, freshExists, "fresh entry should be kept")
}

func TestStartCleanup_StopsOnContextCancel(t *testing.T) {
	t.Parallel()
	rl := NewIPRateLimiter()
	ctx, cancel := context.WithCancel(context.Background())

	rl.StartCleanup(ctx)
	cancel()

	// No assertion needed — the goroutine exits cleanly. If it doesn't,
	// the test binary's leak detector or -race flag will catch it.
	// Give a tiny window for the goroutine to observe the cancel.
	time.Sleep(10 * time.Millisecond)
}

func TestHTTPRateLimitMiddleware_AllowsUnderLimit(t *testing.T) {
	t.Parallel()
	rl := NewIPRateLimiterWithLimits(100, 10)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := HTTPRateLimitMiddleware(rl)
	wrapped := mw(next)

	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHTTPRateLimitMiddleware_BlocksOverLimit(t *testing.T) {
	t.Parallel()
	// burst=1 so the second request is rejected.
	rl := NewIPRateLimiterWithLimits(0, 1)
	callCount := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})
	mw := HTTPRateLimitMiddleware(rl)
	wrapped := mw(next)

	for i := range 3 {
		//nolint:noctx // test helper
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if i == 0 {
			assert.Equal(t, http.StatusOK, rec.Code, "first request should pass (burst)")
		} else {
			assert.Equal(t, http.StatusTooManyRequests, rec.Code, "request %d should be rate-limited", i)
		}
	}
	assert.Equal(t, 1, callCount, "only the first request should reach the handler")
}

func TestHTTPRateLimitMiddleware_IsolatesIPs(t *testing.T) {
	t.Parallel()
	rl := NewIPRateLimiterWithLimits(0, 1)
	mw := HTTPRateLimitMiddleware(rl)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := mw(next)

	// Exhaust IP A.
	for range 2 {
		//nolint:noctx // test helper
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "10.0.0.1:1"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
	}

	// IP B should still be allowed.
	//nolint:noctx // test helper
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.2:1"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "different IP should not be rate-limited")
}

func TestHTTPRateLimitMiddleware_ExemptsLoopbackAndLocalhost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
	}{
		{name: "ipv4 loopback", remoteAddr: "127.0.0.1:12345"},
		{name: "ipv6 loopback", remoteAddr: "[::1]:12345"},
		{name: "localhost", remoteAddr: "localhost:12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rl := NewIPRateLimiterWithLimits(0, 1)
			mw := HTTPRateLimitMiddleware(rl)
			callCount := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				callCount++
				w.WriteHeader(http.StatusOK)
			})
			wrapped := mw(next)

			for range 3 {
				//nolint:noctx // test helper
				req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
				req.RemoteAddr = tt.remoteAddr
				rec := httptest.NewRecorder()
				wrapped.ServeHTTP(rec, req)

				assert.Equal(t, http.StatusOK, rec.Code)
			}
			assert.Equal(t, 3, callCount)
		})
	}
}

func TestHTTPRateLimitMiddleware_DoesNotExemptNonLoopbackHostnames(t *testing.T) {
	t.Parallel()

	rl := NewIPRateLimiterWithLimits(0, 1)
	mw := HTTPRateLimitMiddleware(rl)
	callCount := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})
	wrapped := mw(next)

	for i := range 3 {
		//nolint:noctx // test helper
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "example.test:12345"
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if i == 0 {
			assert.Equal(t, http.StatusOK, rec.Code)
		} else {
			assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		}
	}
	assert.Equal(t, 1, callCount)
}
