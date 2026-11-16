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

package plex

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCastDevice is a Cast receiver on a TLS listener with a self-signed
// certificate, as real devices have.
type fakeCastDevice struct {
	listener net.Listener
	events   chan castMessage
	// loadReply is the media message type sent for a LOAD; empty sends
	// nothing.
	loadReply   string
	playerState string
	received    []castMessage
	mu          syncutil.Mutex
	appRunning  bool
}

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "cast-device"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func newFakeCastDevice(t *testing.T) *fakeCastDevice {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{selfSignedCert(t)},
		MinVersion:   tls.VersionTLS12,
	})
	require.NoError(t, err)
	d := &fakeCastDevice{
		listener: ln, loadReply: "MEDIA_STATUS", playerState: "PLAYING",
		events: make(chan castMessage, 256),
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.handle(conn)
		}
	}()
	return d
}

func (d *fakeCastDevice) addr() string { return d.listener.Addr().String() }

func (d *fakeCastDevice) messages() []castMessage {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]castMessage(nil), d.received...)
}

func (*fakeCastDevice) reply(conn net.Conn, src, ns string, payload map[string]any) {
	data, _ := json.Marshal(payload)
	_ = writeCastFrame(conn, &castMessage{
		SourceID: src, DestinationID: castSenderID, Namespace: ns, PayloadUTF8: string(data),
	})
}

func (d *fakeCastDevice) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	for {
		m, err := readCastFrame(conn)
		if err != nil {
			return
		}
		d.mu.Lock()
		d.received = append(d.received, *m)
		select {
		case d.events <- *m:
		default:
		}
		running := d.appRunning
		loadReply := d.loadReply
		playerState := d.playerState
		d.mu.Unlock()

		var p map[string]any
		_ = json.Unmarshal([]byte(m.PayloadUTF8), &p)
		reqID := p["requestId"]
		apps := []map[string]any{}
		if running {
			apps = append(apps, map[string]any{"appId": castPlexAppID, "transportId": "transport-1"})
		}
		switch {
		case m.Namespace == nsReceiver && p["type"] == "LAUNCH":
			// A heartbeat in the middle of a request must be answered.
			d.reply(conn, castReceiverID, nsHeartbeat, map[string]any{"type": "PING"})
			d.reply(conn, castReceiverID, nsReceiver, map[string]any{
				"type": "RECEIVER_STATUS", "requestId": reqID, "status": map[string]any{"applications": []any{}},
			})
			d.mu.Lock()
			d.appRunning = true
			d.mu.Unlock()
			d.reply(conn, castReceiverID, nsReceiver, map[string]any{
				"type": "RECEIVER_STATUS", "requestId": reqID, "status": map[string]any{
					"applications": []any{map[string]any{"appId": castPlexAppID, "transportId": "transport-1"}},
				},
			})
		case m.Namespace == nsReceiver && p["type"] == "GET_STATUS":
			d.reply(conn, castReceiverID, nsReceiver, map[string]any{
				"type": "RECEIVER_STATUS", "requestId": reqID, "status": map[string]any{"applications": apps},
			})
		case m.Namespace == nsMedia && p["type"] == "LOAD" && loadReply != "":
			d.reply(conn, "transport-1", nsMedia, map[string]any{
				"type": loadReply, "requestId": reqID, "reason": "NOT_FOUND", "status": []any{},
			})
		case m.Namespace == nsMedia && p["type"] == "GET_STATUS":
			d.reply(conn, "transport-1", nsMedia, map[string]any{
				"type": "MEDIA_STATUS", "requestId": reqID, "status": []any{map[string]any{
					"mediaSessionId": 1, "playerState": playerState,
					"media": map[string]any{"contentId": "/library/metadata/200"},
				}},
			})
		}
	}
}

// next waits for the device to receive a message on a namespace.
func (d *fakeCastDevice) next(t *testing.T, namespace string) castMessage {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-d.events:
			if m.Namespace == namespace {
				return m
			}
		case <-timeout:
			t.Fatalf("cast device never received a %s message", namespace)
			return castMessage{}
		}
	}
}

func payloadOf(t *testing.T, m *castMessage) map[string]any {
	t.Helper()
	var p map[string]any
	require.NoError(t, json.Unmarshal([]byte(m.PayloadUTF8), &p))
	return p
}

func castTestRequest() *playRequest {
	return &playRequest{
		Queue: &PlayQueue{ID: 42},
		First: &Metadata{RatingKey: "200"},
		Kind:  kindMovie,
		Server: serverInfo{
			MachineID: testMachineID, Version: "1.40", Protocol: "http",
			Address: "192.168.1.5", Port: 32400, Token: testServerToken,
		},
	}
}

func TestCastPlay(t *testing.T) {
	t.Parallel()
	dev := newFakeCastDevice(t)
	p := &castPlayer{name: "Living Room TV", addr: dev.addr(), dial: dialCastTLS}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, p.Play(ctx, castTestRequest()))

	msgs := dev.messages()
	require.Len(t, msgs, 5)

	assert.Equal(t, nsConnection, msgs[0].Namespace)
	assert.Equal(t, castReceiverID, msgs[0].DestinationID)
	assert.Equal(t, castSenderID, msgs[0].SourceID)
	assert.Equal(t, "CONNECT", payloadOf(t, &msgs[0])["type"])

	assert.Equal(t, nsReceiver, msgs[1].Namespace)
	launch := payloadOf(t, &msgs[1])
	assert.Equal(t, "LAUNCH", launch["type"])
	assert.Equal(t, castPlexAppID, launch["appId"])

	assert.Equal(t, nsHeartbeat, msgs[2].Namespace)
	assert.Equal(t, "PONG", payloadOf(t, &msgs[2])["type"])

	assert.Equal(t, nsConnection, msgs[3].Namespace)
	assert.Equal(t, "transport-1", msgs[3].DestinationID)

	assert.Equal(t, nsMedia, msgs[4].Namespace)
	assert.Equal(t, "transport-1", msgs[4].DestinationID)
	load := payloadOf(t, &msgs[4])
	assert.Equal(t, "LOAD", load["type"])
	media, ok := load["media"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "/library/metadata/200", media["contentId"])
	assert.Equal(t, "video", media["contentType"])
	custom, ok := media["customData"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "/playQueues/42?own=1&window=200", custom["containerKey"])
	server, ok := custom["server"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, testMachineID, server["machineIdentifier"])
	assert.Equal(t, "192.168.1.5", server["address"])
	assert.InDelta(t, 32400, server["port"], 0)
	assert.Equal(t, testServerToken, server["accessToken"])
}

func TestCastPlayLoadFailed(t *testing.T) {
	t.Parallel()
	dev := newFakeCastDevice(t)
	dev.loadReply = "LOAD_FAILED"
	p := &castPlayer{name: "TV", addr: dev.addr(), dial: dialCastTLS}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := p.Play(ctx, castTestRequest())
	require.ErrorContains(t, err, "NOT_FOUND")
}

func TestCastCommandAndState(t *testing.T) {
	t.Parallel()
	dev := newFakeCastDevice(t)
	p := &castPlayer{name: "TV", addr: dev.addr(), dial: dialCastTLS}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.ErrorContains(t, p.Command(ctx, cmdPause, kindMovie), "not running")

	dev.mu.Lock()
	dev.appRunning = true
	dev.mu.Unlock()
	require.NoError(t, p.Command(ctx, cmdStepForward, kindMovie))
	last := dev.next(t, nsPlex)
	assert.Equal(t, nsPlex, last.Namespace)
	assert.Equal(t, "transport-1", last.DestinationID)
	assert.Equal(t, "STEPFORWARD", payloadOf(t, &last)["type"])

	require.Error(t, p.Command(ctx, "eject", kindMovie))

	state, err := p.State(ctx)
	require.NoError(t, err)
	assert.True(t, state.Playing)
	assert.Equal(t, "200", state.RatingKey)

	dev.mu.Lock()
	dev.playerState = "PAUSED"
	dev.mu.Unlock()
	state, err = p.State(ctx)
	require.NoError(t, err)
	assert.True(t, state.Paused)
}

func TestCastCommandTypes(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]string{
		cmdPlay: "PLAY", cmdPause: "PAUSE", cmdStop: "STOP", cmdNext: "NEXT",
		cmdPrevious: "PREVIOUS", cmdStepForward: "STEPFORWARD", cmdStepBack: "STEPBACK",
	} {
		assert.Equal(t, want, castCommandType(cmd))
	}
}

func TestCastFrameRoundTrip(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	in := &castMessage{SourceID: "a", DestinationID: "b", Namespace: nsMedia, PayloadUTF8: `{"type":"PING"}`}
	require.NoError(t, writeCastFrame(&buf, in))
	out, err := readCastFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, in.SourceID, out.SourceID)
	assert.Equal(t, in.DestinationID, out.DestinationID)
	assert.Equal(t, in.Namespace, out.Namespace)
	assert.Equal(t, in.PayloadUTF8, out.PayloadUTF8)
}

func TestReadCastFrameRejectsOversize(t *testing.T) {
	t.Parallel()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], maxCastFrame+1)
	_, err := readCastFrame(bytes.NewReader(header[:]))
	require.ErrorContains(t, err, "exceeds")

	binary.BigEndian.PutUint32(header[:], 10)
	_, err = readCastFrame(bytes.NewReader(append(header[:], 1, 2)))
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func FuzzReadCastFrame(f *testing.F) {
	var buf bytes.Buffer
	_ = writeCastFrame(&buf, &castMessage{
		SourceID: "receiver-0", DestinationID: "sender-0", Namespace: nsReceiver,
		PayloadUTF8: `{"type":"RECEIVER_STATUS"}`,
	})
	f.Add(buf.Bytes())
	f.Add([]byte{0, 0, 0, 2, 0x08, 0x00})
	f.Add([]byte{0, 0, 0, 3, 0x3a, 0x05, 0x01})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := readCastFrame(bytes.NewReader(data))
		if err != nil {
			return
		}
		var again bytes.Buffer
		if werr := writeCastFrame(&again, m); werr != nil {
			if errors.Is(werr, io.ErrShortWrite) {
				t.Fatal(werr)
			}
			return
		}
		back, err := readCastFrame(&again)
		if err != nil {
			t.Fatalf("re-encoded frame does not decode: %v", err)
		}
		if back.SourceID != m.SourceID || back.DestinationID != m.DestinationID ||
			back.Namespace != m.Namespace || back.PayloadUTF8 != m.PayloadUTF8 {
			t.Fatalf("round trip changed the message: %+v != %+v", back, m)
		}
	})
}
