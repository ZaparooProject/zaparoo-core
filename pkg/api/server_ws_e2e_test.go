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
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/gorilla/websocket"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRemoteListener wraps a real listener so tests can pretend connections
// arrived from a non-loopback address. Required because melody/net.http
// expose RemoteAddr through the underlying net.Conn — the only clean way
// to override it is at the listener layer.
type fakeRemoteListener struct {
	net.Listener
	fakeAddr net.Addr
}

func (l *fakeRemoteListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err //nolint:wrapcheck // pass-through wrapper for tests
	}
	return &fakeRemoteConn{Conn: c, fakeAddr: l.fakeAddr}, nil
}

type fakeRemoteConn struct {
	net.Conn
	fakeAddr net.Addr
}

func (c *fakeRemoteConn) RemoteAddr() net.Addr { return c.fakeAddr }

// startWSServer spins up a minimal httptest server whose only message
// handler is decryptIncomingFrame. fakeRemote, if non-nil, replaces the
// connection's RemoteAddr to simulate a non-loopback client.
func startWSServer(
	t *testing.T,
	encryptionEnabled bool,
	gateway *apimiddleware.EncryptionGateway,
	fakeRemote net.Addr,
) (wsURL string, cleanup func()) {
	t.Helper()

	m := melody.New()
	m.HandleMessage(func(s *melody.Session, msg []byte) {
		clientIP := apimiddleware.ParseRemoteIP(s.Request.RemoteAddr)
		isLocal := apimiddleware.IsLoopbackAddr(s.Request.RemoteAddr)
		var sourceIP string
		if clientIP != nil {
			sourceIP = clientIP.String()
		}
		pt, _, ok := decryptIncomingFrame(s, msg, gateway, encryptionEnabled, isLocal, sourceIP)
		if !ok {
			return
		}
		// Echo plaintext back so the loopback bypass test can observe success.
		_ = s.Write(pt)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_ = m.HandleRequest(w, r)
	})

	srv := httptest.NewUnstartedServer(mux)
	if fakeRemote != nil {
		var lc net.ListenConfig
		ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		srv.Listener = &fakeRemoteListener{Listener: ln, fakeAddr: fakeRemote}
	}
	srv.Start()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	wsURL = "ws://" + u.Host + "/api"

	cleanup = func() {
		_ = m.Close()
		srv.Close()
	}
	return wsURL, cleanup
}

// dialWS opens a gorilla/websocket connection. Caller must close the conn.
func dialWS(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return c
}

// TestWSEncryption_RemotePlaintextRejectedWhenEnabled pins:
// remote IP + encryption=true + plaintext frame → -32002 plaintext error,
// then connection closed.
func TestWSEncryption_RemotePlaintextRejectedWhenEnabled(t *testing.T) {
	t.Parallel()

	gateway := apimiddleware.NewEncryptionGateway(helpers.NewMockUserDBI())
	fakeRemote := &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 12345}

	wsURL, cleanup := startWSServer(t, true, gateway, fakeRemote)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()

	// Send a plaintext JSON-RPC request — not an encrypted first frame.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage,
		[]byte(`{"jsonrpc":"2.0","method":"version","id":1}`)))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err, "server must send a plaintext error before closing")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(msg, &parsed))
	errObj, ok := parsed["error"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(-32002), errObj["code"], 0,
		"remote plaintext on encryption=true must trigger -32002")

	// Subsequent read should fail (server closed the connection).
	_, _, err = conn.ReadMessage()
	require.Error(t, err)
}

// TestWSEncryption_UnsupportedVersionPlaintextError pins:
// well-formed first frame with v=2 → -32001 plaintext error + close.
func TestWSEncryption_UnsupportedVersionPlaintextError(t *testing.T) {
	t.Parallel()

	gateway := apimiddleware.NewEncryptionGateway(helpers.NewMockUserDBI())
	fakeRemote := &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 12345}

	wsURL, cleanup := startWSServer(t, true, gateway, fakeRemote)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()

	frame := apimiddleware.EncryptedFirstFrame{
		Version:     2,
		Ciphertext:  "AA==",
		AuthToken:   "00000000-0000-0000-0000-000000000000",
		SessionSalt: strings.Repeat("A", 24), // base64 of 16 bytes
	}
	//nolint:gosec // G117 false positive: hardcoded all-zero UUID, not a credential
	body, err := json.Marshal(frame)
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, body))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(msg, &parsed))
	errObj, ok := parsed["error"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(-32001), errObj["code"], 0,
		"unsupported encryption version must trigger -32001")

	// data.supported must be present per JSON-RPC 2.0 §5.1.
	errData, ok := errObj["data"].(map[string]any)
	require.True(t, ok)
	supported, ok := errData["supported"].([]any)
	require.True(t, ok)
	require.Len(t, supported, 1)
	assert.InDelta(t, float64(1), supported[0], 0)
}

// TestWSEncryption_LoopbackPlaintextAllowedWhenEnabled pins the spec
// contract: even with encryption=true, plaintext from 127.0.0.1 works
// without pairing. Run without overriding RemoteAddr — httptest naturally
// uses 127.0.0.1.
func TestWSEncryption_LoopbackPlaintextAllowedWhenEnabled(t *testing.T) {
	t.Parallel()

	gateway := apimiddleware.NewEncryptionGateway(helpers.NewMockUserDBI())

	wsURL, cleanup := startWSServer(t, true, gateway, nil)
	defer cleanup()

	conn := dialWS(t, wsURL)
	defer func() { _ = conn.Close() }()

	plaintext := []byte(`{"jsonrpc":"2.0","method":"version","id":1}`)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, plaintext))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, got, err := conn.ReadMessage()
	require.NoError(t, err, "loopback plaintext must be accepted on encryption=true")
	assert.Equal(t, plaintext, got, "test echo handler returns the plaintext unchanged")
}
