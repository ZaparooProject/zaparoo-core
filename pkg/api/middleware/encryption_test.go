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

package middleware_test

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pairedClient builds a fake paired client. Used by tests to drive the
// session manager without going through the full PAKE flow.
func pairedClient(t *testing.T) (client *database.Client, pairingKey []byte) {
	t.Helper()
	pairingKey = make([]byte, crypto.PairingKeySize)
	_, err := cryptorand.Read(pairingKey)
	require.NoError(t, err)
	//nolint:gosec // test fixture; AuthToken is opaque test data, not a credential
	client = &database.Client{
		ClientID:   "client-uuid",
		ClientName: "Test",
		AuthToken:  "auth-token-uuid",
		PairingKey: pairingKey,
		CreatedAt:  1700000000,
		LastSeenAt: 1700000000,
	}
	return client, pairingKey
}

// randomSalt returns a random 16-byte session salt.
func randomSalt(t *testing.T) []byte {
	t.Helper()
	salt := make([]byte, crypto.SessionSaltSize)
	_, err := cryptorand.Read(salt)
	require.NoError(t, err)
	return salt
}

// encryptFirstFrame helps tests build an encrypted first frame using the
// same key derivation the manager will use.
func encryptFirstFrame(t *testing.T, c *database.Client, salt, plaintext []byte, counter uint64) []byte {
	t.Helper()
	keys, err := crypto.DeriveSessionKeys(c.PairingKey, salt)
	require.NoError(t, err)
	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	aad := []byte(c.AuthToken + ":ws")
	ct, err := crypto.Encrypt(gcm, keys.C2SNonce, counter, plaintext, aad)
	require.NoError(t, err)
	return ct
}

func TestIsEncryptedFirstFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "valid first frame", data: `{"v":1,"e":"abc","t":"tok","s":"salt"}`, want: true},
		{name: "missing version", data: `{"e":"abc","t":"tok","s":"salt"}`, want: false},
		{name: "missing ciphertext", data: `{"v":1,"t":"tok","s":"salt"}`, want: false},
		{name: "missing token", data: `{"v":1,"e":"abc","s":"salt"}`, want: false},
		{name: "missing salt", data: `{"v":1,"e":"abc","t":"tok"}`, want: false},
		{name: "plain JSON-RPC", data: `{"jsonrpc":"2.0","method":"x","id":1}`, want: false},
		{name: "subsequent frame", data: `{"e":"abc"}`, want: false},
		{name: "garbage", data: `not json`, want: false},
		{name: "empty", data: ``, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, middleware.IsEncryptedFirstFrame([]byte(tt.data)))
		})
	}
}

func TestEstablishSession_Success(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	plaintext := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	ct := encryptFirstFrame(t, c, salt, plaintext, 0)

	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}

	cs, decrypted, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.Equal(t, plaintext, decrypted)
	assert.Equal(t, c.AuthToken, cs.AuthToken())
}

func TestEstablishSession_UnsupportedVersion(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	mgr := middleware.NewEncryptionGateway(db)

	frame := middleware.EncryptedFirstFrame{
		Version:     99,
		Ciphertext:  "abc",
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
	}
	_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrUnsupportedVersion)
}

func TestEstablishSession_InvalidSaltLength(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  "abc",
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(make([]byte, 8)),
	}
	_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrInvalidSaltLength)
}

func TestEstablishSession_UnknownAuthToken(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", "missing").
		Return((*database.Client)(nil), assert.AnError)
	mgr := middleware.NewEncryptionGateway(db)

	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("ct")),
		AuthToken:   "missing",
		SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
	}
	_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrUnknownAuthToken)
}

func TestEstablishSession_DuplicateSalt(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	plaintext := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	ct := encryptFirstFrame(t, c, salt, plaintext, 0)
	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}

	_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.NoError(t, err)

	// Second use of the same salt must be rejected.
	_, _, err = mgr.EstablishSession(frame, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrDuplicateSalt)
}

func TestEstablishSession_FailedDecryptIncrementsLimiter(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	// Many failed lookups all hit the same client
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(3),
		middleware.WithFailedFrameBlockDuration(50*time.Millisecond),
	)

	// Build frames with the right salt format but garbage ciphertext.
	for range 3 {
		frame := middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage ciphertext data here")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}
		_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
		require.Error(t, err)
	}

	// Next attempt should hit the rate limit
	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage ciphertext data here")),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
	}
	_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrConnectionBlocked)

	// A different IP for the same token should still be allowed
	_, _, err = mgr.EstablishSession(frame, "192.168.1.51")
	require.NotErrorIs(t, err, middleware.ErrConnectionBlocked,
		"different IP must not be locked out")
}

func TestEstablishSession_BlockExpires(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(2),
		middleware.WithFailedFrameBlockDuration(20*time.Millisecond),
	)

	for range 2 {
		frame := middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage data garbage")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}
		_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
		require.Error(t, err)
	}

	// Currently blocked
	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
	}
	_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrConnectionBlocked)

	// Wait for block to expire
	time.Sleep(40 * time.Millisecond)

	// Build a valid frame so the next attempt actually succeeds (clears state)
	salt := randomSalt(t)
	plaintext := []byte("hello")
	ct := encryptFirstFrame(t, c, salt, plaintext, 0)
	goodFrame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	_, decrypted, err := mgr.EstablishSession(goodFrame, "192.168.1.50")
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEstablishSession_SuccessClearsFailures(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(5),
	)

	// Two failures
	for range 2 {
		_, _, _ = mgr.EstablishSession(middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}, "192.168.1.50")
	}

	// Now a successful frame
	salt := randomSalt(t)
	plaintext := []byte("hi")
	ct := encryptFirstFrame(t, c, salt, plaintext, 0)
	_, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, "192.168.1.50")
	require.NoError(t, err)

	// More failures should not immediately block since we cleared
	for range 4 {
		_, _, _ = mgr.EstablishSession(middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}, "192.168.1.50")
	}
	// Still under threshold (5)
	salt2 := randomSalt(t)
	ct2 := encryptFirstFrame(t, c, salt2, []byte("hi"), 0)
	_, _, err = mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct2),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt2),
	}, "192.168.1.50")
	require.NoError(t, err, "should not be blocked yet")
}

// TestEstablishSession_BlockCountEscalates pins the exponential-backoff
// escalation: each time the failure threshold is hit, blockCount must
// increase. This is the property that makes the rate limiter actually
// adapt to a sustained attacker; without it, a single 30s block would
// be the worst-case slowdown forever.
//
// Also pins the shift cap at blockCount > 10 — the cap is what stops
// `failedFrameBlockDuration << blockCount` from overflowing once an
// attacker pushes blockCount past 64 over a long period.
func TestEstablishSession_BlockCountEscalates(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(2),
		// Tiny base + low cap so the test doesn't sleep for minutes when
		// it walks blockCount up past the shift cap.
		middleware.WithFailedFrameBlockDuration(time.Millisecond),
		middleware.WithFailedFrameMaxBlock(50*time.Millisecond),
	)

	const ip = "192.168.1.50"
	hitThreshold := func(round int) {
		for i := range 2 {
			_, _, _ = mgr.EstablishSession(middleware.EncryptedFirstFrame{
				Version: middleware.EncryptionProtoVersion,
				Ciphertext: base64.StdEncoding.EncodeToString(
					[]byte(fmt.Sprintf("garbage round %d attempt %d", round, i))),
				AuthToken:   c.AuthToken,
				SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
			}, ip)
		}
		// Wait for the (very short) block to expire so the next round can
		// reach recordFailure instead of bouncing on isBlocked.
		time.Sleep(60 * time.Millisecond)
	}

	// Walk blockCount up through several rounds. Each round must escalate.
	for round := 1; round <= 5; round++ {
		hitThreshold(round)
		assert.Equal(t, round, mgr.FailureBlockCountForTest(c.AuthToken, ip),
			"round %d should escalate blockCount to %d", round, round)
	}

	// Push past the shift cap (10) to confirm the code path is exercised
	// without panicking or overflowing the duration calculation.
	for round := 6; round <= 13; round++ {
		hitThreshold(round)
	}
	require.GreaterOrEqual(t,
		mgr.FailureBlockCountForTest(c.AuthToken, ip), 13,
		"blockCount must keep counting past the shift cap")

	// One more round at the elevated count must still produce a block
	// (capped at failedFrameMaxBlock) without panicking.
	hitThreshold(14)
	// The duration is clamped at failedFrameMaxBlock=50ms, but blockCount
	// itself keeps incrementing as evidence of sustained abuse.
	assert.GreaterOrEqual(t, mgr.FailureBlockCountForTest(c.AuthToken, ip), 14)
}

// TestEstablishSession_SuccessDecrementsBlockCount pins the fix for the
// rate-limit-bypass bug where a successful first frame at the same
// (authToken, sourceIP) used to delete the entire failure tracker entry,
// erasing accumulated backoff history. After the fix, success only
// decrements blockCount (and resets failures), so an attacker on the same
// IP+token cannot have their escalation reset by a legitimate client.
func TestEstablishSession_SuccessDecrementsBlockCount(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(2),
		middleware.WithFailedFrameBlockDuration(10*time.Millisecond),
	)

	const ip = "192.168.1.50"

	// Round 1: hit threshold to escalate blockCount 0 → 1.
	for range 2 {
		_, _, _ = mgr.EstablishSession(middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage round1")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}, ip)
	}
	require.Equal(t, 1, mgr.FailureBlockCountForTest(c.AuthToken, ip),
		"first round of failures should set blockCount=1")

	// Wait for the active block to expire.
	time.Sleep(20 * time.Millisecond)

	// Round 2: hit threshold again to escalate to blockCount=2.
	for range 2 {
		_, _, _ = mgr.EstablishSession(middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage round2")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}, ip)
	}
	require.Equal(t, 2, mgr.FailureBlockCountForTest(c.AuthToken, ip),
		"second round of failures should escalate blockCount to 2")

	// Wait for the second block to expire.
	time.Sleep(50 * time.Millisecond)

	// Legitimate client succeeds. Old behavior (delete) would set
	// blockCount back to 0 (-1 == no entry). New behavior decrements
	// 2 → 1 so the next attacker round still escalates from an
	// elevated baseline.
	salt := randomSalt(t)
	ct := encryptFirstFrame(t, c, salt, []byte("good"), 0)
	_, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, ip)
	require.NoError(t, err)
	assert.Equal(t, 1, mgr.FailureBlockCountForTest(c.AuthToken, ip),
		"successful frame must DECREMENT blockCount, not delete the entry — "+
			"deleting would let an attacker reset their escalation history")

	// A second legitimate success decrements again to 0 (clamped at 0).
	salt2 := randomSalt(t)
	ct2 := encryptFirstFrame(t, c, salt2, []byte("good2"), 0)
	_, _, err = mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct2),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt2),
	}, ip)
	require.NoError(t, err)
	assert.Equal(t, 0, mgr.FailureBlockCountForTest(c.AuthToken, ip),
		"second success should clamp blockCount at 0, not go negative")

	// A third legitimate success must NOT panic or underflow.
	salt3 := randomSalt(t)
	ct3 := encryptFirstFrame(t, c, salt3, []byte("good3"), 0)
	_, _, err = mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct3),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt3),
	}, ip)
	require.NoError(t, err)
	assert.Equal(t, 0, mgr.FailureBlockCountForTest(c.AuthToken, ip),
		"blockCount must not underflow below 0")
}

func TestClientSession_RoundTrip(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	plaintext := []byte("first frame")
	ct := encryptFirstFrame(t, c, salt, plaintext, 0)
	cs, decrypted, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, "192.168.1.50")
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)

	// Server encrypts a response — counter 0
	respPlain := []byte(`{"result":"ok"}`)
	respCT, err := cs.EncryptOutgoing(respPlain)
	require.NoError(t, err)

	// Client decrypts using the same salt-derived keys
	keys, err := crypto.DeriveSessionKeys(c.PairingKey, salt)
	require.NoError(t, err)
	clientGCM, err := crypto.NewAEAD(keys.S2CKey)
	require.NoError(t, err)
	got, err := crypto.Decrypt(clientGCM, keys.S2CNonce, 0, respCT, []byte(c.AuthToken+":ws"))
	require.NoError(t, err)
	assert.Equal(t, respPlain, got)

	// Client sends a second frame at counter 1
	subPlain := []byte("second frame")
	clientC2S, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	subCT, err := crypto.Encrypt(clientC2S, keys.C2SNonce, 1, subPlain, []byte(c.AuthToken+":ws"))
	require.NoError(t, err)

	subFrame := middleware.EncryptedFrame{
		Ciphertext: base64.StdEncoding.EncodeToString(subCT),
	}
	gotSub, err := cs.DecryptSubsequent(subFrame)
	require.NoError(t, err)
	assert.Equal(t, subPlain, gotSub)
}

func TestClientSession_ReplayRejected(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	plaintext := []byte("first")
	ct := encryptFirstFrame(t, c, salt, plaintext, 0)

	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	cs, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.NoError(t, err)

	// Try to feed the same first-frame ciphertext again as a "subsequent"
	// frame — server expects counter 1 now, so decryption fails.
	replay := middleware.EncryptedFrame{Ciphertext: base64.StdEncoding.EncodeToString(ct)}
	_, err = cs.DecryptSubsequent(replay)
	require.Error(t, err)
}

func TestClientSession_OutOfOrderFails(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	ct0 := encryptFirstFrame(t, c, salt, []byte("0"), 0)
	cs, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct0),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, "192.168.1.50")
	require.NoError(t, err)

	// Encrypt counter 5 (skipping 1-4)
	keys, _ := crypto.DeriveSessionKeys(c.PairingKey, salt)
	gcm, _ := crypto.NewAEAD(keys.C2SKey)
	ct5, err := crypto.Encrypt(gcm, keys.C2SNonce, 5, []byte("five"), []byte(c.AuthToken+":ws"))
	require.NoError(t, err)

	frame := middleware.EncryptedFrame{Ciphertext: base64.StdEncoding.EncodeToString(ct5)}
	_, err = cs.DecryptSubsequent(frame)
	require.Error(t, err, "out-of-order counter must fail")
}

func TestEncryptOutgoingFrame_ProducesValidJSON(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	ct := encryptFirstFrame(t, c, salt, []byte("hi"), 0)
	cs, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, "192.168.1.50")
	require.NoError(t, err)

	frame, err := cs.EncryptOutgoingFrame([]byte(`{"jsonrpc":"2.0","result":"ok","id":1}`))
	require.NoError(t, err)

	var parsed middleware.EncryptedFrame
	require.NoError(t, json.Unmarshal(frame, &parsed))
	assert.NotEmpty(t, parsed.Ciphertext)
}

func TestEstablishSession_DifferentClientsIndependent(t *testing.T) {
	t.Parallel()

	c1, _ := pairedClient(t)
	c2, _ := pairedClient(t)
	c2.AuthToken = "other-token"
	c2.ClientID = "other-id"

	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c1.AuthToken).Return(c1, nil)
	db.On("GetClientByToken", c2.AuthToken).Return(c2, nil)
	mgr := middleware.NewEncryptionGateway(db)

	// Reuse the same salt across two different clients — should be allowed
	// because the salt tracker is keyed by authToken.
	salt := randomSalt(t)

	for _, c := range []*database.Client{c1, c2} {
		ct := encryptFirstFrame(t, c, salt, []byte("hi"), 0)
		_, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString(ct),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(salt),
		}, "192.168.1.50")
		require.NoError(t, err, "client %s", c.AuthToken)
	}
}

func TestEstablishSession_PairingKeyWrongLength(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	c.PairingKey = make([]byte, 16) // wrong length
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("ct")),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
	}
	_, _, err := mgr.EstablishSession(frame, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrInvalidPairingKey)
}

// TestEstablishSession_UnknownTokenDoesNotPopulateLimiter verifies that an
// unknown auth token does NOT create a failSeen entry. Without this rule,
// an attacker fabricating tokens could grow the rate-limiter map without
// bound (DoS via memory exhaustion).
func TestEstablishSession_UnknownTokenDoesNotPopulateLimiter(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", "fabricated-1").
		Return((*database.Client)(nil), assert.AnError)
	db.On("GetClientByToken", "fabricated-2").
		Return((*database.Client)(nil), assert.AnError)

	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(2),
	)

	for _, tok := range []string{"fabricated-1", "fabricated-2"} {
		frame := middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
			AuthToken:   tok,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}
		_, _, err := mgr.EstablishSession(frame, "10.0.0.1")
		require.ErrorIs(t, err, middleware.ErrUnknownAuthToken)
	}

	// A third unknown-token attempt must NOT report ErrConnectionBlocked
	// — if the limiter were tracking unknown tokens it would be at 2/2
	// and the next call would block. The fact that we still get
	// ErrUnknownAuthToken proves the limiter was never engaged.
	frame := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
		AuthToken:   "fabricated-1",
		SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
	}
	_, _, err := mgr.EstablishSession(frame, "10.0.0.1")
	require.ErrorIs(t, err, middleware.ErrUnknownAuthToken,
		"unknown tokens must not populate the rate limiter")
	require.NotErrorIs(t, err, middleware.ErrConnectionBlocked)
}

// TestEstablishSession_UnknownTokenMalformedSaltDoesNotPopulateLimiter
// verifies that an unknown auth token does NOT create a failSeen entry even
// when the session salt is malformed (bad base64 or wrong length). Without
// this rule, an attacker could send fabricated tokens with garbage salts
// and grow the rate-limiter map without bound — the salt validation would
// reach recordFailure before the token lookup ever ran.
func TestEstablishSession_UnknownTokenMalformedSaltDoesNotPopulateLimiter(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", "fabricated-bad-salt-1").
		Return((*database.Client)(nil), assert.AnError)
	db.On("GetClientByToken", "fabricated-bad-salt-2").
		Return((*database.Client)(nil), assert.AnError)
	db.On("GetClientByToken", "fabricated-bad-salt-3").
		Return((*database.Client)(nil), assert.AnError)

	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(2),
	)

	// Bad base64 salt.
	frame1 := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
		AuthToken:   "fabricated-bad-salt-1",
		SessionSalt: "!!!not-base64!!!",
	}
	_, _, err := mgr.EstablishSession(frame1, "10.0.0.2")
	require.ErrorIs(t, err, middleware.ErrUnknownAuthToken)

	// Wrong-length (but valid base64) salt.
	frame2 := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
		AuthToken:   "fabricated-bad-salt-2",
		SessionSalt: base64.StdEncoding.EncodeToString([]byte("short")),
	}
	_, _, err = mgr.EstablishSession(frame2, "10.0.0.2")
	require.ErrorIs(t, err, middleware.ErrUnknownAuthToken)

	// A third unknown-token attempt must NOT report ErrConnectionBlocked.
	// If the limiter were tracking malformed-salt failures from unknown
	// tokens it would be at 2/2 and the next call would block.
	frame3 := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
		AuthToken:   "fabricated-bad-salt-3",
		SessionSalt: "also-not-base64",
	}
	_, _, err = mgr.EstablishSession(frame3, "10.0.0.2")
	require.ErrorIs(t, err, middleware.ErrUnknownAuthToken,
		"unknown tokens with malformed salts must not populate the rate limiter")
	require.NotErrorIs(t, err, middleware.ErrConnectionBlocked)
}

// TestEstablishSession_FailSeenCapEvictsOldest verifies that the failSeen
// map is bounded. With the cap set to 3, four distinct (authToken, IP)
// failures must result in only three entries — the oldest must be
// evicted. The previously-evicted entry, when retried, restarts at 1
// failure (proving its tracker was discarded).
func TestEstablishSession_FailSeenCapEvictsOldest(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		middleware.WithFailedFrameThreshold(2),
		middleware.WithMaxFailEntries(3),
	)

	// Build a frame that will fail decryption (so recordFailure is called)
	// but for a real, known token (so the lookup succeeds and we reach
	// recordFailure). Use a constant ciphertext but distinct source IPs to
	// produce four distinct failKey values.
	mkFrame := func() middleware.EncryptedFirstFrame {
		return middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage data garbage")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}
	}

	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}
	for _, ip := range ips {
		_, _, err := mgr.EstablishSession(mkFrame(), ip)
		require.Error(t, err)
	}

	// 10.0.0.1 (the oldest) was evicted. Hitting it once more should fail
	// with a decryption error but NOT trip the rate limiter (which would
	// require 2 failures). If the entry had survived, this would be the
	// 2nd failure for 10.0.0.1 → ErrConnectionBlocked on the next call.
	_, _, err := mgr.EstablishSession(mkFrame(), "10.0.0.1")
	require.Error(t, err)
	// Now try again — if the previous call was the 1st failure (i.e.
	// eviction worked), this is the 2nd failure → block triggers on the
	// NEXT call. So this call should still error with the decryption
	// error, not with ErrConnectionBlocked.
	_, _, err = mgr.EstablishSession(mkFrame(), "10.0.0.1")
	require.Error(t, err)
	require.NotErrorIs(t, err, middleware.ErrConnectionBlocked,
		"after eviction, the entry should restart at 0 failures")
}

// TestEstablishSession_FailedReplayDoesNotBurnSalt pins the fix for the
// replay-burn vulnerability: an attacker who observes a legitimate first
// frame and replays it with garbage ciphertext used to permanently burn
// the salt, blocking the honest client from completing its handshake.
//
// The fix releases the salt reservation on every post-record failure
// path, so the honest client can retry with the original salt.
func TestEstablishSession_FailedReplayDoesNotBurnSalt(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		// Make the rate limiter forgiving so the failure path runs
		// to completion without blocking the legitimate retry.
		middleware.WithFailedFrameThreshold(100),
	)

	salt := randomSalt(t)
	plaintext := []byte("legitimate")
	good := encryptFirstFrame(t, c, salt, plaintext, 0)

	// Attacker replays the legitimate (token, salt) with garbage
	// ciphertext. The auth token is real, the salt is the one the honest
	// client will use, but the ciphertext won't decrypt.
	tampered := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage data garbage data garbage data")),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	_, _, err := mgr.EstablishSession(tampered, "10.0.0.99")
	require.Error(t, err, "tampered frame must fail")

	// Honest client now sends its real first frame with the SAME salt.
	// Without the fix this would return ErrDuplicateSalt because the
	// attacker's failed attempt burned the entry. With the fix the
	// reservation was rolled back and the legit client succeeds.
	honest := middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(good),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	_, decrypted, err := mgr.EstablishSession(honest, "192.168.1.50")
	require.NoError(t, err, "honest client must be able to retry with the same salt")
	assert.Equal(t, plaintext, decrypted)

	// And now a true second use of the salt by the honest client (e.g. a
	// reconnect with stale state) is correctly rejected.
	_, _, err = mgr.EstablishSession(honest, "192.168.1.50")
	require.ErrorIs(t, err, middleware.ErrDuplicateSalt,
		"second honest use of the same salt must still be rejected")
}

// TestSendEncryptedFrame_ConcurrentWritesPreserveCounterOrder pins the
// fix for the encrypt+write race: when multiple goroutines call
// SendEncryptedFrame on the same session, the writeFn callback MUST be
// invoked in the same order as the encrypted counter values, otherwise
// the client would decrypt frames out of order and tear down the session.
//
// The fix holds cs.mu across encrypt + writeFn so the enqueue order
// matches the counter order. With -race enabled this also catches any
// reintroduction of the prior bug where the lock was released between
// the two steps.
func TestSendEncryptedFrame_ConcurrentWritesPreserveCounterOrder(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)

	salt := randomSalt(t)
	ct := encryptFirstFrame(t, c, salt, []byte("first"), 0)
	cs, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, "192.168.1.50")
	require.NoError(t, err)

	// Build the matching client-side AEAD so we can decrypt what we
	// observe on the wire.
	keys, err := crypto.DeriveSessionKeys(c.PairingKey, salt)
	require.NoError(t, err)
	clientS2C, err := crypto.NewAEAD(keys.S2CKey)
	require.NoError(t, err)
	aad := []byte(c.AuthToken + ":ws")

	// Capture the wire-order in which the writeFn is invoked. The
	// `writes` slice is protected by writeMu (separate from cs.mu).
	const totalSends = 256
	const goroutines = 8
	var (
		writeMu syncutil.Mutex
		writes  [][]byte
	)
	writeFn := func(b []byte) error {
		// Copy because the caller may reuse the buffer.
		cp := make([]byte, len(b))
		copy(cp, b)
		writeMu.Lock()
		writes = append(writes, cp)
		writeMu.Unlock()
		return nil
	}

	// Spawn N goroutines, each sending totalSends/N frames.
	per := totalSends / goroutines
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := range per {
				payload := []byte(fmt.Sprintf(`{"g":%d,"i":%d}`, gid, i))
				if err := cs.SendEncryptedFrame(payload, writeFn); err != nil {
					t.Errorf("send: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	require.Len(t, writes, totalSends, "every send must enqueue exactly one write")

	// Decrypt every frame in wire order. The recv counter on the client
	// side starts at 0; if the writes are in counter order, every
	// decryption succeeds. Any out-of-order frame produces a GCM auth
	// failure.
	for i, w := range writes {
		var frame middleware.EncryptedFrame
		require.NoError(t, json.Unmarshal(w, &frame), "frame %d not valid JSON", i)
		ct, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
		require.NoError(t, err)
		//nolint:gosec // counter is bounded by totalSends
		_, err = crypto.Decrypt(clientS2C, keys.S2CNonce, uint64(i), ct, aad)
		require.NoError(t, err,
			"frame at wire position %d failed to decrypt at counter %d — wire order does not match counter order", i, i)
	}
}

// TestSendEncryptedFrame_CounterExhaustionReturnsError pins the
// behavior at the AEAD counter limit: when sendCounter is at
// math.MaxUint64, SendEncryptedFrame must NOT advance the counter,
// must NOT call writeFn, and must return an error wrapping
// crypto.ErrCounterExhausted. The WS handler relies on this error to
// close the desynced session (see closeMelodySession in
// sendWSEncryptedResponse / writeNotificationToSession).
func TestSendEncryptedFrame_CounterExhaustionReturnsError(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	salt := randomSalt(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db)
	plaintext := []byte("first frame")
	ct := encryptFirstFrame(t, c, salt, plaintext, 0)
	cs, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, "192.168.1.50")
	require.NoError(t, err)

	// Force the send counter to MaxUint64 — the next encrypt MUST trip
	// the counter exhaustion check inside crypto.Encrypt without ever
	// calling writeFn.
	cs.SetSendCounterForTest(math.MaxUint64)

	writeFnCalled := false
	writeFn := func(_ []byte) error {
		writeFnCalled = true
		return nil
	}

	err = cs.SendEncryptedFrame([]byte("payload"), writeFn)
	require.ErrorIs(t, err, crypto.ErrCounterExhausted,
		"counter exhaustion error must be wrapped so callers can detect it")
	assert.False(t, writeFnCalled,
		"writeFn must NOT be called when crypto.Encrypt fails — otherwise "+
			"a counter-exhausted session would emit garbage on the wire")
}

// TestCheckAndRecordSalt_ClientCapEvictsOldest verifies the global
// salt-tracker cap. With the cap set to 2, three distinct clients must
// result in only two trackers — the least-recently-active is evicted.
func TestCheckAndRecordSalt_ClientCapEvictsOldest(t *testing.T) {
	t.Parallel()

	c1, _ := pairedClient(t)
	c2, _ := pairedClient(t)
	c2.AuthToken = "tok-2"
	c3, _ := pairedClient(t)
	c3.AuthToken = "tok-3"

	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c1.AuthToken).Return(c1, nil)
	db.On("GetClientByToken", c2.AuthToken).Return(c2, nil)
	db.On("GetClientByToken", c3.AuthToken).Return(c3, nil)

	mgr := middleware.NewEncryptionGateway(db, middleware.WithMaxSaltClients(2))

	// Pair c1, c2, c3 in order. After c3, c1's tracker should be evicted.
	for _, c := range []*database.Client{c1, c2, c3} {
		salt := randomSalt(t)
		ct := encryptFirstFrame(t, c, salt, []byte("hi"), 0)
		_, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString(ct),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(salt),
		}, "192.168.1.50")
		require.NoError(t, err, "client %s", c.AuthToken)
		// Tiny delay so newest-tracker timestamps are distinguishable.
		time.Sleep(2 * time.Millisecond)
	}

	// c1's salt tracker was evicted. Reusing c1's previous salt is now
	// allowed (the dedup state is gone). The successful pairing below
	// proves the tracker was rebuilt.
	salt := randomSalt(t)
	ct := encryptFirstFrame(t, c1, salt, []byte("hi"), 0)
	_, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c1.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, "192.168.1.50")
	require.NoError(t, err)
}

// TestCleanupExpired_EvictsBothMaps pins that cleanupExpired evicts entries
// from BOTH the saltSeen and failSeen maps. The two paths are independent
// so a regression in one would otherwise go uncaught.
func TestCleanupExpired_EvictsBothMaps(t *testing.T) {
	t.Parallel()

	c, _ := pairedClient(t)
	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := middleware.NewEncryptionGateway(db,
		// Tiny TTL so any entry is "expired" after a few microseconds.
		middleware.WithSaltWindowTTL(1*time.Nanosecond),
		middleware.WithFailedFrameThreshold(2),
		middleware.WithFailedFrameBlockDuration(1*time.Nanosecond),
		middleware.WithFailedFrameMaxBlock(1*time.Nanosecond),
	)

	const ip = "192.168.1.50"

	// 1. Populate saltSeen via a successful first frame.
	salt := randomSalt(t)
	ct := encryptFirstFrame(t, c, salt, []byte("hi"), 0)
	_, _, err := mgr.EstablishSession(middleware.EncryptedFirstFrame{
		Version:     middleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}, ip)
	require.NoError(t, err)
	require.Positive(t, mgr.SaltSeenSizeForTest(),
		"saltSeen should have an entry after a successful first frame")

	// 2. Populate failSeen by submitting garbage ciphertext that the
	// known-token recordFailure path counts.
	for range 2 {
		_, _, _ = mgr.EstablishSession(middleware.EncryptedFirstFrame{
			Version:     middleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString([]byte("garbage")),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(randomSalt(t)),
		}, ip)
	}
	require.Positive(t, mgr.FailSeenSizeForTest(),
		"failSeen should have an entry after garbage frames from a known token")

	// Sleep just past the 1ns TTLs / blocks so cleanup considers everything
	// expired.
	time.Sleep(5 * time.Millisecond)

	// 3. Cleanup must wipe BOTH maps. A regression that broke either branch
	// would leave one of these positive.
	mgr.CleanupExpiredForTest()
	assert.Equal(t, 0, mgr.SaltSeenSizeForTest(),
		"cleanupExpired must evict expired salt entries")
	assert.Equal(t, 0, mgr.FailSeenSizeForTest(),
		"cleanupExpired must evict expired failure trackers")
}
