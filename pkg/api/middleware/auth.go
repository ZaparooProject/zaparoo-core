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
	"slices"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/rs/zerolog/log"
)

const (
	SequenceWindow = 64  // Size of sliding window for sequence numbers
	NonceCacheSize = 100 // Maximum number of cached nonces
	MutexCleanupInterval = 10 * time.Minute // Cleanup unused mutexes every 10 minutes
	MutexMaxIdle = 30 * time.Minute // Remove mutexes unused for 30 minutes
)

type deviceKey string

// DeviceMutexManager handles per-device locking to prevent race conditions
// in authentication state updates
type DeviceMutexManager struct {
	mutexes sync.Map // map[string]*deviceMutex
}

type deviceMutex struct {
	mu       sync.Mutex
	lastUsed time.Time
	deviceID string
}

var globalDeviceMutexManager = &DeviceMutexManager{}

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

			// Validate auth token and get device
			device, err := db.UserDB.GetDeviceByAuthToken(encReq.AuthToken)
			if err != nil {
				tokenStr := "empty"
				if len(encReq.AuthToken) >= 8 {
					tokenStr = encReq.AuthToken[:8] + "..."
				} else if encReq.AuthToken != "" {
					tokenStr = encReq.AuthToken
				}
				log.Error().Err(err).Str("token", tokenStr).Msg("invalid auth token")
				http.Error(w, "Invalid auth token", http.StatusUnauthorized)
				return
			}

			// Decrypt payload
			decryptedPayload, err := DecryptPayload(encReq.Encrypted, encReq.IV, device.SharedSecret)
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

			// CRITICAL SECTION: Acquire device lock to prevent race conditions
			// between validation and database update
			unlockDevice := LockDevice(device.DeviceID)
			defer unlockDevice()

			// Re-fetch device state under lock to get latest sequence/nonce state
			freshDevice, err := db.UserDB.GetDeviceByAuthToken(encReq.AuthToken)
			if err != nil {
				log.Error().Err(err).Msg("failed to re-fetch device under lock")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Validate sequence number and nonce with fresh device state
			if !ValidateSequenceAndNonce(freshDevice, payload.Seq, payload.Nonce) {
				log.Warn().
					Str("device_id", freshDevice.DeviceID).
					Uint64("seq", payload.Seq).
					Str("nonce", payload.Nonce).
					Msg("invalid sequence or replay attack detected")
				http.Error(w, "Invalid sequence or replay detected", http.StatusBadRequest)
				return
			}

			// Update device state (sequence and nonce cache)
			updatedDevice := *freshDevice
			updateDeviceSequenceAndNonce(&updatedDevice, payload.Seq, payload.Nonce)

			// Save to database (still under lock)
			if updateErr := db.UserDB.UpdateDeviceSequence(
				updatedDevice.DeviceID, updatedDevice.CurrentSeq, updatedDevice.SeqWindow, updatedDevice.NonceCache,
			); updateErr != nil {
				log.Error().Err(updateErr).Msg("failed to update device state")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Update the device pointer for context (use fresh device with updates)
			device = &updatedDevice

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

			// Store device in context for potential use by handlers
			ctx := context.WithValue(r.Context(), deviceKey("device"), device)
			r = r.WithContext(ctx)

			log.Debug().
				Str("device_id", device.DeviceID).
				Str("method", payload.Method).
				Uint64("seq", payload.Seq).
				Msg("authenticated request processed")

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

func ValidateSequenceAndNonce(device *database.Device, seq uint64, nonce string) bool {
	// Check if nonce was recently used (replay protection)
	if slices.Contains(device.NonceCache, nonce) {
		return false
	}

	// Validate sequence number with sliding window
	if seq <= device.CurrentSeq {
		// Check if sequence is within acceptable window
		diff := device.CurrentSeq - seq
		if diff >= SequenceWindow {
			return false // Too old
		}

		// Check if this sequence was already processed (using seq_window bitmap)
		windowPos := diff % SequenceWindow
		bytePos := windowPos / 8
		bitPos := windowPos % 8

		if bytePos < uint64(len(device.SeqWindow)) {
			if (device.SeqWindow[bytePos] & (1 << bitPos)) != 0 {
				return false // Already processed
			}
		}
	}

	return true
}

func updateDeviceSequenceAndNonce(device *database.Device, seq uint64, nonce string) {
	// Update nonce cache (keep last NonceCacheSize nonces)
	device.NonceCache = append(device.NonceCache, nonce)
	if len(device.NonceCache) > NonceCacheSize {
		device.NonceCache = device.NonceCache[1:] // Remove oldest
	}

	// Update sequence window
	if seq > device.CurrentSeq {
		// New highest sequence - shift window
		shift := seq - device.CurrentSeq
		if shift >= SequenceWindow {
			// Clear entire window
			device.SeqWindow = make([]byte, 8)
		} else {
			// Shift window right
			for range shift {
				shiftWindowRight(device.SeqWindow)
			}
		}
		device.CurrentSeq = seq

		// Mark current sequence as processed (position 0 in window)
		device.SeqWindow[0] |= 1
	} else {
		// Mark this sequence as processed in the window
		diff := device.CurrentSeq - seq
		windowPos := diff % SequenceWindow
		bytePos := windowPos / 8
		bitPos := windowPos % 8

		if bytePos < uint64(len(device.SeqWindow)) {
			device.SeqWindow[bytePos] |= (1 << bitPos)
		}
	}
}

func UpdateDeviceState(userDB *userdb.UserDB, device *database.Device, seq uint64, nonce string) error {
	// Update nonce cache (keep last NonceCacheSize nonces)
	device.NonceCache = append(device.NonceCache, nonce)
	if len(device.NonceCache) > NonceCacheSize {
		device.NonceCache = device.NonceCache[1:] // Remove oldest
	}

	// Update sequence window
	if seq > device.CurrentSeq {
		// New highest sequence - shift window
		shift := seq - device.CurrentSeq
		if shift >= SequenceWindow {
			// Clear entire window
			device.SeqWindow = make([]byte, 8)
		} else {
			// Shift window right
			for range shift {
				shiftWindowRight(device.SeqWindow)
			}
		}
		device.CurrentSeq = seq

		// Mark current sequence as processed (position 0 in window)
		device.SeqWindow[0] |= 1
	} else {
		// Mark this sequence as processed in the window
		diff := device.CurrentSeq - seq
		windowPos := diff % SequenceWindow
		bytePos := windowPos / 8
		bitPos := windowPos % 8

		if bytePos < uint64(len(device.SeqWindow)) {
			device.SeqWindow[bytePos] |= (1 << bitPos)
		}
	}

	// Update database
	if err := userDB.UpdateDeviceSequence(
		device.DeviceID, device.CurrentSeq, device.SeqWindow, device.NonceCache,
	); err != nil {
		return fmt.Errorf("failed to update device sequence: %w", err)
	}
	return nil
}

// getDeviceMutex retrieves or creates a mutex for the given device ID
func (dm *DeviceMutexManager) getDeviceMutex(deviceID string) *deviceMutex {
	// Try to load existing mutex
	if value, exists := dm.mutexes.Load(deviceID); exists {
		mutex := value.(*deviceMutex)
		mutex.lastUsed = time.Now()
		return mutex
	}

	// Create new mutex
	newMutex := &deviceMutex{
		lastUsed: time.Now(),
		deviceID: deviceID,
	}

	// Store and return the mutex (LoadOrStore handles race conditions)
	actual, _ := dm.mutexes.LoadOrStore(deviceID, newMutex)
	actualMutex := actual.(*deviceMutex)
	actualMutex.lastUsed = time.Now()
	return actualMutex
}

// lockDevice acquires a lock for the specified device, preventing race conditions
// in authentication state updates. The returned function must be called to release the lock.
func (dm *DeviceMutexManager) lockDevice(deviceID string) func() {
	mutex := dm.getDeviceMutex(deviceID)
	mutex.mu.Lock()
	
	return func() {
		mutex.mu.Unlock()
	}
}

// cleanup removes unused mutexes to prevent memory leaks
func (dm *DeviceMutexManager) cleanup() {
	now := time.Now()
	dm.mutexes.Range(func(key, value interface{}) bool {
		mutex := value.(*deviceMutex)
		if now.Sub(mutex.lastUsed) > MutexMaxIdle {
			dm.mutexes.Delete(key)
			log.Debug().Str("device_id", mutex.deviceID).Msg("cleaned up unused device mutex")
		}
		return true
	})
}

// startCleanupRoutine starts a background goroutine to periodically clean up unused mutexes
func (dm *DeviceMutexManager) startCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(MutexCleanupInterval)
		defer ticker.Stop()
		
		for range ticker.C {
			dm.cleanup()
		}
	}()
}

// GetDeviceMutex is a convenience function to get a mutex for a device
func GetDeviceMutex(deviceID string) *deviceMutex {
	return globalDeviceMutexManager.getDeviceMutex(deviceID)
}

// LockDevice is a convenience function to lock a device
func LockDevice(deviceID string) func() {
	return globalDeviceMutexManager.lockDevice(deviceID)
}

func init() {
	// Start cleanup routine for device mutexes
	globalDeviceMutexManager.startCleanupRoutine()
}

func shiftWindowRight(window []byte) {
	carry := byte(0)
	for i := len(window) - 1; i >= 0; i-- {
		newCarry := (window[i] & 1) << 7
		window[i] = (window[i] >> 1) | carry
		carry = newCarry
	}
}

func IsAuthenticatedConnection(r *http.Request) bool {
	return !isLocalhost(r.RemoteAddr)
}

func GetDeviceFromContext(ctx context.Context) *database.Device {
	if device, ok := ctx.Value(deviceKey("device")).(*database.Device); ok {
		return device
	}
	return nil
}
