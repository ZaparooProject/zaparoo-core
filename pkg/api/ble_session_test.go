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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/crypto"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/bluez"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/schollz/pake/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bleTestTimeout = 5 * time.Second

// bleTestRig is a BLE transport served on a fake peripheral. Notifications
// are routed to clients by tag, the way each phone filters the shared TX
// characteristic, so clients can be exercised in any order.
type bleTestRig struct {
	transport  *bleTransport
	peripheral *mocks.FakePeripheral
	clock      *clockwork.FakeClock
	cfg        *config.Instance
	inboxes    map[uint16]chan []byte
	mu         syncutil.Mutex
}

type bleTestRigOptions struct {
	methodMap   *MethodMap
	encGateway  *apimiddleware.EncryptionGateway
	pairing     *PairingManager
	notifBroker *broker.Broker
}

func newBLETestRig(t *testing.T, opts bleTestRigOptions) *bleTestRig {
	t.Helper()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	st, _ := state.NewState(nil, "test-boot")
	t.Cleanup(st.StopService)

	methodMap := opts.methodMap
	if methodMap == nil {
		methodMap = &MethodMap{}
	}
	encGateway := opts.encGateway
	if encGateway == nil {
		encGateway = apimiddleware.NewEncryptionGateway(helpers.NewMockUserDBI())
	}

	clock := clockwork.NewFakeClock()
	transport := newBLETransport(&bleTransportDeps{
		core:        &requestDeps{cfg: cfg, st: st},
		methodMap:   methodMap,
		encGateway:  encGateway,
		pairing:     opts.pairing,
		notifBroker: opts.notifBroker,
		clock:       clock,
	})
	peripheral := mocks.NewFakePeripheral()
	transport.serve(peripheral)
	require.Eventually(t, func() bool { return peripheral.Handler() != nil }, bleTestTimeout, 5*time.Millisecond)
	t.Cleanup(transport.stop)

	rig := &bleTestRig{
		transport:  transport,
		peripheral: peripheral,
		clock:      clock,
		cfg:        cfg,
		inboxes:    make(map[uint16]chan []byte),
	}
	go rig.route(t.Context())
	return rig
}

// route delivers every TX chunk to the inbox of the client whose tag it
// carries. Chunks for unknown tags are dropped, as a phone would.
func (r *bleTestRig) route(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-r.peripheral.Notifications:
			if n.CharUUID != apigatt.TXCharUUID {
				continue
			}
			h, _, err := apigatt.ParseChunk(n.Value)
			if err != nil {
				continue
			}
			r.mu.Lock()
			inbox := r.inboxes[h.Tag]
			r.mu.Unlock()
			if inbox != nil {
				select {
				case inbox <- n.Value:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// bleTestClient drives the rig the way a phone would: chunked writes to RX,
// reassembled notifications from TX filtered by its tag.
type bleTestClient struct {
	rig     *bleTestRig
	reasm   *apigatt.Reassembler
	chunker *apigatt.Chunker
	inbox   chan []byte
	peer    bluez.Peer
	pending [][]byte
	mtu     int
	tag     uint16
	acked   uint16
	// silent stops the client acknowledging what it receives.
	silent bool
}

func (r *bleTestRig) client(address string, tag uint16, mtu int) *bleTestClient {
	inbox := make(chan []byte, 1024)
	r.mu.Lock()
	r.inboxes[tag] = inbox
	r.mu.Unlock()
	return &bleTestClient{
		rig:     r,
		reasm:   apigatt.NewReassembler(0),
		chunker: &apigatt.Chunker{Tag: tag},
		inbox:   inbox,
		peer: bluez.Peer{
			Path:    "/org/bluez/hci0/dev_" + strings.ReplaceAll(address, ":", "_"),
			Address: address,
		},
		tag: tag,
		mtu: mtu,
	}
}

func (c *bleTestClient) send(t *testing.T, msg []byte) {
	t.Helper()
	for _, chunk := range c.chunks(t, msg) {
		c.write(chunk)
	}
}

// chunks splits msg into this client's next chunks without sending them.
func (c *bleTestClient) chunks(t *testing.T, msg []byte) [][]byte {
	t.Helper()
	var chunks [][]byte
	require.NoError(t, c.chunker.Split(msg, c.mtu, func(chunk []byte) error {
		chunks = append(chunks, chunk)
		return nil
	}))
	return chunks
}

func (c *bleTestClient) write(chunk []byte) {
	c.rig.peripheral.Handler().OnWrite(c.peer, apigatt.RXCharUUID, chunk, c.mtu)
}

// openPending opens a session without authenticating it, by sending the
// first chunk of a message and nothing more.
func (c *bleTestClient) openPending(t *testing.T) {
	t.Helper()
	errStop := errors.New("stop after the first chunk")
	err := c.chunker.Split(
		[]byte(`{"jsonrpc":"2.0","method":"pair.start","id":1}`), apigatt.DefaultMTU,
		func(chunk []byte) error {
			c.write(chunk)
			return errStop
		},
	)
	require.ErrorIs(t, err, errStop)
}

// acknowledge tells Core how far this client has received, the way a phone
// would: once half a window has arrived since the last acknowledgement.
func (c *bleTestClient) acknowledge(force bool) {
	if c.silent && !force {
		return
	}
	if next := c.reasm.Next(); force || next-c.acked >= apigatt.SendWindow/2 {
		c.acked = next
		c.write(apigatt.EncodeAck(c.tag, next))
	}
}

// recv returns the next complete message addressed to this client.
func (c *bleTestClient) recv(t *testing.T) []byte {
	t.Helper()
	deadline := time.After(bleTestTimeout)
	for len(c.pending) == 0 {
		select {
		case chunk := <-c.inbox:
			h, payload, err := apigatt.ParseChunk(chunk)
			require.NoError(t, err)
			msgs, err := c.reasm.Push(h, payload)
			require.NoError(t, err)
			c.pending = append(c.pending, msgs...)
			c.acknowledge(false)
		case <-deadline:
			t.Fatal("no message from the transport")
			return nil
		}
	}
	msg := c.pending[0]
	c.pending = c.pending[1:]
	return msg
}

// expectNothing asserts that nothing arrives for this client for a while.
func (c *bleTestClient) expectNothing(t *testing.T) {
	t.Helper()
	select {
	case chunk := <-c.inbox:
		t.Fatalf("unexpected chunk of %d bytes", len(chunk))
	case <-time.After(100 * time.Millisecond):
	}
}

func (c *bleTestClient) session() *bleSession {
	c.rig.transport.mu.Lock()
	defer c.rig.transport.mu.Unlock()
	return c.rig.transport.sessions[c.peer.Path]
}

func (c *bleTestClient) waitClosed(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return c.session() == nil }, bleTestTimeout, 5*time.Millisecond)
}

func decryptS2C(t *testing.T, secrets *testEncryptionPeerSecrets, wire []byte, counter uint64) []byte {
	t.Helper()
	return secrets.decryptBinary(t, wire, counter)
}

func versionMethodMap(t *testing.T) *MethodMap {
	t.Helper()
	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("version", func(requests.RequestEnv) (any, error) {
		return map[string]string{"version": "test"}, nil
	}, false))
	return &methodMap
}

func TestBLESession_EncryptedRequestResponse(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 0x1234, 23)

	client.send(t, first.binary(t))

	var resp models.ResponseObject
	require.NoError(t, json.Unmarshal(decryptS2C(t, first.secrets, client.recv(t), 0), &resp))
	assert.Equal(t, models.NewNumberID(1), resp.ID)
	assert.Equal(t, map[string]any{"version": "test"}, resp.Result)

	// The session is authenticated: a second request on the same session
	// decrypts with the next counter.
	client.send(t, first.secrets.encryptSubsequentBinary(t, []byte(`{"jsonrpc":"2.0","method":"version","id":2}`), 1))
	require.NoError(t, json.Unmarshal(decryptS2C(t, first.secrets, client.recv(t), 1), &resp))
	assert.Equal(t, models.NewNumberID(2), resp.ID)

	s := client.session()
	require.NotNil(t, s)
	s.mu.Lock()
	authState := s.state
	s.mu.Unlock()
	assert.Equal(t, bleAuthEncrypted, authState)
}

func TestBLESession_PingPongAfterAuth(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	client.send(t, first.binary(t))
	client.recv(t)

	client.send(t, first.secrets.encryptSubsequentBinary(t, []byte("ping"), 1))
	assert.Equal(t, "pong", string(decryptS2C(t, first.secrets, client.recv(t), 1)))
}

func TestBLESession_WebSocketFrameIsRejected(t *testing.T) {
	t.Parallel()

	// A first frame bound to the WebSocket transport must not open a BLE
	// session, even with valid credentials.
	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportWebSocket)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	client.send(t, first.binary(t))
	client.waitClosed(t)
	client.expectNothing(t)
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(rig.peripheral.Disconnects(), func(p bluez.Peer) bool {
			return p.Path == client.peer.Path
		})
	}, bleTestTimeout, 5*time.Millisecond)
}

// The JSON envelope the WebSocket transport accepts is not spoken here: BLE
// frames are binary, and anything that opens like JSON is plaintext.
func TestBLESession_JSONEnvelopeIsRejected(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	frameJSON, err := json.Marshal(first.frame) //nolint:gosec // test fixture token
	require.NoError(t, err)
	client.send(t, frameJSON)
	client.waitClosed(t)
	client.expectNothing(t)
}

// A first frame cut short anywhere in its header is refused, not read past.
func TestBLESession_TruncatedFirstFrameIsRejected(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	full := first.binary(t)
	headerEnd := len(full) - len(`{"jsonrpc":"2.0","method":"version","id":1}`) - 16
	for _, n := range []int{1, 5, 17, 18, headerEnd} {
		rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
		client := rig.client("11:22:33:44:55:66", 7, 185)
		client.send(t, full[:n])
		client.waitClosed(t)
	}
}

func TestBLESession_PlaintextRequestIsRejected(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t)})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	client.send(t, []byte(`{"jsonrpc":"2.0","method":"version","id":1}`))
	client.waitClosed(t)
	client.expectNothing(t)
}

func TestBLESession_FramingErrorClosesSession(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	rig.peripheral.Handler().OnWrite(client.peer, apigatt.RXCharUUID, []byte{0xff, 0, 0, 0, 1}, 185)
	client.waitClosed(t)
}

func TestBLESession_IdleBeforeAuthClosesSession(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	// A chunk that starts a message but never finishes it opens the session.
	client.openPending(t)
	require.NotNil(t, client.session())

	rig.clock.Advance(blePendingIdleTimeout - time.Second)
	assert.NotNil(t, client.session())
	rig.clock.Advance(2 * time.Second)
	client.waitClosed(t)
}

func TestBLESession_DisconnectRemovesSession(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, 185)
	client.openPending(t)
	require.NotNil(t, client.session())

	rig.peripheral.Handler().OnDisconnect(client.peer)
	client.waitClosed(t)
	assert.Empty(t, rig.peripheral.Disconnects(), "a peer that left is not disconnected again")
}

func TestBLESession_InfoCharacteristic(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	data, err := rig.peripheral.Handler().OnRead(bluez.Peer{}, apigatt.InfoCharUUID)
	require.NoError(t, err)
	var info apigatt.Info
	require.NoError(t, json.Unmarshal(data, &info))
	assert.Equal(t, rig.cfg.DeviceID(), info.DeviceID)
	assert.Equal(t, apigatt.ProtocolVersion, info.Version)
	assert.Equal(t, apigatt.MaxMessageSize, info.MaxMessage)

	_, err = rig.peripheral.Handler().OnRead(bluez.Peer{}, apigatt.RXCharUUID)
	require.Error(t, err)
}

func TestBLESession_NotificationsOnlyAfterAuth(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	// Open a pending session with a partial message.
	client.openPending(t)
	s := client.session()
	require.NotNil(t, s)

	notif := []byte(`{"jsonrpc":"2.0","method":"media.started","params":{}}`)
	s.sendNotification(models.NotificationStarted, notif)
	client.expectNothing(t)

	// Authenticate, then the same notification goes out encrypted.
	client.send(t, first.binary(t))
	client.recv(t)

	s.sendNotification(models.NotificationStarted, notif)
	assert.JSONEq(t, string(notif), string(decryptS2C(t, first.secrets, client.recv(t), 1)))

	// A large notification is streamed like any other message.
	large := []byte(`{"jsonrpc":"2.0","method":"media.started","params":"` + strings.Repeat("y", 300_000) + `"}`)
	s.sendNotification(models.NotificationStarted, large)
	assert.Equal(t, large, decryptS2C(t, first.secrets, client.recv(t), 2))
}

// authenticate sends the fixture's first frame and consumes the response.
func (c *bleTestClient) authenticate(t *testing.T, first *testEncryptionFirstFrame) {
	t.Helper()
	c.send(t, first.binary(t))
	c.recv(t)
}

func TestBLESession_TwoClientsAreIsolated(t *testing.T) {
	t.Parallel()

	// Two paired clients share one gateway; each has its own key and tag.
	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	second := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	second.frame.AuthToken = "second-token"
	secondClient := &database.Client{
		ClientID:   "second-client",
		ClientName: "Second",
		AuthToken:  "second-token",
		Role:       string(permissions.RoleMember),
		PairingKey: second.pairingKey,
	}
	first.db.On("GetClientByToken", "second-token").Return(secondClient, nil)
	second.secrets.aad = []byte("second-token:" + apimiddleware.TransportBLE)
	second.reencrypt(t, `{"jsonrpc":"2.0","method":"version","id":1}`)

	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	a := rig.client("11:22:33:44:55:66", 0x0a0a, 185)
	b := rig.client("77:88:99:AA:BB:CC", 0x0b0b, 185)

	a.authenticate(t, first)
	b.send(t, second.binary(t))
	bReply := b.recv(t)
	var resp models.ResponseObject
	require.NoError(t, json.Unmarshal(decryptS2C(t, second.secrets, bReply, 0), &resp))
	assert.Equal(t, map[string]any{"version": "test"}, resp.Result)

	// B's reply is unreadable under A's keys, and vice versa.
	_, err := crypto.Decrypt(first.secrets.s2cGCM, first.secrets.s2cNonce, 1, bReply, first.secrets.aad)
	require.Error(t, err)

	// Requests interleave without crossing sessions: each reply carries
	// the requester's tag and decrypts only with its keys.
	a.send(t, first.secrets.encryptSubsequentBinary(t, []byte(`{"jsonrpc":"2.0","method":"version","id":"a2"}`), 1))
	b.send(t, second.secrets.encryptSubsequentBinary(t, []byte(`{"jsonrpc":"2.0","method":"version","id":"b2"}`), 1))
	require.NoError(t, json.Unmarshal(decryptS2C(t, first.secrets, a.recv(t), 1), &resp))
	assert.Equal(t, models.NewStringID("a2"), resp.ID)
	require.NoError(t, json.Unmarshal(decryptS2C(t, second.secrets, b.recv(t), 1), &resp))
	assert.Equal(t, models.NewStringID("b2"), resp.ID)

	rig.transport.mu.Lock()
	sessionCount := len(rig.transport.sessions)
	rig.transport.mu.Unlock()
	assert.Equal(t, 2, sessionCount, "sessions are keyed by peer")
}

func TestBLETransport_BroadcastsThroughBroker(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	source := make(chan models.Notification)
	b := broker.NewBroker(t.Context(), source)
	b.Start()
	t.Cleanup(b.Stop)

	rig := newBLETestRig(t, bleTestRigOptions{
		methodMap: versionMethodMap(t), encGateway: first.gateway, notifBroker: b,
	})
	pending := rig.client("11:22:33:44:55:66", 1, 185)
	pending.openPending(t)
	authed := rig.client("77:88:99:AA:BB:CC", 2, 185)
	authed.authenticate(t, first)

	b.Publish(models.Notification{Method: models.NotificationStarted, Params: json.RawMessage(`{"x":1}`)})

	got := decryptS2C(t, first.secrets, authed.recv(t), 1)
	assert.JSONEq(t, `{"jsonrpc":"2.0","method":"media.started","params":{"x":1}}`, string(got))
	pending.expectNothing(t)
}

func TestBLESession_UnsupportedVersionIsAnswered(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)

	frame := first.binary(t)
	frame[0] = apimiddleware.EncryptionProtoVersion + 1
	client.send(t, frame)

	var resp models.ResponseErrorObject
	require.NoError(t, json.Unmarshal(client.recv(t), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32001, resp.Error.Code)
	client.waitClosed(t)
}

func TestBLESession_OutboundOverflowClosesSession(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, 185)
	client.openPending(t)
	s := client.session()
	require.NotNil(t, s)

	// Nothing is acknowledged, so the writer stalls with a full window and
	// the queue behind it fills.
	var err error
	for i := 0; i < apigatt.SendWindow+bleOutboundMessages+2 && err == nil; i++ {
		err = s.Write([]byte("m"))
	}
	require.ErrorIs(t, err, errBLEOutboundFull)
	client.waitClosed(t)
	require.ErrorIs(t, s.Write([]byte("late")), errBLESessionClosed)
}

// largeMethodMap answers "version" with a result of the given size.
func largeMethodMap(t *testing.T, size int) *MethodMap {
	t.Helper()
	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("version", func(requests.RequestEnv) (any, error) {
		return map[string]string{"blob": strings.Repeat("x", size)}, nil
	}, false))
	return &methodMap
}

func TestBLESession_LargeResponseIsStreamed(t *testing.T) {
	t.Parallel()

	// Larger than the old 256 KiB transport cap and than the WebSocket
	// inbound limit: nothing the API returns is refused for size.
	const size = 5 * 1024 * 1024
	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: largeMethodMap(t, size), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 517)

	client.send(t, first.binary(t))

	var resp models.ResponseObject
	require.NoError(t, json.Unmarshal(decryptS2C(t, first.secrets, client.recv(t), 0), &resp))
	result, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	assert.Len(t, result["blob"], size)
}

func TestBLESession_SendsOnlyWithinAcknowledgedWindow(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: largeMethodMap(t, 20_000), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, apigatt.DefaultMTU)
	client.silent = true

	client.send(t, first.binary(t))

	// With no acknowledgement Core sends one window and stops.
	receive := func(want int) {
		t.Helper()
		for range want {
			select {
			case chunk := <-client.inbox:
				h, payload, parseErr := apigatt.ParseChunk(chunk)
				require.NoError(t, parseErr)
				msgs, pushErr := client.reasm.Push(h, payload)
				require.NoError(t, pushErr)
				client.pending = append(client.pending, msgs...)
			case <-time.After(bleTestTimeout):
				t.Fatal("transport stopped short of the window")
			}
		}
		client.expectNothing(t)
	}
	receive(apigatt.SendWindow)

	// Acknowledging half of it releases exactly that many more.
	client.acked = apigatt.SendWindow / 2
	client.write(apigatt.EncodeAck(client.tag, client.acked))
	receive(apigatt.SendWindow / 2)

	// A client that never acknowledges again is dropped.
	require.NoError(t, rig.clock.BlockUntilContext(t.Context(), 2))
	rig.clock.Advance(bleAckTimeout + time.Second)
	client.waitClosed(t)
}

func TestBLESession_BadAcknowledgementClosesSession(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, 185)
	client.openPending(t)
	require.NotNil(t, client.session())

	// Nothing has been sent, so there is nothing to acknowledge.
	client.write(apigatt.EncodeAck(client.tag, 1))
	client.waitClosed(t)
}

func TestBLESession_ChangedTagClosesSession(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, 185)
	client.openPending(t)
	require.NotNil(t, client.session())

	client.write(apigatt.EncodeAck(client.tag+1, 0))
	client.waitClosed(t)
}

// BlueZ hands every write to its own goroutine, so chunks of back-to-back
// requests reach the session in any order. Encrypted frames only decrypt in
// the order they were sent, so the session must restore it.
func TestBLESession_PipelinedRequestsArriveOutOfOrder(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 517)
	client.authenticate(t, first)

	const pipelined = 20
	chunks := make([][]byte, 0, pipelined)
	for i := 1; i <= pipelined; i++ {
		req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"version","id":%d}`, 100+i)
		frame := first.secrets.encryptSubsequentBinary(t, []byte(req), uint64(i)) //nolint:gosec // small loop index
		// Single-chunk at this MTU, as real requests are.
		chunks = append(chunks, client.chunks(t, frame)...)
	}
	require.Len(t, chunks, pipelined)

	// Newest first, every write on its own goroutine.
	var wg sync.WaitGroup
	for i := len(chunks) - 1; i >= 0; i-- {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.write(chunks[i])
		}()
	}
	wg.Wait()

	seen := make(map[string]bool)
	for i := 1; i <= pipelined; i++ {
		var resp models.ResponseObject
		plaintext := decryptS2C(t, first.secrets, client.recv(t), uint64(i)) //nolint:gosec // small loop index
		require.NoError(t, json.Unmarshal(plaintext, &resp))
		id, err := json.Marshal(resp.ID)
		require.NoError(t, err)
		seen[string(id)] = true
	}
	assert.Len(t, seen, pipelined)
	assert.NotNil(t, client.session(), "the session survived the reordering")
}

func TestBLESession_ResponsesWaitWhileTheLinkIsBackedUp(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	client := rig.client("11:22:33:44:55:66", 7, apigatt.DefaultMTU)
	client.openPending(t)
	s := client.session()
	require.NotNil(t, s)
	require.NoError(t, s.WaitWritable(t.Context()), "an empty queue never waits")

	// Unacknowledged, this message keeps the writer busy and the queue
	// over the high-water mark.
	require.NoError(t, s.Write(make([]byte, bleOutboundHighWater)))

	waited := make(chan error, 1)
	go func() { waited <- s.WaitWritable(t.Context()) }()
	select {
	case err := <-waited:
		t.Fatalf("wait returned while backed up: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, s.WaitWritable(ctx), context.Canceled)

	s.shutdown("test")
	require.ErrorIs(t, <-waited, errBLESessionClosed)
}

func TestBLESession_AuthenticatedIdleTimeout(t *testing.T) {
	t.Parallel()

	first := newTestEncryptionFirstFrameFor(t, apimiddleware.TransportBLE)
	rig := newBLETestRig(t, bleTestRigOptions{methodMap: versionMethodMap(t), encGateway: first.gateway})
	client := rig.client("11:22:33:44:55:66", 7, 185)
	client.authenticate(t, first)

	// The pending timeout no longer applies once authenticated.
	rig.clock.Advance(blePendingIdleTimeout + time.Second)
	require.NotNil(t, client.session())

	// Traffic keeps the session alive; silence past the longer timeout ends it.
	client.send(t, first.secrets.encryptSubsequentBinary(t, []byte("ping"), 1))
	client.recv(t)
	rig.clock.Advance(bleAuthenticatedIdleTimeout - time.Second)
	require.NotNil(t, client.session())
	rig.clock.Advance(2 * time.Second)
	client.waitClosed(t)
}

// blePairingClient is the phone side of the PAKE exchange over BLE.
type blePairingClient struct {
	pake *pake.Pake
	name string
	msgA []byte
	msgB []byte
}

func newBLEPairingClient(t *testing.T, pin, name string) *blePairingClient {
	t.Helper()
	p, err := pake.InitCurve([]byte(pin), 0, pairingCurve)
	require.NoError(t, err)
	msgA, err := crypto.EncodePakeMessage(p.Bytes())
	require.NoError(t, err)
	return &blePairingClient{pake: p, msgA: msgA, name: name}
}

func (c *blePairingClient) startRequest(t *testing.T, id int) []byte {
	t.Helper()
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "pair.start",
		"params":  pairStartRequest{PAKE: base64.StdEncoding.EncodeToString(c.msgA), Name: c.name},
	})
	require.NoError(t, err)
	return req
}

// finishRequest consumes the start response and builds the finish request,
// returning it with the pairing key and server HMAC the client expects.
func (c *blePairingClient) finishRequest(
	t *testing.T, id int, startResp pairStartResponse, wrongPIN bool,
) (req, pairingKey, expectedServerHMAC []byte) {
	t.Helper()
	msgB, err := base64.StdEncoding.DecodeString(startResp.PAKE)
	require.NoError(t, err)
	c.msgB = msgB
	msgBInternal, err := crypto.DecodePakeMessage(msgB)
	require.NoError(t, err)
	require.NoError(t, c.pake.Update(msgBInternal))
	sessionKey, err := c.pake.SessionKey()
	require.NoError(t, err)

	prk, err := hkdf.Extract(sha256.New, sessionKey, slices.Concat(c.msgA, c.msgB))
	require.NoError(t, err)
	confirmKeyA, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmA, sha256.Size)
	require.NoError(t, err)
	confirmKeyB, err := hkdf.Expand(sha256.New, prk, pairingInfoConfirmB, sha256.Size)
	require.NoError(t, err)
	pairingKey, err = hkdf.Expand(sha256.New, prk, pairingInfoPairing, crypto.PairingKeySize)
	require.NoError(t, err)

	clientHMAC := computePairingHMAC(confirmKeyA, "client", c.name, c.msgA, c.msgB)
	if wrongPIN {
		clientHMAC[0] ^= 0xff
	}
	req, err = json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "pair.finish",
		"params": pairFinishRequest{
			Session: startResp.Session,
			Confirm: base64.StdEncoding.EncodeToString(clientHMAC),
		},
	})
	require.NoError(t, err)
	return req, pairingKey, computePairingHMAC(confirmKeyB, "server", c.name, c.msgA, c.msgB)
}

func unmarshalResult[T any](t *testing.T, wire []byte) (T, models.RPCID) {
	t.Helper()
	var envelope struct {
		Result json.RawMessage     `json:"result"`
		Error  *models.ErrorObject `json:"error"`
		ID     models.RPCID        `json:"id"`
	}
	require.NoError(t, json.Unmarshal(wire, &envelope))
	require.Nil(t, envelope.Error, "unexpected error response")
	var out T
	require.NoError(t, json.Unmarshal(envelope.Result, &out))
	return out, envelope.ID
}

func TestBLESession_PairThenAuthenticateOnSameConnection(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	pin, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)

	// The encryption gateway resolves whichever client pairing creates.
	gatewayDB := helpers.NewMockUserDBI()
	gateway := apimiddleware.NewEncryptionGateway(gatewayDB)
	rig := newBLETestRig(t, bleTestRigOptions{
		methodMap: versionMethodMap(t), encGateway: gateway, pairing: harness.mgr,
	})
	client := rig.client("11:22:33:44:55:66", 0x4242, 185)
	phone := newBLEPairingClient(t, pin, "Phone")

	client.send(t, phone.startRequest(t, 1))
	startResp, id := unmarshalResult[pairStartResponse](t, client.recv(t))
	assert.Equal(t, models.NewNumberID(1), id)
	require.NotEmpty(t, startResp.Session)

	finishReq, pairingKey, expectedServerHMAC := phone.finishRequest(t, 2, startResp, false)
	client.send(t, finishReq)
	finishResp, id := unmarshalResult[pairFinishResponse](t, client.recv(t))
	assert.Equal(t, models.NewNumberID(2), id)
	require.NotEmpty(t, finishResp.AuthToken)
	serverHMAC, err := base64.StdEncoding.DecodeString(finishResp.Confirm)
	require.NoError(t, err)
	assert.Equal(t, expectedServerHMAC, serverHMAC)

	created := harness.created.Load()
	require.NotNil(t, created)
	assert.Equal(t, finishResp.AuthToken, created.AuthToken)
	assert.Equal(t, pairingKey, created.PairingKey)
	gatewayDB.On("GetClientByToken", created.AuthToken).Return(created, nil)

	// Still pending: an encrypted first frame with the freshly derived key
	// authenticates without reconnecting.
	salt := make([]byte, crypto.SessionSaltSize)
	for i := range salt {
		salt[i] = byte(i)
	}
	keys, err := crypto.DeriveSessionKeys(pairingKey, salt)
	require.NoError(t, err)
	c2s, err := crypto.NewAEAD(keys.C2SKey)
	require.NoError(t, err)
	s2c, err := crypto.NewAEAD(keys.S2CKey)
	require.NoError(t, err)
	aad := []byte(created.AuthToken + ":" + apimiddleware.TransportBLE)
	ct, err := crypto.Encrypt(c2s, keys.C2SNonce, 0, []byte(`{"jsonrpc":"2.0","method":"version","id":3}`), aad)
	require.NoError(t, err)
	client.send(t, binaryFirstFrame(apimiddleware.EncryptionProtoVersion, salt, created.AuthToken, ct))

	secrets := &testEncryptionPeerSecrets{s2cGCM: s2c, s2cNonce: keys.S2CNonce, aad: aad}
	var resp models.ResponseObject
	require.NoError(t, json.Unmarshal(decryptS2C(t, secrets, client.recv(t), 0), &resp))
	assert.Equal(t, models.NewNumberID(3), resp.ID)
	assert.Equal(t, map[string]any{"version": "test"}, resp.Result)
}

func TestBLESession_WrongPINIsReportedAndCounted(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	pin, _, err := harness.mgr.StartPairing("member")
	require.NoError(t, err)
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr})
	client := rig.client("11:22:33:44:55:66", 9, 185)
	phone := newBLEPairingClient(t, pin, "Phone")

	client.send(t, phone.startRequest(t, 1))
	startResp, _ := unmarshalResult[pairStartResponse](t, client.recv(t))

	finishReq, _, _ := phone.finishRequest(t, 2, startResp, true)
	client.send(t, finishReq)

	var resp models.ResponseErrorObject
	require.NoError(t, json.Unmarshal(client.recv(t), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, JSONRPCErrorPairingFailed.Code, resp.Error.Code)
	data, ok := resp.Error.Data.(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(401), data["status"], 0)
	assert.Equal(t, "wrong PIN", data["message"])

	harness.mgr.mu.Lock()
	attempts := harness.mgr.pinAttempts
	harness.mgr.mu.Unlock()
	assert.Equal(t, 1, attempts)
	assert.NotNil(t, client.session(), "a wrong PIN does not end the connection")
}

func TestBLESession_PairingWithoutPINAndRateLimit(t *testing.T) {
	t.Parallel()

	harness := newPairingHarness(t)
	rig := newBLETestRig(t, bleTestRigOptions{pairing: harness.mgr})
	client := rig.client("11:22:33:44:55:66", 9, 185)
	phone := newBLEPairingClient(t, "000000", "Phone")

	client.send(t, phone.startRequest(t, 1))
	var resp models.ResponseErrorObject
	require.NoError(t, json.Unmarshal(client.recv(t), &resp))
	require.NotNil(t, resp.Error)
	data, ok := resp.Error.Data.(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(400), data["status"], 0)
	assert.Equal(t, "no pairing in progress", data["message"])

	// A start and finish may arrive back to back; a third request within
	// the same second hits the per-connection limiter.
	client.send(t, phone.startRequest(t, 2))
	require.NoError(t, json.Unmarshal(client.recv(t), &resp))
	require.NotNil(t, resp.Error)
	data, ok = resp.Error.Data.(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(400), data["status"], 0)

	client.send(t, phone.startRequest(t, 3))
	require.NoError(t, json.Unmarshal(client.recv(t), &resp))
	require.NotNil(t, resp.Error)
	data, ok = resp.Error.Data.(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(429), data["status"], 0)
}

func TestBLEPairingMethodsAreNotOnTheMethodMap(t *testing.T) {
	t.Parallel()

	methodMap := NewMethodMap()
	for _, method := range []string{blePairStartMethod, blePairFinishMethod} {
		result := processRequestObject(methodMap, requests.RequestEnv{IsLocal: true},
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`))
		require.NotNil(t, result.Error, method)
		assert.Equal(t, JSONRPCErrorMethodNotFound.Code, result.Error.Code, method)
	}
}

func TestBLETransport_Application(t *testing.T) {
	t.Parallel()

	rig := newBLETestRig(t, bleTestRigOptions{})
	app := rig.peripheral.Application()
	require.Len(t, app.Services, 1)
	assert.Equal(t, apigatt.ServiceUUID, app.Services[0].UUID)
	assert.True(t, app.Services[0].Primary)
	uuids := make([]string, 0, 3)
	for _, c := range app.Services[0].Characteristics {
		uuids = append(uuids, c.UUID)
	}
	assert.ElementsMatch(t, []string{apigatt.RXCharUUID, apigatt.TXCharUUID, apigatt.InfoCharUUID}, uuids)

	adv := rig.peripheral.Advertisement()
	assert.Equal(t, []string{apigatt.ServiceUUID}, adv.ServiceUUIDs)
	assert.NotEmpty(t, adv.LocalName)
}

func TestBLETransport_AdvertisesConfiguredName(t *testing.T) {
	t.Parallel()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	st, _ := state.NewState(nil, "test-boot")
	t.Cleanup(st.StopService)
	transport := newBLETransport(&bleTransportDeps{core: &requestDeps{cfg: cfg, st: st}, methodMap: &MethodMap{}})
	t.Cleanup(transport.stop)

	cfg.SetDiscoveryInstanceName("Lounge")
	peripheral := mocks.NewFakePeripheral()
	transport.serve(peripheral)
	require.Eventually(t, func() bool { return peripheral.Handler() != nil }, bleTestTimeout, 5*time.Millisecond)
	assert.Equal(t, "Lounge", peripheral.Advertisement().LocalName, "discovery name is the fallback")

	// The BLE name wins, and is read when advertising starts.
	cfg.SetBLEName("Den")
	replacement := mocks.NewFakePeripheral()
	transport.serve(replacement)
	require.Eventually(t, func() bool { return replacement.Handler() != nil }, bleTestTimeout, 5*time.Millisecond)
	assert.Equal(t, "Den", replacement.Advertisement().LocalName)
}
