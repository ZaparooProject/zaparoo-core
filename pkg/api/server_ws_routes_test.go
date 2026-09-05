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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketRoutesHaveNoRequestDeadline pins that the WebSocket upgrade
// routes are not wrapped in a request timeout. The upgrade handler returns
// only when the connection closes, and chi's Timeout middleware writes a 504
// header on return whenever its deadline has passed. On a hijacked connection
// net/http discards that write and logs a warning to stderr, which is captured
// into every log bundle.
func TestWebSocketRoutesHaveNoRequestDeadline(t *testing.T) {
	t.Parallel()

	passthrough := func(next http.Handler) http.Handler { return next }

	type probe struct {
		version     string
		called      bool
		hasDeadline bool
	}
	var got probe
	r := chi.NewRouter()
	mountWebSocketRoutes(r, passthrough, func(_ http.ResponseWriter, req *http.Request, version string) {
		_, hasDeadline := req.Context().Deadline()
		got = probe{version: version, called: true, hasDeadline: hasDeadline}
	})

	routes := map[string]string{
		"/api":      "latest",
		"/api/v0":   "v0",
		"/api/v0.1": "v0.1",
	}
	for path, version := range routes {
		got = probe{}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
		r.ServeHTTP(httptest.NewRecorder(), req)

		require.True(t, got.called, "%s: upgrade handler must be routed", path)
		assert.Equal(t, version, got.version, "%s: version passed to the upgrade handler", path)
		assert.False(t, got.hasDeadline,
			"%s: upgrade request context must carry no deadline; the handler runs for the life of the connection", path)
	}
}
