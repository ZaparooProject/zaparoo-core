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

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRateLimiter_AppRoutesNotLimited verifies that /app/* routes
// are not subject to rate limiting, allowing SPAs to load many assets.
func TestRateLimiter_AppRoutesNotLimited(t *testing.T) {
	t.Parallel()

	// Create mock filesystem
	mockFS := fstest.MapFS{
		"index.html": {Data: []byte("<!DOCTYPE html><html></html>")},
		"app.js":     {Data: []byte("console.log('app');")},
		"style.css":  {Data: []byte("body{}")},
	}

	// Setup router matching production structure
	r := chi.NewRouter()
	rateLimiter := middleware.NewIPRateLimiter()
	apiRateLimitMiddleware := middleware.HTTPRateLimitMiddleware(rateLimiter)

	// API routes WITH rate limiting
	r.Group(func(r chi.Router) {
		r.Use(apiRateLimitMiddleware)
		r.Get("/api/v0.1", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
	})

	// App routes WITHOUT rate limiting
	r.Get("/app/*", func(w http.ResponseWriter, req *http.Request) {
		fsCustom404(http.FS(mockFS)).ServeHTTP(w, req)
	})

	server := httptest.NewServer(r)
	defer server.Close()

	client := server.Client()
	ctx := context.Background()

	// Make more requests than the burst limit allows
	requestCount := middleware.BurstSize + 30 // 50 requests (burst is 20)
	successCount := 0

	for i := range requestCount {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/app/index.html", http.NoBody)
		require.NoError(t, err, "creating request %d", i)

		resp, err := client.Do(req)
		require.NoError(t, err, "request %d failed", i)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			successCount++
		}
	}

	// All requests should succeed - no rate limiting on /app/*
	assert.Equal(t, requestCount, successCount,
		"all /app/* requests should succeed without rate limiting")
}

// TestRateLimiter_APIRoutesLimited verifies that /api/* routes
// are subject to rate limiting after exceeding the burst limit.
func TestRateLimiter_APIRoutesLimited(t *testing.T) {
	t.Parallel()

	// Setup router matching production structure
	r := chi.NewRouter()
	rateLimiter := middleware.NewIPRateLimiter()
	apiRateLimitMiddleware := middleware.HTTPRateLimitMiddleware(rateLimiter)

	// API routes WITH rate limiting
	r.Group(func(r chi.Router) {
		r.Use(apiRateLimitMiddleware)
		r.Get("/api/v0.1", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
	})

	server := httptest.NewServer(r)
	defer server.Close()

	client := server.Client()
	ctx := context.Background()

	// Make more requests than the burst limit allows
	requestCount := middleware.BurstSize + 10 // 30 requests (burst is 20)
	rateLimitedCount := 0

	for i := range requestCount {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v0.1", http.NoBody)
		require.NoError(t, err, "creating request %d", i)

		resp, err := client.Do(req)
		require.NoError(t, err, "request %d failed", i)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	// Some requests should be rate limited
	assert.Positive(t, rateLimitedCount,
		"some /api/* requests should be rate limited after exceeding burst")
}

// TestRateLimiter_RunRoutesLimited verifies that /run/* action routes
// are subject to rate limiting.
func TestRateLimiter_RunRoutesLimited(t *testing.T) {
	t.Parallel()

	// Setup router matching production structure
	r := chi.NewRouter()
	rateLimiter := middleware.NewIPRateLimiter()
	apiRateLimitMiddleware := middleware.HTTPRateLimitMiddleware(rateLimiter)

	// Run routes WITH rate limiting (same group as API)
	r.Group(func(r chi.Router) {
		r.Use(apiRateLimitMiddleware)
		r.Get("/run/*", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	server := httptest.NewServer(r)
	defer server.Close()

	client := server.Client()
	ctx := context.Background()

	// Make more requests than the burst limit allows
	requestCount := middleware.BurstSize + 10
	rateLimitedCount := 0

	for i := range requestCount {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/run/test", http.NoBody)
		require.NoError(t, err, "creating request %d", i)

		resp, err := client.Do(req)
		require.NoError(t, err, "request %d failed", i)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	// Some requests should be rate limited
	assert.Positive(t, rateLimitedCount,
		"some /run/* requests should be rate limited after exceeding burst")
}
