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
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startNotificationAuthServer(
	t *testing.T,
	authState webSocketAuthState,
	deadline time.Duration,
) (*melody.Melody, *websocket.Conn) {
	t.Helper()
	return startNotificationAuthServerFrom(t, authState, deadline, "")
}

// startNotificationAuthServerFrom is startNotificationAuthServer with the
// client seen at remoteAddr, or at its real loopback address when empty.
func startNotificationAuthServerFrom(
	t *testing.T,
	authState webSocketAuthState,
	deadline time.Duration,
	remoteAddr string,
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
		if remoteAddr != "" {
			r.RemoteAddr = remoteAddr
		}
		_ = sessions.HandleRequestWithKeys(w, r, map[string]any{melodySessionAuthStateKey: authState})
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

	// Wait for the session to register, so a broadcast below reaches it. An
	// armed deadline closes the session on its own, and on a loaded machine
	// that can happen before the first poll, so waiting to see it would be a
	// race the caller does not need: those tests only read from the
	// connection and expect the deadline to close it.
	if deadline == 0 {
		require.Eventually(t, func() bool {
			active, sessionsErr := sessions.Sessions()
			return sessionsErr == nil && len(active) == 1
		}, time.Second, time.Millisecond)
	}
	return sessions, conn
}

func TestPendingWebSocketReceivesNoPlaintextNotifications(t *testing.T) {
	t.Parallel()

	sessions, conn := startNotificationAuthServer(t, webSocketAuthPending, 0)
	broadcastToSessions(sessions, []byte(`{"jsonrpc":"2.0","method":"media.started"}`), nil)
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
	broadcastToSessions(sessions, want, nil)
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

func TestBluetoothNotificationsNeedTheBluetoothCapability(t *testing.T) {
	t.Parallel()

	assert.Nil(t, notificationAudience(models.NotificationStarted, "mister"),
		"ungated notifications go to every session")
	require.NotNil(t, notificationAudience(models.NotificationDeviceBluetooth, "mister"))

	local := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
	local.RemoteAddr = "127.0.0.1:5000"
	assert.True(t, sessionGrant(local, nil, "mister").Has(permissions.CapDeviceBluetooth),
		"the device's own frontend hears about nearby devices")

	remote := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api", http.NoBody)
	remote.RemoteAddr = "192.168.1.20:5000"
	assert.False(t, sessionGrant(remote, nil, "mister").Has(permissions.CapDeviceBluetooth),
		"an unpaired remote client does not")
}

// device.changed repeats device.status, so it has the same audience: any
// authenticated client, member included, and no unpaired one.
func TestDeviceChangedNeedsAnAuthenticatedClient(t *testing.T) {
	t.Parallel()

	method := models.NotificationDeviceChanged
	assert.True(t, notificationAllowed(method, permissions.Grant{IsLocal: true}))
	assert.True(t, notificationAllowed(method, permissions.Grant{APIKeyAuthenticated: true}))
	assert.True(t, notificationAllowed(method, permissions.Grant{Role: permissions.RoleAdmin}))
	assert.True(t, notificationAllowed(method, permissions.Grant{Role: permissions.RoleMember}))
	assert.False(t, notificationAllowed(method, permissions.Grant{PlatformID: "mister"}))

	member := permissions.Grant{Role: permissions.RoleMember}
	assert.False(t, notificationAllowed(models.NotificationDeviceBluetooth, member),
		"a member is authenticated but has no Bluetooth capability")
	assert.True(t, notificationAllowed(models.NotificationStarted, permissions.Grant{PlatformID: "mister"}),
		"ungated notifications reach unpaired clients")
}

func TestGatedNotificationsReachOnlyGrantedWebSockets(t *testing.T) {
	t.Parallel()

	started := []byte(`{"jsonrpc":"2.0","method":"media.started"}`)
	for _, method := range []string{models.NotificationDeviceBluetooth, models.NotificationDeviceChanged} {
		gated := []byte(`{"jsonrpc":"2.0","method":"` + method + `"}`)
		tests := []struct {
			name       string
			remoteAddr string
			want       []byte
		}{
			{name: "local", want: gated},
			{name: "remote", remoteAddr: "192.168.1.20:5000", want: started},
		}
		for _, tt := range tests {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				sessions, conn := startNotificationAuthServerFrom(t, webSocketAuthPlaintext, 0, tt.remoteAddr)
				// The first message read shows whether the gated one got through.
				broadcastToSessions(sessions, gated, notificationAudience(method, "mister"))
				broadcastToSessions(sessions, started,
					notificationAudience(models.NotificationStarted, "mister"))
				require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
				_, got, err := conn.ReadMessage()
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	}
}

func TestGatedNotificationsReachOnlyGrantedSSEClients(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		remoteAddr string
		want       string
	}{
		{name: "bluetooth local", method: models.NotificationDeviceBluetooth, want: models.NotificationDeviceBluetooth},
		{
			name: "bluetooth remote", method: models.NotificationDeviceBluetooth,
			remoteAddr: "192.168.1.20:5000", want: models.NotificationStarted,
		},
		{name: "device local", method: models.NotificationDeviceChanged, want: models.NotificationDeviceChanged},
		{
			name: "device remote", method: models.NotificationDeviceChanged,
			remoteAddr: "192.168.1.20:5000", want: models.NotificationStarted,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			platform := mocks.NewMockPlatform()
			platform.SetupBasicMock()
			st, notifCh := state.NewState(platform, "test-boot-uuid")
			t.Cleanup(st.StopService)
			notifBroker := newTestBroker(st.GetContext(), notifCh)

			handler := handleSSE(notifBroker, st, "mister")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.remoteAddr != "" {
					r.RemoteAddr = tt.remoteAddr
				}
				handler(w, r)
			}))
			t.Cleanup(server.Close)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req) //nolint:gosec // test hitting local test server
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			// The headers arrive once the stream has subscribed, so nothing
			// sent from here on is missed.
			st.Notifications <- models.Notification{Method: tt.method, Params: json.RawMessage(`{}`)}
			st.Notifications <- models.Notification{
				Method: models.NotificationStarted, Params: json.RawMessage(`{}`),
			}

			first := make(chan string, 1)
			go func() {
				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
						first <- data
						return
					}
				}
			}()
			select {
			case data := <-first:
				var obj models.NotificationObject
				require.NoError(t, json.Unmarshal([]byte(data), &obj))
				assert.Equal(t, tt.want, obj.Method)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for an SSE event")
			}
		})
	}
}
