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
	"fmt"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestReassembler(t *testing.T) (*Reassembler, *clockwork.FakeClock) {
	t.Helper()
	clock := clockwork.NewFakeClock()
	return NewReassembler(clock, 0), clock
}

// feed pushes every chunk and returns the messages completed along the way.
func feed(t *testing.T, r *Reassembler, chunks [][]byte) [][]byte {
	t.Helper()
	var msgs [][]byte
	for i, c := range chunks {
		msg, err := r.Push(c)
		require.NoError(t, err, "chunk %d", i)
		if msg != nil {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

func message(n int) []byte {
	msg := make([]byte, n)
	for i := range msg {
		msg[i] = byte(i * 7)
	}
	return msg
}

func TestChunkerRoundTrip(t *testing.T) {
	t.Parallel()

	sizes := []int{1, 12, 13, 100, 1000, 65_000}
	mtus := []int{0, 23, 185, 512}
	for _, mtu := range mtus {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("mtu%d_size%d", mtu, size), func(t *testing.T) {
				t.Parallel()
				msg := message(size)
				chunks, err := Chunker{MTU: mtu, Tag: 0xbeef}.Split(msg)
				require.NoError(t, err)

				effective := max(mtu, DefaultMTU)
				for i, c := range chunks {
					assert.LessOrEqual(t, len(c), effective-attHeaderSize, "chunk %d exceeds MTU", i)
					h, _, err := ParseChunk(c)
					require.NoError(t, err)
					assert.Equal(t, uint16(0xbeef), h.Tag)
					assert.Equal(t, uint8(i), h.Seq)
					assert.Equal(t, i == 0, h.First)
					assert.Equal(t, i == len(chunks)-1, h.Last)
				}

				r, _ := newTestReassembler(t)
				msgs := feed(t, r, chunks)
				require.Len(t, msgs, 1)
				assert.Equal(t, msg, msgs[0])
				assert.Equal(t, 0, r.errors)
			})
		}
	}
}

func TestChunkerRejectsEmptyAndOversize(t *testing.T) {
	t.Parallel()

	_, err := Chunker{}.Split(nil)
	require.ErrorIs(t, err, ErrEmptyMessage)
	_, err = Chunker{}.Split(make([]byte, MaxMessageSize+1))
	require.ErrorIs(t, err, ErrMessageTooLarge)
	chunks, err := Chunker{MTU: 512}.Split(make([]byte, MaxMessageSize))
	require.NoError(t, err)
	assert.NotEmpty(t, chunks)
}

func TestChunkerSequenceWraps(t *testing.T) {
	t.Parallel()

	// At MTU 23 a 12-byte first payload and 16-byte follow-ups need well
	// over 256 chunks for 8 KiB, so the sequence byte wraps mid-message.
	msg := message(8 * 1024)
	chunks, err := Chunker{MTU: 23}.Split(msg)
	require.NoError(t, err)
	require.Greater(t, len(chunks), 256)
	r, _ := newTestReassembler(t)
	msgs := feed(t, r, chunks)
	require.Len(t, msgs, 1)
	assert.Equal(t, msg, msgs[0])
}

func TestReassemblerBackToBackMessages(t *testing.T) {
	t.Parallel()

	r, _ := newTestReassembler(t)
	var all [][]byte
	for _, size := range []int{5, 300, 1, 40} {
		chunks, err := Chunker{MTU: 50}.Split(message(size))
		require.NoError(t, err)
		all = append(all, chunks...)
	}
	msgs := feed(t, r, all)
	require.Len(t, msgs, 4)
	assert.Equal(t, message(300), msgs[1])
	assert.Equal(t, message(40), msgs[3])
}

func TestReassemblerReorderWithinWindow(t *testing.T) {
	t.Parallel()

	msg := message(200)
	chunks, err := Chunker{MTU: 30}.Split(msg)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 5)

	// Deliver 2,1,0 then 4,3 then the rest: every displacement stays inside
	// the window.
	order := make([]int, 0, len(chunks))
	order = append(order, 2, 1, 0, 4, 3)
	for i := 5; i < len(chunks); i++ {
		order = append(order, i)
	}
	reordered := make([][]byte, 0, len(chunks))
	for _, i := range order {
		reordered = append(reordered, chunks[i])
	}

	r, _ := newTestReassembler(t)
	msgs := feed(t, r, reordered)
	require.Len(t, msgs, 1)
	assert.Equal(t, msg, msgs[0])
	assert.Equal(t, 0, r.errors)
}

func TestReassemblerRejectsBeyondWindow(t *testing.T) {
	t.Parallel()

	chunks, err := Chunker{MTU: 23}.Split(message(2000))
	require.NoError(t, err)

	r, _ := newTestReassembler(t)
	_, err = r.Push(chunks[0])
	require.NoError(t, err)
	// Chunk 0 was applied, so chunk 1 is expected next.
	inside := EncodeChunk(Header{Version: ProtocolVersion, Seq: 1 + ReorderWindow - 1}, []byte("x"))
	_, err = r.Push(inside)
	require.NoError(t, err, "the last position inside the window is held")
	beyond := EncodeChunk(Header{Version: ProtocolVersion, Seq: 1 + ReorderWindow}, []byte("x"))
	_, err = r.Push(beyond)
	require.ErrorIs(t, err, ErrSequence)
}

func TestReassemblerRestartDropsHeldChunks(t *testing.T) {
	t.Parallel()

	// Chunk 2 of an abandoned message must not be spliced into, or fail,
	// the message the peer starts afresh.
	chunks, err := Chunker{MTU: 23}.Split(message(100))
	require.NoError(t, err)
	r, _ := newTestReassembler(t)
	_, err = r.Push(chunks[0])
	require.NoError(t, err)
	_, err = r.Push(chunks[2])
	require.NoError(t, err)
	require.Len(t, r.held, 1)

	fresh, err := Chunker{MTU: 23}.Split(message(40))
	require.NoError(t, err)
	require.Len(t, fresh, 3)
	msgs := feed(t, r, fresh)
	require.Len(t, msgs, 1)
	assert.Equal(t, message(40), msgs[0])
	assert.Equal(t, 1, r.errors)
	assert.Empty(t, r.held)
}

func TestReassemblerRejectsDuplicateChunk(t *testing.T) {
	t.Parallel()

	chunks, err := Chunker{MTU: 23}.Split(message(200))
	require.NoError(t, err)

	r, _ := newTestReassembler(t)
	_, err = r.Push(chunks[0])
	require.NoError(t, err)
	_, err = r.Push(chunks[2])
	require.NoError(t, err)
	_, err = r.Push(chunks[2])
	require.ErrorIs(t, err, ErrSequence)

	r, _ = newTestReassembler(t)
	_, err = r.Push(chunks[0])
	require.NoError(t, err)
	_, err = r.Push(chunks[1])
	require.NoError(t, err)
	_, err = r.Push(chunks[1])
	require.ErrorIs(t, err, ErrSequence, "a chunk already applied is behind the window")
}

func TestReassemblerLengthMismatch(t *testing.T) {
	t.Parallel()

	t.Run("last chunk arrives short", func(t *testing.T) {
		t.Parallel()
		chunks, err := Chunker{MTU: 23}.Split(message(100))
		require.NoError(t, err)
		r, _ := newTestReassembler(t)
		_, err = r.Push(chunks[0])
		require.NoError(t, err)
		// Forge a LAST chunk with sequence 1 that ends the message early.
		short := EncodeChunk(Header{Version: ProtocolVersion, Seq: 1, Last: true}, []byte("x"))
		_, err = r.Push(short)
		require.ErrorIs(t, err, ErrLengthMismatch)
	})

	t.Run("payload overruns declared length", func(t *testing.T) {
		t.Parallel()
		r, _ := newTestReassembler(t)
		first := EncodeChunk(Header{Version: ProtocolVersion, Seq: 0, First: true, Length: 3}, []byte("ab"))
		_, err := r.Push(first)
		require.NoError(t, err)
		over := EncodeChunk(Header{Version: ProtocolVersion, Seq: 1}, []byte("cde"))
		_, err = r.Push(over)
		require.ErrorIs(t, err, ErrLengthMismatch)
	})
}

func TestReassemblerCaps(t *testing.T) {
	t.Parallel()

	t.Run("declared length over the cap", func(t *testing.T) {
		t.Parallel()
		r := NewReassembler(clockwork.NewFakeClock(), 64)
		first := EncodeChunk(Header{Version: ProtocolVersion, First: true, Length: 65}, []byte("a"))
		_, err := r.Push(first)
		require.ErrorIs(t, err, ErrMessageTooLarge)
	})

	t.Run("declared length over the protocol maximum is malformed at parse", func(t *testing.T) {
		t.Parallel()
		r, _ := newTestReassembler(t)
		first := EncodeChunk(Header{Version: ProtocolVersion, First: true, Length: MaxMessageSize + 1}, []byte("a"))
		_, err := r.Push(first)
		require.ErrorIs(t, err, ErrMessageTooLarge)
	})

	t.Run("held chunks count toward the cap", func(t *testing.T) {
		t.Parallel()
		r := NewReassembler(clockwork.NewFakeClock(), 32)
		first := EncodeChunk(Header{Version: ProtocolVersion, First: true, Length: 32}, bytes.Repeat([]byte("a"), 10))
		_, err := r.Push(first)
		require.NoError(t, err)
		held := EncodeChunk(Header{Version: ProtocolVersion, Seq: 2}, bytes.Repeat([]byte("b"), 30))
		_, err = r.Push(held)
		require.ErrorIs(t, err, ErrMessageTooLarge)
	})
}

func TestReassemblerMalformedChunks(t *testing.T) {
	t.Parallel()

	good := EncodeChunk(Header{Version: ProtocolVersion, First: true, Last: true, Length: 1}, []byte("a"))
	tests := []struct {
		want  error
		name  string
		chunk []byte
	}{
		{name: "too short", chunk: good[:3], want: ErrMalformedChunk},
		{name: "first header truncated", chunk: good[:6], want: ErrMalformedChunk},
		{name: "empty payload", chunk: good[:FirstHeaderSize], want: ErrMalformedChunk},
		{name: "wrong version", chunk: append([]byte{0x23}, good[1:]...), want: ErrMalformedChunk},
		{name: "reserved bits", chunk: append([]byte{good[0] | 0x04}, good[1:]...), want: ErrMalformedChunk},
		{
			name:  "zero length",
			chunk: EncodeChunk(Header{Version: ProtocolVersion, First: true, Last: true, Length: 0}, []byte("a")),
			want:  ErrMalformedChunk,
		},
		{
			name: "first chunk with non-zero sequence",
			chunk: EncodeChunk(
				Header{Version: ProtocolVersion, First: true, Last: true, Length: 1, Seq: 5}, []byte("a"),
			),
			want: ErrMalformedChunk,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, _ := newTestReassembler(t)
			_, err := r.Push(tt.chunk)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestReassemblerRecoverableErrorsAreBudgeted(t *testing.T) {
	t.Parallel()

	t.Run("first chunk mid-message restarts", func(t *testing.T) {
		t.Parallel()
		chunks, err := Chunker{MTU: 23}.Split(message(100))
		require.NoError(t, err)
		r, _ := newTestReassembler(t)
		_, err = r.Push(chunks[0])
		require.NoError(t, err)
		// Start over with a complete one-chunk message: it wins.
		fresh, err := Chunker{MTU: 23}.Split([]byte("hi"))
		require.NoError(t, err)
		msg, err := r.Push(fresh[0])
		require.NoError(t, err)
		assert.Equal(t, []byte("hi"), msg)
		assert.Equal(t, 1, r.errors)
	})

	t.Run("stray chunks exhaust the budget", func(t *testing.T) {
		t.Parallel()
		r, _ := newTestReassembler(t)
		stray := EncodeChunk(Header{Version: ProtocolVersion, Seq: 200}, []byte("x"))
		for i := 1; i < MaxProtocolErrors; i++ {
			_, err := r.Push(stray)
			require.NoError(t, err, "mistake %d is still tolerated", i)
			assert.Equal(t, i, r.errors)
		}
		_, err := r.Push(stray)
		require.ErrorIs(t, err, ErrTooManyErrors)
	})

	t.Run("sequence zero without first flag", func(t *testing.T) {
		t.Parallel()
		r, _ := newTestReassembler(t)
		bad := EncodeChunk(Header{Version: ProtocolVersion, Seq: 0}, []byte("x"))
		_, err := r.Push(bad)
		require.NoError(t, err)
		assert.Equal(t, 1, r.errors)
	})
}

func TestReassemblerEarlyChunkBeforeFirst(t *testing.T) {
	t.Parallel()

	// BlueZ may hand us chunk 1 before chunk 0; it must be held, not lost.
	chunks, err := Chunker{MTU: 23}.Split(message(40))
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	r, _ := newTestReassembler(t)
	msgs := feed(t, r, [][]byte{chunks[1], chunks[0], chunks[2]})
	require.Len(t, msgs, 1)
	assert.Equal(t, message(40), msgs[0])
	assert.Equal(t, 0, r.errors)
}

func TestReassemblerIdlePartialIsDiscarded(t *testing.T) {
	t.Parallel()

	chunks, err := Chunker{MTU: 23}.Split(message(100))
	require.NoError(t, err)

	r, clock := newTestReassembler(t)
	_, err = r.Push(chunks[0])
	require.NoError(t, err)

	clock.Advance(PartialIdleTimeout + time.Millisecond)

	// The stale partial is gone: a fresh message completes cleanly and the
	// restart is not charged as a mistake.
	fresh, err := Chunker{MTU: 23}.Split([]byte("hello"))
	require.NoError(t, err)
	msg, err := r.Push(fresh[0])
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), msg)
	assert.Equal(t, 0, r.errors)

	// The continuation of the stale message looks like an early chunk of
	// the next message, so it is held rather than charged, and forgotten
	// once it too goes idle.
	msg, err = r.Push(chunks[1])
	require.NoError(t, err)
	assert.Nil(t, msg)
	assert.Equal(t, 0, r.errors)
	assert.Len(t, r.held, 1)
	clock.Advance(PartialIdleTimeout + time.Millisecond)
	msg, err = r.Push(fresh[0])
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), msg)
	assert.Empty(t, r.held)
}

func TestNewInfo(t *testing.T) {
	t.Parallel()

	info := NewInfo("device-1")
	assert.Equal(t, "device-1", info.DeviceID)
	assert.Equal(t, ProtocolVersion, info.Version)
	assert.Equal(t, MaxMessageSize, info.MaxMessage)
	assert.Equal(t, PreferredMTU, info.PreferredMTU)
}
