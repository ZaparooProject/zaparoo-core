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

package shared

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSameOriginRedirect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from, to string
		allowed  bool
	}{
		{"http://plex.lan:32400/a", "http://plex.lan:32400/b", true},
		{"http://plex.lan/a", "http://PLEX.lan:80/b", true},
		{"https://plex.lan/a", "https://plex.lan:443/b", true},
		{"http://plex.lan:32400/a", "https://plex.lan:32443/b", true},
		{"https://plex.lan/a", "http://plex.lan/b", false},
		{"http://plex.lan:32400/a", "http://plex.lan:8080/b", false},
		{"http://plex.lan/a", "http://evil.example/b", false},
		{"https://plex.lan/a", "https://evil.example/b", false},
	}
	for _, tt := range tests {
		from := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.from, http.NoBody)
		to := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.to, http.NoBody)
		err := SameOriginRedirect(to, []*http.Request{from})
		if tt.allowed {
			assert.NoError(t, err, "%s -> %s", tt.from, tt.to)
		} else {
			assert.Error(t, err, "%s -> %s", tt.from, tt.to)
		}
	}
}

func TestSameOriginRedirectChecksOriginalRequest(t *testing.T) {
	t.Parallel()
	first := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://plex.lan/a", http.NoBody)
	hop := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://plex.lan/b", http.NoBody)
	to := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://plex.lan/c", http.NoBody)
	require.Error(t, SameOriginRedirect(to, []*http.Request{first, hop}),
		"a later hop is still held to the first request's origin")
}

func TestSameOriginRedirectLimit(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://plex.lan/a", http.NoBody)
	via := make([]*http.Request, maxRedirects)
	for i := range via {
		via[i] = req
	}
	require.ErrorContains(t, SameOriginRedirect(req, via), "stopped after")
	require.NoError(t, SameOriginRedirect(req, via[:maxRedirects-1]))
}

func TestSameOriginRedirectWithClient(t *testing.T) {
	t.Parallel()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("X-Plex-Token"), "the token must not reach another origin")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(other.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	t.Cleanup(origin.Close)
	client := &http.Client{CheckRedirect: SameOriginRedirect}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL, http.NoBody)
	require.NoError(t, err)
	req.Header.Set("X-Plex-Token", "secret")
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "a redirect to another port is refused")
}
