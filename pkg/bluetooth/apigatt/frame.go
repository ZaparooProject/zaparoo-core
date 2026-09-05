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
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
)

// Chunk layout, both directions:
//
//	byte 0    flags   bits 7..4 protocol version, bit 1 LAST, bit 0 FIRST,
//	                  bits 3..2 reserved and zero
//	byte 1    seq     0 on FIRST, +1 per chunk, wraps at 256
//	byte 2-3  tag     session tag, big endian
//	byte 4-7  length  total message length, big endian, FIRST only
//	payload           at least one byte
const (
	// HeaderSize is the header on every chunk but the first of a message.
	HeaderSize = 4
	// FirstHeaderSize is the header on the first chunk of a message.
	FirstHeaderSize = 8

	flagFirst        = 0x01
	flagLast         = 0x02
	flagReservedMask = 0x0c
	versionShift     = 4
)

var (
	// ErrMalformedChunk means the chunk cannot be parsed at all: too short,
	// wrong version, reserved bits set. The connection should be dropped.
	ErrMalformedChunk = errors.New("apigatt: malformed chunk")
	// ErrMessageTooLarge means a message declared or accumulated more than
	// MaxMessageSize bytes.
	ErrMessageTooLarge = errors.New("apigatt: message too large")
	// ErrLengthMismatch means the chunks did not add up to the declared
	// length.
	ErrLengthMismatch = errors.New("apigatt: message length mismatch")
	// ErrSequence means a chunk arrived outside the reorder window or
	// repeated a sequence number.
	ErrSequence = errors.New("apigatt: chunk out of sequence")
	// ErrTooManyErrors means the peer made more recoverable mistakes than
	// MaxProtocolErrors allows.
	ErrTooManyErrors = errors.New("apigatt: too many protocol errors")
	// ErrEmptyMessage means there is nothing to send.
	ErrEmptyMessage = errors.New("apigatt: empty message")
)

// Header is the decoded chunk header.
type Header struct {
	Length  uint32 // valid only when First
	Tag     uint16
	Version uint8
	Seq     uint8
	First   bool
	Last    bool
}

// ParseChunk decodes one chunk into its header and payload. The payload
// aliases chunk.
func ParseChunk(chunk []byte) (Header, []byte, error) {
	if len(chunk) < HeaderSize {
		return Header{}, nil, fmt.Errorf("%w: %d bytes", ErrMalformedChunk, len(chunk))
	}
	flags := chunk[0]
	h := Header{
		Version: flags >> versionShift,
		First:   flags&flagFirst != 0,
		Last:    flags&flagLast != 0,
		Seq:     chunk[1],
		Tag:     binary.BigEndian.Uint16(chunk[2:4]),
	}
	if h.Version != ProtocolVersion {
		return Header{}, nil, fmt.Errorf("%w: version %d", ErrMalformedChunk, h.Version)
	}
	if flags&flagReservedMask != 0 {
		return Header{}, nil, fmt.Errorf("%w: reserved flag bits set", ErrMalformedChunk)
	}
	headerSize := HeaderSize
	if h.First {
		if h.Seq != 0 {
			return Header{}, nil, fmt.Errorf("%w: first chunk has sequence %d", ErrMalformedChunk, h.Seq)
		}
		if len(chunk) < FirstHeaderSize {
			return Header{}, nil, fmt.Errorf("%w: first chunk is %d bytes", ErrMalformedChunk, len(chunk))
		}
		h.Length = binary.BigEndian.Uint32(chunk[4:8])
		headerSize = FirstHeaderSize
		if h.Length == 0 {
			return Header{}, nil, fmt.Errorf("%w: zero length", ErrMalformedChunk)
		}
		if h.Length > MaxMessageSize {
			return Header{}, nil, fmt.Errorf("%w: declared %d bytes", ErrMessageTooLarge, h.Length)
		}
	}
	if len(chunk) == headerSize {
		return Header{}, nil, fmt.Errorf("%w: empty payload", ErrMalformedChunk)
	}
	return h, chunk[headerSize:], nil
}

// EncodeChunk builds one chunk. Length is written only for a first chunk.
func EncodeChunk(h Header, payload []byte) []byte {
	size := HeaderSize
	if h.First {
		size = FirstHeaderSize
	}
	out := make([]byte, size, size+len(payload))
	flags := byte(ProtocolVersion) << versionShift
	if h.First {
		flags |= flagFirst
	}
	if h.Last {
		flags |= flagLast
	}
	out[0] = flags
	out[1] = h.Seq
	binary.BigEndian.PutUint16(out[2:4], h.Tag)
	if h.First {
		binary.BigEndian.PutUint32(out[4:8], h.Length)
	}
	return append(out, payload...)
}

// Chunker splits messages into chunks that fit the link's ATT MTU.
type Chunker struct {
	// MTU is the negotiated ATT MTU; anything below DefaultMTU is treated
	// as DefaultMTU.
	MTU int
	// Tag is stamped on every chunk.
	Tag uint16
}

// chunkPayload is how many payload bytes fit after a header of the given
// size.
func (c Chunker) chunkPayload(headerSize int) int {
	mtu := c.MTU
	if mtu < DefaultMTU {
		mtu = DefaultMTU
	}
	return mtu - attHeaderSize - headerSize
}

// Split cuts msg into chunks in transmission order.
func (c Chunker) Split(msg []byte) ([][]byte, error) {
	if len(msg) == 0 {
		return nil, ErrEmptyMessage
	}
	if len(msg) > MaxMessageSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrMessageTooLarge, len(msg))
	}

	var chunks [][]byte
	var seq uint8
	offset := 0
	for offset < len(msg) {
		first := offset == 0
		headerSize := HeaderSize
		if first {
			headerSize = FirstHeaderSize
		}
		n := min(c.chunkPayload(headerSize), len(msg)-offset)
		h := Header{
			Version: ProtocolVersion,
			Seq:     seq,
			Tag:     c.Tag,
			First:   first,
			Last:    offset+n == len(msg),
		}
		if first {
			h.Length = uint32(len(msg)) //nolint:gosec // bounded by MaxMessageSize above
		}
		chunks = append(chunks, EncodeChunk(h, msg[offset:offset+n]))
		offset += n
		seq++
	}
	return chunks, nil
}

// heldChunk is a chunk waiting for the ones before it.
type heldChunk struct {
	payload []byte
	last    bool
}

// Reassembler rebuilds messages from chunks for one connection. It
// tolerates chunks arriving slightly out of order, caps memory, and drops a
// half-received message that stalls.
type Reassembler struct {
	clock       clockwork.Clock
	held        map[uint8]heldChunk
	lastChunkAt time.Time
	buf         []byte
	maxMessage  int
	heldBytes   int
	errors      int
	expected    uint32
	nextSeq     uint8
	inProgress  bool
}

// NewReassembler returns a reassembler; maxMessage of zero or less means
// MaxMessageSize.
func NewReassembler(clock clockwork.Clock, maxMessage int) *Reassembler {
	if maxMessage <= 0 || maxMessage > MaxMessageSize {
		maxMessage = MaxMessageSize
	}
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &Reassembler{clock: clock, held: make(map[uint8]heldChunk), maxMessage: maxMessage}
}

// Push consumes one chunk. It returns the completed message when this chunk
// finished one. A non-nil error means the connection should be dropped;
// the reassembler is unusable afterwards.
func (r *Reassembler) Push(chunk []byte) ([]byte, error) {
	h, payload, err := ParseChunk(chunk)
	if err != nil {
		return nil, err
	}

	now := r.clock.Now()
	if (r.inProgress || len(r.held) > 0) && now.Sub(r.lastChunkAt) > PartialIdleTimeout {
		r.reset()
	}
	r.lastChunkAt = now

	if h.First {
		if r.inProgress {
			// The peer started over mid-message: a restarted client, or
			// one that gave up on a message. Recoverable, but counted, and
			// whatever was held belonged to the abandoned message.
			if err := r.recoverable(); err != nil {
				return nil, err
			}
			r.dropHeld()
		}
		r.startMessage(h.Length)
		return r.apply(payload, h.Last)
	}

	distance := h.Seq - r.nextSeq
	switch {
	case distance == 0 && !r.inProgress:
		// Sequence zero without the FIRST flag can never be applied.
		return nil, r.recoverable()
	case distance == 0:
		return r.apply(payload, h.Last)
	case distance < ReorderWindow:
		if _, dup := r.held[h.Seq]; dup {
			return nil, fmt.Errorf("%w: duplicate chunk %d", ErrSequence, h.Seq)
		}
		if err := r.hold(h.Seq, payload, h.Last); err != nil {
			return nil, err
		}
		return nil, nil
	case !r.inProgress:
		// A stray chunk far from any message we are receiving.
		return nil, r.recoverable()
	default:
		return nil, fmt.Errorf("%w: chunk %d, expected %d", ErrSequence, h.Seq, r.nextSeq)
	}
}

// recoverable counts a protocol mistake and fails once the budget is used.
func (r *Reassembler) recoverable() error {
	r.errors++
	if r.errors >= MaxProtocolErrors {
		return ErrTooManyErrors
	}
	return nil
}

func (r *Reassembler) startMessage(length uint32) {
	r.buf = r.buf[:0]
	r.expected = length
	r.nextSeq = 0
	r.inProgress = true
}

func (r *Reassembler) reset() {
	r.buf = r.buf[:0]
	r.expected = 0
	r.nextSeq = 0
	r.inProgress = false
	r.dropHeld()
}

func (r *Reassembler) dropHeld() {
	r.held = make(map[uint8]heldChunk)
	r.heldBytes = 0
}

// hold keeps an early chunk until its predecessors arrive.
func (r *Reassembler) hold(seq uint8, payload []byte, last bool) error {
	if len(r.buf)+r.heldBytes+len(payload) > r.maxMessage {
		return fmt.Errorf("%w: held chunks exceed %d bytes", ErrMessageTooLarge, r.maxMessage)
	}
	r.held[seq] = heldChunk{payload: append([]byte(nil), payload...), last: last}
	r.heldBytes += len(payload)
	return nil
}

// apply appends the expected chunk, then any held chunks that now follow
// in sequence, and returns the message once the last chunk lands.
func (r *Reassembler) apply(payload []byte, last bool) ([]byte, error) {
	for {
		if int(r.expected) > r.maxMessage {
			return nil, fmt.Errorf("%w: declared %d bytes", ErrMessageTooLarge, r.expected)
		}
		if len(r.buf)+len(payload) > int(r.expected) {
			return nil, fmt.Errorf("%w: %d bytes exceeds declared %d",
				ErrLengthMismatch, len(r.buf)+len(payload), r.expected)
		}
		r.buf = append(r.buf, payload...)
		r.nextSeq++
		if last {
			if len(r.buf) != int(r.expected) {
				return nil, fmt.Errorf("%w: got %d bytes, declared %d", ErrLengthMismatch, len(r.buf), r.expected)
			}
			if len(r.held) > 0 {
				return nil, fmt.Errorf("%w: chunks beyond the last one", ErrSequence)
			}
			msg := append([]byte(nil), r.buf...)
			r.reset()
			return msg, nil
		}
		next, ok := r.held[r.nextSeq]
		if !ok {
			return nil, nil
		}
		delete(r.held, r.nextSeq)
		r.heldBytes -= len(next.payload)
		payload, last = next.payload, next.last
	}
}
