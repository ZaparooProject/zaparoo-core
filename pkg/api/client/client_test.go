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

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConfigWithPort creates a minimal config with the given port for testing.
func testConfigWithPort(t *testing.T, port int) *config.Instance {
	t.Helper()
	fs := helpers.NewMemoryFS()
	configDir := t.TempDir()
	cfg, err := helpers.NewTestConfigWithPort(fs, configDir, port)
	require.NoError(t, err)
	return cfg
}

// parseServerPort extracts the port from an httptest.Server URL.
func parseServerPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return port
}

// unusedPort returns a port that is guaranteed to not have anything listening.
// It binds to port 0 (OS assigns a free port), gets the assigned port, then
// closes the listener. There's a small race window but it's reliable for tests.
func unusedPort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func TestLocalClient_ValidRequest(t *testing.T) {
	t.Parallel()

	// Create a WebSocket server that responds to JSON-RPC requests
	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		response := map[string]any{
			"jsonrpc": "2.0",
			"result":  map[string]any{"status": "ok"},
			"id":      request["id"],
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	result, err := LocalClient(context.Background(), cfg, "test.method", `{"key":"value"}`)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "ok", parsed["status"])
}

func TestLocalClient_EmptyParams(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		// Verify params is nil when empty string passed
		assert.Nil(t, request["params"])

		response := map[string]any{
			"jsonrpc": "2.0",
			"result":  "success",
			"id":      request["id"],
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	result, err := LocalClient(context.Background(), cfg, "test.method", "")
	require.NoError(t, err)
	assert.Equal(t, `"success"`, result)
}

func TestLocalClient_InvalidParams(t *testing.T) {
	t.Parallel()

	// Server should never be called with invalid params
	server := helpers.NewWebSocketTestServer(t, func(_ *melody.Session, _ []byte) {
		t.Error("server should not be called with invalid params")
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	_, err := LocalClient(context.Background(), cfg, "test.method", "not valid json")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidParams)
}

func TestLocalClient_ErrorResponse(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		response := map[string]any{
			"jsonrpc": "2.0",
			"error": map[string]any{
				"code":    -32600,
				"message": "Invalid Request",
			},
			"id": request["id"],
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	_, err := LocalClient(context.Background(), cfg, "invalid.method", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid Request")
}

func TestLocalClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(_ *melody.Session, _ []byte) {
		// Don't respond, let context cancel
		time.Sleep(5 * time.Second)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := LocalClient(ctx, cfg, "test.method", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRequestCancelled)
}

func TestLocalClient_Timeout(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(_ *melody.Session, _ []byte) {
		// Don't respond, let timeout occur
		time.Sleep(5 * time.Second)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	// Use a short context deadline
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := LocalClient(ctx, cfg, "test.method", "")
	require.Error(t, err)
	// Either timeout or cancellation error is acceptable
	assert.True(t, errors.Is(err, ErrRequestTimeout) || errors.Is(err, ErrRequestCancelled))
}

func TestLocalClient_IgnoresMismatchedIDs(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		callCount++
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		// First send a response with wrong ID
		wrongResponse := map[string]any{
			"jsonrpc": "2.0",
			"result":  "wrong",
			"id":      "completely-wrong-id",
		}
		wrongData, _ := json.Marshal(wrongResponse)
		_ = session.Write(wrongData)

		// Then send correct response
		correctResponse := map[string]any{
			"jsonrpc": "2.0",
			"result":  "correct",
			"id":      request["id"],
		}
		correctData, _ := json.Marshal(correctResponse)
		_ = session.Write(correctData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	result, err := LocalClient(context.Background(), cfg, "test.method", "")
	require.NoError(t, err)
	assert.Equal(t, `"correct"`, result)
}

func TestLocalClient_IgnoresInvalidJSONRPCVersion(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		// Send response with invalid JSONRPC version first
		invalidResponse := map[string]any{
			"jsonrpc": "1.0", // Wrong version
			"result":  "invalid",
			"id":      request["id"],
		}
		invalidData, _ := json.Marshal(invalidResponse)
		_ = session.Write(invalidData)

		// Send valid response
		validResponse := map[string]any{
			"jsonrpc": "2.0",
			"result":  "valid",
			"id":      request["id"],
		}
		validData, _ := json.Marshal(validResponse)
		_ = session.Write(validData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	result, err := LocalClient(context.Background(), cfg, "test.method", "")
	require.NoError(t, err)
	assert.Equal(t, `"valid"`, result)
}

func TestLocalClient_ConnectionFailure(t *testing.T) {
	t.Parallel()

	// Use a port that's guaranteed to not have anything listening
	cfg := testConfigWithPort(t, unusedPort(t))

	_, err := LocalClient(context.Background(), cfg, "test.method", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to dial websocket")
}

func TestWaitNotification_ReceivesNotification(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(_ *melody.Session, _ []byte) {
		// This handler is for requests; notifications are sent proactively
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	// Send notification after connection is established
	go func() {
		time.Sleep(50 * time.Millisecond)
		notification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.added",
			"params":  map[string]any{"token": "test123"},
		}
		data, _ := json.Marshal(notification)
		_ = server.Melody.Broadcast(data)
	}()

	result, err := WaitNotification(context.Background(), time.Second, cfg, "tokens.added")
	require.NoError(t, err)

	var params map[string]any
	err = json.Unmarshal([]byte(result), &params)
	require.NoError(t, err)
	assert.Equal(t, "test123", params["token"])
}

func TestWaitNotification_IgnoresWrongMethod(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	go func() {
		time.Sleep(50 * time.Millisecond)

		// Send wrong notification first
		wrongNotification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.removed",
			"params":  map[string]any{"wrong": true},
		}
		wrongData, _ := json.Marshal(wrongNotification)
		_ = server.Melody.Broadcast(wrongData)

		// Then send correct notification
		correctNotification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.added",
			"params":  map[string]any{"correct": true},
		}
		correctData, _ := json.Marshal(correctNotification)
		_ = server.Melody.Broadcast(correctData)
	}()

	result, err := WaitNotification(context.Background(), time.Second, cfg, "tokens.added")
	require.NoError(t, err)

	var params map[string]any
	err = json.Unmarshal([]byte(result), &params)
	require.NoError(t, err)
	assert.True(t, params["correct"].(bool))
}

func TestWaitNotification_IgnoresRequestObjects(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	go func() {
		time.Sleep(50 * time.Millisecond)

		// Send a request object (has ID) - should be ignored
		request := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.added",
			"params":  map[string]any{"from_request": true},
			"id":      "some-id",
		}
		requestData, _ := json.Marshal(request)
		_ = server.Melody.Broadcast(requestData)

		// Send a true notification (no ID)
		notification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.added",
			"params":  map[string]any{"from_notification": true},
		}
		notificationData, _ := json.Marshal(notification)
		_ = server.Melody.Broadcast(notificationData)
	}()

	result, err := WaitNotification(context.Background(), time.Second, cfg, "tokens.added")
	require.NoError(t, err)

	var params map[string]any
	err = json.Unmarshal([]byte(result), &params)
	require.NoError(t, err)
	assert.True(t, params["from_notification"].(bool))
}

func TestWaitNotification_Timeout(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	// Don't send any notification, let it timeout
	_, err := WaitNotification(context.Background(), 100*time.Millisecond, cfg, "tokens.added")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRequestTimeout)
}

func TestWaitNotification_ContextCancellation(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := WaitNotification(ctx, time.Second, cfg, "tokens.added")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRequestCancelled)
}

func TestWaitNotifications_ReceivesAnyOfMultiple(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	go func() {
		time.Sleep(50 * time.Millisecond)

		notification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.removed",
			"params":  map[string]any{"token": "removed123"},
		}
		data, _ := json.Marshal(notification)
		_ = server.Melody.Broadcast(data)
	}()

	method, params, err := WaitNotifications(
		context.Background(),
		time.Second,
		cfg,
		"tokens.added",
		"tokens.removed",
		"tokens.scanned",
	)
	require.NoError(t, err)
	assert.Equal(t, "tokens.removed", method)

	var parsedParams map[string]any
	err = json.Unmarshal([]byte(params), &parsedParams)
	require.NoError(t, err)
	assert.Equal(t, "removed123", parsedParams["token"])
}

func TestWaitNotifications_IgnoresUnregisteredMethods(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	go func() {
		time.Sleep(50 * time.Millisecond)

		// Send unregistered method first
		unregistered := map[string]any{
			"jsonrpc": "2.0",
			"method":  "some.other.method",
			"params":  map[string]any{"wrong": true},
		}
		unregisteredData, _ := json.Marshal(unregistered)
		_ = server.Melody.Broadcast(unregisteredData)

		// Then send registered method
		registered := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.added",
			"params":  map[string]any{"correct": true},
		}
		registeredData, _ := json.Marshal(registered)
		_ = server.Melody.Broadcast(registeredData)
	}()

	method, params, err := WaitNotifications(
		context.Background(),
		time.Second,
		cfg,
		"tokens.added",
		"tokens.removed",
	)
	require.NoError(t, err)
	assert.Equal(t, "tokens.added", method)

	var parsedParams map[string]any
	err = json.Unmarshal([]byte(params), &parsedParams)
	require.NoError(t, err)
	assert.True(t, parsedParams["correct"].(bool))
}

func TestWaitNotifications_Timeout(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	_, _, err := WaitNotifications(
		context.Background(),
		100*time.Millisecond,
		cfg,
		"tokens.added",
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRequestTimeout)
}

func TestLocalAPIClient_Call(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		response := map[string]any{
			"jsonrpc": "2.0",
			"result":  map[string]any{"data": "response"},
			"id":      request["id"],
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	client := NewLocalAPIClient(cfg)
	result, err := client.Call(context.Background(), "test.method", `{"param":"value"}`)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "response", parsed["data"])
}

func TestLocalAPIClient_CallError(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		response := map[string]any{
			"jsonrpc": "2.0",
			"error": map[string]any{
				"code":    -32000,
				"message": "Server error",
			},
			"id": request["id"],
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	client := NewLocalAPIClient(cfg)
	_, err := client.Call(context.Background(), "test.method", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api call failed")
}

func TestLocalAPIClient_WaitNotification(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, nil)
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	go func() {
		time.Sleep(50 * time.Millisecond)
		notification := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tokens.added",
			"params":  map[string]any{"token": "abc"},
		}
		data, _ := json.Marshal(notification)
		_ = server.Melody.Broadcast(data)
	}()

	client := NewLocalAPIClient(cfg)
	result, err := client.WaitNotification(context.Background(), time.Second, "tokens.added")
	require.NoError(t, err)
	assert.Contains(t, result, "abc")
}

func TestNewLocalAPIClient(t *testing.T) {
	t.Parallel()

	cfg := testConfigWithPort(t, 7497)
	client := NewLocalAPIClient(cfg)

	require.NotNil(t, client)
	assert.Equal(t, cfg, client.cfg)
}

// TestAPIPath verifies the constant is correctly set.
func TestAPIPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/api/v0.1", APIPath)
}

// TestErrors verifies error variables are properly defined.
func TestErrors(t *testing.T) {
	t.Parallel()

	require.Error(t, ErrRequestTimeout)
	require.Error(t, ErrInvalidParams)
	require.Error(t, ErrRequestCancelled)

	assert.Equal(t, "request timed out", ErrRequestTimeout.Error())
	assert.Equal(t, "invalid params", ErrInvalidParams.Error())
	assert.Equal(t, "request cancelled", ErrRequestCancelled.Error())
}

// TestWebSocketUpgrader verifies the server correctly upgrades HTTP to WebSocket.
func TestWebSocketUpgrade(t *testing.T) {
	t.Parallel()

	// Create a minimal WebSocket server using the standard library
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0.1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var request map[string]any
			if err := json.Unmarshal(message, &request); err != nil {
				continue
			}

			response := map[string]any{
				"jsonrpc": "2.0",
				"result":  "upgraded",
				"id":      request["id"],
			}
			respData, _ := json.Marshal(response)
			_ = conn.WriteMessage(mt, respData)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	port := parseServerPort(t, server)
	cfg := testConfigWithPort(t, port)

	result, err := LocalClient(context.Background(), cfg, "test", "")
	require.NoError(t, err)
	assert.Equal(t, `"upgraded"`, result)
}

func TestIsServiceRunning_ServiceUp(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		response := map[string]any{
			"jsonrpc": "2.0",
			"result":  map[string]any{"version": "1.0.0"},
			"id":      request["id"],
		}
		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	assert.True(t, IsServiceRunning(cfg))
}

func TestIsServiceRunning_ServiceDown(t *testing.T) {
	t.Parallel()

	// Use a port that's guaranteed to not have anything listening
	cfg := testConfigWithPort(t, unusedPort(t))

	assert.False(t, IsServiceRunning(cfg))
}

func TestWaitForAPI_ServiceAlreadyUp(t *testing.T) {
	t.Parallel()

	server := helpers.NewWebSocketTestServer(t, func(session *melody.Session, msg []byte) {
		var request map[string]any
		if err := json.Unmarshal(msg, &request); err != nil {
			return
		}

		response := map[string]any{
			"jsonrpc": "2.0",
			"result":  map[string]any{"version": "1.0.0"},
			"id":      request["id"],
		}
		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	})
	defer server.Close()

	port := parseServerPort(t, server.Server)
	cfg := testConfigWithPort(t, port)

	// Service is already up, should return immediately
	start := time.Now()
	result := WaitForAPI(cfg, 5*time.Second, 100*time.Millisecond)
	elapsed := time.Since(start)

	assert.True(t, result)
	assert.Less(t, elapsed, 200*time.Millisecond)
}

func TestWaitForAPI_Timeout(t *testing.T) {
	t.Parallel()

	// Use a port that's guaranteed to not have anything listening
	cfg := testConfigWithPort(t, unusedPort(t))

	start := time.Now()
	result := WaitForAPI(cfg, 200*time.Millisecond, 50*time.Millisecond)
	elapsed := time.Since(start)

	assert.False(t, result)
	assert.GreaterOrEqual(t, elapsed, 200*time.Millisecond)
}
