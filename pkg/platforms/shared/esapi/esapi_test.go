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

package esapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRunningGameResponse_NoGame(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
	}{
		{name: "compact", body: `{"msg":"NO GAME RUNNING"}`},
		{name: "formatted", body: "{\n  \"msg\": \"NO GAME RUNNING\"\n}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			game, running, err := parseRunningGameResponse([]byte(tc.body))

			require.NoError(t, err)
			assert.False(t, running)
			assert.Empty(t, game)
		})
	}
}

func TestParseRunningGameResponse_RunningGame(t *testing.T) {
	t.Parallel()

	body := `{"id":"42","path":"C:\\RetroBat\\roms\\snes\\game.sfc","name":"Game","systemName":"snes"}`

	game, running, err := parseRunningGameResponse([]byte(body))

	require.NoError(t, err)
	assert.True(t, running)
	assert.Equal(t, "42", game.ID)
	assert.Equal(t, `C:\RetroBat\roms\snes\game.sfc`, game.Path)
	assert.Equal(t, "Game", game.Name)
	assert.Equal(t, "snes", game.SystemName)
}

func TestParseRunningGameResponse_InvalidGameResponse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "unknown message", body: `{"msg":"ERROR"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			game, running, err := parseRunningGameResponse([]byte(tc.body))

			require.Error(t, err)
			assert.False(t, running)
			assert.Empty(t, game)
			assert.Contains(t, err.Error(), "did not include game identity")
		})
	}
}

// TestAPIRequest_RefusedIsLauncherUnavailable verifies a request nothing is
// listening for, as when EmulationStation is not running, is reported as an
// unavailable launcher rather than an unclassified failure.
func TestAPIRequest_RefusedIsLauncherUnavailable(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	_, err = sendAPIRequest("http://"+addr, "/launch", "/userdata/roms/snes/game.sfc", time.Second)

	require.ErrorIs(t, err, platforms.ErrLauncherUnavailable)
	assert.Contains(t, err.Error(), "failed to send request")
}

// TestAPIRequest_DroppedConnectionIsNotLauncherUnavailable verifies only a
// refused connection is classified as an unavailable launcher: a server that
// accepts and then fails is still reported as a fault.
func TestAPIRequest_DroppedConnectionIsNotLauncherUnavailable(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()

	_, err = sendAPIRequest("http://"+listener.Addr().String(), "/launch", "game", time.Second)

	require.Error(t, err)
	require.NotErrorIs(t, err, platforms.ErrLauncherUnavailable)
}

func TestAPIRequest_GetWithDefaultTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	body, err := sendAPIRequest(server.URL, "/runningGame", "", 0)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
}

// The exported request targets EmulationStation's fixed local address; with
// nothing on it the launcher is unavailable rather than broken.
func TestAPIRequest_LocalEmulationStation(t *testing.T) {
	t.Parallel()

	// A refused connection returns at once, so the timeout only caps a
	// listener that never answers. It must not race the dial itself: on a
	// loaded runner a short deadline expires first and reports a timeout.
	_, err := APIRequest("/runningGame", "", 5*time.Second)
	if err != nil {
		assert.ErrorIs(t, err, platforms.ErrLauncherUnavailable)
	}
}

func TestAPIRequest_StatusCodes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runningGame":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"msg":"NO GAME RUNNING"}`))
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body, err := sendAPIRequest(server.URL, "/runningGame", "", time.Second)
	require.NoError(t, err, "ES answers 201 when no game runs")
	assert.Contains(t, string(body), "NO GAME RUNNING")

	_, err = sendAPIRequest(server.URL, "/missing", "", time.Second)
	require.ErrorIs(t, err, ErrRouteUnsupported)

	_, err = sendAPIRequest(server.URL, "/forbidden", "", time.Second)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRouteUnsupported)
}
