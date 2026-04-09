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
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/schollz/pake/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// pairingTestHarness wraps a manager and a mock DB so tests can simulate the
// full pair flow without HTTP plumbing.
type pairingTestHarness struct {
	mgr       *PairingManager
	db        *helpers.MockUserDBI
	notifChan chan models.Notification
	created   *atomic.Pointer[database.Client]
}

func newPairingHarness(t *testing.T, opts ...PairingOption) *pairingTestHarness {
	t.Helper()
	db := helpers.NewMockUserDBI()
	notifChan := make(chan models.Notification, 16)
	created := &atomic.Pointer[database.Client]{}

	db.On("CountClients").Return(0, nil).Maybe()
	db.On("CreateClient", mock.AnythingOfType("*database.Client")).
		Run(func(args mock.Arguments) {
			c, ok := args.Get(0).(*database.Client)
			if !ok || c == nil {
				return
			}
			cp := *c
			created.Store(&cp)
		}).
		Return(nil).
		Maybe()

	mgr := NewPairingManager(db, notifChan, opts...)
	return &pairingTestHarness{
		mgr:       mgr,
		db:        db,
		notifChan: notifChan,
		created:   created,
	}
}

// runHandshake executes a successful PAKE handshake against the manager and
// returns the resulting Client + the pairing key the client derived. Tests
// that need wrong-PIN behavior can call the lower-level methods directly.
func (h *pairingTestHarness) runHandshake(
	t *testing.T,
	pin, name string,
) (clientResp *database.Client, pairingKey []byte) {
	t.Helper()
	clientPake, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	msgA := clientPake.Bytes()

	sessionID, msgB, err := h.mgr.startSession(name, msgA)
	require.NoError(t, err)

	require.NoError(t, clientPake.Update(msgB))
	clientSessionKey, err := clientPake.SessionKey()
	require.NoError(t, err)

	prk, err := hkdf.Extract(sha256.New, clientSessionKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)
	confirmKeyB, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmB, sha256.Size)
	require.NoError(t, err)
	derivedPairingKey, err := hkdf.Expand(sha256.New, prk, pairingInfoPairing, crypto.PairingKeySize)
	require.NoError(t, err)

	clientHMAC := computePairingHMAC(confirmKeyA, "client", name, msgA, msgB)

	result, err := h.mgr.finishSession(sessionID, clientHMAC)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Client)

	expectedServer := computePairingHMAC(confirmKeyB, "server", name, msgA, msgB)
	require.Equal(t, expectedServer, result.ServerHMAC, "server HMAC must match what client computes")
	require.Equal(t, derivedPairingKey, result.Client.PairingKey, "pairing keys must agree")

	return result.Client, result.Client.PairingKey
}

func TestStartPairing_GeneratesPIN(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	pin, expiresAt, err := h.mgr.StartPairing()
	require.NoError(t, err)

	assert.Len(t, pin, pairingPINLength)
	for _, c := range pin {
		assert.True(t, c >= '0' && c <= '9', "PIN must be all digits, got %q", pin)
	}
	assert.True(t, expiresAt.After(time.Now()), "expiry must be in the future")
}

func TestStartPairing_AlreadyInProgress(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)
	_, _, err = h.mgr.StartPairing()
	require.ErrorIs(t, err, errPairingInProgress)
}

func TestStartPairing_AfterCancel(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)
	h.mgr.CancelPairing()
	_, _, err = h.mgr.StartPairing()
	require.NoError(t, err, "should be able to start a new pairing after cancel")
}

func TestStartPairing_AfterExpiry(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t, WithPairingPINTTL(50*time.Millisecond))

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	_, _, err = h.mgr.StartPairing()
	require.NoError(t, err, "expired PIN should not block a new one")
}

func TestPendingPIN_Empty(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	pin, _ := h.mgr.PendingPIN()
	assert.Empty(t, pin)
}

func TestPendingPIN_Active(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	gotPIN, _ := h.mgr.PendingPIN()
	assert.Equal(t, pin, gotPIN)
}

func TestPendingPIN_Expired(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t, WithPairingPINTTL(5*time.Millisecond))

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)
	time.Sleep(15 * time.Millisecond)

	pin, _ := h.mgr.PendingPIN()
	assert.Empty(t, pin, "expired PIN should not be returned")
}

// TestStartSession_RejectsOversizedPakeMessage pins the input length cap
// for the PAKE message. The cap is well above any legitimate message size
// (a real P-256 client message is ~633 bytes vs the 2048-byte cap), so
// the cap only kicks in for clearly-malformed input.
func TestStartSession_RejectsOversizedPakeMessage(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	huge := make([]byte, pairingMaxPakeMessageBytes+1)
	_, _, err = h.mgr.startSession("Test App", huge)
	require.ErrorIs(t, err, errPairingMessageTooLong)
}

func TestSuccessfulHandshake(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	c, pairingKey := h.runHandshake(t, pin, "Test App")

	assert.NotEmpty(t, c.ClientID)
	assert.NotEmpty(t, c.AuthToken)
	assert.Equal(t, "Test App", c.ClientName)
	assert.Len(t, pairingKey, crypto.PairingKeySize)

	// PIN should have been cleared
	pin2, _ := h.mgr.PendingPIN()
	assert.Empty(t, pin2)

	// Notification should have been sent
	select {
	case notif := <-h.notifChan:
		assert.Equal(t, models.NotificationClientsPaired, notif.Method)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected clients.paired notification")
	}
}

func TestWrongPIN_Rejected(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	// Use a wrong PIN — different session key, HMAC will not match.
	wrongPake, err := pake.InitCurve([]byte("999999"), 0, pairingCurve)
	require.NoError(t, err)
	msgA := wrongPake.Bytes()
	sessionID, msgB, err := h.mgr.startSession("App", msgA)
	require.NoError(t, err)

	require.NoError(t, wrongPake.Update(msgB))
	clientKey, err := wrongPake.SessionKey()
	require.NoError(t, err)

	prk, err := hkdf.Extract(sha256.New, clientKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)

	// Compute an HMAC with the wrong key — server will reject it.
	wrongHMAC := computePairingHMAC(confirmKeyA, "client", "App", msgA, msgB)
	_, err = h.mgr.finishSession(sessionID, wrongHMAC)
	require.ErrorIs(t, err, errPairingHMACMismatch)
}

func TestMaxAttempts_PINInvalidatedAfterExhaustion(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t, WithPairingMaxAttempts(2))

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	for i := range 2 {
		wrongPake, pkErr := pake.InitCurve([]byte("999999"), 0, pairingCurve)
		require.NoError(t, pkErr)
		msgA := wrongPake.Bytes()
		sessionID, _, sErr := h.mgr.startSession("App", msgA)
		require.NoError(t, sErr)
		_, finishErr := h.mgr.finishSession(sessionID, []byte("garbage hmac"))
		if i < 1 {
			require.ErrorIs(t, finishErr, errPairingHMACMismatch, "attempt %d should mismatch", i)
		} else {
			require.ErrorIs(t, finishErr, errPairingExhausted, "final attempt should exhaust")
		}
	}

	// PIN should be cleared
	pin, _ := h.mgr.PendingPIN()
	assert.Empty(t, pin)
}

func TestPairStart_NameTooLong(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte("000000"), 0, pairingCurve)
	require.NoError(t, err)
	_, _, err = h.mgr.startSession(strings.Repeat("a", pairingMaxNameLen+1), clientPake.Bytes())
	require.ErrorIs(t, err, errPairingNameTooLong)
}

func TestPairStart_NameEmpty(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte("000000"), 0, pairingCurve)
	require.NoError(t, err)
	_, _, err = h.mgr.startSession("", clientPake.Bytes())
	require.ErrorIs(t, err, errPairingNameEmpty)
}

func TestPairStart_NoPendingPIN(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	clientPake, err := pake.InitCurve([]byte("000000"), 0, pairingCurve)
	require.NoError(t, err)
	_, _, err = h.mgr.startSession("App", clientPake.Bytes())
	require.ErrorIs(t, err, errNoPairingPending)
}

func TestPairStart_PINExpired(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t, WithPairingPINTTL(5*time.Millisecond))

	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)
	time.Sleep(15 * time.Millisecond)

	clientPake, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	_, _, err = h.mgr.startSession("App", clientPake.Bytes())
	require.ErrorIs(t, err, errPairingExpired)
}

func TestPairFinish_SessionExpired(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t, WithPairingSessionTTL(5*time.Millisecond))

	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	sessionID, _, err := h.mgr.startSession("App", clientPake.Bytes())
	require.NoError(t, err)

	time.Sleep(15 * time.Millisecond)

	_, err = h.mgr.finishSession(sessionID, []byte("anything"))
	require.ErrorIs(t, err, errPairingExpired)
}

func TestPairFinish_UnknownSession(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	_, err := h.mgr.finishSession("nonexistent", []byte("x"))
	require.ErrorIs(t, err, errPairingSessionUnknown)
}

// TestPairFinish_ConcurrentCallsOneWins pins that two concurrent
// finishSession calls with the same sessionID serialize correctly: exactly
// one succeeds, and the other gets errPairingSessionUnknown because the
// winning goroutine deleted the session under the lock.
func TestPairFinish_ConcurrentCallsOneWins(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	msgA := clientPake.Bytes()

	sessionID, msgB, err := h.mgr.startSession("ConcurrentApp", msgA)
	require.NoError(t, err)

	require.NoError(t, clientPake.Update(msgB))
	sessionKey, err := clientPake.SessionKey()
	require.NoError(t, err)

	prk, err := hkdf.Extract(sha256.New, sessionKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)
	validHMAC := computePairingHMAC(confirmKeyA, "client", "ConcurrentApp", msgA, msgB)

	type result struct {
		res *PairingResult
		err error
	}
	ch := make(chan result, 2)

	for range 2 {
		go func() {
			r, finishErr := h.mgr.finishSession(sessionID, validHMAC)
			ch <- result{res: r, err: finishErr}
		}()
	}

	r1 := <-ch
	r2 := <-ch

	successes := 0
	unknowns := 0
	for _, r := range []result{r1, r2} {
		switch r.err { //nolint:errorlint // exhaustive expected-error matching
		case nil:
			require.NotNil(t, r.res, "successful result must be non-nil")
			successes++
		default:
			require.ErrorIs(t, r.err, errPairingSessionUnknown,
				"the loser must see errPairingSessionUnknown, not a different error")
			unknowns++
		}
	}
	assert.Equal(t, 1, successes, "exactly one goroutine must succeed")
	assert.Equal(t, 1, unknowns, "exactly one goroutine must lose")
}

// TestMaxClients_StartPairingFailsFast pins the fail-fast behavior in
// StartPairing: when the client table is already at max, the operator
// must not even get a PIN. This avoids the bad UX where the operator
// types a PIN into a new device only to be rejected at /pair/finish.
func TestMaxClients_StartPairingFailsFast(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockUserDBI()
	notifChan := make(chan models.Notification, 4)
	db.On("CountClients").Return(50, nil)
	mgr := NewPairingManager(db, notifChan, WithPairingMaxClients(50))

	_, _, err := mgr.StartPairing()
	require.ErrorIs(t, err, errTooManyClients)
}

// TestMaxClients_FinishSessionDefenseInDepth covers the residual
// finishSession check that fires when a client is added between
// StartPairing and finishSession. There is no production code path that
// can do this today (the only way to add a client is via the same
// PairingManager under m.mu), but the check exists as defense in depth
// and we don't want it to silently rot.
func TestMaxClients_FinishSessionDefenseInDepth(t *testing.T) {
	t.Parallel()
	db := helpers.NewMockUserDBI()
	notifChan := make(chan models.Notification, 4)
	// First CountClients (StartPairing) → 0 ⇒ proceed.
	// Subsequent CountClients (finishSession) → 50 ⇒ reject.
	db.On("CountClients").Return(0, nil).Once()
	db.On("CountClients").Return(50, nil)
	mgr := NewPairingManager(db, notifChan, WithPairingMaxClients(50))

	pin, _, err := mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	msgA := clientPake.Bytes()
	sessionID, msgB, err := mgr.startSession("App", msgA)
	require.NoError(t, err)

	require.NoError(t, clientPake.Update(msgB))
	clientKey, err := clientPake.SessionKey()
	require.NoError(t, err)
	prk, err := hkdf.Extract(sha256.New, clientKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)
	clientHMAC := computePairingHMAC(confirmKeyA, "client", "App", msgA, msgB)

	_, err = mgr.finishSession(sessionID, clientHMAC)
	require.ErrorIs(t, err, errTooManyClients)
}

func TestStartPairing_WipesOldSessions(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	pin1, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte(pin1), 0, pairingCurve)
	require.NoError(t, err)
	sessionID, _, err := h.mgr.startSession("App", clientPake.Bytes())
	require.NoError(t, err)

	h.mgr.CancelPairing()
	_, _, err = h.mgr.StartPairing()
	require.NoError(t, err)

	// Old session should no longer be findable.
	_, err = h.mgr.finishSession(sessionID, []byte("x"))
	require.ErrorIs(t, err, errPairingSessionUnknown)
}

func TestHTTPHandlers_FullFlow(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)

	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	startHandler := h.mgr.HandlePairStart()
	finishHandler := h.mgr.HandlePairFinish()

	clientPake, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	msgA := clientPake.Bytes()

	startBody, err := json.Marshal(pairStartRequest{
		PAKE: base64.StdEncoding.EncodeToString(msgA),
		Name: "Web App",
	})
	require.NoError(t, err)

	startReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/start",
		strings.NewReader(string(startBody)))
	startReq.Header.Set("Content-Type", "application/json")
	startRec := httptest.NewRecorder()
	startHandler.ServeHTTP(startRec, startReq)
	require.Equal(t, http.StatusOK, startRec.Code, "start: %s", startRec.Body.String())

	var startResp pairStartResponse
	require.NoError(t, json.Unmarshal(startRec.Body.Bytes(), &startResp))
	require.NotEmpty(t, startResp.Session)

	msgB, err := base64.StdEncoding.DecodeString(startResp.PAKE)
	require.NoError(t, err)

	require.NoError(t, clientPake.Update(msgB))
	clientKey, err := clientPake.SessionKey()
	require.NoError(t, err)
	prk, err := hkdf.Extract(sha256.New, clientKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)
	confirmKeyB, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmB, sha256.Size)
	require.NoError(t, err)
	derivedPairingKey, err := hkdf.Expand(sha256.New, prk, pairingInfoPairing, crypto.PairingKeySize)
	require.NoError(t, err)

	clientHMAC := computePairingHMAC(confirmKeyA, "client", "Web App", msgA, msgB)

	finishBody, err := json.Marshal(pairFinishRequest{
		Session: startResp.Session,
		Confirm: base64.StdEncoding.EncodeToString(clientHMAC),
	})
	require.NoError(t, err)

	finishReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/finish",
		strings.NewReader(string(finishBody)))
	finishReq.Header.Set("Content-Type", "application/json")
	finishRec := httptest.NewRecorder()
	finishHandler.ServeHTTP(finishRec, finishReq)
	require.Equal(t, http.StatusOK, finishRec.Code, "finish: %s", finishRec.Body.String())

	var finishResp pairFinishResponse
	require.NoError(t, json.Unmarshal(finishRec.Body.Bytes(), &finishResp))

	assert.NotEmpty(t, finishResp.AuthToken)
	assert.NotEmpty(t, finishResp.ClientID)

	// The pairing key MUST NOT be on the wire — verify by checking the
	// JSON body has no pairingKey field at all.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(finishRec.Body.Bytes(), &raw))
	_, leaked := raw["pairingKey"]
	assert.False(t, leaked, "pairingKey must not be transmitted in /pair/finish response")

	// The client derives the pairing key locally; verify it matches what
	// the server stored in the database.
	storedClient := h.created.Load()
	require.NotNil(t, storedClient, "CreateClient must have been called")
	assert.Equal(t, derivedPairingKey, storedClient.PairingKey,
		"client-derived pairing key must match what the server stored")

	gotServerHMAC, err := base64.StdEncoding.DecodeString(finishResp.Confirm)
	require.NoError(t, err)
	expectedServerHMAC := computePairingHMAC(confirmKeyB, "server", "Web App", msgA, msgB)
	assert.Equal(t, expectedServerHMAC, gotServerHMAC, "server HMAC must match")
}

// TestHandlePairFinish_AuditLogsHMACMismatch verifies that a wrong-PIN
// /pair/finish attempt produces a warn-level audit log line including the
// source IP and the pairing_hmac_mismatch event tag.
//
// Not t.Parallel — mutates the global zerolog logger to capture output.
func TestHandlePairFinish_AuditLogsHMACMismatch(t *testing.T) {
	var buf bytes.Buffer
	originalLogger := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.WarnLevel)
	t.Cleanup(func() { log.Logger = originalLogger })

	h := newPairingHarness(t)
	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	// Drive a wrong-PIN handshake at the HTTP layer so the handler runs
	// the audit log path. Use a wrong PAKE password — the resulting HMAC
	// will not match the server's expected value.
	startHandler := h.mgr.HandlePairStart()
	finishHandler := h.mgr.HandlePairFinish()

	wrongPake, err := pake.InitCurve([]byte("000000"), 0, pairingCurve)
	require.NoError(t, err)
	if pin == "000000" {
		// Astronomically unlikely but possible — use a different wrong PIN.
		wrongPake, err = pake.InitCurve([]byte("111111"), 0, pairingCurve)
		require.NoError(t, err)
	}
	msgA := wrongPake.Bytes()

	startBody, err := json.Marshal(pairStartRequest{
		PAKE: base64.StdEncoding.EncodeToString(msgA),
		Name: "App",
	})
	require.NoError(t, err)
	startReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/start",
		strings.NewReader(string(startBody)))
	startReq.Header.Set("Content-Type", "application/json")
	startReq.RemoteAddr = "203.0.113.42:54321"
	startRec := httptest.NewRecorder()
	startHandler.ServeHTTP(startRec, startReq)
	require.Equal(t, http.StatusOK, startRec.Code)

	var startResp pairStartResponse
	require.NoError(t, json.Unmarshal(startRec.Body.Bytes(), &startResp))
	msgB, err := base64.StdEncoding.DecodeString(startResp.PAKE)
	require.NoError(t, err)
	require.NoError(t, wrongPake.Update(msgB))
	clientKey, err := wrongPake.SessionKey()
	require.NoError(t, err)
	prk, err := hkdf.Extract(sha256.New, clientKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)
	wrongHMAC := computePairingHMAC(confirmKeyA, "client", "App", msgA, msgB)

	finishBody, err := json.Marshal(pairFinishRequest{
		Session: startResp.Session,
		Confirm: base64.StdEncoding.EncodeToString(wrongHMAC),
	})
	require.NoError(t, err)
	finishReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/finish",
		strings.NewReader(string(finishBody)))
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.RemoteAddr = "203.0.113.42:54321"
	finishRec := httptest.NewRecorder()
	finishHandler.ServeHTTP(finishRec, finishReq)
	require.Equal(t, http.StatusUnauthorized, finishRec.Code)

	logged := buf.String()
	assert.Contains(t, logged, "pairing_hmac_mismatch", "audit log must tag the event")
	assert.Contains(t, logged, "203.0.113.42", "audit log must include source IP")
}

// TestHandlePairFinish_AuditLogsExhaustion verifies that exhausting the
// PIN attempts via the HTTP handler produces a pairing_attempts_exhausted
// audit log line.
//
// Not t.Parallel — mutates the global zerolog logger.
func TestHandlePairFinish_AuditLogsExhaustion(t *testing.T) {
	var buf bytes.Buffer
	originalLogger := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.WarnLevel)
	t.Cleanup(func() { log.Logger = originalLogger })

	h := newPairingHarness(t, WithPairingMaxAttempts(1))
	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	// One allowed attempt → first failure trips errPairingExhausted.
	// Drive the failed attempt through the HTTP handler so the audit log
	// path is exercised.
	wrongPIN := "000000"
	if pin == wrongPIN {
		wrongPIN = "111111"
	}
	wrongPake, err := pake.InitCurve([]byte(wrongPIN), 0, pairingCurve)
	require.NoError(t, err)
	msgA := wrongPake.Bytes()
	startBody, err := json.Marshal(pairStartRequest{
		PAKE: base64.StdEncoding.EncodeToString(msgA),
		Name: "App",
	})
	require.NoError(t, err)
	startReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/start",
		strings.NewReader(string(startBody)))
	startReq.Header.Set("Content-Type", "application/json")
	startReq.RemoteAddr = "198.51.100.7:8080"
	startRec := httptest.NewRecorder()
	h.mgr.HandlePairStart().ServeHTTP(startRec, startReq)
	require.Equal(t, http.StatusOK, startRec.Code)

	var startResp pairStartResponse
	require.NoError(t, json.Unmarshal(startRec.Body.Bytes(), &startResp))
	msgB, err := base64.StdEncoding.DecodeString(startResp.PAKE)
	require.NoError(t, err)
	require.NoError(t, wrongPake.Update(msgB))
	clientKey, err := wrongPake.SessionKey()
	require.NoError(t, err)
	prk, err := hkdf.Extract(sha256.New, clientKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)
	wrongHMAC := computePairingHMAC(confirmKeyA, "client", "App", msgA, msgB)

	finishBody, err := json.Marshal(pairFinishRequest{
		Session: startResp.Session,
		Confirm: base64.StdEncoding.EncodeToString(wrongHMAC),
	})
	require.NoError(t, err)
	finishReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/finish",
		strings.NewReader(string(finishBody)))
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.RemoteAddr = "198.51.100.7:8080"
	finishRec := httptest.NewRecorder()
	h.mgr.HandlePairFinish().ServeHTTP(finishRec, finishReq)
	require.Equal(t, http.StatusForbidden, finishRec.Code)

	logged := buf.String()
	assert.Contains(t, logged, "pairing_attempts_exhausted",
		"audit log must tag the exhaustion event")
	assert.Contains(t, logged, "198.51.100.7",
		"audit log must include source IP")
}

func TestHTTPHandler_BadRequestJSON(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)
	startHandler := h.mgr.HandlePairStart()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/start",
		strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	startHandler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHTTPHandler_BadBase64(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t)
	startHandler := h.mgr.HandlePairStart()

	body, err := json.Marshal(pairStartRequest{PAKE: "not-base64!!", Name: "App"})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/pair/start",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	startHandler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGeneratePIN_AllDigits(t *testing.T) {
	t.Parallel()
	for range 100 {
		pin, err := generatePIN()
		require.NoError(t, err)
		assert.Len(t, pin, pairingPINLength)
		for _, c := range pin {
			assert.True(t, c >= '0' && c <= '9', "non-digit in pin %q", pin)
		}
	}
}

func TestComputePairingHMAC_Deterministic(t *testing.T) {
	t.Parallel()
	key := []byte("0123456789abcdef0123456789abcdef")
	a := computePairingHMAC(key, "client", "App", []byte("a"), []byte("b"))
	b := computePairingHMAC(key, "client", "App", []byte("a"), []byte("b"))
	assert.Equal(t, a, b)
}

func TestComputePairingHMAC_DifferentRolesDifferent(t *testing.T) {
	t.Parallel()
	key := []byte("0123456789abcdef0123456789abcdef")
	a := computePairingHMAC(key, "client", "App", []byte("a"), []byte("b"))
	b := computePairingHMAC(key, "server", "App", []byte("a"), []byte("b"))
	assert.NotEqual(t, a, b)
}

func TestComputePairingHMAC_LengthPrefixingPreventsCollision(t *testing.T) {
	t.Parallel()
	key := []byte("0123456789abcdef0123456789abcdef")
	// Without length prefixing, "ab" + "c" and "a" + "bc" would produce the
	// same HMAC for the same role/version. With length prefixing, they must
	// differ.
	a := computePairingHMAC(key, "client", "ab", []byte("c"), []byte("d"))
	b := computePairingHMAC(key, "client", "a", []byte("bc"), []byte("d"))
	assert.NotEqual(t, a, b, "length prefix must prevent canonicalization collision")
}

// TestComputePairingHMAC_RoleNameBoundary pins the length-prefix protection
// at the (role | name) field boundary. Without length-prefixing, an attacker
// could shift characters between role and name to forge an HMAC for a
// different (role, name) pair that hashes to the same input bytes.
func TestComputePairingHMAC_RoleNameBoundary(t *testing.T) {
	t.Parallel()
	key := []byte("0123456789abcdef0123456789abcdef")
	// Both inputs have role+name = "clientApp" if naively concatenated.
	a := computePairingHMAC(key, "client", "App", []byte("msgA"), []byte("msgB"))
	b := computePairingHMAC(key, "clien", "tApp", []byte("msgA"), []byte("msgB"))
	assert.NotEqual(t, a, b,
		"length prefix must distinguish (role=client, name=App) from (role=clien, name=tApp)")
}

// TestComputePairingHMAC_MsgABoundary pins the length-prefix protection at
// the (msgA | msgB) field boundary. PAKE messages have fixed structure in
// production, but the HMAC scheme must still defend against canonicalization
// across this boundary in case message lengths ever vary.
func TestComputePairingHMAC_MsgABoundary(t *testing.T) {
	t.Parallel()
	key := []byte("0123456789abcdef0123456789abcdef")
	// Both inputs have msgA+msgB = "abc" if naively concatenated.
	a := computePairingHMAC(key, "client", "App", []byte("ab"), []byte("c"))
	b := computePairingHMAC(key, "client", "App", []byte("a"), []byte("bc"))
	assert.NotEqual(t, a, b,
		"length prefix must distinguish (msgA=ab, msgB=c) from (msgA=a, msgB=bc)")
}

func TestCleanupExpired_RemovesOldSessions(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t,
		WithPairingPINTTL(time.Hour),
		WithPairingSessionTTL(5*time.Millisecond),
	)

	pin, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	_, _, err = h.mgr.startSession("App", clientPake.Bytes())
	require.NoError(t, err)

	time.Sleep(15 * time.Millisecond)
	h.mgr.cleanupExpired()

	h.mgr.mu.Lock()
	defer h.mgr.mu.Unlock()
	assert.Empty(t, h.mgr.sessions, "expired session should be removed")
	assert.NotEmpty(t, h.mgr.pin, "PIN should still be valid")
}

func TestCleanupExpired_RemovesExpiredPIN(t *testing.T) {
	t.Parallel()
	h := newPairingHarness(t, WithPairingPINTTL(5*time.Millisecond))

	_, _, err := h.mgr.StartPairing()
	require.NoError(t, err)

	time.Sleep(15 * time.Millisecond)
	h.mgr.cleanupExpired()

	pin, _ := h.mgr.PendingPIN()
	assert.Empty(t, pin)
}
