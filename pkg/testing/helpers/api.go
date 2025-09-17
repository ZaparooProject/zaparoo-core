// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

// Package helpers provides testing utilities for API operations.
//
// This package includes WebSocket test servers and helper functions for testing
// API endpoints, WebSocket communication, and JSON-RPC methods without requiring
// a full API server setup.
//
// Example usage:
//
//	func TestWebSocketAPI(t *testing.T) {
//		// Create message handler
//		handler := func(session *melody.Session, msg []byte) {
//			session.Write([]byte(`{"response": "ok"}`))
//		}
//
//		// Create test WebSocket server
//		server := helpers.NewWebSocketTestServer(t, handler)
//		defer server.Close()
//
//		// Connect and test
//		client := server.NewClient(t)
//		defer client.Close()
//
//		err := client.SendMessage([]byte(`{"test": "message"}`))
//		require.NoError(t, err)
//	}
//
// For complete examples, see pkg/testing/examples/api_example_test.go
package helpers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/require"
)

// WebSocketTestServer provides utilities for testing WebSocket connections
type WebSocketTestServer struct {
	Server   *httptest.Server
	Melody   *melody.Melody
	t        *testing.T
	Messages []WebSocketMessage
	mu       sync.RWMutex
}

// WebSocketMessage captures a message sent or received during testing
type WebSocketMessage struct {
	Timestamp time.Time `json:"timestamp"`
	Error     error     `json:"error,omitempty"`
	Type      string    `json:"type"`
	SessionID string    `json:"sessionId,omitempty"`
	Data      []byte    `json:"data"`
}

// JSONRPCRequest represents a JSON-RPC request for testing
type JSONRPCRequest struct {
	Params any       `json:"params,omitempty"`
	Method string    `json:"method"`
	ID     uuid.UUID `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC response for testing
type JSONRPCResponse struct {
	Result any                 `json:"result,omitempty"`
	Error  *models.ErrorObject `json:"error,omitempty"`
	ID     uuid.UUID           `json:"id"`
}

// NewWebSocketTestServer creates a new WebSocket test server
func NewWebSocketTestServer(t *testing.T, handler func(*melody.Session, []byte)) *WebSocketTestServer {
	m := melody.New()

	wsts := &WebSocketTestServer{
		Melody:   m,
		Messages: make([]WebSocketMessage, 0),
		t:        t,
	}

	// Set up connection handler to assign session IDs
	m.HandleConnect(func(session *melody.Session) {
		session.Keys = make(map[string]any)
		session.Keys["session_id"] = "test-session-" + session.Request.RemoteAddr
	})

	// Set up message handlers
	if handler != nil {
		m.HandleMessage(func(session *melody.Session, msg []byte) {
			sessionID := ""
			if id, ok := session.Keys["session_id"]; ok && id != nil {
				if s, ok := id.(string); ok {
					sessionID = s
				}
			}
			wsts.recordMessage("received", msg, sessionID, nil)
			handler(session, msg)
		})
	}

	// Track outbound messages
	m.HandleMessageBinary(func(session *melody.Session, msg []byte) {
		sessionID := ""
		if id, ok := session.Keys["session_id"]; ok && id != nil {
			if s, ok := id.(string); ok {
				sessionID = s
			}
		}
		wsts.recordMessage("sent", msg, sessionID, nil)
	})

	// Set up HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0.1", func(w http.ResponseWriter, r *http.Request) {
		err := m.HandleRequest(w, r)
		if err != nil {
			wsts.recordMessage("error", nil, "", err)
		}
	})

	wsts.Server = httptest.NewServer(mux)

	// Brief wait to ensure server is fully ready for WebSocket connections
	// This prevents "bad handshake" errors in CI environments with high load
	time.Sleep(5 * time.Millisecond)

	return wsts
}

// recordMessage safely records a message for testing verification
func (wsts *WebSocketTestServer) recordMessage(msgType string, data []byte, sessionID string, err error) {
	wsts.mu.Lock()
	defer wsts.mu.Unlock()

	wsts.Messages = append(wsts.Messages, WebSocketMessage{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Error:     err,
	})
}

// Close shuts down the test server
func (wsts *WebSocketTestServer) Close() {
	wsts.Server.Close()
	_ = wsts.Melody.Close()
}

// GetMessages returns all recorded messages (thread-safe)
func (wsts *WebSocketTestServer) GetMessages() []WebSocketMessage {
	wsts.mu.RLock()
	defer wsts.mu.RUnlock()

	msgs := make([]WebSocketMessage, len(wsts.Messages))
	copy(msgs, wsts.Messages)
	return msgs
}

// ClearMessages clears all recorded messages (thread-safe)
func (wsts *WebSocketTestServer) ClearMessages() {
	wsts.mu.Lock()
	defer wsts.mu.Unlock()

	wsts.Messages = wsts.Messages[:0]
}

// CreateWebSocketClient creates a WebSocket client connected to the test server
func (wsts *WebSocketTestServer) CreateWebSocketClient() (*websocket.Conn, error) {
	u, err := url.Parse(wsts.Server.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server URL: %w", err)
	}

	u.Scheme = "ws"
	u.Path = "/api/v0.1"

	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial WebSocket: %w", err)
	}
	if resp != nil && resp.Body != nil {
		// Close the response body to avoid resource leak
		_ = resp.Body.Close()
	}

	return conn, nil
}

// SendJSONRPCRequest sends a JSON-RPC request and returns the response
func SendJSONRPCRequest(conn *websocket.Conn, method string, params any) (*JSONRPCResponse, error) {
	request := JSONRPCRequest{
		ID:     uuid.New(),
		Method: method,
		Params: params,
	}

	requestData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	err = conn.WriteMessage(websocket.TextMessage, requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	_, responseData, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response JSONRPCResponse
	err = json.Unmarshal(responseData, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// AssertJSONRPCSuccess verifies a JSON-RPC response was successful
func AssertJSONRPCSuccess(t *testing.T, response *JSONRPCResponse) {
	require.NotNil(t, response, "response should not be nil")
	require.Nil(t, response.Error, "response should not contain an error")
	require.NotNil(t, response.Result, "response should contain a result")
}

// AssertJSONRPCError verifies a JSON-RPC response contains an error
func AssertJSONRPCError(t *testing.T, response *JSONRPCResponse, expectedCode int) {
	require.NotNil(t, response, "response should not be nil")
	require.NotNil(t, response.Error, "response should contain an error")
	require.Equal(t, expectedCode, response.Error.Code, "error code should match")
}

// HTTPTestHelper provides utilities for testing HTTP API endpoints
type HTTPTestHelper struct {
	Server *httptest.Server
	Client *http.Client
}

// NewHTTPTestHelper creates a new HTTP test helper with the given handler
func NewHTTPTestHelper(handler http.Handler) *HTTPTestHelper {
	server := httptest.NewServer(handler)
	client := server.Client()

	return &HTTPTestHelper{
		Server: server,
		Client: client,
	}
}

// Close shuts down the test server
func (h *HTTPTestHelper) Close() {
	h.Server.Close()
}

// PostJSONRPC sends a JSON-RPC request via HTTP POST
func (h *HTTPTestHelper) PostJSONRPC(method string, params any) (*http.Response, error) {
	request := JSONRPCRequest{
		ID:     uuid.New(),
		Method: method,
		Params: params,
	}

	requestData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	apiURL := h.Server.URL + "/api/v0.1"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, apiURL,
		strings.NewReader(string(requestData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send POST request: %w", err)
	}

	return resp, nil
}

// MockWebSocketConnection provides a mock implementation for testing
type MockWebSocketConnection struct {
	CloseError       error
	WriteError       error
	ReadError        error
	SentMessages     [][]byte
	ReceivedMessages [][]byte
	mu               sync.RWMutex
	Closed           bool
}

// NewMockWebSocketConnection creates a new mock WebSocket connection
func NewMockWebSocketConnection() *MockWebSocketConnection {
	return &MockWebSocketConnection{
		SentMessages:     make([][]byte, 0),
		ReceivedMessages: make([][]byte, 0),
	}
}

// WriteMessage simulates writing a message
func (m *MockWebSocketConnection) WriteMessage(_ int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.WriteError != nil {
		return m.WriteError
	}

	if m.Closed {
		return errors.New("connection closed")
	}

	// Make a copy of the data to avoid mutations
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.SentMessages = append(m.SentMessages, dataCopy)

	return nil
}

// ReadMessage simulates reading a message
func (m *MockWebSocketConnection) ReadMessage() (messageType int, data []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ReadError != nil {
		return 0, nil, m.ReadError
	}

	if m.Closed {
		return 0, nil, errors.New("connection closed")
	}

	if len(m.ReceivedMessages) == 0 {
		return 0, nil, errors.New("no messages available")
	}

	// Return the first message and remove it from the queue
	msg := m.ReceivedMessages[0]
	m.ReceivedMessages = m.ReceivedMessages[1:]

	return websocket.TextMessage, msg, nil
}

// Close simulates closing the connection
func (m *MockWebSocketConnection) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CloseError != nil {
		return m.CloseError
	}

	m.Closed = true
	return nil
}

// QueueMessage adds a message to the read queue
func (m *MockWebSocketConnection) QueueMessage(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.ReceivedMessages = append(m.ReceivedMessages, dataCopy)
}

// GetSentMessages returns all sent messages (thread-safe copy)
func (m *MockWebSocketConnection) GetSentMessages() [][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages := make([][]byte, len(m.SentMessages))
	for i, msg := range m.SentMessages {
		messages[i] = make([]byte, len(msg))
		copy(messages[i], msg)
	}

	return messages
}

// ClearSentMessages clears all sent messages
func (m *MockWebSocketConnection) ClearSentMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SentMessages = m.SentMessages[:0]
}

// SetWriteError sets an error to be returned on write operations
func (m *MockWebSocketConnection) SetWriteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WriteError = err
}

// SetReadError sets an error to be returned on read operations
func (m *MockWebSocketConnection) SetReadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReadError = err
}

// SetCloseError sets an error to be returned on close operations
func (m *MockWebSocketConnection) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CloseError = err
}

// CreateTestContext creates a context with timeout for testing
func CreateTestContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// WaitForMessages waits for a specific number of messages with timeout
func (wsts *WebSocketTestServer) WaitForMessages(count int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %d messages, got %d", count, len(wsts.GetMessages()))
		case <-ticker.C:
			if len(wsts.GetMessages()) >= count {
				return nil
			}
		}
	}
}
