/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package proto_test

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/ccid/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedTransport answers each sent message with the next queued replies,
// delivered in chunks of chunkSize bytes like USB packets.
type scriptedTransport struct {
	sendErr   error
	recvErr   error
	replies   [][]byte
	sent      [][]byte
	pending   []byte
	chunkSize int
}

func (s *scriptedTransport) Send(_ context.Context, msg []byte) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, append([]byte(nil), msg...))
	return nil
}

func (s *scriptedTransport) Receive(_ context.Context, _ int) ([]byte, error) {
	if s.recvErr != nil {
		return nil, s.recvErr
	}
	if len(s.pending) == 0 {
		if len(s.replies) == 0 {
			return nil, nil
		}
		s.pending = s.replies[0]
		s.replies = s.replies[1:]
	}
	n := len(s.pending)
	if s.chunkSize > 0 && n > s.chunkSize {
		n = s.chunkSize
	}
	chunk := s.pending[:n]
	s.pending = s.pending[n:]
	return chunk, nil
}

func reply(msgType, seq, status, code byte, payload ...byte) []byte {
	msg := make([]byte, 10+len(payload))
	msg[0] = msgType
	binary.LittleEndian.PutUint32(msg[1:5], uint32(len(payload))) //nolint:gosec // test payloads are tiny
	msg[6] = seq
	msg[7] = status
	msg[8] = code
	copy(msg[10:], payload)
	return msg
}

var ntagATR = []byte{
	0x3B, 0x8F, 0x80, 0x01, 0x80, 0x4F, 0x0C, 0xA0, 0x00, 0x00,
	0x03, 0x06, 0x03, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x68,
}

func TestConn_MessagesMatchTheWireFormat(t *testing.T) {
	t.Parallel()
	transport := &scriptedTransport{replies: [][]byte{
		reply(0x81, 1, 0x00, 0x00),
		reply(0x80, 2, 0x00, 0x00, ntagATR...),
		reply(0x80, 3, 0x00, 0x00, 0x04, 0x7A, 0x7E, 0x0A, 0xC0, 0x1C, 0x91, 0x90, 0x00),
	}}
	conn := proto.New(transport, 0)
	ctx := context.Background()

	status, err := conn.SlotStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, proto.ICCActive, status)

	atr, err := conn.PowerOn(ctx)
	require.NoError(t, err)
	assert.Equal(t, ntagATR, atr)

	resp, err := conn.Transmit(ctx, []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
	require.NoError(t, err)
	assert.Equal(t, []byte{0x04, 0x7A, 0x7E, 0x0A, 0xC0, 0x1C, 0x91, 0x90, 0x00}, resp)

	// Captured from the ACR1552 with the same three commands.
	assert.Equal(t, [][]byte{
		{0x65, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00},
		{0x62, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00},
		{0x6F, 0x05, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0xFF, 0xCA, 0x00, 0x00, 0x00},
	}, transport.sent)
}

func TestConn_ErrorByteIsIgnoredOnSuccess(t *testing.T) {
	t.Parallel()
	// The ACR122U sets the error byte to 0x81 on every response.
	transport := &scriptedTransport{replies: [][]byte{
		reply(0x81, 1, 0x00, 0x81),
		reply(0x80, 2, 0x00, 0x81, ntagATR...),
		reply(0x80, 3, 0x00, 0x81, 0x90, 0x00),
	}}
	conn := proto.New(transport, 0)
	ctx := context.Background()

	status, err := conn.SlotStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, proto.ICCActive, status)

	_, err = conn.PowerOn(ctx)
	require.NoError(t, err)

	resp, err := conn.Transmit(ctx, []byte{0xFF, 0x82, 0x00, 0x00, 0x06, 1, 2, 3, 4, 5, 6})
	require.NoError(t, err)
	assert.Equal(t, []byte{0x90, 0x00}, resp)
}

func TestConn_SlotStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status byte
		code   byte
		want   proto.ICCStatus
	}{
		{name: "active", status: 0x00, want: proto.ICCActive},
		{name: "inactive", status: 0x01, want: proto.ICCInactive},
		{name: "absent", status: 0x02, want: proto.ICCAbsent},
		// The bR500 reports an empty slot as a failed command.
		{name: "absent as failure", status: 0x42, code: 0xFE, want: proto.ICCAbsent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			transport := &scriptedTransport{replies: [][]byte{reply(0x81, 1, tt.status, tt.code)}}

			status, err := proto.New(transport, 0).SlotStatus(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tt.want, status)
		})
	}
}

func TestConn_PowerOnFailureReportsCardState(t *testing.T) {
	t.Parallel()
	// The ACR1552 SAM slot with nothing in it.
	transport := &scriptedTransport{replies: [][]byte{reply(0x80, 1, 0x41, 0xF6)}}

	_, err := proto.New(transport, 0).PowerOn(context.Background())

	var cmdErr *proto.CommandError
	require.ErrorAs(t, err, &cmdErr)
	assert.Equal(t, proto.ICCInactive, cmdErr.ICC)
	assert.Equal(t, uint8(0xF6), cmdErr.Code)
}

func TestConn_TransmitFailure(t *testing.T) {
	t.Parallel()
	transport := &scriptedTransport{replies: [][]byte{reply(0x80, 1, 0x42, 0xFE)}}

	_, err := proto.New(transport, 0).Transmit(context.Background(), []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})

	var cmdErr *proto.CommandError
	require.ErrorAs(t, err, &cmdErr)
	assert.Equal(t, proto.ICCAbsent, cmdErr.ICC)
}

func TestConn_ReassemblesPackets(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 258)
	for i := range payload {
		payload[i] = byte(i)
	}
	transport := &scriptedTransport{
		chunkSize: 64,
		replies:   [][]byte{reply(0x80, 1, 0x00, 0x00, payload...)},
	}

	resp, err := proto.New(transport, 0).Transmit(context.Background(), []byte{0xFF, 0xB0, 0x00, 0x04, 0x00})

	require.NoError(t, err)
	assert.Equal(t, payload, resp)
}

func TestConn_WaitsThroughTimeExtension(t *testing.T) {
	t.Parallel()
	transport := &scriptedTransport{replies: [][]byte{
		reply(0x80, 1, 0x80, 0x01),
		reply(0x80, 1, 0x80, 0x01),
		reply(0x80, 1, 0x00, 0x00, 0x90, 0x00),
	}}

	apdu := []byte{0xFF, 0xD6, 0x00, 0x04, 0x04, 1, 2, 3, 4}
	resp, err := proto.New(transport, 0).Transmit(context.Background(), apdu)

	require.NoError(t, err)
	assert.Equal(t, []byte{0x90, 0x00}, resp)
}

func TestConn_EndlessTimeExtensionGivesUp(t *testing.T) {
	t.Parallel()
	replies := make([][]byte, 0, 100)
	for range 100 {
		replies = append(replies, reply(0x80, 1, 0x80, 0x01))
	}

	_, err := proto.New(&scriptedTransport{replies: replies}, 0).PowerOn(context.Background())

	require.ErrorIs(t, err, proto.ErrTimeExtended)
}

func TestConn_SkipsAnswerToAnAbandonedCommand(t *testing.T) {
	t.Parallel()
	transport := &scriptedTransport{replies: [][]byte{
		reply(0x81, 9, 0x02, 0x00),
		reply(0x81, 1, 0x00, 0x00),
	}}

	status, err := proto.New(transport, 0).SlotStatus(context.Background())

	require.NoError(t, err)
	assert.Equal(t, proto.ICCActive, status)
}

func TestConn_RejectsBadResponses(t *testing.T) {
	t.Parallel()

	oversized := reply(0x80, 1, 0x00, 0x00)
	binary.LittleEndian.PutUint32(oversized[1:5], 1<<24)

	stale := make([][]byte, 0, 8)
	for range 8 {
		stale = append(stale, reply(0x81, 9, 0x00, 0x00))
	}

	tests := []struct {
		wantErr error
		name    string
		replies [][]byte
	}{
		{name: "oversized length", replies: [][]byte{oversized}, wantErr: proto.ErrOversized},
		{name: "wrong sequence forever", replies: stale, wantErr: proto.ErrSequence},
		{name: "wrong type", replies: [][]byte{reply(0x80, 1, 0x00, 0x00)}, wantErr: proto.ErrMessageType},
		{name: "silent reader", replies: nil, wantErr: proto.ErrNoData},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := proto.New(&scriptedTransport{replies: tt.replies}, 0).SlotStatus(context.Background())
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestConn_TransportErrors(t *testing.T) {
	t.Parallel()
	errGone := errors.New("device gone")

	_, err := proto.New(&scriptedTransport{sendErr: errGone}, 0).SlotStatus(context.Background())
	require.ErrorIs(t, err, errGone)

	_, err = proto.New(&scriptedTransport{recvErr: errGone}, 0).SlotStatus(context.Background())
	require.ErrorIs(t, err, errGone)
}

func TestConn_UsesItsSlotNumber(t *testing.T) {
	t.Parallel()
	transport := &scriptedTransport{replies: [][]byte{reply(0x81, 1, 0x00, 0x00)}}

	_, err := proto.New(transport, 1).SlotStatus(context.Background())

	require.NoError(t, err)
	assert.Equal(t, byte(1), transport.sent[0][5])
}

func FuzzConn(f *testing.F) {
	f.Add(reply(0x81, 1, 0x00, 0x00), 64)
	f.Add(reply(0x80, 1, 0x41, 0xF6), 8)
	f.Add(reply(0x80, 1, 0x00, 0x00, ntagATR...), 1)
	f.Add([]byte{0x80, 0xFF, 0xFF, 0xFF, 0xFF}, 3)

	f.Fuzz(func(_ *testing.T, data []byte, chunkSize int) {
		if chunkSize < 1 || chunkSize > 4096 {
			chunkSize = 64
		}
		ctx := context.Background()
		transport := &scriptedTransport{chunkSize: chunkSize, replies: [][]byte{data, data, data}}
		conn := proto.New(transport, 0)
		_, _ = conn.SlotStatus(ctx)
		_, _ = conn.PowerOn(ctx)
		_, _ = conn.Transmit(ctx, []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
	})
}
