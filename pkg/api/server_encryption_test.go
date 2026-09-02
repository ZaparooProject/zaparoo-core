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
	"github.com/gorilla/websocket"
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

// testEncryptionSourceIP is the client address the test frames are built for.
// EstablishSession binds a session to it, so callers driving the frame through
// the server have to present the same one.
const testEncryptionSourceIP = "192.168.1.50"

// testEncryptionFirstFrame carries everything needed to drive a real encrypted
// first frame through the server: the gateway that will accept it, the frame
// itself, and the client-side cipher state for reading what comes back.
type testEncryptionFirstFrame struct {
	gateway *apimiddleware.EncryptionGateway
	secrets *testEncryptionPeerSecrets
	frame   apimiddleware.EncryptedFirstFrame
}

// establishTestEncryptionSession constructs a real *apimiddleware.ClientSession
// the same way the production server does: pair a fake client, build a valid
// encrypted first frame, and run it through EncryptionGateway.EstablishSession.
// Returns the resulting session and the matching client-side cipher state.
func establishTestEncryptionSession(t *testing.T) (*apimiddleware.ClientSession, *testEncryptionPeerSecrets) {
	t.Helper()

	first := newTestEncryptionFirstFrame(t)
	cs, _, err := first.gateway.EstablishSession(first.frame, testEncryptionSourceIP)
	require.NoError(t, err)
	require.NotNil(t, cs)
	return cs, first.secrets
}

// newTestEncryptionFirstFrame pairs a fake client and builds the encrypted
// first frame it would send, without establishing the session, so a test can
// hand the frame to the code under test instead of the gateway.
func newTestEncryptionFirstFrame(t *testing.T) *testEncryptionFirstFrame {
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

	return &testEncryptionFirstFrame{
		gateway: mgr,
		frame: apimiddleware.EncryptedFirstFrame{
			Version:     apimiddleware.EncryptionProtoVersion,
			Ciphertext:  base64.StdEncoding.EncodeToString(ct),
			AuthToken:   c.AuthToken,
			SessionSalt: base64.StdEncoding.EncodeToString(salt),
		},
		secrets: &testEncryptionPeerSecrets{
			s2cGCM:   clientS2C,
			s2cNonce: keys.S2CNonce,
			aad:      aad,
		},
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

	queue := getNotificationQueue(session)
	require.NotNil(t, queue)
	assert.Empty(t, queue.pending, "settling as encrypted must drop what was held, not keep it for a later flush")

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

// The settle grace resolves a silent session as plaintext, so a client whose
// encrypted first frame arrives after it expires does see the queue flushed in
// the clear. That window is the accepted trade-off, but it has to stay a
// window: once the late frame establishes encryption, everything after it is
// encrypted, so the exposure cannot continue for the life of the session.
func TestSettleWebSocketTransport_LateEncryptedUpgradeEndsPlaintext(t *testing.T) {
	t.Parallel()

	cs, clientSecrets := establishTestEncryptionSession(t)

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

	// Broadcast while the client is still silent, then let the grace expire.
	beforeUpgrade := []byte(`{"jsonrpc":"2.0","method":"tokens.added"}`)
	writeNotificationToSession(session, beforeUpgrade)
	settleWebSocketTransport(session, webSocketAuthPlaintext, true)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, beforeUpgrade, msg,
		"the grace settles a silent session as plaintext; this is the accepted window")

	// The delayed encrypted first frame finally arrives.
	setClientSession(session, cs)
	settleWebSocketTransport(session, webSocketAuthEncrypted, false)
	assert.Equal(t, webSocketAuthEncrypted, getWebSocketAuthState(session))

	afterUpgrade := []byte(`{"jsonrpc":"2.0","method":"tokens.removed"}`)
	writeNotificationToSession(session, afterUpgrade)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err = conn.ReadMessage()
	require.NoError(t, err)
	require.NotEqual(t, afterUpgrade, msg, "nothing may go out in the clear after the upgrade")

	var frame apimiddleware.EncryptedFrame
	require.NoError(t, json.Unmarshal(msg, &frame))
	ct, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	require.NoError(t, err)
	decrypted, err := crypto.Decrypt(
		clientSecrets.s2cGCM, clientSecrets.s2cNonce, 0, ct, clientSecrets.aad,
	)
	require.NoError(t, err)
	assert.Equal(t, afterUpgrade, decrypted)
}

// newQueuedTestSession dials a melody session set up the way the WebSocket
// upgrade sets one up for optional encryption: transport mode unknown, with a
// queue attached. Returns the server-side session and the client connection.
func newQueuedTestSession(t *testing.T) (*melody.Session, *websocket.Conn) {
	t.Helper()

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
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = m.Close() })

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	conn := dialWS(t, "ws://"+u.Host+"/api")
	t.Cleanup(func() { _ = conn.Close() })
	<-ready
	return session, conn
}

// The queue holds notifications for a session whose mode is unknown, so it has
// to bound what one session can hold and it has to own its copy: the caller's
// buffer is not guaranteed to survive until the flush.
func TestWSNotificationQueue_EnqueueBounds(t *testing.T) {
	t.Parallel()

	q := &wsNotificationQueue{}

	first := []byte(`{"n":0}`)
	require.True(t, q.enqueue(first))
	first[3] = 'X'
	assert.Equal(t, `{"n":0}`, string(q.pending[0]),
		"the queue must copy, so a caller reusing its buffer cannot rewrite a held notification")

	for range webSocketPendingNotifLimit {
		require.True(t, q.enqueue([]byte(`{"n":1}`)))
	}
	assert.Len(t, q.pending, webSocketPendingNotifLimit,
		"a burst during the grace must not grow the queue without bound")

	q.settled = true
	assert.False(t, q.enqueue([]byte(`{"n":2}`)),
		"once the mode is settled the caller writes directly instead of queueing")
	assert.Len(t, q.pending, webSocketPendingNotifLimit, "a refused enqueue must not store anything")
}

// A session that disconnects while notifications are held cannot be written
// to. The flush has to give up on the first failure rather than work through
// the rest of the queue against a dead connection.
func TestSettleWebSocketTransport_FlushStopsOnWriteError(t *testing.T) {
	t.Parallel()

	session, _ := newQueuedTestSession(t)
	writeNotificationToSession(session, []byte(`{"n":1}`))
	writeNotificationToSession(session, []byte(`{"n":2}`))

	require.NoError(t, session.Close())
	require.Eventually(t, session.IsClosed, time.Second, 5*time.Millisecond)

	settleWebSocketTransport(session, webSocketAuthPlaintext, false)

	queue := getNotificationQueue(session)
	require.NotNil(t, queue)
	assert.True(t, queue.settled, "the mode is settled even when the flush cannot be delivered")
	assert.Empty(t, queue.pending, "a failed flush must not leave the queue holding notifications")
}

// The settle grace exists for sessions whose mode is merely unknown. A session
// that is required to authenticate is pending, not unsettled, and must never
// be handed a timer that would settle it as plaintext without a client frame.
func TestStartWebSocketSettleGrace_OnlyArmsForUnsettled(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		state    webSocketAuthState
		name     string
		wantArms bool
	}{
		{name: "unsettled arms", state: webSocketAuthUnsettled, wantArms: true},
		{name: "required encryption does not arm", state: webSocketAuthPending},
		{name: "already plaintext does not arm", state: webSocketAuthPlaintext},
		{name: "already encrypted does not arm", state: webSocketAuthEncrypted},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session, _ := newQueuedTestSession(t)
			session.Set(melodySessionAuthStateKey, tt.state)
			startWebSocketSettleGrace(session, time.Hour)

			_, armed := session.Get(melodySessionSettleTimerKey)
			assert.Equal(t, tt.wantArms, armed)
			if armed {
				stopWebSocketSettleGrace(session)
			}
		})
	}
}

// Not every melody session carries a queue: only the ones the upgrade handler
// builds do. Settling must still record the mode for the others rather than
// silently doing nothing, and the grace timer must still stand down once the
// client has spoken.
func TestSettleWebSocketTransport_WithoutQueue(t *testing.T) {
	t.Parallel()

	m := newWebSocketSession()
	var session *melody.Session
	ready := make(chan struct{})
	m.HandleConnect(func(s *melody.Session) {
		s.Set(melodySessionAuthStateKey, webSocketAuthUnsettled)
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

	require.Nil(t, getNotificationQueue(session))

	settleWebSocketTransport(session, webSocketAuthEncrypted, false)
	require.Equal(t, webSocketAuthEncrypted, getWebSocketAuthState(session))

	settleWebSocketTransport(session, webSocketAuthPlaintext, true)
	assert.Equal(t, webSocketAuthEncrypted, getWebSocketAuthState(session),
		"a late grace must not downgrade a session that already spoke")
}

// The reported bug, driven through the production entry point: a paired client
// negotiates encryption on its first frame even though the server only treats
// encryption as optional. Anything broadcast before that frame arrived is
// queued, and establishing the session has to discard it rather than let it
// reach a client that has already committed to encrypted mode.
func TestDecryptIncomingFrame_EncryptedFirstFrameDiscardsQueuedPlaintext(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrame(t)
	session, conn := newQueuedTestSession(t)

	queued := []byte(`{"jsonrpc":"2.0","method":"tokens.added"}`)
	writeNotificationToSession(session, queued)

	//nolint:gosec // G117 false positive: the auth token is a test fixture, not a credential
	body, err := json.Marshal(first.frame)
	require.NoError(t, err)
	pt, cs, ok := decryptIncomingFrame(
		session, body, first.gateway, false, false, testEncryptionSourceIP,
	)
	require.True(t, ok, "an encrypted first frame is accepted when encryption is optional")
	require.NotNil(t, cs)
	assert.JSONEq(t, `{"jsonrpc":"2.0","method":"version","id":1}`, string(pt))
	assert.Equal(t, webSocketAuthEncrypted, getWebSocketAuthState(session))

	// Assert on the queue rather than only on the wire: with the client
	// session attached, later notifications are encrypted whether or not the
	// queue was emptied, so a queue still holding plaintext would go unnoticed
	// until something settled it and flushed it in the clear.
	queue := getNotificationQueue(session)
	require.NotNil(t, queue)
	assert.True(t, queue.settled, "establishing encryption settles the transport mode")
	assert.Empty(t, queue.pending, "queued plaintext must be discarded by the upgrade, not left to flush later")

	after := []byte(`{"jsonrpc":"2.0","method":"tokens.removed"}`)
	writeNotificationToSession(session, after)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	require.NotEqual(t, queued, msg, "plaintext queued before the upgrade must be discarded, not flushed")

	var frame apimiddleware.EncryptedFrame
	require.NoError(t, json.Unmarshal(msg, &frame))
	ct, err := base64.StdEncoding.DecodeString(frame.Ciphertext)
	require.NoError(t, err)
	decrypted, err := crypto.Decrypt(
		first.secrets.s2cGCM, first.secrets.s2cNonce, 0, ct, first.secrets.aad,
	)
	require.NoError(t, err)
	assert.Equal(t, after, decrypted, "the first frame on the wire is the one sent after the upgrade")
}
