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

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	fakeCoreAddr     = "11:22:33:44:55:66"
	fakeCoreDeviceID = "core-under-test"
)

// What the fake Core does with a plaintext request that is not pairing.
const (
	plainDrop   = "drop"   // drop the link, as Core does on Linux
	plainNotice = "notice" // answer with a plaintext error, as on Windows
	plainAnswer = "answer" // answer it, which Core must never do
	plainIgnore = "ignore" // say nothing
)

type fakeCoreOptions struct {
	// plain overrides the answer to a plaintext request when it returns
	// handled; pairing falls through to the real PairingManager otherwise.
	plain func(method string) (result json.RawMessage, rpcErr *rpcError, handled bool)
	// rxWrite, when set, can fail a client write before the core sees it.
	rxWrite func(chunk []byte) error
	// onPlaintext is one of the plain* constants; plainDrop when empty.
	onPlaintext string
	// transport is the label the core binds sessions to; "ble" when empty.
	transport string
	// version is how "version" is answered: a result when empty, an
	// error response for "error" or for all but the first with
	// "error-later", a dropped link for "drop".
	version string
	// stall is how long the core waits for an acknowledgement before it
	// gives up on the client and drops it.
	stall time.Duration
	// bigSize is the size of the result of the "big" method.
	bigSize int
	// mtu is what the TX characteristic reports.
	mtu int
	// window is the acknowledgement window the Info characteristic
	// advertises and the core enforces.
	window int
	// chatty sends a notification ahead of every response.
	chatty bool
	// dropOnPing drops the link instead of answering a heartbeat.
	dropOnPing bool
}

// fakeCore answers like Core's BLE GATT service, using the real chunk
// framing, the real PairingManager and the real EncryptionGateway, so the
// client is tested against the protocol rather than against itself.
type fakeCore struct {
	gateway   *apimiddleware.EncryptionGateway
	pairing   *api.PairingManager
	central   *mocks.FakeCentral
	dev       *mocks.FakeDevice
	rx        *mocks.FakeCharacteristic
	tx        *mocks.FakeCharacteristic
	info      *mocks.FakeCharacteristic
	clients   map[string]*database.Client
	data      chan []byte
	acked     chan struct{}
	session   *apimiddleware.ClientSession
	opts      fakeCoreOptions
	versions  int
	pings     atomic.Int32
	dataIn    atomic.Int32
	lastAck   atomic.Uint32
	sent      atomic.Uint32
	tag       atomic.Uint32
	chunker   apigatt.Chunker
	clientsMu syncutil.Mutex
}

func newFakeCore(t *testing.T, o *fakeCoreOptions) *fakeCore {
	t.Helper()
	opts := *o
	if opts.onPlaintext == "" {
		opts.onPlaintext = plainDrop
	}
	if opts.transport == "" {
		opts.transport = apimiddleware.TransportBLE
	}
	if opts.stall == 0 {
		opts.stall = 5 * time.Second
	}
	if opts.window == 0 {
		opts.window = 4
	}
	if opts.mtu == 0 {
		opts.mtu = 64
	}
	if opts.bigSize == 0 {
		opts.bigSize = 4000
	}

	c := &fakeCore{
		opts:    opts,
		central: mocks.NewFakeCentral(),
		dev:     mocks.NewFakeDevice(fakeCoreAddr),
		rx:      mocks.NewFakeCharacteristic(),
		tx:      mocks.NewFakeCharacteristic(),
		info:    mocks.NewFakeCharacteristic(),
		clients: make(map[string]*database.Client),
		data:    make(chan []byte, 4096),
		acked:   make(chan struct{}, 1),
	}

	db := helpers.NewMockUserDBI()
	db.On("CountClients").Return(0, nil).Maybe()
	db.On("CreateClient", mock.AnythingOfType("*database.Client")).
		Run(func(args mock.Arguments) {
			client, ok := args.Get(0).(*database.Client)
			if ok {
				c.addClient(client)
			}
		}).Return(nil).Maybe()
	lookup := db.On("GetClientByToken", mock.Anything).Maybe()
	lookup.Run(func(args mock.Arguments) {
		client := c.client(args.String(0))
		if client == nil {
			lookup.ReturnArguments = mock.Arguments{nil, errors.New("unknown token")}
			return
		}
		lookup.ReturnArguments = mock.Arguments{client, nil}
	})
	c.pairing = api.NewPairingManager(db, nil)
	c.gateway = apimiddleware.NewEncryptionGateway(db)

	info := apigatt.NewInfo(fakeCoreDeviceID)
	info.Window = opts.window
	raw, err := json.Marshal(info)
	require.NoError(t, err)
	c.info.Value = raw
	c.tx.Link = opts.mtu
	c.rx.OnWrite = c.onWrite
	c.dev.Name = "Zaparoo Test"
	c.dev.AddCharacteristic(apigatt.ServiceUUID, apigatt.RXCharUUID, c.rx)
	c.dev.AddCharacteristic(apigatt.ServiceUUID, apigatt.TXCharUUID, c.tx)
	c.dev.AddCharacteristic(apigatt.ServiceUUID, apigatt.InfoCharUUID, c.info)
	c.central.AddDevice(c.dev)

	go c.serve(t.Context())
	return c
}

func (c *fakeCore) addClient(client *database.Client) {
	c.clientsMu.Lock()
	defer c.clientsMu.Unlock()
	c.clients[client.AuthToken] = client
}

func (c *fakeCore) client(token string) *database.Client {
	c.clientsMu.Lock()
	defer c.clientsMu.Unlock()
	return c.clients[token]
}

// seedCredentials registers a paired client and writes the credentials
// file bletest would have saved for it.
func (c *fakeCore) seedCredentials(t *testing.T) string {
	t.Helper()
	key := make([]byte, crypto.PairingKeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	c.addClient(&database.Client{
		ClientID: "client-1", ClientName: "bletest", AuthToken: "token-1", Role: "admin", PairingKey: key,
	})
	path := filepath.Join(t.TempDir(), "creds.json")
	creds := &credentials{
		DeviceID: fakeCoreDeviceID, AuthToken: "token-1", PairingKey: base64.StdEncoding.EncodeToString(key),
	}
	require.NoError(t, creds.save(path))
	return path
}

// dial connects a link to the fake core.
func (c *fakeCore) dial(t *testing.T) *link {
	t.Helper()
	l, err := dial(t.Context(), c.central, linkOptions{address: fakeCoreAddr, withResponse: true})
	require.NoError(t, err)
	t.Cleanup(l.close)
	return l
}

// onWrite is the RX characteristic: acknowledgements are handled at once,
// as Core's write handler does, and data goes to the serving goroutine.
func (c *fakeCore) onWrite(chunk []byte, _ bool) error {
	if c.opts.rxWrite != nil {
		if err := c.opts.rxWrite(chunk); err != nil {
			return err
		}
	}
	h, _, err := apigatt.ParseChunk(chunk)
	if err != nil {
		return nil //nolint:nilerr // a malformed write is the core's problem, not the writer's
	}
	c.tag.Store(uint32(h.Tag))
	if !h.Ack {
		c.dataIn.Add(1)
		c.data <- chunk
		return nil
	}
	sent := uint16(c.sent.Load())    //nolint:gosec // holds a uint16
	last := uint16(c.lastAck.Load()) //nolint:gosec // holds a uint16
	if h.Seq-last > sent-last {
		// An acknowledgement of chunks never sent ends the session.
		c.dev.Drop()
		return nil
	}
	c.lastAck.Store(uint32(h.Seq))
	select {
	case c.acked <- struct{}{}:
	default:
	}
	return nil
}

func (c *fakeCore) serve(ctx context.Context) {
	reasm := apigatt.NewReassembler(0)
	for {
		var chunk []byte
		select {
		case <-ctx.Done():
			return
		case chunk = <-c.data:
		}
		h, payload, err := apigatt.ParseChunk(chunk)
		if err != nil {
			continue
		}
		msgs, err := reasm.Push(h, payload)
		if err != nil {
			c.dev.Drop()
			return
		}
		for _, msg := range msgs {
			if !c.handle(msg) {
				return
			}
		}
	}
}

// handle answers one message and reports whether to keep serving.
func (c *fakeCore) handle(msg []byte) bool {
	if c.session != nil {
		pt, err := c.session.DecryptFrame(msg)
		if err != nil {
			c.dev.Drop()
			return false
		}
		return c.dispatch(pt)
	}
	if msg[0] == '{' {
		return c.handlePlain(msg)
	}
	frame, err := apimiddleware.ParseBinaryFirstFrame(msg)
	if err != nil {
		c.dev.Drop()
		return false
	}
	cs, pt, err := c.gateway.EstablishBinarySession(frame, "ble:"+fakeCoreAddr, c.opts.transport)
	if err != nil {
		// Core answers a refused first frame in plaintext before it closes.
		c.sendJSON(map[string]any{
			"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32001, "message": err.Error()},
		})
		return true
	}
	c.session = cs
	return c.dispatch(pt)
}

func (c *fakeCore) handlePlain(msg []byte) bool {
	var req rpcMessage
	if err := json.Unmarshal(msg, &req); err != nil {
		c.dev.Drop()
		return false
	}
	if c.opts.plain != nil {
		if result, rpcErr, handled := c.opts.plain(req.Method); handled {
			c.reply(req.ID, result, rpcErr)
			return true
		}
	}
	switch req.Method {
	case "pair.start":
		c.reply(c.pairStep(&req, c.pairing.HandlePairStart()))
		return true
	case "pair.finish":
		c.reply(c.pairStep(&req, c.pairing.HandlePairFinish()))
		return true
	}
	switch c.opts.onPlaintext {
	case plainNotice:
		c.sendJSON(map[string]any{
			"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32600, "message": "encryption required"},
		})
	case plainAnswer:
		c.reply(req.ID, json.RawMessage(`{"version":"plain"}`), nil)
	case plainIgnore:
	default:
		c.dev.Drop()
		return false
	}
	return true
}

// pairStep runs a pairing method through Core's own HTTP handler, which is
// what Core's BLE transport does with the same parameters.
func (*fakeCore) pairStep(req *rpcMessage, h http.HandlerFunc) (id, result json.RawMessage, rpcErr *rpcError) {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(req.Params)))
	body := bytes.TrimSpace(rec.Body.Bytes())
	if rec.Code != http.StatusOK {
		var failed struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &failed)
		return req.ID, nil, &rpcError{Code: -32000, Message: failed.Error}
	}
	return req.ID, body, nil
}

func (c *fakeCore) dispatch(pt []byte) bool {
	if string(pt) == "ping" {
		c.pings.Add(1)
		if c.opts.dropOnPing {
			c.dev.Drop()
			return false
		}
		c.notify()
		return c.sendEncrypted([]byte("pong"))
	}
	var req rpcMessage
	if err := json.Unmarshal(pt, &req); err != nil {
		c.dev.Drop()
		return false
	}
	if c.opts.chatty {
		c.notify()
	}
	var resp map[string]any
	switch req.Method {
	case "version":
		c.versions++
		switch {
		case c.opts.version == "error", c.opts.version == "error-later" && c.versions > 1:
			resp = map[string]any{"error": map[string]any{"code": -32000, "message": "busy"}}
		case c.opts.version == "drop":
			c.dev.Drop()
			return false
		default:
			resp = map[string]any{"result": map[string]string{"version": "fake"}}
		}
	case "big":
		resp = map[string]any{"result": strings.Repeat("y", c.opts.bigSize)}
	case "garbage":
		return c.sendEncrypted([]byte("not json"))
	case "drop":
		c.dev.Drop()
		return false
	default:
		resp = map[string]any{"error": map[string]any{"code": -32601, "message": "method not found"}}
	}
	resp["jsonrpc"] = "2.0"
	resp["id"] = req.ID
	data, err := json.Marshal(resp)
	if err != nil {
		return false
	}
	return c.sendEncrypted(data)
}

func (c *fakeCore) notify() {
	c.sendEncrypted([]byte(`{"jsonrpc":"2.0","method":"tokens.added","params":{"uid":"abc"}}`))
}

func (c *fakeCore) reply(id, result json.RawMessage, rpcErr *rpcError) {
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	c.sendJSON(resp)
}

func (c *fakeCore) sendJSON(v any) {
	data, err := json.Marshal(v)
	if err == nil {
		c.send(data)
	}
}

func (c *fakeCore) sendEncrypted(pt []byte) bool {
	frame, err := c.session.EncryptOutgoingFrame(pt)
	if err != nil {
		return false
	}
	return c.send(frame)
}

// send chunks one message onto TX, holding to the acknowledgement window
// and dropping a client that stops acknowledging, as Core does.
func (c *fakeCore) send(msg []byte) bool {
	c.chunker.Tag = uint16(c.tag.Load()) //nolint:gosec // holds a uint16
	err := c.chunker.Split(msg, c.opts.mtu, func(chunk []byte) error {
		for {
			h, _, _ := apigatt.ParseChunk(chunk)
			if int(h.Seq-uint16(c.lastAck.Load())) < c.opts.window { //nolint:gosec // holds a uint16
				break
			}
			select {
			case <-c.acked:
			case <-time.After(c.opts.stall):
				c.dev.Drop()
				return errStopSending
			}
		}
		c.sent.Add(1)
		c.tx.Push(chunk)
		return nil
	})
	return err == nil
}
