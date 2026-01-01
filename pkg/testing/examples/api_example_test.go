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

package examples

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebSocketBasicCommunication demonstrates basic WebSocket testing
func TestWebSocketBasicCommunication(t *testing.T) {
	t.Parallel()
	// Create a simple message handler
	messageHandler := func(session *melody.Session, msg []byte) {
		// Echo the message back
		err := session.Write(msg)
		require.NoError(t, err)
	}

	// Create test server
	server := helpers.NewWebSocketTestServer(t, messageHandler)
	defer server.Close()

	// Connect client
	conn, err := server.CreateWebSocketClient()
	require.NoError(t, err)
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("Failed to close WebSocket connection: %v", closeErr)
		}
	}()

	// Send test message
	testMessage := []byte(`{"jsonrpc":"2.0","method":"ping","id":1}`)
	err = conn.WriteMessage(websocket.TextMessage, testMessage)
	require.NoError(t, err)

	// Read response
	_, response, err := conn.ReadMessage()
	require.NoError(t, err)

	// Verify echo
	assert.Equal(t, testMessage, response)

	// Wait for server to record messages
	err = server.WaitForMessages(1, time.Second)
	require.NoError(t, err)

	// Verify server recorded the message
	messages := server.GetMessages()
	assert.Len(t, messages, 1)
	assert.Equal(t, "received", messages[0].Type)
	assert.Equal(t, testMessage, messages[0].Data)
}

// TestWebSocketJSONRPCCommunication demonstrates JSON-RPC testing
func TestWebSocketJSONRPCCommunication(t *testing.T) {
	t.Parallel()
	// Create a JSON-RPC handler
	messageHandler := func(session *melody.Session, msg []byte) {
		var request map[string]any
		err := json.Unmarshal(msg, &request)
		if err != nil {
			return
		}

		// Create a success response
		response := map[string]any{
			"jsonrpc": "2.0",
			"result":  "pong",
			"id":      request["id"],
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	}

	// Create test server
	server := helpers.NewWebSocketTestServer(t, messageHandler)
	defer server.Close()

	// Connect client
	conn, err := server.CreateWebSocketClient()
	require.NoError(t, err)
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("Failed to close WebSocket connection: %v", closeErr)
		}
	}()

	// Send JSON-RPC request
	response, err := helpers.SendJSONRPCRequest(conn, "ping", nil)
	require.NoError(t, err)

	// Verify response
	helpers.AssertJSONRPCSuccess(t, response)
	assert.Equal(t, "pong", response.Result)
}

// TestWebSocketErrorHandling demonstrates error testing
func TestWebSocketErrorHandling(t *testing.T) {
	t.Parallel()
	// Create a handler that returns errors for certain methods
	messageHandler := func(session *melody.Session, msg []byte) {
		var request map[string]any
		err := json.Unmarshal(msg, &request)
		if err != nil {
			return
		}

		method, ok := request["method"].(string)
		if !ok {
			return
		}

		var response map[string]any
		if method == "invalid_method" {
			response = map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32601,
					"message": "Method not found",
				},
				"id": request["id"],
			}
		} else {
			response = map[string]any{
				"jsonrpc": "2.0",
				"result":  "success",
				"id":      request["id"],
			}
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	}

	// Create test server
	server := helpers.NewWebSocketTestServer(t, messageHandler)
	defer server.Close()

	// Connect client
	conn, err := server.CreateWebSocketClient()
	require.NoError(t, err)
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("Failed to close WebSocket connection: %v", closeErr)
		}
	}()

	// Test error case
	response, err := helpers.SendJSONRPCRequest(conn, "invalid_method", nil)
	require.NoError(t, err)
	helpers.AssertJSONRPCError(t, response, -32601)

	// Test success case
	response, err = helpers.SendJSONRPCRequest(conn, "valid_method", nil)
	require.NoError(t, err)
	helpers.AssertJSONRPCSuccess(t, response)
}

// TestHTTPAPIEndpoints demonstrates HTTP API testing
func TestHTTPAPIEndpoints(t *testing.T) {
	t.Parallel()
	// Create a simple HTTP handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Parse JSON-RPC request
		var request helpers.JSONRPCRequest
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Create response
		response := helpers.JSONRPCResponse{
			ID:     request.ID,
			Result: "HTTP API works",
		}

		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode(response); encodeErr != nil {
			t.Logf("Failed to encode JSON response: %v", encodeErr)
		}
	})

	// Create HTTP test helper
	httpHelper := helpers.NewHTTPTestHelper(handler)
	defer httpHelper.Close()

	// Send request
	resp, err := httpHelper.PostJSONRPC("test_method", nil)
	require.NoError(t, err)
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	// Verify response
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var jsonResponse helpers.JSONRPCResponse
	err = json.NewDecoder(resp.Body).Decode(&jsonResponse)
	require.NoError(t, err)
	assert.Equal(t, "HTTP API works", jsonResponse.Result)
}

// TestMockWebSocketConnection demonstrates mock WebSocket usage
func TestMockWebSocketConnection(t *testing.T) {
	t.Parallel()
	// Create mock connection
	mockConn := helpers.NewMockWebSocketConnection()

	// Queue some messages for reading
	mockConn.QueueMessage([]byte(`{"message": "hello"}`))
	mockConn.QueueMessage([]byte(`{"message": "world"}`))

	// Test writing
	err := mockConn.WriteMessage(websocket.TextMessage, []byte(`{"response": "ok"}`))
	require.NoError(t, err)

	// Test reading
	_, msg1, err := mockConn.ReadMessage()
	require.NoError(t, err)
	assert.JSONEq(t, `{"message": "hello"}`, string(msg1))

	_, msg2, err := mockConn.ReadMessage()
	require.NoError(t, err)
	assert.JSONEq(t, `{"message": "world"}`, string(msg2))

	// Verify sent messages
	sentMessages := mockConn.GetSentMessages()
	assert.Len(t, sentMessages, 1)
	assert.JSONEq(t, `{"response": "ok"}`, string(sentMessages[0]))

	// Test error simulation
	mockConn.SetWriteError(assert.AnError)
	err = mockConn.WriteMessage(websocket.TextMessage, []byte(`{"test": "error"}`))
	assert.Error(t, err)
}

// TestMelodySessionMock demonstrates melody session mocking
func TestMelodySessionMock(t *testing.T) {
	t.Parallel()
	// Create mock session
	mockSession := mocks.NewMockMelodySession()
	mockSession.SetupBasicMock()

	// Test writing to session
	testMessage := []byte(`{"test": "message"}`)
	err := mockSession.Write(testMessage)
	require.NoError(t, err)

	// Verify message was recorded
	sentMessages := mockSession.GetSentMessages()
	assert.Len(t, sentMessages, 1)
	assert.Equal(t, testMessage, sentMessages[0])

	// Test session keys
	mockSession.Set("user_id", "test-user")
	mockSession.Set("connected_at", time.Now())

	userID, exists := mockSession.Get("user_id")
	assert.True(t, exists)
	assert.Equal(t, "test-user", userID)

	// Test closing
	err = mockSession.Close()
	require.NoError(t, err)
	assert.True(t, mockSession.IsClosed())
}

// TestWebSocketBroadcasting demonstrates broadcast testing
func TestWebSocketBroadcasting(t *testing.T) {
	t.Parallel()
	// Create mock melody
	mockMelody := mocks.NewMockMelody()
	mockMelody.SetupBasicMock()

	// Add some mock sessions
	session1 := mockMelody.AddMockSession()
	session2 := mockMelody.AddMockSession()
	session3 := mockMelody.AddMockSession()

	// Test broadcast
	broadcastMessage := []byte(`{"broadcast": "message"}`)
	err := mockMelody.Broadcast(broadcastMessage)
	require.NoError(t, err)

	// Verify all sessions received the message
	assert.Equal(t, broadcastMessage, session1.GetSentMessages()[0])
	assert.Equal(t, broadcastMessage, session2.GetSentMessages()[0])
	assert.Equal(t, broadcastMessage, session3.GetSentMessages()[0])

	// Test session count
	assert.Equal(t, 3, mockMelody.GetSessionCount())

	// Simulate disconnect
	mockMelody.SimulateDisconnect(1)
	assert.Equal(t, 2, mockMelody.GetSessionCount())
}

// TestComplexWebSocketScenario demonstrates a complex testing scenario
func TestComplexWebSocketScenario(t *testing.T) {
	t.Parallel()
	receivedCommands := make([]string, 0)

	// Create a handler that processes different command types
	messageHandler := func(session *melody.Session, msg []byte) {
		var request map[string]any
		err := json.Unmarshal(msg, &request)
		if err != nil {
			return
		}

		method, ok := request["method"].(string)
		if !ok {
			return
		}
		receivedCommands = append(receivedCommands, method)

		var response map[string]any

		switch method {
		case "auth":
			// Simulate authentication
			session.Set("authenticated", true)
			response = map[string]any{
				"jsonrpc": "2.0",
				"result":  map[string]any{"status": "authenticated"},
				"id":      request["id"],
			}

		case "get_status":
			// Check authentication
			auth, exists := session.Get("authenticated")
			if !exists || !auth.(bool) {
				response = map[string]any{
					"jsonrpc": "2.0",
					"error": map[string]any{
						"code":    -32001,
						"message": "Not authenticated",
					},
					"id": request["id"],
				}
			} else {
				response = map[string]any{
					"jsonrpc": "2.0",
					"result":  map[string]any{"status": "ready"},
					"id":      request["id"],
				}
			}

		default:
			response = map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32601,
					"message": "Method not found",
				},
				"id": request["id"],
			}
		}

		responseData, _ := json.Marshal(response)
		_ = session.Write(responseData)
	}

	// Create test server
	server := helpers.NewWebSocketTestServer(t, messageHandler)
	defer server.Close()

	// Connect client
	conn, err := server.CreateWebSocketClient()
	require.NoError(t, err)
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("Failed to close WebSocket connection: %v", closeErr)
		}
	}()

	// Test unauthenticated access
	response, err := helpers.SendJSONRPCRequest(conn, "get_status", nil)
	require.NoError(t, err)
	helpers.AssertJSONRPCError(t, response, -32001)

	// Authenticate
	response, err = helpers.SendJSONRPCRequest(conn, "auth", map[string]string{
		"username": "test",
		"password": "test",
	})
	require.NoError(t, err)
	helpers.AssertJSONRPCSuccess(t, response)

	// Test authenticated access
	response, err = helpers.SendJSONRPCRequest(conn, "get_status", nil)
	require.NoError(t, err)
	helpers.AssertJSONRPCSuccess(t, response)

	// Verify command sequence
	expected := []string{"get_status", "auth", "get_status"}
	assert.Equal(t, expected, receivedCommands)
}
