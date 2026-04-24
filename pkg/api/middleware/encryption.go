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

package middleware

import (
	"context"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// EncryptionProtoVersion is the current encryption protocol version. The
// value is included in the first WebSocket frame and rejected by the server
// if the client requests something unsupported.
const EncryptionProtoVersion = 1

// Limits and timing for the encryption session manager.
const (
	maxSaltsPerClient           = 200    // per-client salt dedup cap (prevents memory exhaustion)
	maxSaltClients              = 10_000 // global cap on tracked clients in saltSeen
	maxFailedFrameEntries       = 10_000 // global cap on failSeen entries (LRU on overflow)
	saltWindowTTL               = 10 * time.Minute
	failedFrameThreshold        = 10               // failures before blocking
	failedFrameBlockDuration    = 30 * time.Second // initial block; exponential backoff follows
	failedFrameMaxBlockDuration = 30 * time.Minute // backoff cap

	// encryptionCleanupInterval is how often the cleanup goroutine runs.
	encryptionCleanupInterval = 1 * time.Minute
)

// Encryption errors. These are returned by EncryptionGateway methods so callers
// can map them to appropriate WebSocket close codes or plaintext errors.
var (
	ErrUnsupportedVersion    = errors.New("unsupported encryption version")
	ErrInvalidFrame          = errors.New("invalid encrypted frame")
	ErrInvalidSaltLength     = errors.New("session salt must be 16 bytes")
	ErrDuplicateSalt         = errors.New("duplicate session salt for client")
	ErrUnknownAuthToken      = errors.New("unknown auth token")
	ErrConnectionBlocked     = errors.New("connection blocked due to repeated failures")
	ErrInvalidPairingKey     = errors.New("stored pairing key is invalid")
	ErrSessionNotEstablished = errors.New("encryption session not established")
)

// EncryptedFirstFrame is the JSON payload sent on the first WebSocket frame
// to establish an encrypted session.
type EncryptedFirstFrame struct {
	Ciphertext  string `json:"e"`
	AuthToken   string `json:"t"`
	SessionSalt string `json:"s"`
	Version     int    `json:"v"`
}

// EncryptedFrame is the JSON payload sent on subsequent frames after the
// session has been established.
type EncryptedFrame struct {
	Ciphertext string `json:"e"`
}

// frameProbe is a permissive parse used to detect whether an incoming frame
// looks like an encrypted first frame. This is a separate type from
// EncryptedFirstFrame so we can detect malformed frames without rejecting
// the connection — they're just treated as plaintext.
type frameProbe struct {
	E string `json:"e"`
	T string `json:"t"`
	S string `json:"s"`
	V int    `json:"v"`
}

// IsEncryptedFirstFrame reports whether the given JSON bytes look like an
// encrypted first frame (has v + e + t + s populated). False if the bytes
// are not valid JSON or any required field is missing.
func IsEncryptedFirstFrame(data []byte) bool {
	var probe frameProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.V > 0 && probe.E != "" && probe.T != "" && probe.S != ""
}

// ClientSession holds per-WebSocket encryption state. All AEAD calls
// MUST happen inside the mutex (golang/go#25882, golang-fips/go#187).
type ClientSession struct {
	client      *database.Client
	c2sGCM      cipher.AEAD
	s2cGCM      cipher.AEAD
	c2sNonce    []byte
	s2cNonce    []byte
	aad         []byte
	recvCounter uint64
	sendCounter uint64
	mu          syncutil.Mutex
}

// AuthToken returns the auth token (immutable after construction, no lock needed).
func (cs *ClientSession) AuthToken() string {
	return cs.client.AuthToken
}

// DecryptIncoming decrypts with the next expected counter. Caller should
// close the WebSocket on error.
func (cs *ClientSession) DecryptIncoming(ciphertext []byte) ([]byte, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	plaintext, err := crypto.Decrypt(cs.c2sGCM, cs.c2sNonce, cs.recvCounter, ciphertext, cs.aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt incoming: %w", err)
	}
	cs.recvCounter++
	return plaintext, nil
}

// EncryptOutgoing encrypts plaintext and advances the send counter.
func (cs *ClientSession) EncryptOutgoing(plaintext []byte) ([]byte, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ciphertext, err := crypto.Encrypt(cs.s2cGCM, cs.s2cNonce, cs.sendCounter, plaintext, cs.aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt outgoing: %w", err)
	}
	cs.sendCounter++
	return ciphertext, nil
}

// EncryptOutgoingFrame encrypts and wraps in the {"e":"..."} JSON envelope.
// Prefer SendEncryptedFrame when writing directly to a WebSocket (holds
// the lock across encrypt + write to preserve counter order).
func (cs *ClientSession) EncryptOutgoingFrame(plaintext []byte) ([]byte, error) {
	ciphertext, err := cs.EncryptOutgoing(plaintext)
	if err != nil {
		return nil, err
	}
	wrapped, err := json.Marshal(EncryptedFrame{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal outgoing frame: %w", err)
	}
	return wrapped, nil
}

// SendEncryptedFrame encrypts, wraps, and writes under the mutex so
// concurrent writers cannot reorder counters. Counter advances only on
// success — caller should close the connection on error.
func (cs *ClientSession) SendEncryptedFrame(plaintext []byte, writeFn func([]byte) error) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ciphertext, err := crypto.Encrypt(cs.s2cGCM, cs.s2cNonce, cs.sendCounter, plaintext, cs.aad)
	if err != nil {
		return fmt.Errorf("encrypt outgoing: %w", err)
	}
	wrapped, err := json.Marshal(EncryptedFrame{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return fmt.Errorf("marshal outgoing frame: %w", err)
	}
	if err := writeFn(wrapped); err != nil {
		return fmt.Errorf("write encrypted frame: %w", err)
	}
	cs.sendCounter++
	return nil
}

// failedFrameTracker counts consecutive failed first-frame decryptions for a
// (authToken, sourceIP) pair and tracks the current block deadline.
type failedFrameTracker struct {
	blockedUntil  time.Time
	lastFailureAt time.Time
	failures      int
	blockCount    int // exponential backoff multiplier
}

// EncryptionGateway owns salt dedup and failed-frame rate limiting across
// all sessions. Sessions are stored on the melody session after establishment.
type EncryptionGateway struct {
	db                       database.UserDBI
	clock                    clockwork.Clock
	saltSeen                 map[string]map[string]time.Time
	failSeen                 map[string]*failedFrameTracker
	maxSaltsPerClient        int
	maxSaltClients           int
	maxFailEntries           int
	saltWindowTTL            time.Duration
	failedFrameThreshold     int
	failedFrameBlockDuration time.Duration
	failedFrameMaxBlock      time.Duration
	cleanupInterval          time.Duration
	saltMu                   syncutil.Mutex
	failMu                   syncutil.Mutex
}

// EncryptionGatewayOption configures an EncryptionGateway at construction time.
type EncryptionGatewayOption func(*EncryptionGateway)

// WithMaxSaltsPerClient overrides the per-client salt history cap.
func WithMaxSaltsPerClient(n int) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.maxSaltsPerClient = n }
}

// WithSaltWindowTTL overrides the salt history TTL.
func WithSaltWindowTTL(d time.Duration) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.saltWindowTTL = d }
}

// WithFailedFrameThreshold overrides the failed-frame block threshold.
func WithFailedFrameThreshold(n int) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.failedFrameThreshold = n }
}

// WithFailedFrameBlockDuration overrides the initial block duration.
func WithFailedFrameBlockDuration(d time.Duration) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.failedFrameBlockDuration = d }
}

// WithFailedFrameMaxBlock overrides the cap on exponential backoff.
func WithFailedFrameMaxBlock(d time.Duration) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.failedFrameMaxBlock = d }
}

// WithSessionCleanupInterval overrides the cleanup goroutine tick interval.
func WithSessionCleanupInterval(d time.Duration) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.cleanupInterval = d }
}

// WithMaxSaltClients overrides the global cap on distinct clients tracked
// in the salt deduplication map.
func WithMaxSaltClients(n int) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.maxSaltClients = n }
}

// WithMaxFailEntries overrides the global cap on entries in the
// failed-frame rate limiter map.
func WithMaxFailEntries(n int) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.maxFailEntries = n }
}

// WithClock overrides the clock used for timestamps (useful for testing).
func WithClock(c clockwork.Clock) EncryptionGatewayOption {
	return func(m *EncryptionGateway) { m.clock = c }
}

// NewEncryptionGateway constructs a EncryptionGateway with default limits.
func NewEncryptionGateway(db database.UserDBI, opts ...EncryptionGatewayOption) *EncryptionGateway {
	m := &EncryptionGateway{
		db:                       db,
		saltSeen:                 make(map[string]map[string]time.Time),
		failSeen:                 make(map[string]*failedFrameTracker),
		maxSaltsPerClient:        maxSaltsPerClient,
		maxSaltClients:           maxSaltClients,
		maxFailEntries:           maxFailedFrameEntries,
		saltWindowTTL:            saltWindowTTL,
		failedFrameThreshold:     failedFrameThreshold,
		failedFrameBlockDuration: failedFrameBlockDuration,
		failedFrameMaxBlock:      failedFrameMaxBlockDuration,
		cleanupInterval:          encryptionCleanupInterval,
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.clock == nil {
		m.clock = clockwork.NewRealClock()
	}
	return m
}

// StartCleanup begins the background cleanup goroutine that evicts expired
// salt and rate-limit entries. The goroutine exits when ctx is canceled.
// Safe to call multiple times only with distinct contexts; callers should
// call this once during server startup.
func (m *EncryptionGateway) StartCleanup(ctx context.Context) {
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

// EstablishSession validates, decrypts, and returns a ClientSession for the
// first encrypted frame. Failures increment the (authToken, sourceIP) rate
// limiter; callers should close the WebSocket on error.
//
// Non-constant-time: auth token validity is distinguishable by timing, but
// tokens are already plaintext on the wire and grant no capability without
// the 32-byte pairing key. If a future credential is NOT public on the
// wire, these branches MUST be refactored to constant-time.
func (m *EncryptionGateway) EstablishSession(
	frame EncryptedFirstFrame,
	sourceIP string,
) (*ClientSession, []byte, error) {
	if frame.Version != EncryptionProtoVersion {
		return nil, nil, ErrUnsupportedVersion
	}
	if frame.AuthToken == "" {
		return nil, nil, ErrUnknownAuthToken
	}

	// Rate limit by (authToken, IP) BEFORE doing any expensive work.
	if blocked := m.isBlocked(frame.AuthToken, sourceIP); blocked {
		return nil, nil, ErrConnectionBlocked
	}

	// Lookup before failure recording — unknown tokens skip recordFailure
	// to prevent failSeen map exhaustion from fabricated tokens.
	c, err := m.db.GetClientByToken(frame.AuthToken)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnknownAuthToken, err)
	}
	if c == nil {
		return nil, nil, ErrUnknownAuthToken
	}
	if len(c.PairingKey) != crypto.PairingKeySize {
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, ErrInvalidPairingKey
	}

	// Validate salt (recordFailure safe — token exists).
	salt, err := base64.StdEncoding.DecodeString(frame.SessionSalt)
	if err != nil {
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidFrame, err)
	}
	if len(salt) != crypto.SessionSaltSize {
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, ErrInvalidSaltLength
	}

	// Reserve salt (rolled back on failure to defend against replay attacks).
	if dupErr := m.checkAndRecordSalt(frame.AuthToken, salt); dupErr != nil {
		// Salt dedup failures aren't rate-limited (CSPRNG bugs, not attacks).
		return nil, nil, dupErr
	}

	keys, err := crypto.DeriveSessionKeys(c.PairingKey, salt)
	if err != nil {
		m.releaseSalt(frame.AuthToken, salt)
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, fmt.Errorf("derive session keys: %w", err)
	}

	c2sGCM, err := crypto.NewAEAD(keys.C2SKey)
	if err != nil {
		m.releaseSalt(frame.AuthToken, salt)
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, fmt.Errorf("new c2s aead: %w", err)
	}
	s2cGCM, err := crypto.NewAEAD(keys.S2CKey)
	if err != nil {
		m.releaseSalt(frame.AuthToken, salt)
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, fmt.Errorf("new s2c aead: %w", err)
	}

	cs := &ClientSession{
		client:   c,
		c2sGCM:   c2sGCM,
		s2cGCM:   s2cGCM,
		c2sNonce: keys.C2SNonce,
		s2cNonce: keys.S2CNonce,
		// AAD bound to DB-resolved token (resilient to future canonicalization).
		aad:         []byte(c.AuthToken + ":ws"),
		recvCounter: 0,
		sendCounter: 0,
	}

	// Decrypt first frame to validate keys.
	ciphertext, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	if err != nil {
		m.releaseSalt(frame.AuthToken, salt)
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalidFrame, err)
	}
	plaintext, err := cs.DecryptIncoming(ciphertext)
	if err != nil {
		m.releaseSalt(frame.AuthToken, salt)
		m.recordFailure(frame.AuthToken, sourceIP)
		return nil, nil, err
	}

	// Successful first frame — clear any prior failure state.
	m.clearFailures(frame.AuthToken, sourceIP)
	return cs, plaintext, nil
}

// DecryptSubsequent decrypts a subsequent frame (base64 decode + validation).
func (cs *ClientSession) DecryptSubsequent(frame EncryptedFrame) ([]byte, error) {
	if cs == nil {
		return nil, ErrSessionNotEstablished
	}
	ciphertext, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFrame, err)
	}
	return cs.DecryptIncoming(ciphertext)
}

// failKey returns the lookup key for the failed-frame tracker. The NUL byte
// separator ensures that no combination of auth token and source IP can
// collide with a different pair (auth tokens are valid UTF-8 from a UUID
// generator and IP strings never contain NUL).
func failKey(authToken, sourceIP string) string {
	return authToken + "\x00" + sourceIP
}

// isBlocked reports whether the given (authToken, sourceIP) is currently
// blocked by the failed-frame rate limiter.
func (m *EncryptionGateway) isBlocked(authToken, sourceIP string) bool {
	m.failMu.Lock()
	defer m.failMu.Unlock()

	t, ok := m.failSeen[failKey(authToken, sourceIP)]
	if !ok {
		return false
	}
	if m.clock.Now().Before(t.blockedUntil) {
		return true
	}
	return false
}

// recordFailure increments the counter and applies exponential backoff blocks.
// Enforces a global cap on failSeen with LRU eviction.
func (m *EncryptionGateway) recordFailure(authToken, sourceIP string) {
	m.failMu.Lock()
	defer m.failMu.Unlock()

	key := failKey(authToken, sourceIP)
	t, ok := m.failSeen[key]
	if !ok {
		if len(m.failSeen) >= m.maxFailEntries {
			m.evictOldestFailureLocked()
		}
		t = &failedFrameTracker{}
		m.failSeen[key] = t
	}
	t.failures++
	t.lastFailureAt = m.clock.Now()

	if t.failures >= m.failedFrameThreshold {
		// Apply backoff. blockCount starts at 0 → 1x duration.
		shift := t.blockCount
		if shift > 10 {
			shift = 10
		}
		dur := m.failedFrameBlockDuration << shift
		if dur > m.failedFrameMaxBlock {
			dur = m.failedFrameMaxBlock
		}
		t.blockedUntil = m.clock.Now().Add(dur)
		t.blockCount++
		t.failures = 0
		log.Warn().
			Str("auth_token", redactToken(authToken)).
			Str("ip", sourceIP).
			Dur("block_duration", dur).
			Msg("encryption: blocking after repeated first-frame failures")
	}
}

// clearFailures decrements the backoff multiplier on success (preserving
// attack history — deleting would let attackers reset escalation via
// interleaved legitimate connections). Evicted by cleanupExpired when stale.
func (m *EncryptionGateway) clearFailures(authToken, sourceIP string) {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	t, ok := m.failSeen[failKey(authToken, sourceIP)]
	if !ok {
		return
	}
	t.failures = 0
	if t.blockCount > 0 {
		t.blockCount--
	}
}

// releaseSalt rolls back a salt reservation on failure so legitimate
// clients can retry. Reserve-then-rollback ensures atomicity under races.
func (m *EncryptionGateway) releaseSalt(authToken string, salt []byte) {
	m.saltMu.Lock()
	defer m.saltMu.Unlock()

	t, ok := m.saltSeen[authToken]
	if !ok {
		return
	}
	delete(t, hex.EncodeToString(salt))
	if len(t) == 0 {
		delete(m.saltSeen, authToken)
	}
}

// evictOldestFailureLocked evicts the oldest failSeen entry. Caller must hold failMu.
func (m *EncryptionGateway) evictOldestFailureLocked() {
	var oldestKey string
	var oldestAt time.Time
	for k, v := range m.failSeen {
		if oldestKey == "" || v.lastFailureAt.Before(oldestAt) {
			oldestKey = k
			oldestAt = v.lastFailureAt
		}
	}
	if oldestKey != "" {
		delete(m.failSeen, oldestKey)
	}
}

// evictOldestSaltClientLocked evicts the least-recently-active salt tracker. Caller must hold saltMu.
func (m *EncryptionGateway) evictOldestSaltClientLocked() {
	var oldestKey string
	var oldestAt time.Time
	for token, tracker := range m.saltSeen {
		var newest time.Time
		for _, ts := range tracker {
			if ts.After(newest) {
				newest = ts
			}
		}
		if oldestKey == "" || newest.Before(oldestAt) {
			oldestKey = token
			oldestAt = newest
		}
	}
	if oldestKey != "" {
		delete(m.saltSeen, oldestKey)
	}
}

// checkAndRecordSalt records a salt or returns ErrDuplicateSalt. Global cap
// with LRU eviction bounds memory.
func (m *EncryptionGateway) checkAndRecordSalt(authToken string, salt []byte) error {
	m.saltMu.Lock()
	defer m.saltMu.Unlock()

	t, ok := m.saltSeen[authToken]
	if !ok {
		if len(m.saltSeen) >= m.maxSaltClients {
			m.evictOldestSaltClientLocked()
		}
		t = make(map[string]time.Time)
		m.saltSeen[authToken] = t
	}

	saltHex := hex.EncodeToString(salt)
	if _, dup := t[saltHex]; dup {
		return ErrDuplicateSalt
	}

	// Evict the oldest entry if we're at the cap. The cap is far above any
	// legitimate usage so this should rarely fire.
	if len(t) >= m.maxSaltsPerClient {
		var oldestKey string
		var oldestAt time.Time
		for k, v := range t {
			if oldestAt.IsZero() || v.Before(oldestAt) {
				oldestKey = k
				oldestAt = v
			}
		}
		delete(t, oldestKey)
	}

	t[saltHex] = m.clock.Now()
	return nil
}

// cleanupExpired evicts old salt entries and stale failure trackers.
func (m *EncryptionGateway) cleanupExpired() {
	now := m.clock.Now()

	m.saltMu.Lock()
	for token, tracker := range m.saltSeen {
		for k, t := range tracker {
			if now.Sub(t) > m.saltWindowTTL {
				delete(tracker, k)
			}
		}
		if len(tracker) == 0 {
			delete(m.saltSeen, token)
		}
	}
	m.saltMu.Unlock()

	m.failMu.Lock()
	for k, t := range m.failSeen {
		// Drop entries with no recent activity AND no active block.
		if now.After(t.blockedUntil) && now.Sub(t.lastFailureAt) > m.saltWindowTTL {
			delete(m.failSeen, k)
		}
	}
	m.failMu.Unlock()
}

// redactToken truncates to 8 chars for log correlation.
func redactToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:8] + "…"
}
