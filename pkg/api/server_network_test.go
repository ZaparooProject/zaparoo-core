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

package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	corehelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/gorilla/websocket"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	networkTestListenerKey = "private-listener-key"
	networkTestConfigKey   = "configured-network-key"
	networkTestVersionCall = `{"jsonrpc":"2.0","id":1,"method":"version"}`
)

// networkTestServer is an API server on a Unix listener with the network
// listener requested, the arrangement an embedding host uses.
type networkTestServer struct {
	cfg       *config.Instance
	st        *state.State
	listener  net.Listener
	unix      *http.Client
	dialUnix  func(ctx context.Context, _, _ string) (net.Conn, error)
	done      chan error
	ports     chan int
	ready     error
	configDir string
}

func startNetworkTestServer(
	t *testing.T, fs *helpers.FSHelper, listenHost string, port int,
) *networkTestServer {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "s")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{
		DataDir: dir, ConfigDir: dir, TempDir: dir, HostManagedPaths: true,
	})
	platform.SetupBasicMock()
	cfg, err := helpers.NewTestConfigWithListenAndPort(fs, dir, listenHost, port)
	require.NoError(t, err)
	st, notifications := state.NewState(platform, "network-listener-test")
	t.Cleanup(st.StopService)
	broker := newTestBroker(st.GetContext(), notifications)
	t.Cleanup(broker.Stop)
	db := &database.Database{UserDB: helpers.NewMockUserDBI(), MediaDB: helpers.NewMockMediaDBI()}

	server := &networkTestServer{
		cfg: cfg, st: st, listener: listener, configDir: dir,
		done: make(chan error, 1), ports: make(chan int, 4),
		dialUnix: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	transport := &http.Transport{DialContext: server.dialUnix}
	t.Cleanup(transport.CloseIdleConnections)
	server.unix = &http.Client{Transport: transport, Timeout: 2 * time.Second}

	ready := make(chan error, 1)
	opts := ListenerOptions{
		Listener:  listener,
		APIKeys:   func() []string { return []string{networkTestListenerKey} },
		Network:   true,
		OnNetwork: func(bound int) { server.ports <- bound },
	}
	go func() {
		server.done <- StartWithListener(opts, platform, cfg, st, make(chan tokens.Token), nil, db,
			nil, nil, broker, nil, nil, nil, nil, nil, nil, ready, nil)
	}()
	select {
	case server.ready = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("server never reported ready")
	}
	return server
}

func (s *networkTestServer) boundPort(t *testing.T) int {
	t.Helper()
	select {
	case port := <-s.ports:
		return port
	case <-time.After(5 * time.Second):
		t.Fatal("OnNetwork was never called")
		return 0
	}
}

func (s *networkTestServer) stop(t *testing.T) {
	t.Helper()
	s.st.StopService()
	select {
	case err := <-s.done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
}

func networkTestPost(t *testing.T, client *http.Client, url, key string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url,
		strings.NewReader(networkTestVersionCall))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := client.Do(request) //nolint:gosec // Test URLs are loopback, a local interface or a Unix socket.
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response.StatusCode
}

// networkTestWebSocket dials the WebSocket endpoint and reports the upgrade
// status. A successful upgrade also reports whether a plaintext request was
// answered with a result.
func networkTestWebSocket(
	t *testing.T, dialer *websocket.Dialer, url, key string,
) (status int, answered bool) {
	t.Helper()
	headers := http.Header{}
	if key != "" {
		headers.Set("Authorization", "Bearer "+key)
	}
	ws, reply, err := dialer.DialContext(t.Context(), url, headers)
	if reply != nil {
		status = reply.StatusCode
		require.NoError(t, reply.Body.Close())
	}
	if err != nil {
		return status, false
	}
	defer func() { _ = ws.Close() }()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(networkTestVersionCall)))
	var payload map[string]json.RawMessage
	if err := ws.ReadJSON(&payload); err != nil {
		return status, false
	}
	_, answered = payload["result"]
	return status, answered
}

func TestNetworkListenerServesBothAndStopsBoth(t *testing.T) {
	t.Parallel()
	server := startNetworkTestServer(t, helpers.NewMemoryFS(), "127.0.0.1", 0)
	require.NoError(t, server.ready)
	port := server.boundPort(t)
	require.NotZero(t, port)
	assert.Equal(t, port, server.cfg.APIPort(), "a port-zero bind must be adopted from the network listener")
	tcpBase := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	tcp := &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(tcp.CloseIdleConnections)

	for _, target := range []struct {
		client *http.Client
		url    string
	}{{server.unix, "http://core.invalid/health"}, {tcp, tcpBase + "/health"}} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.url, http.NoBody)
		require.NoError(t, err)
		response, err := target.client.Do(request) //nolint:gosec // Test URLs are loopback or a Unix socket.
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusOK, response.StatusCode, target.url)
	}

	// The private listener still demands its key; TCP loopback is local without
	// one, exactly as it is for a standalone server.
	assert.Equal(t, http.StatusUnauthorized, networkTestPost(t, server.unix, "http://core.invalid/api/v0.1", ""))
	assert.Equal(t, http.StatusOK,
		networkTestPost(t, server.unix, "http://core.invalid/api/v0.1", networkTestListenerKey))
	assert.Equal(t, http.StatusOK, networkTestPost(t, tcp, tcpBase+"/api/v0.1", ""))

	select {
	case extra := <-server.ports:
		t.Fatalf("OnNetwork called more than once: %d", extra)
	default:
	}

	tcp.CloseIdleConnections()
	server.stop(t)
	_, err := server.listener.Accept()
	require.ErrorIs(t, err, net.ErrClosed)
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp",
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil {
		_ = conn.Close()
	}
	require.Error(t, err, "the network listener must be closed on shutdown")
}

func TestNetworkListenerBindFailureIsNotFatal(t *testing.T) {
	t.Parallel()
	occupied, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = occupied.Close() })
	addr, ok := occupied.Addr().(*net.TCPAddr)
	require.True(t, ok)

	server := startNetworkTestServer(t, helpers.NewMemoryFS(), "127.0.0.1", addr.Port)
	require.NoError(t, server.ready, "an unavailable TCP port must not fail the supplied listener")
	assert.Equal(t, http.StatusOK,
		networkTestPost(t, server.unix, "http://core.invalid/api/v0.1", networkTestListenerKey))
	assert.Equal(t, http.StatusUnauthorized, networkTestPost(t, server.unix, "http://core.invalid/api/v0.1", ""))
	server.stop(t)
	select {
	case port := <-server.ports:
		t.Fatalf("OnNetwork reported port %d for a listener that was never bound", port)
	default:
	}
}

func TestNetworkListenerRequiresSuppliedListener(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()
	cfg, err := helpers.NewTestConfigWithListenAndPort(helpers.NewMemoryFS(), dir, "127.0.0.1", 0)
	require.NoError(t, err)
	st, _ := state.NewState(platform, "network-option-test")
	defer st.StopService()
	ready := make(chan error, 1)
	called := false
	err = StartWithListener(ListenerOptions{Network: true, OnNetwork: func(int) { called = true }},
		platform, cfg, st, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, ready, nil)
	require.ErrorContains(t, err, "requires a supplied listener")
	require.ErrorContains(t, <-ready, "requires a supplied listener")
	assert.False(t, called)
}

// firstLocalIP returns an address of this machine that the server does not
// classify as loopback, so a connection to it arrives as a remote client.
func firstLocalIP(t *testing.T) string {
	t.Helper()
	ips := corehelpers.GetAllLocalIPs()
	if len(ips) == 0 {
		t.Skip("no non-loopback local IPv4 address to connect from")
	}
	return ips[0]
}

// TestNetworkListenerKeyIsolation proves over real connections that the
// private listener's key and the configured network keys never cross. It is
// serial because configured API keys are process-wide.
//
//nolint:paralleltest // Replaces the process-wide configured API keys.
func TestNetworkListenerKeyIsolation(t *testing.T) {
	localIP := firstLocalIP(t)
	fs := helpers.NewMemoryFS()
	server := startNetworkTestServer(t, fs, localIP, 0)
	require.NoError(t, server.ready)
	port := server.boundPort(t)
	hostPort := net.JoinHostPort(localIP, strconv.Itoa(port))
	tcp := &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(tcp.CloseIdleConnections)
	tcpDialer := &websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	unixDialer := &websocket.Dialer{NetDialContext: server.dialUnix, HandshakeTimeout: 2 * time.Second}
	const unixWS = "ws://core.invalid/api/v0.1"
	tcpWS := "ws://" + hostPort + "/api/v0.1"

	setConfiguredKeys := func(keys string) {
		authPath := filepath.Join(server.configDir, config.AuthFile)
		require.NoError(t, afero.WriteFile(fs.Fs, authPath, []byte("api_keys = ["+keys+"]\n"), 0o600))
		require.NoError(t, server.cfg.Load())
	}
	t.Cleanup(func() { setConfiguredKeys("") })

	t.Run("no_configured_keys", func(t *testing.T) {
		setConfiguredKeys("")
		require.Empty(t, config.GetAPIKeys())
		for _, key := range []string{"", networkTestListenerKey} {
			status, answered := networkTestWebSocket(t, tcpDialer, tcpWS, key)
			assert.Equal(t, http.StatusUnauthorized, status, "key %q", key)
			assert.False(t, answered)
		}
	})

	setConfiguredKeys(`"` + networkTestConfigKey + `"`)
	require.Equal(t, []string{networkTestConfigKey}, config.GetAPIKeys())

	t.Run("network_websocket", func(t *testing.T) {
		for _, key := range []string{"", "incorrect", networkTestListenerKey} {
			status, answered := networkTestWebSocket(t, tcpDialer, tcpWS, key)
			assert.Equal(t, http.StatusUnauthorized, status, "key %q", key)
			assert.False(t, answered)
		}
		status, answered := networkTestWebSocket(t, tcpDialer, tcpWS, networkTestConfigKey)
		assert.Equal(t, http.StatusSwitchingProtocols, status)
		assert.True(t, answered, "the configured key authenticates a network client as standalone does")
	})

	t.Run("private_websocket", func(t *testing.T) {
		for _, key := range []string{"", networkTestConfigKey} {
			status, answered := networkTestWebSocket(t, unixDialer, unixWS, key)
			assert.Equal(t, http.StatusUnauthorized, status, "key %q", key)
			assert.False(t, answered)
		}
		status, answered := networkTestWebSocket(t, unixDialer, unixWS, networkTestListenerKey)
		assert.Equal(t, http.StatusSwitchingProtocols, status)
		assert.True(t, answered)
	})

	t.Run("private_http", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized,
			networkTestPost(t, server.unix, "http://core.invalid/api/v0.1", networkTestConfigKey))
		assert.Equal(t, http.StatusOK,
			networkTestPost(t, server.unix, "http://core.invalid/api/v0.1", networkTestListenerKey))
	})

	t.Run("network_http_keeps_ip_filter", func(t *testing.T) {
		// Remote HTTP is denied by the IP allowlist before any key is read.
		for _, key := range []string{"", networkTestListenerKey, networkTestConfigKey} {
			assert.Equal(t, http.StatusForbidden,
				networkTestPost(t, tcp, "http://"+hostPort+"/api/v0.1", key), "key %q", key)
		}
	})

	t.Run("network_encryption_required", func(t *testing.T) {
		server.cfg.SetEncryptionEnabled(true)
		defer server.cfg.SetEncryptionEnabled(false)
		// With encryption required the upgrade is open and the first frame must
		// be encrypted by a paired client; no bearer key stands in for that.
		for _, key := range []string{"", networkTestListenerKey, networkTestConfigKey} {
			_, answered := networkTestWebSocket(t, tcpDialer, tcpWS, key)
			assert.False(t, answered, "key %q must not unlock plaintext over the network", key)
		}
		status, answered := networkTestWebSocket(t, unixDialer, unixWS, networkTestListenerKey)
		assert.Equal(t, http.StatusSwitchingProtocols, status)
		assert.True(t, answered, "the private listener stays plaintext for its host")
	})

	server.stop(t)
}

// TestAllowedOriginsWithoutLocalIPs covers a host that denies interface
// enumeration: nothing fails, and only origins built from a local IP are lost.
func TestAllowedOriginsWithoutLocalIPs(t *testing.T) {
	t.Parallel()
	const port = 7497
	noIPs := func() []string { return nil }
	noCustom := func() []string { return nil }
	static := buildStaticAllowedOrigins(allowedOrigins, noIPs(), port)

	assert.True(t, isAllowedOrigin("", static, noIPs, noCustom, port, true, "websocket"),
		"native clients send no Origin and stay allowed")
	assert.True(t, isAllowedOrigin(allowedOrigins[0], static, noIPs, noCustom, port, true, "websocket"))
	assert.False(t, isAllowedOrigin("http://192.168.1.20:7497", static, noIPs, noCustom, port, true, "websocket"),
		"a browser page served from the device's own IP is not recognised")
	custom := func() []string { return []string{"http://192.168.1.20:7497"} }
	assert.True(t, isAllowedOrigin("http://192.168.1.20:7497", static, noIPs, custom, port, true, "websocket"),
		"configured origins still cover it")
}
