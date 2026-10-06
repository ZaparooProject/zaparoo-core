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
package apigatt

import (
	"bytes"
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMessage(n int) []byte {
	msg := make([]byte, n)
	for i := range msg {
		msg[i] = byte(i*7 + i/251)
	}
	return msg
}

// pushChunk parses and pushes one raw chunk.
func pushChunk(t *testing.T, r *Reassembler, chunk []byte) ([][]byte, error) {
	t.Helper()
	h, payload, err := ParseChunk(chunk)
	if err != nil {
		return nil, err
	}
	return r.Push(h, payload)
}

// pushAll pushes chunks in the given order and returns every message.
func pushAll(t *testing.T, r *Reassembler, chunks [][]byte) [][]byte {
	t.Helper()
	var msgs [][]byte
	for _, chunk := range chunks {
		got, err := pushChunk(t, r, chunk)
		require.NoError(t, err)
		msgs = append(msgs, got...)
	}
	return msgs
}

func TestChunkerRoundTrip(t *testing.T) {
	t.Parallel()

	for _, mtu := range []int{0, DefaultMTU, 100, 185, 247, 512, 517} {
		for _, size := range []int{1, 10, 11, 12, 100, 1000, 70000} {
			msg := testMessage(size)
			chunks := splitAll(t, &Chunker{Tag: 0xBEEF}, msg, mtu)
			limit := min(max(mtu, DefaultMTU)-attHeaderSize, MaxChunkSize)
			for i, chunk := range chunks {
				assert.LessOrEqual(t, len(chunk), limit, "mtu %d size %d chunk %d", mtu, size, i)
				h, _, err := ParseChunk(chunk)
				require.NoError(t, err)
				assert.Equal(t, uint16(0xBEEF), h.Tag)
				assert.Equal(t, uint16(i), h.Seq) //nolint:gosec // test sizes stay under 65536 chunks
				assert.Equal(t, i == 0, h.First)
				assert.Equal(t, i == len(chunks)-1, h.Last)
			}
			msgs := pushAll(t, NewReassembler(0), chunks)
			require.Len(t, msgs, 1)
			assert.True(t, bytes.Equal(msg, msgs[0]), "mtu %d size %d", mtu, size)
		}
	}
}

func TestChunkerRejectsEmpty(t *testing.T) {
	t.Parallel()

	c := &Chunker{}
	err := c.Split(nil, 100, func([]byte) error { return nil })
	require.ErrorIs(t, err, ErrEmptyMessage)
	assert.Equal(t, uint16(0), c.Next())
}

func TestChunkerStopsOnEmitError(t *testing.T) {
	t.Parallel()

	c := &Chunker{}
	boom := errors.New("link gone")
	calls := 0
	err := c.Split(testMessage(1000), DefaultMTU, func([]byte) error {
		calls++
		if calls == 3 {
			return boom
		}
		return nil
	})
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 3, calls)
	assert.Equal(t, uint16(3), c.Next())
}

func TestChunkerSequenceContinuesAcrossMessagesAndWraps(t *testing.T) {
	t.Parallel()

	c := &Chunker{seq: 65530}
	r := NewReassembler(0)
	r.nextSeq = 65530
	first, second := testMessage(100), testMessage(77)
	chunks := splitAll(t, c, first, DefaultMTU)
	chunks = append(chunks, splitAll(t, c, second, DefaultMTU)...)
	require.Greater(t, len(chunks), 6, "the stream must cross the wrap")

	h, _, err := ParseChunk(chunks[len(chunks)-1])
	require.NoError(t, err)
	assert.Equal(t, c.Next()-1, h.Seq)
	assert.Less(t, h.Seq, uint16(100), "sequence wrapped")

	msgs := pushAll(t, r, chunks)
	require.Len(t, msgs, 2)
	assert.Equal(t, first, msgs[0])
	assert.Equal(t, second, msgs[1])
	assert.Equal(t, c.Next(), r.Next())
}

// Chunks reach Core on separate goroutines, so any order is possible, across
// message boundaries too. Messages must still come out in sending order.
func TestReassemblerReordersAcrossMessages(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a repeatable shuffle, not a secret
	for round := range 200 {
		c := &Chunker{Tag: 3}
		want := make([][]byte, 0, 12)
		chunks := make([][]byte, 0, ReorderWindow)
		for i := range 12 {
			// Mostly single-chunk messages, as requests are at a large MTU.
			size := 1 + rng.IntN(8)
			if i%4 == 0 {
				size = 40 + rng.IntN(60)
			}
			msg := testMessage(size + round)
			want = append(want, msg)
			chunks = append(chunks, splitAll(t, c, msg, DefaultMTU)...)
		}
		require.Less(t, len(chunks), ReorderWindow)
		rng.Shuffle(len(chunks), func(i, j int) { chunks[i], chunks[j] = chunks[j], chunks[i] })

		got := pushAll(t, NewReassembler(0), chunks)
		require.Equal(t, want, got, "round %d", round)
	}
}

func TestReassemblerReleasesSeveralMessagesAtOnce(t *testing.T) {
	t.Parallel()

	c := &Chunker{}
	chunks := make([][]byte, 0, 3)
	for _, s := range []string{"one", "two", "three"} {
		chunks = append(chunks, splitAll(t, c, []byte(s), 512)...)
	}
	require.Len(t, chunks, 3)

	r := NewReassembler(0)
	for _, i := range []int{2, 1} {
		msgs, err := pushChunk(t, r, chunks[i])
		require.NoError(t, err)
		assert.Empty(t, msgs)
	}
	msgs, err := pushChunk(t, r, chunks[0])
	require.NoError(t, err)
	assert.Equal(t, [][]byte{[]byte("one"), []byte("two"), []byte("three")}, msgs)
	assert.Equal(t, uint16(3), r.Next())
}

func TestReassemblerRejectsBeyondWindowAndBehind(t *testing.T) {
	t.Parallel()

	ahead := EncodeChunk(Header{Seq: ReorderWindow}, []byte("x"))
	_, err := pushChunk(t, NewReassembler(0), ahead)
	require.ErrorIs(t, err, ErrSequence)

	r := NewReassembler(0)
	chunks := splitAll(t, &Chunker{}, []byte("done"), 512)
	pushAll(t, r, chunks)
	_, err = pushChunk(t, r, chunks[0])
	require.ErrorIs(t, err, ErrSequence, "a chunk already consumed is behind the window")
}

func TestReassemblerRejectsDuplicateHeldChunk(t *testing.T) {
	t.Parallel()

	r := NewReassembler(0)
	early := EncodeChunk(Header{Seq: 2}, []byte("x"))
	_, err := pushChunk(t, r, early)
	require.NoError(t, err)
	_, err = pushChunk(t, r, early)
	require.ErrorIs(t, err, ErrSequence)
}

func TestReassemblerCapsHeldBytes(t *testing.T) {
	t.Parallel()

	r := NewReassembler(0)
	payload := make([]byte, 4096)
	var err error
	for seq := uint16(1); seq < ReorderWindow && err == nil; seq++ {
		_, err = pushChunk(t, r, EncodeChunk(Header{Seq: seq}, payload))
	}
	require.ErrorIs(t, err, ErrSequence)
	assert.LessOrEqual(t, r.heldBytes, maxHeldBytes)
}

func TestReassemblerLengthMismatch(t *testing.T) {
	t.Parallel()

	short := EncodeChunk(Header{First: true, Last: true, Length: 10}, []byte("abc"))
	_, err := pushChunk(t, NewReassembler(0), short)
	require.ErrorIs(t, err, ErrLengthMismatch)

	long := EncodeChunk(Header{First: true, Length: 2}, []byte("abc"))
	_, err = pushChunk(t, NewReassembler(0), long)
	require.ErrorIs(t, err, ErrLengthMismatch)
}

func TestReassemblerMaxMessage(t *testing.T) {
	t.Parallel()

	r := NewReassembler(8)
	over := EncodeChunk(Header{First: true, Length: 9}, []byte("a"))
	_, err := pushChunk(t, r, over)
	require.ErrorIs(t, err, ErrMessageTooLarge)

	// The limit is raised once a client authenticates.
	r = NewReassembler(8)
	r.SetMaxMessage(64)
	msg := testMessage(64)
	msgs := pushAll(t, r, splitAll(t, &Chunker{}, msg, DefaultMTU))
	require.Len(t, msgs, 1)
	assert.Equal(t, msg, msgs[0])

	// Zero means no limit, for a client receiving from Core.
	r.SetMaxMessage(0)
	_, err = pushChunk(t, r, EncodeChunk(Header{Seq: r.Next(), First: true, Length: math.MaxUint32}, []byte("a")))
	require.NoError(t, err)
}

func TestReassemblerDoesNotAllocateTheDeclaredLength(t *testing.T) {
	t.Parallel()

	r := NewReassembler(0)
	first := EncodeChunk(Header{First: true, Length: MaxMessageSize}, []byte("a"))
	_, err := pushChunk(t, r, first)
	require.NoError(t, err)
	assert.Less(t, cap(r.buf), 1024, "a declared length alone must not reserve memory")
}

func TestParseChunkMalformed(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"empty":               {},
		"short":               {0x10, 0, 0, 0},
		"wrong version":       {0x20, 0, 0, 0, 0, 'x'},
		"reserved bit":        {0x18, 0, 0, 0, 0, 'x'},
		"no payload":          {0x10, 0, 0, 0, 0},
		"short first":         {0x11, 0, 0, 0, 0, 0, 0},
		"first no payload":    {0x11, 0, 0, 0, 0, 0, 0, 0, 1},
		"zero length":         {0x11, 0, 0, 0, 0, 0, 0, 0, 0, 'x'},
		"ack with payload":    {0x14, 0, 0, 0, 0, 'x'},
		"ack with first flag": {0x15, 0, 0, 0, 0},
		"ack with last flag":  {0x16, 0, 0, 0, 0},
	}
	for name, chunk := range cases {
		_, _, err := ParseChunk(chunk)
		require.ErrorIs(t, err, ErrMalformedChunk, name)
	}
}

func TestAckRoundTrip(t *testing.T) {
	t.Parallel()

	h, payload, err := ParseChunk(EncodeAck(0x1234, 0xFFFE))
	require.NoError(t, err)
	assert.Empty(t, payload)
	assert.Equal(t, Header{Version: ProtocolVersion, Ack: true, Seq: 0xFFFE, Tag: 0x1234}, h)

	_, err = NewReassembler(0).Push(h, nil)
	require.ErrorIs(t, err, ErrMalformedChunk, "an acknowledgement is not data")
}

func TestReassemblerRecoverableErrorsAreBudgeted(t *testing.T) {
	t.Parallel()

	// A continuation with no message in progress is dropped and counted.
	r := NewReassembler(0)
	for seq := range uint16(MaxProtocolErrors - 1) {
		msgs, err := pushChunk(t, r, EncodeChunk(Header{Seq: seq}, []byte("x")))
		require.NoError(t, err)
		assert.Empty(t, msgs)
	}
	_, err := pushChunk(t, r, EncodeChunk(Header{Seq: MaxProtocolErrors - 1}, []byte("x")))
	require.ErrorIs(t, err, ErrTooManyErrors)

	// So is starting a new message before finishing the last, and the new
	// message is the one delivered.
	r = NewReassembler(0)
	_, err = pushChunk(t, r, EncodeChunk(Header{First: true, Length: 100}, []byte("abandoned")))
	require.NoError(t, err)
	msgs, err := pushChunk(t, r, EncodeChunk(Header{Seq: 1, First: true, Last: true, Length: 3}, []byte("new")))
	require.NoError(t, err)
	assert.Equal(t, [][]byte{[]byte("new")}, msgs)
	assert.Equal(t, 1, r.errors)
}

func TestNewInfo(t *testing.T) {
	t.Parallel()

	info := NewInfo("device-1")
	assert.Equal(t, Info{
		DeviceID:           "device-1",
		Version:            ProtocolVersion,
		MaxMessage:         MaxMessageSize,
		MaxUnauthenticated: MaxUnauthenticatedMessageSize,
		PreferredMTU:       PreferredMTU,
		Window:             SendWindow,
	}, info)
}
