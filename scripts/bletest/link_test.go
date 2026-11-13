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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/apigatt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/bluetooth/ble"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialScansForTheZaparooService(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	other := mocks.NewFakeDevice("AA:AA:AA:AA:AA:AA")
	other.Name = "Somebody Else"
	core.central.AddDevice(other)

	l, err := dial(t.Context(), core.central, linkOptions{name: "zaparoo", scanFor: time.Minute})
	require.NoError(t, err)
	t.Cleanup(l.close)

	assert.Equal(t, fakeCoreDeviceID, l.info.DeviceID)
	assert.Equal(t, 4, l.info.Window)
	assert.Equal(t, 64, l.mtu, "the MTU comes from the TX characteristic when not overridden")
	assert.Equal(t, uint16(1), l.tag&1, "the session tag is always odd")
	assert.Equal(t, l.tag, l.chunker.Tag)
	assert.True(t, core.dev.Connected())
}

func TestDialWithoutAMatchGivesUpAfterTheScan(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})

	_, err := dial(t.Context(), core.central, linkOptions{name: "nothing-like-it", scanFor: 20 * time.Millisecond})
	require.EqualError(t, err, "no device advertising the zaparoo service was found")
	assert.Empty(t, core.central.Finds())
}

func TestDialReportsAScanFailure(t *testing.T) {
	t.Parallel()
	central := mocks.NewFakeCentral()
	central.ScanErr = ble.ErrPoweredOff

	_, err := dial(t.Context(), central, linkOptions{scanFor: time.Minute})
	require.ErrorIs(t, err, ble.ErrPoweredOff)
}

func TestDialReportsFindAndConnectFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := dial(ctx, mocks.NewFakeCentral(), linkOptions{address: fakeCoreAddr})
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "find "+fakeCoreAddr)

	core := newFakeCore(t, &fakeCoreOptions{})
	core.dev.ConnectErr = errors.New("le-connection-abort-by-local")
	_, err = dial(t.Context(), core.central, linkOptions{address: fakeCoreAddr})
	require.ErrorContains(t, err, "connect "+fakeCoreAddr+": le-connection-abort-by-local")
}

func TestDialFailsWhenTheServiceIsIncomplete(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{apigatt.RXCharUUID, apigatt.TXCharUUID, apigatt.InfoCharUUID} {
		dev := mocks.NewFakeDevice(fakeCoreAddr)
		for _, uuid := range []string{apigatt.RXCharUUID, apigatt.TXCharUUID, apigatt.InfoCharUUID} {
			if uuid != missing {
				dev.AddCharacteristic(apigatt.ServiceUUID, uuid, mocks.NewFakeCharacteristic())
			}
		}
		central := mocks.NewFakeCentral()
		central.AddDevice(dev)

		_, err := dial(t.Context(), central, linkOptions{address: fakeCoreAddr})
		require.ErrorIs(t, err, ble.ErrNotFound, missing)
		assert.False(t, dev.Connected(), "a failed attach disconnects")
	}
}

func TestDialFailsOnABadInfoOrTX(t *testing.T) {
	t.Parallel()
	tests := []struct {
		breakIt func(c *fakeCore)
		name    string
		want    string
	}{
		{name: "info unreadable", want: "read info", breakIt: func(c *fakeCore) {
			c.info.ReadErr = errors.New("not permitted")
		}},
		{name: "info not json", want: "decode info", breakIt: func(c *fakeCore) {
			c.info.Value = []byte("nope")
		}},
		{name: "mtu unreadable", want: "read mtu", breakIt: func(c *fakeCore) {
			c.tx.MTUErr = errors.New("no mtu")
		}},
		{name: "tx refuses subscription", want: "subscribe to tx", breakIt: func(c *fakeCore) {
			c.tx.SubscribeErr = errors.New("notify refused")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core := newFakeCore(t, &fakeCoreOptions{})
			tt.breakIt(core)
			_, err := dial(t.Context(), core.central, linkOptions{address: fakeCoreAddr})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestDialMTUFallbacks(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	core.tx.Link = 0
	l, err := dial(t.Context(), core.central, linkOptions{address: fakeCoreAddr})
	require.NoError(t, err)
	assert.Equal(t, apigatt.DefaultMTU, l.mtu, "an unreported MTU falls back to the ATT default")
	l.close()

	core = newFakeCore(t, &fakeCoreOptions{})
	l, err = dial(t.Context(), core.central, linkOptions{address: fakeCoreAddr, mtu: 247})
	require.NoError(t, err)
	assert.Equal(t, 247, l.mtu, "-mtu wins over what the stack reports")
	l.close()
}

func TestLinkSendRequestsAResponseEveryEighthChunk(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{onPlaintext: plainIgnore})
	var mu syncutil.Mutex
	var flags []bool
	core.rx.OnWrite = func(_ []byte, withResponse bool) error {
		mu.Lock()
		defer mu.Unlock()
		flags = append(flags, withResponse)
		return nil
	}
	l, err := dial(t.Context(), core.central, linkOptions{address: fakeCoreAddr})
	require.NoError(t, err)
	t.Cleanup(l.close)

	require.NoError(t, l.send(t.Context(), bytes.Repeat([]byte("z"), 1000)))

	mu.Lock()
	defer mu.Unlock()
	require.Greater(t, len(flags), 16)
	for i, acked := range flags {
		assert.Equal(t, (i+1)%ackedWriteEvery == 0, acked, "chunk %d", i+1)
	}
}

func TestLinkSendReportsAFailedWrite(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{rxWrite: func([]byte) error { return errors.New("att error 0x0e") }})
	l := core.dial(t)

	err := l.send(t.Context(), []byte("hello"))
	require.EqualError(t, err, "write chunk: att error 0x0e")
}

func TestLinkReceiveSkipsOtherClientsAndAcknowledges(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	var mu syncutil.Mutex
	var acks []apigatt.Header
	core.rx.OnWrite = func(chunk []byte, _ bool) error {
		h, _, err := apigatt.ParseChunk(chunk)
		if err == nil && h.Ack {
			mu.Lock()
			acks = append(acks, h)
			mu.Unlock()
		}
		return nil
	}
	l := core.dial(t)

	other := apigatt.Chunker{Tag: l.tag + 2}
	require.NoError(t, other.Split([]byte("for someone else"), 64, func(chunk []byte) error {
		core.tx.Push(chunk)
		return nil
	}))
	mine := apigatt.Chunker{Tag: l.tag}
	msg := bytes.Repeat([]byte("m"), 300)
	require.NoError(t, mine.Split(msg, 64, func(chunk []byte) error {
		core.tx.Push(chunk)
		return nil
	}))

	got, err := l.recv(t.Context())
	require.NoError(t, err)
	assert.Equal(t, msg, got)
	assert.Equal(t, int64(mine.Next()), l.chunksIn.Load(), "only this client's chunks count")
	assert.Equal(t, int64(len(msg)), l.bytesIn.Load())

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(acks) > 0 && acks[len(acks)-1].Seq >= mine.Next()-1
	}, 5*time.Second, time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, h := range acks {
		assert.Equal(t, l.tag, h.Tag)
	}
}

func TestLinkReceiveStopsOnBadInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		chunk func(tag uint16) []byte
		name  string
	}{
		{name: "unparseable", chunk: func(uint16) []byte { return []byte{0xff} }},
		{name: "out of sequence", chunk: func(tag uint16) []byte {
			return apigatt.EncodeChunk(apigatt.Header{
				Version: apigatt.ProtocolVersion, Seq: 5000, Tag: tag, First: true, Last: true, Length: 1,
			}, []byte("x"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			core := newFakeCore(t, &fakeCoreOptions{})
			l := core.dial(t)
			core.tx.Push(tt.chunk(l.tag))
			_, err := l.recv(t.Context())
			require.EqualError(t, err, "link closed")
		})
	}
}

func TestLinkReceiveStopsWhenAnAcknowledgementFails(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	l := core.dial(t)
	core.rx.OnWrite = func([]byte, bool) error { return errors.New("gone") }

	c := apigatt.Chunker{Tag: l.tag}
	require.NoError(t, c.Split(bytes.Repeat([]byte("a"), 100), 64, func(chunk []byte) error {
		core.tx.Push(chunk)
		return nil
	}))
	got, err := l.recv(t.Context())
	require.NoError(t, err, "the message is delivered before the acknowledgement")
	assert.Len(t, got, 100)
	_, err = l.recv(t.Context())
	require.EqualError(t, err, "link closed")
}

func TestLinkRecvEndsOnDisconnectOrContext(t *testing.T) {
	t.Parallel()
	core := newFakeCore(t, &fakeCoreOptions{})
	l := core.dial(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := l.recv(ctx)
	require.ErrorIs(t, err, context.Canceled)

	core.dev.Drop()
	_, err = l.recv(t.Context())
	require.EqualError(t, err, "core disconnected")
}
