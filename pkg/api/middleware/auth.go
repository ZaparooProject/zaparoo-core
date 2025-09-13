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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const (
	MutexCleanupInterval = 10 * time.Minute // Cleanup unused mutexes every 10 minutes
	MutexMaxIdle         = 30 * time.Minute // Remove mutexes unused for 30 minutes
)

type clientKey string

// ClientMutexManager handles per-client locking to prevent race conditions
// in authentication state updates
type ClientMutexManager struct {
	mutexes sync.Map // map[string]*clientMutex
}

// ClientMutex represents a per-client mutex for thread-safe operations
type ClientMutex struct {
	lastUsed time.Time
	clientID string
	mu       sync.Mutex
}

// Legacy alias for backward compatibility
type clientMutex = ClientMutex

var globalClientMutexManager = &ClientMutexManager{}

type EncryptedRequest struct {
	Encrypted string `json:"encrypted"`
	IV        string `json:"iv"`
	AuthToken string `json:"authToken"`
}

type DecryptedPayload struct {
	ID      any             `json:"id,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Nonce   string          `json:"nonce"`
	Params  json.RawMessage `json:"params,omitempty"`
	Seq     uint64          `json:"seq"`
}

func isLocalhost(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return host == "localhost"
	}

	return ip.IsLoopback()
}

func AuthMiddleware(db *database.Database) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authentication for localhost connections
			if isLocalhost(r.RemoteAddr) {
				log.Debug().Str("remote_addr", r.RemoteAddr).Msg("localhost connection - skipping auth")
				next.ServeHTTP(w, r)
				return
			}

			// Read request body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				log.Error().Err(err).Msg("failed to read request body")
				http.Error(w, "Failed to read request body", http.StatusInternalServerError)
				return
			}

			// Try to parse as encrypted request
			var encReq EncryptedRequest
			if parseErr := json.Unmarshal(body, &encReq); parseErr != nil {
				log.Error().Err(parseErr).Msg("invalid encrypted request format")
				http.Error(w, "Invalid request format", http.StatusBadRequest)
				return
			}

			// First, validate auth token and get client for initial validation
			initialClient, err := db.UserDB.GetClientByAuthToken(encReq.AuthToken)
			if err != nil {
				tokenStr := "empty"
				if len(encReq.AuthToken) >= 8 {
					tokenStr = encReq.AuthToken[:8] + "..."
				} else if encReq.AuthToken != "" {
					tokenStr = encReq.AuthToken
				}
				log.Warn().Err(err).
					Str("token", tokenStr).
					Str("remote_addr", r.RemoteAddr).
					Str("user_agent", r.Header.Get("User-Agent")).
					Msg("SECURITY: invalid auth token - potential attack")
				http.Error(w, "Invalid auth token", http.StatusUnauthorized)
				return
			}

			// Acquire client lock BEFORE any decryption or validation
			// to prevent race conditions between concurrent requests
			unlockClient := LockClient(initialClient.ClientID)
			defer unlockClient()

			// Re-fetch client state under lock to get latest sequence/nonce state
			client, err := db.UserDB.GetClientByAuthToken(encReq.AuthToken)
			if err != nil {
				log.Error().Err(err).Msg("failed to re-fetch client under lock")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Decrypt payload with locked client data
			decryptedPayload, err := DecryptPayload(encReq.Encrypted, encReq.IV, client.SharedSecret)
			if err != nil {
				log.Error().Err(err).Msg("failed to decrypt payload")
				http.Error(w, "Decryption failed", http.StatusBadRequest)
				return
			}

			// Parse decrypted payload
			var payload DecryptedPayload
			if parseErr := json.Unmarshal(decryptedPayload, &payload); parseErr != nil {
				log.Error().Err(parseErr).Msg("invalid decrypted payload format")
				http.Error(w, "Invalid payload format", http.StatusBadRequest)
				return
			}

			// Create replay protector from client state
			replayProtector := NewReplayProtector(client)

			// Validate sequence number and nonce with locked client state
			if !replayProtector.ValidateSequenceAndNonce(payload.Seq, payload.Nonce) {
				log.Warn().
					Str("client_id", client.ClientID).
					Uint64("seq", payload.Seq).
					Str("nonce", payload.Nonce).
					Str("remote_addr", r.RemoteAddr).
					Str("user_agent", r.Header.Get("User-Agent")).
					Msg("SECURITY: replay attack detected")
				http.Error(w, "Invalid sequence or replay detected", http.StatusBadRequest)
				return
			}

			// Update replay protector state
			replayProtector.UpdateSequenceAndNonce(payload.Seq, payload.Nonce)

			// Get updated state for database storage
			currentSeq, seqWindow, nonceCache := replayProtector.GetStateForDatabase()
			client.CurrentSeq = currentSeq
			client.SeqWindow = seqWindow
			client.NonceCache = nonceCache

			// Save to database (still under lock)
			if updateErr := db.UserDB.UpdateClientSequence(
				client.ClientID, client.CurrentSeq, client.SeqWindow, client.NonceCache,
			); updateErr != nil {
				log.Error().Err(updateErr).Msg("failed to update client state")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Replace request body with decrypted JSON-RPC payload
			originalPayload := map[string]any{
				"jsonrpc": payload.JSONRPC,
				"method":  payload.Method,
				"id":      payload.ID,
			}
			if payload.Params != nil {
				originalPayload["params"] = payload.Params
			}

			newBody, err := json.Marshal(originalPayload)
			if err != nil {
				log.Error().Err(err).Msg("failed to marshal decrypted payload")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Create new request with decrypted body
			r.Body = io.NopCloser(bytes.NewReader(newBody))
			r.ContentLength = int64(len(newBody))

			// Store client in context for potential use by handlers
			ctx := context.WithValue(r.Context(), clientKey("client"), client)
			r = r.WithContext(ctx)

			log.Info().
				Str("client_id", client.ClientID).
				Str("method", payload.Method).
				Uint64("seq", payload.Seq).
				Str("remote_addr", r.RemoteAddr).
				Msg("SECURITY: authenticated request processed")

			next.ServeHTTP(w, r)
		})
	}
}

func DecryptPayload(encryptedB64, ivB64 string, key []byte) ([]byte, error) {
	// Decode base64
	encrypted, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		return nil, fmt.Errorf("invalid encrypted data: %w", err)
	}

	iv, err := base64.StdEncoding.DecodeString(ivB64)
	if err != nil {
		return nil, fmt.Errorf("invalid IV: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt
	plaintext, err := gcm.Open(nil, iv, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

func EncryptPayload(data, key []byte) (encrypted, iv string, err error) {
	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random IV
	ivBytes := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, ivBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate IV: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nil, ivBytes, data, nil)

	// Return base64 encoded values
	return base64.StdEncoding.EncodeToString(ciphertext),
		base64.StdEncoding.EncodeToString(ivBytes), nil
}

// getClientMutex retrieves or creates a mutex for the given client ID
func (cm *ClientMutexManager) getClientMutex(clientID string) *ClientMutex {
	// Try to load existing mutex
	if value, exists := cm.mutexes.Load(clientID); exists {
		mutex, ok := value.(*clientMutex)
		if !ok {
			log.Error().Str("client_id", clientID).Msg("invalid mutex type in cache")
			return nil
		}
		mutex.lastUsed = time.Now()
		return mutex
	}

	// Create new mutex
	newMutex := &ClientMutex{
		lastUsed: time.Now(),
		clientID: clientID,
	}

	// Store and return the mutex (LoadOrStore handles race conditions)
	actual, _ := cm.mutexes.LoadOrStore(clientID, newMutex)
	actualMutex, ok := actual.(*clientMutex)
	if !ok {
		log.Error().Str("client_id", clientID).Msg("invalid mutex type after LoadOrStore")
		return nil
	}
	actualMutex.lastUsed = time.Now()
	return actualMutex
}

// lockClient acquires a lock for the specified client, preventing race conditions
// in authentication state updates. The returned function must be called to release the lock.
func (cm *ClientMutexManager) lockClient(clientID string) func() {
	mutex := cm.getClientMutex(clientID)
	mutex.mu.Lock()

	return func() {
		mutex.mu.Unlock()
	}
}

// cleanup removes unused mutexes to prevent memory leaks
func (cm *ClientMutexManager) cleanup() {
	now := time.Now()
	cm.mutexes.Range(func(key, value any) bool {
		mutex, ok := value.(*clientMutex)
		if !ok {
			log.Error().Interface("key", key).Msg("invalid mutex type in cleanup")
			return true
		}
		if now.Sub(mutex.lastUsed) > MutexMaxIdle {
			cm.mutexes.Delete(key)
			log.Debug().Str("client_id", mutex.clientID).Msg("cleaned up unused client mutex")
		}
		return true
	})
}

// StartCleanupRoutine starts a background goroutine to periodically clean up unused mutexes
func (cm *ClientMutexManager) StartCleanupRoutine(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(MutexCleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("client mutex cleanup routine stopped")
				return
			case <-ticker.C:
				cm.cleanup()
			}
		}
	}()
}

// GetClientMutex is a convenience function to get a mutex for a client
func GetClientMutex(clientID string) *ClientMutex {
	return globalClientMutexManager.getClientMutex(clientID)
}

// LockClient is a convenience function to lock a client
func LockClient(clientID string) func() {
	return globalClientMutexManager.lockClient(clientID)
}

// StartGlobalMutexCleanup starts the global mutex cleanup routine
func StartGlobalMutexCleanup(ctx context.Context) {
	globalClientMutexManager.StartCleanupRoutine(ctx)
}

func IsAuthenticatedConnection(r *http.Request) bool {
	return !isLocalhost(r.RemoteAddr)
}

func GetClientFromContext(ctx context.Context) *database.Client {
	if client, ok := ctx.Value(clientKey("client")).(*database.Client); ok {
		return client
	}
	return nil
}
