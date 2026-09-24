//go:build !windows

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
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostUnixListener(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	occupied, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { require.NoError(t, occupied.Close()) }()
	addr, ok := occupied.Addr().(*net.TCPAddr)
	require.True(t, ok)

	dir := t.TempDir()
	socket := helpers.TempSocketPath(t, "s")
	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{
		DataDir: dir, ConfigDir: dir, TempDir: dir, HostManagedPaths: true,
	})
	platform.SetupBasicMock()
	cfg, err := helpers.NewTestConfigWithListenAndPort(helpers.NewMemoryFS(), dir, "127.0.0.1", addr.Port)
	require.NoError(t, err)
	st, notifications := state.NewState(platform, "host-listener-test")
	defer st.StopService()
	broker := newTestBroker(st.GetContext(), notifications)
	defer broker.Stop()
	db := &database.Database{UserDB: helpers.NewMockUserDBI(), MediaDB: helpers.NewMockMediaDBI()}
	ready := make(chan error, 1)
	done := make(chan error, 1)
	listenerOptions := ListenerOptions{
		Listener: listener,
		APIKeys:  func() []string { return []string{"host-test-key"} },
	}
	go func() {
		done <- StartWithListener(listenerOptions, platform, cfg, st, make(chan tokens.Token), nil, db,
			nil, nil, broker, nil, nil, nil, nil, nil, nil, ready, nil)
	}()
	select {
	case err = <-ready:
		require.NoError(t, err, "the occupied TCP address must not be bound")
	case <-ctx.Done():
		t.Fatal("supplied listener never reported ready")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://core.invalid/health", http.NoBody)
	require.NoError(t, err)
	response, err := client.Do(request) //nolint:gosec // DialContext confines requests to the test Unix socket.
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusOK, response.StatusCode)
	for _, key := range []string{"", "incorrect", "host-test-key"} {
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, "http://core.invalid/api/v0.1",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"version"}`))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+key)
		response, err = client.Do(request) //nolint:gosec // DialContext confines requests to the test Unix socket.
		require.NoError(t, err)
		if key == "host-test-key" {
			assert.Equal(t, http.StatusOK, response.StatusCode)
			var reply map[string]json.RawMessage
			require.NoError(t, json.NewDecoder(response.Body).Decode(&reply))
			assert.Contains(t, reply, "result")
			assert.NotContains(t, reply, "error")
		} else {
			assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		}
		require.NoError(t, response.Body.Close())
	}
	for _, encrypted := range []bool{false, true} {
		cfg.SetEncryptionEnabled(encrypted)
		dialer := websocket.Dialer{NetDialContext: transport.DialContext, HandshakeTimeout: 2 * time.Second}
		for _, key := range []string{"", "incorrect", "host-test-key"} {
			headers := http.Header{"Authorization": []string{"Bearer " + key}}
			ws, reply, dialErr := dialer.DialContext(ctx, "ws://core.invalid/api/v0.1", headers)
			if key != "host-test-key" {
				require.Error(t, dialErr)
				require.NotNil(t, reply)
				assert.Equal(t, http.StatusUnauthorized, reply.StatusCode)
				require.NoError(t, reply.Body.Close())
				continue
			}
			require.NoError(t, dialErr)
			require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
			message := []byte(`{"jsonrpc":"2.0","id":2,"method":"version"}`)
			require.NoError(t, ws.WriteMessage(websocket.TextMessage, message))
			var payload map[string]json.RawMessage
			require.NoError(t, ws.ReadJSON(&payload))
			assert.Contains(t, payload, "result", "Unix plaintext must work even when LAN encryption is required")
			assert.NotContains(t, payload, "error")
			require.NoError(t, ws.Close())
		}
	}
	st.StopService()
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("server did not stop after cancellation")
	}
	_, err = listener.Accept()
	assert.ErrorIs(t, err, net.ErrClosed)
}

func TestSuppliedListenerRejectsStartupServer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", helpers.TempSocketPath(t, "s"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()
	cfg, err := helpers.NewTestConfigWithListenAndPort(helpers.NewMemoryFS(), dir, "127.0.0.1", 0)
	require.NoError(t, err)
	st, _ := state.NewState(platform, "listener-conflict-test")
	defer st.StopService()
	startup, err := NewStartupServer(st.GetContext(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, startup.Shutdown(ctx))
	})

	ready := make(chan error, 1)
	err = StartWithListener(ListenerOptions{Listener: listener}, platform, cfg, st, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, ready, startup)
	require.ErrorContains(t, err, "cannot be combined")
	require.ErrorContains(t, <-ready, "cannot be combined")
	_, err = listener.Accept()
	assert.ErrorIs(t, err, net.ErrClosed, "ownership of the supplied listener transfers even on rejection")
}
