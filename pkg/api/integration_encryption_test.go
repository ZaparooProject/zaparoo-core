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

package api_test

import (
	"crypto/hkdf"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/schollz/pake/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// pairingHTTPResponse captures a small subset of an httptest.Recorder.
type pairingHTTPResponse struct {
	body       []byte
	statusCode int
}

// callPairingEndpoint invokes a pairing HTTP handler via httptest and
// returns the body + status code.
func callPairingEndpoint(t *testing.T, h http.HandlerFunc, path string, body []byte) pairingHTTPResponse {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	respBody, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	return pairingHTTPResponse{statusCode: rec.Code, body: respBody}
}

// computeIntegrationHMAC duplicates the manager's HMAC scheme so the test
// can act as the client. Length-prefixed encoding matches pairing.go.
func computeIntegrationHMAC(key []byte, role, name string, msgA, msgB []byte) []byte {
	h := hmac.New(sha256.New, key)
	writeIntegrationLP(h, []byte("zaparoo-v1"))
	writeIntegrationLP(h, []byte(pakeCurve))
	writeIntegrationLP(h, []byte(role))
	writeIntegrationLP(h, []byte(name))
	writeIntegrationLP(h, msgA)
	writeIntegrationLP(h, msgB)
	return h.Sum(nil)
}

func writeIntegrationLP(w io.Writer, b []byte) {
	var lp [4]byte
	//nolint:gosec // bounded test inputs
	binary.BigEndian.PutUint32(lp[:], uint32(len(b)))
	_, _ = w.Write(lp[:])
	_, _ = w.Write(b)
}

// hkdfInfo strings used inside the pairing manager. Duplicated here so the
// integration test can independently derive the same keys the manager does.
const (
	infoConfirmA = "zaparoo-confirm-A"
	infoConfirmB = "zaparoo-confirm-B"
	infoPairing  = "zaparoo-pairing-v1"
	pakeCurve    = "p256"
)

// pairAndDeriveKey runs the full PAKE handshake against a PairingManager and
// returns the resulting database client + the pairing key the client computes.
func pairAndDeriveKey(
	t *testing.T,
	mgr *api.PairingManager,
	clientName string,
) (storedClient *database.Client, pairingKey []byte) {
	t.Helper()

	pin, _, err := mgr.StartPairing()
	require.NoError(t, err)

	clientPake, err := pake.InitCurve([]byte(pin), 0, pakeCurve)
	require.NoError(t, err)
	msgA := clientPake.Bytes()

	// Drive the start session via the HTTP handler so we exercise the
	// real request/response shapes.
	startBody, err := json.Marshal(map[string]string{
		"pake": base64.StdEncoding.EncodeToString(msgA),
		"name": clientName,
	})
	require.NoError(t, err)

	startResp := callPairingEndpoint(t, mgr.HandlePairStart(), "/api/pair/start", startBody)
	require.Equal(t, 200, startResp.statusCode, "start: %s", string(startResp.body))

	var startResult struct {
		Session string `json:"session"`
		PAKE    string `json:"pake"`
	}
	require.NoError(t, json.Unmarshal(startResp.body, &startResult))

	msgB, err := base64.StdEncoding.DecodeString(startResult.PAKE)
	require.NoError(t, err)
	require.NoError(t, clientPake.Update(msgB))
	clientSessionKey, err := clientPake.SessionKey()
	require.NoError(t, err)

	// Derive the same confirmation keys + pairing key the server will.
	prk, err := hkdf.Extract(sha256.New, clientSessionKey, nil)
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, infoConfirmA, sha256.Size)
	require.NoError(t, err)
	confirmKeyB, err := hkdf.Expand(sha256.New, prk, infoConfirmB, sha256.Size)
	require.NoError(t, err)
	pairingKey, err = hkdf.Expand(sha256.New, prk, infoPairing, crypto.PairingKeySize)
	require.NoError(t, err)

	clientHMAC := computeIntegrationHMAC(confirmKeyA, "client", clientName, msgA, msgB)

	finishBody, err := json.Marshal(map[string]string{
		"session": startResult.Session,
		"confirm": base64.StdEncoding.EncodeToString(clientHMAC),
	})
	require.NoError(t, err)

	finishResp := callPairingEndpoint(t, mgr.HandlePairFinish(), "/api/pair/finish", finishBody)
	require.Equal(t, 200, finishResp.statusCode, "finish: %s", string(finishResp.body))

	var finishResult struct {
		AuthToken string `json:"authToken"`
		ClientID  string `json:"clientId"`
		Confirm   string `json:"confirm"`
	}
	require.NoError(t, json.Unmarshal(finishResp.body, &finishResult))

	// Defense in depth: assert the wire body has no pairingKey field at
	// all. The long-term key must NEVER traverse the network.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(finishResp.body, &raw))
	_, leaked := raw["pairingKey"]
	require.False(t, leaked, "pairingKey must not appear in /pair/finish response body")

	expectedServerHMAC := computeIntegrationHMAC(confirmKeyB, "server", clientName, msgA, msgB)
	gotServerHMAC, err := base64.StdEncoding.DecodeString(finishResult.Confirm)
	require.NoError(t, err)
	require.Equal(t, expectedServerHMAC, gotServerHMAC, "server HMAC must match for client to trust")

	return &database.Client{
		ClientID:   finishResult.ClientID,
		ClientName: clientName,
		AuthToken:  finishResult.AuthToken,
		PairingKey: pairingKey,
	}, pairingKey
}

func TestIntegration_PairThenEncryptedSession(t *testing.T) {
	t.Parallel()

	// Set up a mock UserDB that captures the created client.
	db := helpers.NewMockUserDBI()
	var stored *database.Client
	db.On("CountClients").Return(0, nil)
	db.On("CreateClient", mock.AnythingOfType("*database.Client")).
		Run(func(args mock.Arguments) {
			c, ok := args.Get(0).(*database.Client)
			require.True(t, ok)
			cp := *c
			stored = &cp
		}).
		Return(nil)

	notifChan := make(chan models.Notification, 16)
	pairingMgr := api.NewPairingManager(db, notifChan)

	// 1. Run the full PAKE handshake.
	c, pairingKey := pairAndDeriveKey(t, pairingMgr, "Integration App")
	require.NotNil(t, stored, "client must be persisted to DB")
	require.Equal(t, stored.ClientID, c.ClientID)
	require.Equal(t, stored.AuthToken, c.AuthToken)
	require.Equal(t, pairingKey, stored.PairingKey)

	// 2. Verify clients.paired notification was sent.
	select {
	case notif := <-notifChan:
		assert.Equal(t, models.NotificationClientsPaired, notif.Method)
	default:
		t.Fatal("expected clients.paired notification to be sent")
	}

	// Now that we have a stored client, set up the lookup expectation.
	db.On("GetClientByToken", stored.AuthToken).Return(stored, nil)

	// 3. Establish an encrypted WebSocket session using the pairing key.
	encGateway := apimiddleware.NewEncryptionGateway(db)
	salt := make([]byte, crypto.SessionSaltSize)
	_, err := cryptorand.Read(salt)
	require.NoError(t, err)

	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)
	clientGCMC2S, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	clientGCMS2C, err := crypto.NewAEAD(keys.S2CKey)
	require.NoError(t, err)

	aad := []byte(c.AuthToken + ":ws")
	plaintextReq := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	ctReq, err := crypto.Encrypt(clientGCMC2S, keys.C2SNonce, 0, plaintextReq, aad)
	require.NoError(t, err)

	firstFrame := apimiddleware.EncryptedFirstFrame{
		Version:     apimiddleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ctReq),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	cs, decrypted, err := encGateway.EstablishSession(firstFrame, "192.168.1.50")
	require.NoError(t, err, "first frame should decrypt with paired key")
	assert.Equal(t, plaintextReq, decrypted)
	require.NotNil(t, cs)

	// 4. Server encrypts a response (counter 0), client decrypts.
	plaintextResp := []byte(`{"jsonrpc":"2.0","result":{"version":"test"},"id":1}`)
	ctResp, err := cs.EncryptOutgoing(plaintextResp)
	require.NoError(t, err)

	gotResp, err := crypto.Decrypt(clientGCMS2C, keys.S2CNonce, 0, ctResp, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintextResp, gotResp)

	// 5. Client sends a second frame (counter 1).
	plaintextReq2 := []byte(`{"jsonrpc":"2.0","method":"systems","id":2}`)
	ctReq2, err := crypto.Encrypt(clientGCMC2S, keys.C2SNonce, 1, plaintextReq2, aad)
	require.NoError(t, err)
	subFrame := apimiddleware.EncryptedFrame{
		Ciphertext: base64.StdEncoding.EncodeToString(ctReq2),
	}
	gotReq2, err := cs.DecryptSubsequent(subFrame)
	require.NoError(t, err)
	assert.Equal(t, plaintextReq2, gotReq2)
}

func TestIntegration_RevokedClientCannotConnect(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	var stored *database.Client

	db.On("CountClients").Return(0, nil)
	db.On("CreateClient", mock.AnythingOfType("*database.Client")).
		Run(func(args mock.Arguments) {
			c, ok := args.Get(0).(*database.Client)
			require.True(t, ok)
			cp := *c
			stored = &cp
		}).
		Return(nil)

	notifChan := make(chan models.Notification, 4)
	pairingMgr := api.NewPairingManager(db, notifChan)
	_, pairingKey := pairAndDeriveKey(t, pairingMgr, "Revoke Test")
	require.NotNil(t, stored)

	// Simulate revocation: GetClientByToken returns an error.
	db.On("GetClientByToken", stored.AuthToken).Return((*database.Client)(nil), assert.AnError)

	encGateway := apimiddleware.NewEncryptionGateway(db)
	salt := make([]byte, crypto.SessionSaltSize)
	_, err := cryptorand.Read(salt)
	require.NoError(t, err)
	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)
	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	aad := []byte(stored.AuthToken + ":ws")
	ct, err := crypto.Encrypt(gcm, keys.C2SNonce, 0, []byte(`{"jsonrpc":"2.0","method":"version","id":1}`), aad)
	require.NoError(t, err)

	frame := apimiddleware.EncryptedFirstFrame{
		Version:     apimiddleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   stored.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	_, _, err = encGateway.EstablishSession(frame, "192.168.1.50")
	require.Error(t, err, "revoked client must not be able to establish a session")
	require.ErrorIs(t, err, apimiddleware.ErrUnknownAuthToken)
}

func TestIntegration_WrongPairingKeyRejected(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockUserDBI()
	var stored *database.Client
	db.On("CountClients").Return(0, nil)
	db.On("CreateClient", mock.AnythingOfType("*database.Client")).
		Run(func(args mock.Arguments) {
			c, ok := args.Get(0).(*database.Client)
			require.True(t, ok)
			cp := *c
			stored = &cp
		}).
		Return(nil)

	notifChan := make(chan models.Notification, 4)
	pairingMgr := api.NewPairingManager(db, notifChan)
	_, _ = pairAndDeriveKey(t, pairingMgr, "Wrong Key Test")
	require.NotNil(t, stored)

	db.On("GetClientByToken", stored.AuthToken).Return(stored, nil)

	// Use a DIFFERENT pairing key — server has the real one, attacker uses
	// a guessed one. First frame must fail decryption.
	attackerKey := make([]byte, crypto.PairingKeySize)
	_, err := cryptorand.Read(attackerKey)
	require.NoError(t, err)

	encGateway := apimiddleware.NewEncryptionGateway(db)
	salt := make([]byte, crypto.SessionSaltSize)
	_, err = cryptorand.Read(salt)
	require.NoError(t, err)
	keys, err := crypto.DeriveSessionKeys(attackerKey, salt)
	require.NoError(t, err)
	gcm, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	aad := []byte(stored.AuthToken + ":ws")
	ct, err := crypto.Encrypt(gcm, keys.C2SNonce, 0, []byte(`{"jsonrpc":"2.0","method":"version","id":1}`), aad)
	require.NoError(t, err)

	frame := apimiddleware.EncryptedFirstFrame{
		Version:     apimiddleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   stored.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	_, _, err = encGateway.EstablishSession(frame, "192.168.1.50")
	require.Error(t, err, "wrong pairing key must fail decryption")
}
