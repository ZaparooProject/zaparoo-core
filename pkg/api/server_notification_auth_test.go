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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startNotificationAuthServer(
	t *testing.T,
	state webSocketAuthState,
	deadline time.Duration,
) (*melody.Melody, *websocket.Conn) {
	t.Helper()

	sessions := newWebSocketSession()
	if deadline > 0 {
		sessions.HandleConnect(func(session *melody.Session) {
			startWebSocketAuthDeadline(session, deadline)
		})
		sessions.HandleDisconnect(stopWebSocketAuthDeadline)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = sessions.HandleRequestWithKeys(w, r, map[string]any{melodySessionAuthStateKey: state})
	}))
	t.Cleanup(func() {
		_ = sessions.Close()
		server.Close()
	})

	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	conn, response, err := websocket.DefaultDialer.Dial("ws://"+u.Host, nil)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.Eventually(t, func() bool {
		active, sessionsErr := sessions.Sessions()
		return sessionsErr == nil && len(active) == 1
	}, time.Second, time.Millisecond)
	return sessions, conn
}

func TestPendingWebSocketReceivesNoPlaintextNotifications(t *testing.T) {
	t.Parallel()

	sessions, conn := startNotificationAuthServer(t, webSocketAuthPending, 0)
	broadcastToSessions(sessions, []byte(`{"jsonrpc":"2.0","method":"media.started"}`))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, _, err := conn.ReadMessage()
	require.Error(t, err)
	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	assert.True(t, netErr.Timeout())
}

func TestAuthorizedPlaintextWebSocketReceivesNotifications(t *testing.T) {
	t.Parallel()

	sessions, conn := startNotificationAuthServer(t, webSocketAuthPlaintext, 0)
	want := []byte(`{"jsonrpc":"2.0","method":"media.started"}`)
	broadcastToSessions(sessions, want)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, got, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestPendingWebSocketAuthenticationDeadline(t *testing.T) {
	t.Parallel()

	_, conn := startNotificationAuthServer(t, webSocketAuthPending, 20*time.Millisecond)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := conn.ReadMessage()
	require.Error(t, err)
}
