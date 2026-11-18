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

package jellyfin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newQuickConnectServer serves the Quick Connect endpoints from handlers
// keyed by "METHOD /path", answering 404 for anything else.
func newQuickConnectServer(t *testing.T, routes map[string]http.HandlerFunc) *LinkClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), `DeviceId="device-1"`)
		h, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := NewLinkClient(srv.URL+"/", "device-1")
	require.NoError(t, err)
	return c
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func reply(v any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, v) }
}

func TestNewLinkClient(t *testing.T) {
	t.Parallel()

	c, err := NewLinkClient("jellyfin.local:8096/", "device-1")
	require.NoError(t, err)
	assert.Equal(t, "http://jellyfin.local:8096", c.BaseURL())
	assert.Equal(t, "http://jellyfin.local:8096/web/#/quickconnect", c.VerificationURL())

	_, err = NewLinkClient("", "device-1")
	require.ErrorIs(t, err, ErrNotConfigured)
	_, err = NewLinkClient("ftp://jellyfin.local", "device-1")
	require.Error(t, err)
}

func TestLinkClient_QuickConnectFlow(t *testing.T) {
	t.Parallel()

	c := newQuickConnectServer(t, map[string]http.HandlerFunc{
		"GET /QuickConnect/Enabled":   reply(true),
		"POST /QuickConnect/Initiate": reply(QuickConnect{Secret: "s3cret", Code: "123456"}),
		"GET /QuickConnect/Connect": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "s3cret", r.URL.Query().Get("secret"))
			writeJSON(w, QuickConnect{Secret: "s3cret", Code: "123456", Authenticated: true})
		},
		"POST /Users/AuthenticateWithQuickConnect": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "s3cret", body["Secret"])
			writeJSON(w, map[string]string{"AccessToken": "token-1"})
		},
	})
	ctx := t.Context()

	enabled, err := c.QuickConnectEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)

	qc, err := c.InitiateQuickConnect(ctx)
	require.NoError(t, err)
	assert.Equal(t, "123456", qc.Code)

	state, err := c.QuickConnectState(ctx, qc.Secret)
	require.NoError(t, err)
	assert.True(t, state.Authenticated)

	token, err := c.AuthenticateWithQuickConnect(ctx, qc.Secret)
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)
}

func TestLinkClient_QuickConnectErrors(t *testing.T) {
	t.Parallel()

	t.Run("enabled check fails", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"GET /QuickConnect/Enabled": status(http.StatusInternalServerError),
		})
		_, err := c.QuickConnectEnabled(t.Context())
		require.Error(t, err)
	})

	t.Run("initiate refused means disabled", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"POST /QuickConnect/Initiate": status(http.StatusUnauthorized),
		})
		_, err := c.InitiateQuickConnect(t.Context())
		require.ErrorIs(t, err, ErrQuickConnectDisabled)
	})

	t.Run("initiate server error", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"POST /QuickConnect/Initiate": status(http.StatusInternalServerError),
		})
		_, err := c.InitiateQuickConnect(t.Context())
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrQuickConnectDisabled)
	})

	t.Run("initiate without a code", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"POST /QuickConnect/Initiate": reply(QuickConnect{Secret: "s3cret"}),
		})
		_, err := c.InitiateQuickConnect(t.Context())
		require.ErrorContains(t, err, "incomplete")
	})

	t.Run("state of a forgotten request", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, nil)
		_, err := c.QuickConnectState(t.Context(), "gone")
		require.ErrorIs(t, err, ErrQuickConnectExpired)
	})

	t.Run("state after quick connect is turned off", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"GET /QuickConnect/Connect": status(http.StatusUnauthorized),
		})
		_, err := c.QuickConnectState(t.Context(), "s3cret")
		require.ErrorIs(t, err, ErrQuickConnectDisabled)
	})

	t.Run("state server error", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"GET /QuickConnect/Connect": status(http.StatusInternalServerError),
		})
		_, err := c.QuickConnectState(t.Context(), "s3cret")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrQuickConnectExpired)
		require.NotErrorIs(t, err, ErrQuickConnectDisabled)
	})

	t.Run("authenticate refused", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"POST /Users/AuthenticateWithQuickConnect": status(http.StatusUnauthorized),
		})
		_, err := c.AuthenticateWithQuickConnect(t.Context(), "s3cret")
		require.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("authenticate without a token", func(t *testing.T) {
		t.Parallel()
		c := newQuickConnectServer(t, map[string]http.HandlerFunc{
			"POST /Users/AuthenticateWithQuickConnect": reply(map[string]string{}),
		})
		_, err := c.AuthenticateWithQuickConnect(t.Context(), "s3cret")
		require.ErrorContains(t, err, "no access token")
	})
}
