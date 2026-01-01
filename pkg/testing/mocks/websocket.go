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

package mocks

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/mock"
)

// MockMelodySession mocks a melody.Session for testing
type MockMelodySession struct {
	Keys map[string]any
	mock.Mock
	CloseReason    string
	SentMessages   [][]byte
	BinaryMessages [][]byte
	mu             syncutil.RWMutex
	Closed         bool
}

// NewMockMelodySession creates a new mock melody session
func NewMockMelodySession() *MockMelodySession {
	session := &MockMelodySession{
		Keys:           make(map[string]any),
		SentMessages:   make([][]byte, 0),
		BinaryMessages: make([][]byte, 0),
	}

	// Set default session ID
	session.Keys["session_id"] = "test-session-id"

	return session
}

// Write mocks writing a message to the WebSocket
func (m *MockMelodySession) Write(msg []byte) error {
	args := m.Called(msg)

	m.mu.Lock()
	defer m.mu.Unlock()

	if !args.Bool(0) { // if write should fail
		if err := args.Error(1); err != nil {
			return fmt.Errorf("mock operation failed: %w", err)
		}
		return nil
	}

	// Make a copy to avoid mutation
	msgCopy := make([]byte, len(msg))
	copy(msgCopy, msg)
	m.SentMessages = append(m.SentMessages, msgCopy)

	return nil
}

// WriteBinary mocks writing binary data to the WebSocket
func (m *MockMelodySession) WriteBinary(msg []byte) error {
	args := m.Called(msg)

	m.mu.Lock()
	defer m.mu.Unlock()

	if !args.Bool(0) { // if write should fail
		if err := args.Error(1); err != nil {
			return fmt.Errorf("mock operation failed: %w", err)
		}
		return nil
	}

	// Make a copy to avoid mutation
	msgCopy := make([]byte, len(msg))
	copy(msgCopy, msg)
	m.BinaryMessages = append(m.BinaryMessages, msgCopy)

	return nil
}

// Close mocks closing the WebSocket session
func (m *MockMelodySession) Close() error {
	args := m.Called()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.Closed = true
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// CloseWithMsg mocks closing the WebSocket session with a message
func (m *MockMelodySession) CloseWithMsg(msg []byte) error {
	args := m.Called(msg)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.Closed = true
	m.CloseReason = string(msg)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// IsClosed returns whether the session is closed
func (m *MockMelodySession) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Closed
}

// LocalAddr mocks getting the local address
func (m *MockMelodySession) LocalAddr() string {
	args := m.Called()
	return args.String(0)
}

// RemoteAddr mocks getting the remote address
func (m *MockMelodySession) RemoteAddr() string {
	args := m.Called()
	return args.String(0)
}

// Request mocks getting the HTTP request
func (m *MockMelodySession) Request() any {
	args := m.Called()
	return args.Get(0)
}

// Set mocks setting a key-value pair
func (m *MockMelodySession) Set(key string, value any) {
	m.Called(key, value)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.Keys[key] = value
}

// Get mocks getting a value by key
func (m *MockMelodySession) Get(key string) (any, bool) {
	args := m.Called(key)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if value, exists := m.Keys[key]; exists {
		return value, true
	}

	return args.Get(0), args.Bool(1)
}

// MustGet mocks getting a value by key (panics if not found)
func (m *MockMelodySession) MustGet(key string) any {
	args := m.Called(key)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if value, exists := m.Keys[key]; exists {
		return value
	}

	return args.Get(0)
}

// GetSentMessages returns all sent messages (thread-safe copy)
func (m *MockMelodySession) GetSentMessages() [][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages := make([][]byte, len(m.SentMessages))
	for i, msg := range m.SentMessages {
		messages[i] = make([]byte, len(msg))
		copy(messages[i], msg)
	}

	return messages
}

// GetBinaryMessages returns all sent binary messages (thread-safe copy)
func (m *MockMelodySession) GetBinaryMessages() [][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages := make([][]byte, len(m.BinaryMessages))
	for i, msg := range m.BinaryMessages {
		messages[i] = make([]byte, len(msg))
		copy(messages[i], msg)
	}

	return messages
}

// ClearMessages clears all sent messages
func (m *MockMelodySession) ClearMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SentMessages = m.SentMessages[:0]
	m.BinaryMessages = m.BinaryMessages[:0]
}

// SetupBasicMock sets up basic mock expectations for common session operations
func (m *MockMelodySession) SetupBasicMock() {
	m.On("Write", mock.AnythingOfType("[]uint8")).Return(true, nil)
	m.On("WriteBinary", mock.AnythingOfType("[]uint8")).Return(true, nil)
	m.On("Close").Return(nil)
	m.On("CloseWithMsg", mock.AnythingOfType("[]uint8")).Return(nil)
	m.On("LocalAddr").Return("127.0.0.1:7497")
	m.On("RemoteAddr").Return("127.0.0.1:12345")
	m.On("Request").Return(nil)
	m.On("Set", mock.AnythingOfType("string"), mock.Anything).Return()
	m.On("Get", mock.AnythingOfType("string")).Return(nil, false)
	m.On("MustGet", mock.AnythingOfType("string")).Return(nil)
}

// MockMelody mocks the melody.Melody WebSocket manager
type MockMelody struct {
	MessageHandler    func(*melody.Session, []byte)
	ConnectHandler    func(*melody.Session)
	DisconnectHandler func(*melody.Session)
	ErrorHandler      func(*melody.Session, error)
	mock.Mock
	Sessions []*MockMelodySession
	mu       syncutil.RWMutex
}

// NewMockMelody creates a new mock melody instance
func NewMockMelody() *MockMelody {
	return &MockMelody{
		Sessions: make([]*MockMelodySession, 0),
	}
}

// HandleMessage mocks setting the message handler
func (m *MockMelody) HandleMessage(handler func(*melody.Session, []byte)) {
	m.Called(handler)
	m.MessageHandler = handler
}

// HandleConnect mocks setting the connect handler
func (m *MockMelody) HandleConnect(handler func(*melody.Session)) {
	m.Called(handler)
	m.ConnectHandler = handler
}

// HandleDisconnect mocks setting the disconnect handler
func (m *MockMelody) HandleDisconnect(handler func(*melody.Session)) {
	m.Called(handler)
	m.DisconnectHandler = handler
}

// HandleError mocks setting the error handler
func (m *MockMelody) HandleError(handler func(*melody.Session, error)) {
	m.Called(handler)
	m.ErrorHandler = handler
}

// Broadcast mocks broadcasting a message to all sessions
func (m *MockMelody) Broadcast(msg []byte) error {
	args := m.Called(msg)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.Sessions {
		_ = session.Write(msg)
	}

	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// BroadcastFilter mocks broadcasting with a filter function
func (m *MockMelody) BroadcastFilter(msg []byte, filter func(*melody.Session) bool) error {
	args := m.Called(msg, filter)

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.Sessions {
		// We can't call the actual filter function on our mock session,
		// so we'll assume the filter passes for testing
		_ = session.Write(msg)
	}

	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// Close mocks closing all sessions
func (m *MockMelody) Close() error {
	args := m.Called()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, session := range m.Sessions {
		_ = session.Close()
	}

	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// AddMockSession adds a mock session for testing
func (m *MockMelody) AddMockSession() *MockMelodySession {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := NewMockMelodySession()
	session.SetupBasicMock()
	m.Sessions = append(m.Sessions, session)

	return session
}

// SimulateMessage simulates receiving a message on a session
func (m *MockMelody) SimulateMessage(sessionIndex int, message []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if sessionIndex < len(m.Sessions) && m.MessageHandler != nil {
		// Cast to melody.Session interface - this won't work in practice
		// but provides the testing structure
		m.MessageHandler(nil, message)
	}
}

// SimulateConnect simulates a new connection
func (m *MockMelody) SimulateConnect() *MockMelodySession {
	session := m.AddMockSession()

	if m.ConnectHandler != nil {
		m.ConnectHandler(nil)
	}

	return session
}

// SimulateDisconnect simulates a disconnection
func (m *MockMelody) SimulateDisconnect(sessionIndex int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if sessionIndex < len(m.Sessions) {
		_ = m.Sessions[sessionIndex].Close()

		if m.DisconnectHandler != nil {
			m.DisconnectHandler(nil)
		}
	}
}

// GetSessionCount returns the number of active sessions
func (m *MockMelody) GetSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, session := range m.Sessions {
		if !session.IsClosed() {
			count++
		}
	}

	return count
}

// ClearSessions removes all sessions
func (m *MockMelody) ClearSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Sessions = m.Sessions[:0]
}

// SetupBasicMock sets up basic expectations for the melody mock
func (m *MockMelody) SetupBasicMock() {
	m.On("HandleMessage", mock.AnythingOfType("func(*melody.Session, []uint8)")).Return()
	m.On("HandleConnect", mock.AnythingOfType("func(*melody.Session)")).Return()
	m.On("HandleDisconnect", mock.AnythingOfType("func(*melody.Session)")).Return()
	m.On("HandleError", mock.AnythingOfType("func(*melody.Session, error)")).Return()
	m.On("Broadcast", mock.AnythingOfType("[]uint8")).Return(nil)
	m.On("BroadcastFilter", mock.AnythingOfType("[]uint8"),
		mock.AnythingOfType("func(*melody.Session) bool")).Return(nil)
	m.On("Close").Return(nil)
}

// WebSocketTestMessage represents a test message structure
type WebSocketTestMessage struct {
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
	Type      string    `json:"type"`
}

// CreateTestJSONRPCMessage creates a test JSON-RPC message
func CreateTestJSONRPCMessage(method string, params any) []byte {
	message := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	data, _ := json.Marshal(message)
	return data
}

// CreateTestJSONRPCResponse creates a test JSON-RPC response
func CreateTestJSONRPCResponse(id, result any) []byte {
	response := map[string]any{
		"jsonrpc": "2.0",
		"result":  result,
		"id":      id,
	}

	data, _ := json.Marshal(response)
	return data
}

// CreateTestJSONRPCError creates a test JSON-RPC error response
func CreateTestJSONRPCError(id any, code int, message string) []byte {
	response := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
		"id": id,
	}

	data, _ := json.Marshal(response)
	return data
}
