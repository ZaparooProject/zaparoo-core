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
	"crypto/hkdf"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/schollz/pake/v3"
)

// Pairing constants.
const (
	pairingPINLength = 6 // ~20 bits; PAKE prevents offline brute force

	// pairingPINMax is 10^pairingPINLength.
	pairingPINMax int64 = 1_000_000

	// pairingPINTTL is how long a generated PIN remains valid.
	pairingPINTTL = 5 * time.Minute

	// pairingSessionTTL is how long a /pair/start session remains valid
	// before it must be completed via /pair/finish.
	pairingSessionTTL = 2 * time.Minute

	// pairingMaxAttempts is the maximum number of failed /pair/finish HMAC
	// verifications across all sessions for a single PIN before the PIN is
	// invalidated and the user must start over.
	pairingMaxAttempts = 3

	// pairingMaxClients is the maximum number of paired clients per device.
	pairingMaxClients = 50

	// pairingMaxNameLen is the maximum length in bytes of a client name.
	pairingMaxNameLen = 128

	// pairingCleanupInterval is how often the cleanup goroutine runs.
	pairingCleanupInterval = 1 * time.Minute

	pairingCurve = "p256" // P-256 for Web Crypto API compatibility

	// pairingProtoVersion is the protocol version included in the HMAC
	// transcript binding to enable future versioning.
	pairingProtoVersion = "zaparoo-v1"

	pairingMaxPakeMessageBytes = 2048 // ~3× typical message size
)

// Pairing errors (unexported; public contract is HTTP status codes + JSON).
var (
	errPairingInProgress     = errors.New("pairing already in progress")
	errNoPairingPending      = errors.New("no pairing pending")
	errPairingExpired        = errors.New("pairing expired")
	errPairingExhausted      = errors.New("pairing attempts exhausted")
	errPairingSessionUnknown = errors.New("pairing session unknown")
	errPairingNameTooLong    = errors.New("client name too long")
	errPairingNameEmpty      = errors.New("client name required")
	errTooManyClients        = errors.New("maximum number of paired clients reached")
	errPairingHMACMismatch   = errors.New("pairing confirmation HMAC mismatch")
	errPairingMessageTooLong = errors.New("pairing PAKE message too long")
)

// HKDF info strings used to derive confirmation keys and the long-term
// pairing key from the raw PAKE session key.
const (
	pairingInfoConfirmA = "zaparoo-confirm-A"
	pairingInfoConfirmB = "zaparoo-confirm-B"
	pairingInfoPairing  = "zaparoo-pairing-v1"
)

// pairingSession represents an in-flight PAKE exchange.
type pairingSession struct {
	createdAt  time.Time
	pake       *pake.Pake
	sessionID  string
	name       string
	msgA       []byte // raw bytes received from client at /pair/start
	msgB       []byte // raw bytes sent to client at /pair/start
	sessionKey []byte // raw PAKE session key
}

// PairingManager owns the PIN and in-flight sessions (single mutex protects all state).
type PairingManager struct {
	pinExpiresAt    time.Time
	db              database.UserDBI
	notifChan       chan<- models.Notification
	sessions        map[string]*pairingSession
	pin             string
	pinAttempts     int
	maxClients      int
	maxAttempts     int
	maxNameLen      int
	pinTTL          time.Duration
	sessionTTL      time.Duration
	cleanupInterval time.Duration
	mu              syncutil.Mutex
}

// PairingOption configures a PairingManager at construction time.
type PairingOption func(*PairingManager)

// WithPairingPINTTL overrides the PIN time-to-live (default 5min).
func WithPairingPINTTL(d time.Duration) PairingOption {
	return func(m *PairingManager) { m.pinTTL = d }
}

// WithPairingSessionTTL overrides the /pair/start session TTL (default 2min).
func WithPairingSessionTTL(d time.Duration) PairingOption {
	return func(m *PairingManager) { m.sessionTTL = d }
}

// WithPairingMaxClients overrides the maximum paired clients (default 50).
func WithPairingMaxClients(n int) PairingOption {
	return func(m *PairingManager) { m.maxClients = n }
}

// WithPairingMaxAttempts overrides the maximum PIN attempts (default 3).
func WithPairingMaxAttempts(n int) PairingOption {
	return func(m *PairingManager) { m.maxAttempts = n }
}

// WithPairingCleanupInterval overrides the cleanup goroutine tick interval.
func WithPairingCleanupInterval(d time.Duration) PairingOption {
	return func(m *PairingManager) { m.cleanupInterval = d }
}

// NewPairingManager constructs a PairingManager (pass nil notifChan to disable notifications).
func NewPairingManager(
	db database.UserDBI,
	notifChan chan<- models.Notification,
	opts ...PairingOption,
) *PairingManager {
	m := &PairingManager{
		db:              db,
		notifChan:       notifChan,
		sessions:        make(map[string]*pairingSession),
		maxClients:      pairingMaxClients,
		maxAttempts:     pairingMaxAttempts,
		maxNameLen:      pairingMaxNameLen,
		pinTTL:          pairingPINTTL,
		sessionTTL:      pairingSessionTTL,
		cleanupInterval: pairingCleanupInterval,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// StartCleanup begins the background cleanup goroutine. The goroutine exits
// when ctx is canceled. Safe to call multiple times only with distinct
// contexts; callers should call this once during server startup.
func (m *PairingManager) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(m.cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.cleanupExpired()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// PendingPIN returns the currently displayed PIN and its expiry, or an empty
// PIN if no pairing is in progress.
func (m *PairingManager) PendingPIN() (pin string, expiresAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pin == "" || time.Now().After(m.pinExpiresAt) {
		return "", time.Time{}
	}
	return m.pin, m.pinExpiresAt
}

// StartPairing generates a new PIN (fails fast if clients are at max).
func (m *PairingManager) StartPairing() (pin string, expiresAt time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pin != "" && time.Now().Before(m.pinExpiresAt) {
		return "", time.Time{}, errPairingInProgress
	}

	// Fail fast if full (re-checked in finishSession as defense in depth).
	count, countErr := m.db.CountClients()
	if countErr != nil {
		return "", time.Time{}, fmt.Errorf("count clients: %w", countErr)
	}
	if count >= m.maxClients {
		return "", time.Time{}, errTooManyClients
	}

	pin, err = generatePIN()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generate pin: %w", err)
	}
	m.pin = pin
	m.pinExpiresAt = time.Now().Add(m.pinTTL)
	m.pinAttempts = 0
	// Drop any leftover sessions from a previous PIN — they cannot succeed.
	m.sessions = make(map[string]*pairingSession)
	return pin, m.pinExpiresAt, nil
}

// CancelPairing clears the current PIN and any in-flight sessions. Safe to
// call when no pairing is in progress.
func (m *PairingManager) CancelPairing() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearPINLocked()
}

// clearPINLocked resets all pairing state. Caller must hold mu.
func (m *PairingManager) clearPINLocked() {
	m.pin = ""
	m.pinExpiresAt = time.Time{}
	m.pinAttempts = 0
	m.sessions = make(map[string]*pairingSession)
}

// pairStartRequest is the JSON request body for POST /api/pair/start.
type pairStartRequest struct {
	PAKE string `json:"pake"` // base64(client PAKE message A)
	Name string `json:"name"`
}

// pairStartResponse is the JSON response body for POST /api/pair/start.
type pairStartResponse struct {
	Session string `json:"session"`
	PAKE    string `json:"pake"` // base64(server PAKE message B)
}

// pairFinishRequest is the JSON request body for POST /api/pair/finish.
type pairFinishRequest struct {
	Session string `json:"session"`
	Confirm string `json:"confirm"` // base64(client HMAC)
}

// pairFinishResponse is the JSON response body for POST /api/pair/finish.
// The pairing key is NEVER transmitted: the client derives it independently
// from the PAKE session key via HKDF-Expand(prk, "zaparoo-pairing-v1", 32).
type pairFinishResponse struct {
	AuthToken string `json:"authToken"`
	ClientID  string `json:"clientId"`
	Confirm   string `json:"confirm"` // base64(server HMAC)
}

// startSession runs the server side of the PAKE exchange and stores a
// new in-flight session. Returns the sessionID and the server's PAKE
// message B for the client.
func (m *PairingManager) startSession(name string, msgA []byte) (sessionID string, msgB []byte, err error) {
	if name == "" {
		return "", nil, errPairingNameEmpty
	}
	if len(name) > m.maxNameLen {
		return "", nil, errPairingNameTooLong
	}
	if len(msgA) > pairingMaxPakeMessageBytes {
		return "", nil, errPairingMessageTooLong
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pin == "" {
		return "", nil, errNoPairingPending
	}
	if time.Now().After(m.pinExpiresAt) {
		m.clearPINLocked()
		return "", nil, errPairingExpired
	}
	if m.pinAttempts >= m.maxAttempts {
		m.clearPINLocked()
		return "", nil, errPairingExhausted
	}

	// Server initializes as responder (role 1).
	server, err := pake.InitCurve([]byte(m.pin), 1, pairingCurve)
	if err != nil {
		return "", nil, fmt.Errorf("pake init: %w", err)
	}
	if updateErr := server.Update(msgA); updateErr != nil {
		return "", nil, fmt.Errorf("pake update with client message: %w", updateErr)
	}

	sessionKey, err := server.SessionKey()
	if err != nil {
		return "", nil, fmt.Errorf("derive pake session key: %w", err)
	}

	// Capture serialized state before mutation.
	msgB = server.Bytes()

	sessionID = uuid.New().String()
	m.sessions[sessionID] = &pairingSession{
		sessionID:  sessionID,
		pake:       server,
		sessionKey: sessionKey,
		msgA:       append([]byte(nil), msgA...),
		msgB:       append([]byte(nil), msgB...),
		name:       name,
		createdAt:  time.Now(),
	}
	return sessionID, msgB, nil
}

// PairingResult is the outcome of a successful PAKE handshake. The Client is
// persisted to the database; the ServerHMAC must be returned to the client so
// it can verify the server's identity.
type PairingResult struct {
	Client     *database.Client
	ServerHMAC []byte
}

// finishSession verifies HMAC, persists client, and publishes notification
// (notification sent after releasing mu to avoid deadlock).
func (m *PairingManager) finishSession(sessionID string, clientHMAC []byte) (*PairingResult, error) {
	m.mu.Lock()
	result, notifyPayload, err := m.finishSessionLocked(sessionID, clientHMAC)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if notifyPayload != nil && m.notifChan != nil {
		notifications.ClientsPaired(m.notifChan, *notifyPayload)
	}
	return result, nil
}

// finishSessionLocked performs locked pairing validation. Caller must hold mu.
func (m *PairingManager) finishSessionLocked(
	sessionID string,
	clientHMAC []byte,
) (*PairingResult, *models.ClientsPairedNotification, error) {
	sess, ok := m.sessions[sessionID]
	if !ok {
		return nil, nil, errPairingSessionUnknown
	}

	if time.Since(sess.createdAt) > m.sessionTTL {
		delete(m.sessions, sessionID)
		return nil, nil, errPairingExpired
	}

	// Derive confirmation keys + pairing key from the raw PAKE session key.
	prk, err := hkdf.Extract(sha256.New, sess.sessionKey, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("hkdf extract: %w", err)
	}
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	if err != nil {
		return nil, nil, fmt.Errorf("hkdf expand confirm A: %w", err)
	}
	confirmKeyB, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmB, sha256.Size)
	if err != nil {
		return nil, nil, fmt.Errorf("hkdf expand confirm B: %w", err)
	}
	derivedPairingKey, err := hkdf.Expand(sha256.New, prk, pairingInfoPairing, crypto.PairingKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("hkdf expand pairing key: %w", err)
	}

	// Verify client HMAC (wrong PIN or brute-force on mismatch).
	expectedClient := computePairingHMAC(confirmKeyA, "client", sess.name, sess.msgA, sess.msgB)
	if !hmac.Equal(expectedClient, clientHMAC) {
		delete(m.sessions, sessionID)
		m.pinAttempts++
		if m.pinAttempts >= m.maxAttempts {
			m.clearPINLocked()
			return nil, nil, errPairingExhausted
		}
		return nil, nil, errPairingHMACMismatch
	}

	// Enforce client cap (safe without transaction — mu serializes access).
	count, err := m.db.CountClients()
	if err != nil {
		return nil, nil, fmt.Errorf("count clients: %w", err)
	}
	if count >= m.maxClients {
		// Don't reset the PIN — the operator can revoke a client and retry.
		delete(m.sessions, sessionID)
		return nil, nil, errTooManyClients
	}

	now := time.Now().Unix()
	c := &database.Client{
		ClientID:   uuid.New().String(),
		ClientName: sess.name,
		AuthToken:  uuid.New().String(),
		PairingKey: derivedPairingKey,
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if createErr := m.db.CreateClient(c); createErr != nil {
		return nil, nil, fmt.Errorf("create client: %w", createErr)
	}

	serverHMAC := computePairingHMAC(confirmKeyB, "server", sess.name, sess.msgA, sess.msgB)

	// Pairing succeeded — clean up state.
	delete(m.sessions, sessionID)
	m.clearPINLocked()

	notifyPayload := &models.ClientsPairedNotification{
		ClientID:   c.ClientID,
		ClientName: c.ClientName,
	}
	return &PairingResult{Client: c, ServerHMAC: serverHMAC}, notifyPayload, nil
}

// cleanupExpired removes expired PIN and sessions. Called by the cleanup
// goroutine on each tick.
func (m *PairingManager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if m.pin != "" && now.After(m.pinExpiresAt) {
		log.Debug().Msg("pairing: PIN expired, clearing")
		// clearPINLocked also wipes m.sessions, so the per-session loop
		// below is not needed in this branch. Any change that decouples
		// session wiping from clearPINLocked must drop this early return.
		m.clearPINLocked()
		return
	}
	for id, sess := range m.sessions {
		if now.Sub(sess.createdAt) > m.sessionTTL {
			delete(m.sessions, id)
		}
	}
}

// generatePIN returns a zero-padded decimal PIN of pairingPINLength digits
// using crypto/rand. Returns an error if the random source fails.
func generatePIN() (string, error) {
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(pairingPINMax))
	if err != nil {
		return "", fmt.Errorf("rand int: %w", err)
	}
	return fmt.Sprintf("%0*d", pairingPINLength, n.Int64()), nil
}

// computePairingHMAC returns HMAC-SHA256 with length-prefixed fields
// (prevents canonicalization attacks).
func computePairingHMAC(key []byte, role, name string, msgA, msgB []byte) []byte {
	h := hmac.New(sha256.New, key)
	writeLP(h, []byte(pairingProtoVersion))
	writeLP(h, []byte(pairingCurve))
	writeLP(h, []byte(role))
	writeLP(h, []byte(name))
	writeLP(h, msgA)
	writeLP(h, msgB)
	return h.Sum(nil)
}

// writeLP writes a 4-byte big-endian length prefix then bytes.
func writeLP(h io.Writer, b []byte) {
	var lp [4]byte
	//nolint:gosec // input length is bounded by caller; never exceeds uint32 max
	binary.BigEndian.PutUint32(lp[:], uint32(len(b)))
	_, _ = h.Write(lp[:])
	_, _ = h.Write(b)
}

// HandlePairStart runs the PAKE exchange and returns sessionID + server message.
func (m *PairingManager) HandlePairStart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const maxBodySize = 16 * 1024
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var req pairStartRequest
		if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
			pairingErrorResponse(w, http.StatusBadRequest, "invalid request body")
			return
		}
		msgA, decErr := base64.StdEncoding.DecodeString(req.PAKE)
		if decErr != nil || len(msgA) == 0 {
			pairingErrorResponse(w, http.StatusBadRequest, "invalid pake message")
			return
		}

		sessionID, msgB, err := m.startSession(req.Name, msgA)
		if err != nil {
			status, msg := pairingErrorStatus(err)
			pairingErrorResponse(w, status, msg)
			return
		}

		writeJSON(w, http.StatusOK, pairStartResponse{
			Session: sessionID,
			PAKE:    base64.StdEncoding.EncodeToString(msgB),
		})
	}
}

// HandlePairFinish verifies HMAC, persists client, and returns auth token + confirmation.
func (m *PairingManager) HandlePairFinish() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const maxBodySize = 4 * 1024
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var req pairFinishRequest
		if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
			pairingErrorResponse(w, http.StatusBadRequest, "invalid request body")
			return
		}
		clientHMAC, decErr := base64.StdEncoding.DecodeString(req.Confirm)
		if decErr != nil || len(clientHMAC) == 0 {
			pairingErrorResponse(w, http.StatusBadRequest, "invalid confirmation")
			return
		}

		result, err := m.finishSession(req.Session, clientHMAC)
		if err != nil {
			// Audit-log security-relevant failures with the source IP.
			// Other errors (expired session, unknown session, etc.) are
			// handled by the generic mapping below.
			logFailedPairingAttempt(r, err)
			status, msg := pairingErrorStatus(err)
			pairingErrorResponse(w, status, msg)
			return
		}

		writeJSON(w, http.StatusOK, pairFinishResponse{
			AuthToken: result.Client.AuthToken,
			ClientID:  result.Client.ClientID,
			Confirm:   base64.StdEncoding.EncodeToString(result.ServerHMAC),
		})
	}
}

// logFailedPairingAttempt logs HMAC mismatch and exhaustion (not operational errors).
func logFailedPairingAttempt(r *http.Request, err error) {
	switch {
	case errors.Is(err, errPairingHMACMismatch):
		log.Warn().
			Str("source_ip", sourceIPForAudit(r)).
			Str("event", "pairing_hmac_mismatch").
			Msg("pairing: failed PIN verification")
	case errors.Is(err, errPairingExhausted):
		log.Warn().
			Str("source_ip", sourceIPForAudit(r)).
			Str("event", "pairing_attempts_exhausted").
			Msg("pairing: PIN attempts exhausted, PIN invalidated")
	}
}

// sourceIPForAudit returns parsed source IP (or "unknown"), IPv6-safe.
func sourceIPForAudit(r *http.Request) string {
	if ip := apimiddleware.ParseRemoteIP(r.RemoteAddr); ip != nil {
		return ip.String()
	}
	return "unknown"
}

// pairingErrorStatus maps a manager error to the HTTP status + public message.
func pairingErrorStatus(err error) (status int, msg string) {
	switch {
	case errors.Is(err, errNoPairingPending):
		return http.StatusBadRequest, "no pairing in progress"
	case errors.Is(err, errPairingExpired):
		return http.StatusGone, "pairing expired"
	case errors.Is(err, errPairingExhausted):
		return http.StatusForbidden, "too many failed attempts"
	case errors.Is(err, errPairingSessionUnknown):
		return http.StatusNotFound, "unknown pairing session"
	case errors.Is(err, errPairingNameTooLong):
		return http.StatusBadRequest, "client name too long"
	case errors.Is(err, errPairingNameEmpty):
		return http.StatusBadRequest, "client name required"
	case errors.Is(err, errPairingMessageTooLong):
		return http.StatusBadRequest, "PAKE message too long"
	case errors.Is(err, errTooManyClients):
		return http.StatusForbidden, "maximum paired clients reached"
	case errors.Is(err, errPairingHMACMismatch):
		return http.StatusUnauthorized, "wrong PIN"
	default:
		log.Error().Err(err).Msg("pairing internal error")
		return http.StatusInternalServerError, "internal error"
	}
}

// pairingErrorResponse writes a JSON error to the HTTP response.
func pairingErrorResponse(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}
