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

package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/hkdf"
)

const (
	PairingTokenExpiry  = 5 * time.Minute
	PairingAttemptLimit = 10
)

type PairingSession struct {
	CreatedAt time.Time
	Token     string
	Challenge []byte
	Salt      []byte
	Attempts  int
}

type PairingManager struct {
	sessions map[string]*PairingSession
	mu       sync.RWMutex
}

type PairingInitiateRequest struct {
	DeviceName string `json:"deviceName"`
}

type PairingInitiateResponse struct {
	PairingToken string `json:"pairingToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

type PairingCompleteRequest struct {
	PairingToken string `json:"pairingToken"`
	Verifier     string `json:"verifier"`
	DeviceName   string `json:"deviceName"`
}

type PairingCompleteResponse struct {
	DeviceID     string `json:"deviceId"`
	AuthToken    string `json:"authToken"`
	SharedSecret string `json:"sharedSecret"` // Base64 encoded
}

var pairingManager = &PairingManager{
	sessions: make(map[string]*PairingSession),
}

func init() {
	// Start cleanup routine
	go pairingManager.cleanup()
}

func (pm *PairingManager) createSession() (*PairingSession, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Generate random token and challenge
	token := uuid.New().String()
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("failed to generate challenge: %w", err)
	}

	// Generate random salt (32 bytes for SHA-256)
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	session := &PairingSession{
		Token:     token,
		Challenge: challenge,
		Salt:      salt,
		CreatedAt: time.Now(),
		Attempts:  0,
	}

	pm.sessions[token] = session

	log.Debug().Str("token", token[:8]+"...").Msg("created pairing session")
	return session, nil
}

func (pm *PairingManager) consumeSession(token string) (*PairingSession, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	session, exists := pm.sessions[token]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Since(session.CreatedAt) > PairingTokenExpiry {
		delete(pm.sessions, token)
		return nil, false
	}

	// Increment attempts
	session.Attempts++

	// Check attempt limit
	if session.Attempts >= PairingAttemptLimit {
		delete(pm.sessions, token)
		return nil, false
	}

	return session, true
}

func (pm *PairingManager) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		pm.mu.Lock()
		now := time.Now()
		for token, session := range pm.sessions {
			if now.Sub(session.CreatedAt) > PairingTokenExpiry {
				delete(pm.sessions, token)
			}
		}
		pm.mu.Unlock()
	}
}

func handlePairingInitiate(_ *database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		session, err := pairingManager.createSession()
		if err != nil {
			log.Error().Err(err).Msg("failed to create pairing session")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		response := PairingInitiateResponse{
			PairingToken: session.Token,
			ExpiresIn:    int(PairingTokenExpiry.Seconds()),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Error().Err(err).Msg("failed to encode pairing response")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Info().Str("token", session.Token[:8]+"...").Msg("pairing session initiated")
	}
}

func handlePairingComplete(db *database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req PairingCompleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}

		if req.PairingToken == "" || req.Verifier == "" || req.DeviceName == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// Get and consume pairing session
		session, exists := pairingManager.consumeSession(req.PairingToken)
		if !exists {
			http.Error(w, "Invalid or expired pairing token", http.StatusBadRequest)
			return
		}

		// Derive shared secret using HKDF (challenge + verifier)
		combinedSecret := make([]byte, len(session.Challenge)+len(req.Verifier))
		copy(combinedSecret, session.Challenge)
		copy(combinedSecret[len(session.Challenge):], req.Verifier)
		sharedSecret := make([]byte, 32) // 256 bits for AES-256

		// Construct context-specific info string for domain separation
		info := []byte("zaparoo-pairing-v1|" + req.PairingToken + "|" + req.DeviceName)

		kdf := hkdf.New(sha256.New, combinedSecret, session.Salt, info)
		if _, err := kdf.Read(sharedSecret); err != nil {
			log.Error().Err(err).Msg("failed to derive shared secret")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Generate auth token
		authToken := uuid.New().String()

		// Create device in database
		device, err := db.UserDB.CreateDevice(req.DeviceName, authToken, sharedSecret)
		if err != nil {
			log.Error().Err(err).Msg("failed to create device")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Remove session from manager
		pairingManager.mu.Lock()
		delete(pairingManager.sessions, req.PairingToken)
		pairingManager.mu.Unlock()

		response := PairingCompleteResponse{
			DeviceID:     device.DeviceID,
			AuthToken:    authToken,
			SharedSecret: hex.EncodeToString(sharedSecret),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Error().Err(err).Msg("failed to encode pairing response")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Info().
			Str("device_id", device.DeviceID).
			Str("device_name", device.DeviceName).
			Msg("device paired successfully")
	}
}

// QRCodeData represents the data embedded in the pairing QR code
type QRCodeData struct {
	Address string `json:"address"`
	Token   string `json:"token"`
}
