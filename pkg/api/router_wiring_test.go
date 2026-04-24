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

package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// buildWiringTestRouter constructs a minimal chi router that mirrors the
// middleware stack and route grouping in api.Start(). Handlers are stub
// "OK" responders so the test can isolate middleware behavior — the goal
// is to assert that the right middleware is applied to the right route
// group, not to exercise the underlying handler logic (which is covered
// by unit tests in this package).
//
// IMPORTANT: this duplicates the wiring shape from server.go's Start().
// If the production wiring drifts (a middleware moves between groups,
// a new group is added, etc.), this test will not catch it. Code review
// is the safety net for that drift; this test catches regressions in
// the *behavior* of the wiring as it exists today.
func buildWiringTestRouter(allowedIPs []string) http.Handler {
	r := chi.NewRouter()

	rateLimiter := apimiddleware.NewIPRateLimiter()
	// Pairing limiter: 1 req/sec, burst 1 — same as api.Start().
	pairingRateLimiter := apimiddleware.NewIPRateLimiterWithLimits(rate.Limit(1), 1)

	apiRateLimitMiddleware := apimiddleware.HTTPRateLimitMiddleware(rateLimiter)
	pairingRateMiddleware := apimiddleware.HTTPRateLimitMiddleware(pairingRateLimiter)
	nonWSIPFilter := apimiddleware.NonWSIPFilterMiddleware(func() []string {
		return allowedIPs
	})

	stubOK := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}

	// Pairing group: stacked rate limiters (general first, pairing
	// second), no IP filter.
	r.Group(func(r chi.Router) {
		r.Use(apiRateLimitMiddleware)
		r.Use(pairingRateMiddleware)
		r.Post("/api/pair/start", stubOK)
		r.Post("/api/pair/finish", stubOK)
	})

	// WebSocket group: rate limiter only, no IP filter (encryption
	// or API key auth handles security inside the upgrade).
	r.Group(func(r chi.Router) {
		r.Use(apiRateLimitMiddleware)
		r.Get("/api", stubOK)
		r.Get("/api/v0", stubOK)
		r.Get("/api/v0.1", stubOK)
	})

	// Non-WS group: NonWSIPFilter + rate limiter — locked down to
	// localhost / AllowedIPs.
	r.Group(func(r chi.Router) {
		r.Use(nonWSIPFilter)
		r.Use(apiRateLimitMiddleware)
		r.Post("/api", stubOK)
		r.Post("/api/v0", stubOK)
		r.Post("/api/v0.1", stubOK)
		r.Get("/r/*", stubOK)
		r.Get("/run/*", stubOK)
	})

	// SSE group: same as non-WS group.
	r.Group(func(r chi.Router) {
		r.Use(nonWSIPFilter)
		r.Use(apiRateLimitMiddleware)
		r.Get("/api/events", stubOK)
		r.Get("/api/v0/events", stubOK)
		r.Get("/api/v0.1/events", stubOK)
	})

	// Outer router endpoints — no per-group middleware.
	r.Get("/health", stubOK)
	r.Get("/app/*", stubOK)

	return r
}

func doWiringRequest(
	t *testing.T,
	srv *httptest.Server,
	method, path, remoteAddr string,
) (status int, body string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, http.NoBody)
	require.NoError(t, err)

	// Override the source IP that the server sees by routing through a
	// custom dialer is overkill — the simplest path is to issue the
	// request via the test server's handler directly with a fabricated
	// RemoteAddr.
	rec := httptest.NewRecorder()
	httpReq := req.Clone(req.Context())
	httpReq.RemoteAddr = remoteAddr
	srv.Config.Handler.ServeHTTP(rec, httpReq)

	bodyBytes, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	return rec.Code, string(bodyBytes)
}

// TestRouterWiring_PairingReachableFromRemote pins that the pairing
// endpoints are NOT behind NonWSIPFilterMiddleware — a remote client
// must be able to start pairing without being on the AllowedIPs list.
func TestRouterWiring_PairingReachableFromRemote(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter(nil))
	defer srv.Close()

	status, _ := doWiringRequest(t, srv, http.MethodPost, "/api/pair/start", "203.0.113.5:54321")
	assert.Equal(t, http.StatusOK, status,
		"pairing route must be reachable from a remote IP without AllowedIPs")
}

// TestRouterWiring_PairingRateLimited pins that the strict pairing
// rate limiter is wired into the pairing group: a second back-to-back
// request from the same IP must hit the burst-1 limit.
func TestRouterWiring_PairingRateLimited(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter(nil))
	defer srv.Close()

	const remote = "203.0.113.7:11111"
	status1, _ := doWiringRequest(t, srv, http.MethodPost, "/api/pair/start", remote)
	require.Equal(t, http.StatusOK, status1, "first pairing request must succeed")

	// Burst is 1, so the second back-to-back request from the same IP
	// must be rejected by the pairing rate limiter (or by the stacked
	// general limiter if the burst lines up; either is acceptable).
	status2, _ := doWiringRequest(t, srv, http.MethodPost, "/api/pair/start", remote)
	assert.Equal(t, http.StatusTooManyRequests, status2,
		"second back-to-back pairing request from the same IP must be rate-limited")
}

// TestRouterWiring_NonWSDeniesRemoteEmptyAllowlist pins that POST /api
// (non-WS HTTP transport) is locked down to localhost when AllowedIPs
// is empty: a remote client must get 403, not 200.
func TestRouterWiring_NonWSDeniesRemoteEmptyAllowlist(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter(nil))
	defer srv.Close()

	status, _ := doWiringRequest(t, srv, http.MethodPost, "/api", "203.0.113.5:54321")
	assert.Equal(t, http.StatusForbidden, status,
		"non-WS POST from remote must be denied with empty AllowedIPs")
}

// TestRouterWiring_NonWSAllowsLoopback pins that loopback addresses
// always bypass NonWSIPFilterMiddleware, even with an empty allowlist.
func TestRouterWiring_NonWSAllowsLoopback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter(nil))
	defer srv.Close()

	status, _ := doWiringRequest(t, srv, http.MethodPost, "/api", "127.0.0.1:54321")
	assert.Equal(t, http.StatusOK, status,
		"non-WS POST from loopback must succeed even with empty AllowedIPs")
}

// TestRouterWiring_NonWSAllowsExplicitlyAllowedRemote pins that a remote
// IP listed in AllowedIPs is allowed through.
func TestRouterWiring_NonWSAllowsExplicitlyAllowedRemote(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter([]string{"203.0.113.5"}))
	defer srv.Close()

	status, _ := doWiringRequest(t, srv, http.MethodPost, "/api", "203.0.113.5:54321")
	assert.Equal(t, http.StatusOK, status,
		"non-WS POST from explicitly allowed remote IP must succeed")
}

// TestRouterWiring_SSEDeniesRemoteEmptyAllowlist pins that the SSE
// routes share the non-WS lockdown: empty AllowedIPs ⇒ remote 403.
func TestRouterWiring_SSEDeniesRemoteEmptyAllowlist(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter(nil))
	defer srv.Close()

	status, _ := doWiringRequest(t, srv, http.MethodGet, "/api/events", "203.0.113.5:54321")
	assert.Equal(t, http.StatusForbidden, status,
		"SSE GET from remote must be denied with empty AllowedIPs")
}

// TestRouterWiring_HealthReachableFromRemote pins the behavior that /health
// is in the outer router and reachable from any IP (load balancers, uptime
// checks, app discovery flow).
func TestRouterWiring_HealthReachableFromRemote(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter(nil))
	defer srv.Close()

	status, body := doWiringRequest(t, srv, http.MethodGet, "/health", "203.0.113.5:54321")
	assert.Equal(t, http.StatusOK, status,
		"/health must be reachable from a remote IP without AllowedIPs")
	assert.Equal(t, "ok", strings.TrimSpace(body))
}

// TestRouterWiring_AppReachableFromRemote pins that /app/* is in the
// outer router and reachable from any IP (the web app is the primary
// way users interact with their device).
func TestRouterWiring_AppReachableFromRemote(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(buildWiringTestRouter(nil))
	defer srv.Close()

	status, _ := doWiringRequest(t, srv, http.MethodGet, "/app/index.html", "203.0.113.5:54321")
	assert.Equal(t, http.StatusOK, status,
		"/app/* must be reachable from a remote IP without AllowedIPs")
}
