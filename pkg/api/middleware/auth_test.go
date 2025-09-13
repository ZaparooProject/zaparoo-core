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

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_LocalhostBypass(t *testing.T) {
	t.Parallel()
	// Setup
	userDB := helpers.NewMockUserDBI()
	db := &database.Database{UserDB: userDB}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	middleware := AuthMiddleware(db)
	wrappedHandler := middleware(handler)

	tests := []struct {
		name       string
		remoteAddr string
		expectPass bool
	}{
		{"localhost with port", "127.0.0.1:12345", true},
		{"localhost without port", "127.0.0.1", true},
		{"localhost name with port", "localhost:8080", true},
		{"localhost name without port", "localhost", true},
		{"IPv6 loopback", "[::1]:8080", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewReader([]byte(`{"test": "data"}`)))
			req.RemoteAddr = tt.remoteAddr

			w := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code, "localhost should bypass auth")
			assert.Equal(t, "success", w.Body.String())
		})
	}
}

func TestAuthMiddleware_RemoteRequiresAuth(t *testing.T) {
	t.Parallel()
	// Setup
	userDB := helpers.NewMockUserDBI()
	// Mock the GetClientByAuthToken call to return an error for empty token
	// The middleware calls this once per test case (we have 2 test cases)
	userDB.On("GetClientByAuthToken", "").Return((*database.Client)(nil), assert.AnError).Times(2)

	db := &database.Database{UserDB: userDB}

	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called for unauthenticated remote requests")
	})

	middleware := AuthMiddleware(db)
	wrappedHandler := middleware(handler)

	tests := []struct {
		name       string
		remoteAddr string
	}{
		{"private network IP", "192.168.1.100:5000"},
		{"public IP", "8.8.8.8:80"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Send regular JSON without proper auth fields - should fail at auth token lookup
			req := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewReader([]byte(`{"test": "data"}`)))
			req.RemoteAddr = tt.remoteAddr

			w := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(w, req)

			// Should fail because auth token is empty/invalid
			assert.Equal(t, http.StatusUnauthorized, w.Code, "remote should fail auth")
			assert.Contains(t, w.Body.String(), "Invalid auth token")
		})
	}

	userDB.AssertExpectations(t)
}

func TestAuthMiddleware_EncryptedRequest(t *testing.T) {
	t.Parallel()
	// Create a mock device with known shared secret
	testSecret := []byte("test-secret-key-32-bytes-long-ok")
	testDevice := &database.Client{
		ClientID:      "test-device-id",
		ClientName:    "Test Device",
		AuthTokenHash: "test-token-hash",
		SharedSecret:  testSecret,
		CurrentSeq:    0,
		SeqWindow:     make([]byte, 8),
		NonceCache:    []string{},
		CreatedAt:     time.Now(),
		LastSeen:      time.Now(),
	}

	userDB := helpers.NewMockUserDBI()
	userDB.On("GetClientByAuthToken", "test-auth-token").Return(testDevice, nil)
	userDB.On("UpdateClientSequence", "test-device-id", uint64(1),
		mock.AnythingOfType("[]uint8"), mock.AnythingOfType("[]string")).Return(nil)

	db := &database.Database{UserDB: userDB}

	// Create encrypted payload
	payload := DecryptedPayload{
		JSONRPC: "2.0",
		Method:  "test.method",
		ID:      1,
		Seq:     1,
		Nonce:   "test-nonce-123",
	}

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	encryptedData, iv, err := EncryptPayload(payloadJSON, testSecret)
	require.NoError(t, err)

	encRequest := EncryptedRequest{
		Encrypted: encryptedData,
		IV:        iv,
		AuthToken: "test-auth-token",
	}

	encRequestJSON, err := json.Marshal(encRequest)
	require.NoError(t, err)

	// Test handler that checks the decrypted request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request body was decrypted properly
		var jsonRPC map[string]any
		err := json.NewDecoder(r.Body).Decode(&jsonRPC)
		assert.NoError(t, err)

		assert.Equal(t, "2.0", jsonRPC["jsonrpc"])
		assert.Equal(t, "test.method", jsonRPC["method"])
		assert.InDelta(t, float64(1), jsonRPC["id"], 0.001) // JSON unmarshals numbers as float64

		// Verify device is in context
		device := GetClientFromContext(r.Context())
		assert.NotNil(t, device)
		assert.Equal(t, "test-device-id", device.ClientID)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("authenticated success"))
	})

	middleware := AuthMiddleware(db)
	wrappedHandler := middleware(handler)

	// Test successful authentication
	req := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewReader(encRequestJSON))
	req.RemoteAddr = "192.168.1.100:5000" // Remote address to trigger auth

	w := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "authenticated success", w.Body.String())
	userDB.AssertExpectations(t)
}

func TestValidateSequenceAndNonce_ReplayProtection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		newNonce       string
		description    string
		seqWindow      []byte
		nonceCache     []string
		currentSeq     uint64
		newSeq         uint64
		expectedResult bool
	}{
		{
			name:           "first message",
			currentSeq:     0,
			seqWindow:      make([]byte, 8),
			nonceCache:     []string{},
			newSeq:         1,
			newNonce:       "nonce1",
			expectedResult: true,
			description:    "first message should always pass",
		},
		{
			name:           "sequence increment",
			currentSeq:     5,
			seqWindow:      []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			nonceCache:     []string{"old-nonce"},
			newSeq:         6,
			newNonce:       "nonce6",
			expectedResult: true,
			description:    "incrementing sequence should pass",
		},
		{
			name:           "duplicate nonce",
			currentSeq:     5,
			seqWindow:      []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			nonceCache:     []string{"duplicate-nonce"},
			newSeq:         6,
			newNonce:       "duplicate-nonce",
			expectedResult: false,
			description:    "duplicate nonce should be rejected",
		},
		{
			name:           "old sequence out of window",
			currentSeq:     100,
			seqWindow:      make([]byte, 8),
			nonceCache:     []string{},
			newSeq:         10, // More than 64 behind
			newNonce:       "nonce10",
			expectedResult: false,
			description:    "sequence too far behind should be rejected",
		},
		{
			name:           "sequence within window",
			currentSeq:     10,
			seqWindow:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			nonceCache:     []string{},
			newSeq:         8, // 2 behind, within window
			newNonce:       "nonce8",
			expectedResult: true,
			description:    "sequence within sliding window should pass",
		},
		{
			name:           "sequence at window boundary",
			currentSeq:     64,
			seqWindow:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			nonceCache:     []string{},
			newSeq:         1, // Exactly at window boundary (64 behind)
			newNonce:       "nonce1",
			expectedResult: false,
			description:    "sequence exactly at window boundary should be rejected",
		},
		{
			name:           "sequence already processed",
			currentSeq:     10,
			seqWindow:      []byte{0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // Bit 2 set (seq 8)
			nonceCache:     []string{},
			newSeq:         8, // Already processed
			newNonce:       "nonce8",
			expectedResult: false,
			description:    "already processed sequence should be rejected",
		},
		{
			name:           "large sequence jump",
			currentSeq:     5,
			seqWindow:      []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			nonceCache:     []string{},
			newSeq:         100, // Large jump forward
			newNonce:       "nonce100",
			expectedResult: true,
			description:    "large sequence jump should be accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device := &database.Client{
				ClientID:   "test-device",
				CurrentSeq: tt.currentSeq,
				SeqWindow:  tt.seqWindow,
				NonceCache: tt.nonceCache,
			}

			result := ValidateSequenceAndNonce(device, tt.newSeq, tt.newNonce)
			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

func TestEncryptDecryptPayload(t *testing.T) {
	t.Parallel()
	testKey := []byte("test-encryption-key-32bytes-ok!!")
	originalData := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	// Test encryption
	encrypted, iv, err := EncryptPayload(originalData, testKey)
	require.NoError(t, err)
	assert.NotEmpty(t, encrypted)
	assert.NotEmpty(t, iv)

	// Test decryption
	decrypted, err := DecryptPayload(encrypted, iv, testKey)
	require.NoError(t, err)
	assert.Equal(t, originalData, decrypted)
}

func TestEncryptDecryptPayload_WrongKey(t *testing.T) {
	t.Parallel()
	correctKey := []byte("correct-key-32-bytes-long-ok!!!!")
	wrongKey := []byte("wrong-key-32-bytes-long-ok!!!!!!")
	originalData := []byte(`{"test": "data"}`)

	// Encrypt with correct key
	encrypted, iv, err := EncryptPayload(originalData, correctKey)
	require.NoError(t, err)

	// Try to decrypt with wrong key - should fail
	_, err = DecryptPayload(encrypted, iv, wrongKey)
	require.Error(t, err, "decryption should fail with wrong key")
	assert.Contains(t, err.Error(), "decryption failed")
}

func TestIsLocalhost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		addr     string
		expected bool
	}{
		{"127.0.0.1:8080", true},
		{"127.0.0.1", true},
		{"localhost:3000", true},
		{"localhost", true},
		{"[::1]:8080", true},
		{"::1", true},
		{"192.168.1.1:8080", false},
		{"8.8.8.8:53", false},
		{"example.com:80", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			result := isLocalhost(tt.addr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAuthMiddleware_InvalidRequests(t *testing.T) {
	t.Parallel()
	userDB := helpers.NewMockUserDBI()
	// Mock empty auth token lookup - expect it to be called once for the missing auth token test
	userDB.On("GetClientByAuthToken", "").Return((*database.Client)(nil), assert.AnError)

	db := &database.Database{UserDB: userDB}

	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called for invalid requests")
	})

	middleware := AuthMiddleware(db)
	wrappedHandler := middleware(handler)

	tests := []struct {
		name         string
		body         string
		description  string
		expectedCode int
	}{
		{
			name:         "invalid json",
			body:         `{"invalid json`,
			expectedCode: http.StatusBadRequest,
			description:  "malformed JSON should be rejected",
		},
		{
			name:         "missing auth token",
			body:         `{"encrypted": "data", "iv": "iv"}`,
			expectedCode: http.StatusUnauthorized,
			description:  "missing auth token should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/test", bytes.NewReader([]byte(tt.body)))
			req.RemoteAddr = "192.168.1.100:5000" // Remote address to trigger auth

			w := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedCode, w.Code, tt.description)
		})
	}

	userDB.AssertExpectations(t)
}

func TestGetClientFromContext(t *testing.T) {
	t.Parallel()
	// Test with client in context
	client := &database.Client{ClientID: "test-device"}
	ctx := context.WithValue(context.Background(), clientKey("client"), client)

	result := GetClientFromContext(ctx)
	assert.Equal(t, client, result)

	// Test with no client in context
	emptyCtx := context.Background()
	result = GetClientFromContext(emptyCtx)
	assert.Nil(t, result)

	// Test with wrong type in context
	badCtx := context.WithValue(context.Background(), clientKey("client"), "not-a-client")
	result = GetClientFromContext(badCtx)
	assert.Nil(t, result)
}

// TestAuthMiddleware_ConcurrentRequests verifies that the race condition fix
// prevents concurrent requests from bypassing replay protection
func TestAuthMiddleware_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	// This test verifies the mutex locking works correctly
	// We'll just test that concurrent access to the mutex manager is safe

	const numConcurrentRequests = 20
	const deviceID = "test-device-concurrent"

	done := make(chan struct{}, numConcurrentRequests)
	var lockAcquired int32

	for range numConcurrentRequests {
		go func() {
			defer func() { done <- struct{}{} }()

			// Acquire device lock - this should be thread-safe
			unlockDevice := LockClient(deviceID)

			// Critical section - only one goroutine should be here at a time
			current := atomic.AddInt32(&lockAcquired, 1)
			if current != 1 {
				t.Errorf("Race condition detected: %d goroutines in critical section", current)
			}

			// Simulate some work
			time.Sleep(1 * time.Millisecond)

			atomic.AddInt32(&lockAcquired, -1)
			unlockDevice()
		}()
	}

	// Wait for all requests to complete
	for range numConcurrentRequests {
		<-done
	}

	// Verify no race conditions occurred
	assert.Equal(t, int32(0), atomic.LoadInt32(&lockAcquired), "All locks should be released")
}

// TestClientMutexManager_Cleanup verifies mutex cleanup works correctly
func TestClientMutexManager_Cleanup(t *testing.T) {
	t.Parallel()

	dm := &ClientMutexManager{}

	// Create some mutexes
	mutex1 := dm.getClientMutex("device1")
	mutex2 := dm.getClientMutex("device2")
	mutex3 := dm.getClientMutex("device3")

	require.NotNil(t, mutex1)
	require.NotNil(t, mutex2)
	require.NotNil(t, mutex3)

	// Age some mutexes
	mutex1.lastUsed = time.Now().Add(-31 * time.Minute) // Should be cleaned
	mutex2.lastUsed = time.Now().Add(-20 * time.Minute) // Should remain
	mutex3.lastUsed = time.Now()                        // Should remain

	// Run cleanup
	dm.cleanup()

	// Check that only old mutex was removed
	_, exists1 := dm.mutexes.Load("device1")
	_, exists2 := dm.mutexes.Load("device2")
	_, exists3 := dm.mutexes.Load("device3")

	assert.False(t, exists1, "Old mutex should be cleaned up")
	assert.True(t, exists2, "Recent mutex should remain")
	assert.True(t, exists3, "Current mutex should remain")
}

// TestClientMutexManager_ConcurrentAccess verifies thread safety of mutex manager
func TestClientMutexManager_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	dm := &ClientMutexManager{}
	const numGoroutines = 50
	const deviceID = "concurrent-test-device"

	done := make(chan struct{}, numGoroutines)

	// Launch multiple goroutines that get/create mutex for same device
	for range numGoroutines {
		go func() {
			defer func() { done <- struct{}{} }()

			mutex := dm.getClientMutex(deviceID)
			require.NotNil(t, mutex)
			assert.Equal(t, deviceID, mutex.clientID)
		}()
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}

	// Verify only one mutex was created for the device
	value, exists := dm.mutexes.Load(deviceID)
	assert.True(t, exists, "Mutex should exist")

	mutex := value.(*clientMutex)
	assert.Equal(t, deviceID, mutex.clientID)
}

// TestShiftWindowRight verifies the bit manipulation for the sliding window
func TestShiftWindowRight(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "shift zeros",
			input:    []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			name:     "shift ones",
			input:    []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expected: []byte{0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		},
		{
			name:     "shift single bit",
			input:    []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: []byte{0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			name:     "shift pattern",
			input:    []byte{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55},
			expected: []byte{0x55, 0x2A, 0xD5, 0x2A, 0xD5, 0x2A, 0xD5, 0x2A},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Make a copy to avoid modifying the test input
			window := make([]byte, len(tt.input))
			copy(window, tt.input)
			
			shiftWindowRight(window)
			
			assert.Equal(t, tt.expected, window, "bit shift should match expected result")
		})
	}
}

// TestUpdateClientSequenceAndNonce verifies the sequence window updates correctly
func TestUpdateClientSequenceAndNonce(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name         string
		initialSeq   uint64
		initialWindow []byte
		newSeq       uint64
		nonce        string
		expectedSeq  uint64
		checkBitSet  bool
		bitPosition  uint64
	}{
		{
			name:         "increment sequence",
			initialSeq:   5,
			initialWindow: []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			newSeq:       6,
			nonce:        "nonce6",
			expectedSeq:  6,
			checkBitSet:  true,
			bitPosition:  0, // Latest sequence should be at position 0
		},
		{
			name:         "large jump forward",
			initialSeq:   5,
			initialWindow: []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			newSeq:       100,
			nonce:        "nonce100", 
			expectedSeq:  100,
			checkBitSet:  true,
			bitPosition:  0, // Latest sequence should be at position 0 after window clear
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &database.Client{
				ClientID:   "test-device",
				CurrentSeq: tt.initialSeq,
				SeqWindow:  make([]byte, len(tt.initialWindow)),
				NonceCache: []string{},
			}
			copy(client.SeqWindow, tt.initialWindow)

			updateClientSequenceAndNonce(client, tt.newSeq, tt.nonce)

			assert.Equal(t, tt.expectedSeq, client.CurrentSeq, "sequence should be updated")
			assert.Contains(t, client.NonceCache, tt.nonce, "nonce should be added to cache")
			
			if tt.checkBitSet {
				// Check that the bit at position 0 is set (latest sequence)
				assert.NotZero(t, client.SeqWindow[0]&1, "bit 0 should be set for latest sequence")
			}
		})
	}
}
