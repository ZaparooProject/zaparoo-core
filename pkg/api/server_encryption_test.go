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
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWritePong_Plaintext verifies that without an established encryption
// session the heartbeat reply is the raw bytes "pong" (preserves the
// existing client contract for plaintext sessions).
func TestWritePong_Plaintext(t *testing.T) {
	t.Parallel()

	var got []byte
	capture := func(b []byte) error {
		got = append([]byte(nil), b...)
		return nil
	}
	require.NoError(t, writePong(capture, nil))
	assert.Equal(t, []byte("pong"), got)
}

// TestWritePong_Encrypted verifies that on an established encryption
// session the heartbeat reply is wrapped in an AEAD frame whose decrypted
// body is "pong". This pins the fix for the ping-bypass bug where the
// heartbeat used to bypass encryption entirely. The test exercises the
// real production path (writePong → SendEncryptedFrame) with a capturing
// writer in place of melody's session.Write.
func TestWriteNotificationFrame_Encrypted(t *testing.T) {
	t.Parallel()

	cs, clientSecrets := establishTestEncryptionSession(t)
	plaintext := []byte(`{"jsonrpc":"2.0","method":"media.started"}`)
	var got []byte
	require.NoError(t, writeNotificationFrame(func(data []byte) error {
		got = append([]byte(nil), data...)
		return nil
	}, cs, webSocketAuthEncrypted, plaintext))

	var frame apimiddleware.EncryptedFrame
	require.NoError(t, json.Unmarshal(got, &frame))
	ct, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	require.NoError(t, err)
	decrypted, err := crypto.Decrypt(
		clientSecrets.s2cGCM,
		clientSecrets.s2cNonce,
		0,
		ct,
		clientSecrets.aad,
	)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestWritePong_Encrypted(t *testing.T) {
	t.Parallel()

	cs, clientSecrets := establishTestEncryptionSession(t)

	var got []byte
	capture := func(b []byte) error {
		got = append([]byte(nil), b...)
		return nil
	}
	require.NoError(t, writePong(capture, cs))

	var frame apimiddleware.EncryptedFrame
	require.NoError(t, json.Unmarshal(got, &frame))
	require.NotEmpty(t, frame.Ciphertext, "encrypted pong must produce a non-empty ciphertext")

	ct, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	require.NoError(t, err)

	pt, err := crypto.Decrypt(
		clientSecrets.s2cGCM,
		clientSecrets.s2cNonce,
		0, // first server-to-client frame
		ct,
		clientSecrets.aad,
	)
	require.NoError(t, err)
	assert.Equal(t, []byte("pong"), pt)
}

// testEncryptionPeerSecrets holds the cipher state the *client* side of an
// established session needs to decrypt server messages. Tests use it to
// verify wire shape end-to-end.
type testEncryptionPeerSecrets struct {
	s2cGCM   cipher.AEAD
	s2cNonce []byte
	aad      []byte
}

// establishTestEncryptionSession constructs a real *apimiddleware.ClientSession
// the same way the production server does: pair a fake client, build a valid
// encrypted first frame, and run it through EncryptionGateway.EstablishSession.
// Returns the resulting session and the matching client-side cipher state.
func establishTestEncryptionSession(t *testing.T) (*apimiddleware.ClientSession, *testEncryptionPeerSecrets) {
	t.Helper()

	pairingKey := make([]byte, crypto.PairingKeySize)
	_, err := cryptorand.Read(pairingKey)
	require.NoError(t, err)

	//nolint:gosec // test fixture; AuthToken is opaque test data, not a credential
	c := &database.Client{
		ClientID:   "test-client",
		ClientName: "Test",
		AuthToken:  "test-auth-token",
		PairingKey: pairingKey,
	}

	db := helpers.NewMockUserDBI()
	db.On("GetClientByToken", c.AuthToken).Return(c, nil)
	mgr := apimiddleware.NewEncryptionGateway(db)

	salt := make([]byte, crypto.SessionSaltSize)
	_, err = cryptorand.Read(salt)
	require.NoError(t, err)

	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)

	clientC2S, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	clientS2C, err := crypto.NewAEAD(keys.S2CKey)
	require.NoError(t, err)

	aad := []byte(c.AuthToken + ":ws")
	plaintextReq := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	ct, err := crypto.Encrypt(clientC2S, keys.C2SNonce, 0, plaintextReq, aad)
	require.NoError(t, err)

	first := apimiddleware.EncryptedFirstFrame{
		Version:     apimiddleware.EncryptionProtoVersion,
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		AuthToken:   c.AuthToken,
		SessionSalt: base64.StdEncoding.EncodeToString(salt),
	}
	cs, _, err := mgr.EstablishSession(first, "192.168.1.50")
	require.NoError(t, err)
	require.NotNil(t, cs)

	return cs, &testEncryptionPeerSecrets{
		s2cGCM:   clientS2C,
		s2cNonce: keys.S2CNonce,
		aad:      aad,
	}
}

// TestRemoteAddrParsing_IPv6Loopback pins the IPv6-aware parsing path used
// by handleWSMessage and handlePostRequest. The previous implementation
// used strings.SplitN(addr, ":", 2) which mangled IPv6 brackets — `[::1]`
// became `[`, ParseIP returned nil, IPv6 loopback was treated as remote,
// and every IPv6 client shared the same `<nil>` rate-limit bucket.
//
// This test fails if anyone reverts to a non-bracket-aware parser.
func TestRemoteAddrParsing_IPv6Loopback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		wantIPStr  string
		wantLocal  bool
	}{
		{
			name:       "IPv4 loopback",
			remoteAddr: "127.0.0.1:54321",
			wantLocal:  true,
			wantIPStr:  "127.0.0.1",
		},
		{
			name:       "IPv6 loopback bracketed",
			remoteAddr: "[::1]:54321",
			wantLocal:  true,
			wantIPStr:  "::1",
		},
		{
			name:       "IPv4 remote",
			remoteAddr: "192.168.1.50:9000",
			wantLocal:  false,
			wantIPStr:  "192.168.1.50",
		},
		{
			name:       "IPv6 remote",
			remoteAddr: "[2001:db8::1]:9000",
			wantLocal:  false,
			wantIPStr:  "2001:db8::1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ip := apimiddleware.ParseRemoteIP(tc.remoteAddr)
			require.NotNil(t, ip, "ParseRemoteIP must not return nil for valid host:port")
			assert.Equal(t, tc.wantIPStr, ip.String())
			assert.Equal(t, tc.wantLocal, apimiddleware.IsLoopbackAddr(tc.remoteAddr))
		})
	}
}

// TestUnsupportedEncryptionVersionResponse_WireShape pins the JSON wire
// format of the -32001 error to JSON-RPC 2.0 §5.1: the `data` field MUST
// be nested inside the `error` object, not a top-level sibling.
func TestUnsupportedEncryptionVersionResponse_WireShape(t *testing.T) {
	t.Parallel()

	raw, err := unsupportedEncryptionVersionResponse()
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed))

	// JSON-RPC envelope
	assert.Equal(t, "2.0", parsed["jsonrpc"])
	assert.Nil(t, parsed["id"], "id must be null on protocol-level errors")

	// `data` MUST NOT be at the top level — it belongs inside `error`.
	_, dataAtTop := parsed["data"]
	assert.False(t, dataAtTop,
		"data must be nested inside error per JSON-RPC 2.0 §5.1, not a sibling")

	errObj, ok := parsed["error"].(map[string]any)
	require.True(t, ok, "error must be an object")
	assert.InDelta(t, float64(-32001), errObj["code"], 0)
	assert.Equal(t, "unsupported encryption version", errObj["message"])

	errData, ok := errObj["data"].(map[string]any)
	require.True(t, ok, "error.data must be an object")
	supported, ok := errData["supported"].([]any)
	require.True(t, ok, "error.data.supported must be an array")
	require.Len(t, supported, 1)
	assert.InDelta(t, float64(1), supported[0], 0,
		"only protocol version 1 is currently supported")
}

// A client connecting while encryption is optional has not yet revealed
// whether it will negotiate an encrypted session, because that happens on its
// first frame. Notifications broadcast in that window must not be written in
// the clear: the client may already have committed to encrypted mode, and a
// plaintext frame arriving afterwards desyncs it.
func TestWriteNotificationFrame_UnsettledSessionIsNotWrittenPlaintext(t *testing.T) {
	t.Parallel()

	plaintext := []byte(`{"jsonrpc":"2.0","method":"media.scrape.update"}`)
	tests := []struct {
		name      string
		authState webSocketAuthState
		wantWrite bool
	}{
		{name: "unsettled suppresses", authState: webSocketAuthUnsettled, wantWrite: false},
		{name: "pending suppresses", authState: webSocketAuthPending, wantWrite: false},
		{name: "settled plaintext writes", authState: webSocketAuthPlaintext, wantWrite: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []byte
			require.NoError(t, writeNotificationFrame(func(data []byte) error {
				got = append([]byte(nil), data...)
				return nil
			}, nil, tt.authState, plaintext))

			if tt.wantWrite {
				assert.Equal(t, plaintext, got)
			} else {
				assert.Nil(t, got, "notification must not reach a session of unknown transport mode")
			}
		})
	}
}

// A notification broadcast before the client's first frame must survive until
// the transport mode is known, then be delivered in the order it was queued.
func TestWSNotificationQueue_FlushesInOrderOnPlaintext(t *testing.T) {
	t.Parallel()

	m := newWebSocketSession()
	var session *melody.Session
	ready := make(chan struct{})
	m.HandleConnect(func(s *melody.Session) {
		s.Set(melodySessionAuthStateKey, webSocketAuthUnsettled)
		s.Set(melodySessionNotifQueueKey, &wsNotificationQueue{})
		session = s
		close(ready)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_ = m.HandleRequest(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer func() { _ = m.Close() }()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	conn := dialWS(t, "ws://"+u.Host+"/api")
	defer func() { _ = conn.Close() }()
	<-ready

	writeNotificationToSession(session, []byte(`{"n":1}`))
	writeNotificationToSession(session, []byte(`{"n":2}`))

	settleWebSocketTransport(session, webSocketAuthPlaintext, false)
	writeNotificationToSession(session, []byte(`{"n":3}`))

	got := make([]string, 0, 3)
	for range 3 {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		_, msg, readErr := conn.ReadMessage()
		require.NoError(t, readErr)
		got = append(got, string(msg))
	}
	assert.Equal(t, []string{`{"n":1}`, `{"n":2}`, `{"n":3}`}, got,
		"queued notifications must be delivered before the ones that follow the settle")
}

// The point of queueing: a client that turns out to have negotiated encryption
// must never see the plaintext that was broadcast while its mode was unknown.
func TestWSNotificationQueue_DiscardsOnEncrypted(t *testing.T) {
	t.Parallel()

	m := newWebSocketSession()
	var session *melody.Session
	ready := make(chan struct{})
	m.HandleConnect(func(s *melody.Session) {
		s.Set(melodySessionAuthStateKey, webSocketAuthUnsettled)
		s.Set(melodySessionNotifQueueKey, &wsNotificationQueue{})
		session = s
		close(ready)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_ = m.HandleRequest(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer func() { _ = m.Close() }()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	conn := dialWS(t, "ws://"+u.Host+"/api")
	defer func() { _ = conn.Close() }()
	<-ready

	writeNotificationToSession(session, []byte(`{"secret":true}`))
	settleWebSocketTransport(session, webSocketAuthEncrypted, false)
	// A later broadcast has no encryption session attached in this harness, so
	// writeNotificationFrame drops it too: nothing may reach the wire.
	writeNotificationToSession(session, []byte(`{"secret":false}`))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err, "plaintext queued before an encrypted upgrade must be discarded")
}

// The grace timer stands down once the client has spoken, so an encrypted
// session cannot be flipped back to plaintext by a late firing.
func TestSettleWebSocketTransport_GraceDoesNotOverrideEncrypted(t *testing.T) {
	t.Parallel()

	m := newWebSocketSession()
	var session *melody.Session
	ready := make(chan struct{})
	m.HandleConnect(func(s *melody.Session) {
		s.Set(melodySessionAuthStateKey, webSocketAuthUnsettled)
		s.Set(melodySessionNotifQueueKey, &wsNotificationQueue{})
		session = s
		close(ready)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_ = m.HandleRequest(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer func() { _ = m.Close() }()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	conn := dialWS(t, "ws://"+u.Host+"/api")
	defer func() { _ = conn.Close() }()
	<-ready

	settleWebSocketTransport(session, webSocketAuthEncrypted, false)
	settleWebSocketTransport(session, webSocketAuthPlaintext, true)
	assert.Equal(t, webSocketAuthEncrypted, getWebSocketAuthState(session))
}
