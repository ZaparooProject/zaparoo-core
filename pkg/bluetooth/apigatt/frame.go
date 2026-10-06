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
	"math"
)

// Chunk layout, both directions:
//
//	byte 0    flags   bits 7..4 protocol version, bit 2 ACK, bit 1 LAST,
//	                  bit 0 FIRST, bit 3 reserved and zero
//	byte 1-2  seq     big endian; see below
//	byte 3-4  tag     session tag, big endian
//	byte 5-8  length  total message length, big endian, FIRST only
//	payload           at least one byte, none on an ACK
//
// On a data chunk seq counts every data chunk the sender has sent on this
// connection, starting at 0 and wrapping at 65536. It does not restart with
// each message, so the receiver can put chunks back in order across message
// boundaries as well as inside one.
//
// An ACK chunk is sent by the client only. Its seq is the sequence number of
// the next data chunk the client expects from Core, which acknowledges
// everything before it. Core never sends more than SendWindow chunks beyond
// the last acknowledgement.
const (
	// HeaderSize is the header on every chunk but the first of a message.
	HeaderSize = 5
	// FirstHeaderSize is the header on the first chunk of a message.
	FirstHeaderSize = 9

	flagFirst        = 0x01
	flagLast         = 0x02
	flagAck          = 0x04
	flagReservedMask = 0x08
	versionShift     = 4
)

var (
	// ErrMalformedChunk means the chunk cannot be parsed at all: too short,
	// wrong version, reserved bits set. The connection should be dropped.
	ErrMalformedChunk = errors.New("apigatt: malformed chunk")
	// ErrMessageTooLarge means a message declared more bytes than the
	// receiver accepts.
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
	Seq     uint16
	Tag     uint16
	Version uint8
	First   bool
	Last    bool
	Ack     bool
}

// ParseChunk decodes one chunk into its header and payload. The payload
// aliases chunk and is empty for an ACK.
func ParseChunk(chunk []byte) (Header, []byte, error) {
	if len(chunk) < HeaderSize {
		return Header{}, nil, fmt.Errorf("%w: %d bytes", ErrMalformedChunk, len(chunk))
	}
	flags := chunk[0]
	h := Header{
		Version: flags >> versionShift,
		First:   flags&flagFirst != 0,
		Last:    flags&flagLast != 0,
		Ack:     flags&flagAck != 0,
		Seq:     binary.BigEndian.Uint16(chunk[1:3]),
		Tag:     binary.BigEndian.Uint16(chunk[3:5]),
	}
	if h.Version != ProtocolVersion {
		return Header{}, nil, fmt.Errorf("%w: version %d", ErrMalformedChunk, h.Version)
	}
	if flags&flagReservedMask != 0 {
		return Header{}, nil, fmt.Errorf("%w: reserved flag bits set", ErrMalformedChunk)
	}
	if h.Ack {
		if h.First || h.Last || len(chunk) != HeaderSize {
			return Header{}, nil, fmt.Errorf("%w: acknowledgement carries data", ErrMalformedChunk)
		}
		return h, nil, nil
	}
	headerSize := HeaderSize
	if h.First {
		if len(chunk) < FirstHeaderSize {
			return Header{}, nil, fmt.Errorf("%w: first chunk is %d bytes", ErrMalformedChunk, len(chunk))
		}
		h.Length = binary.BigEndian.Uint32(chunk[5:9])
		headerSize = FirstHeaderSize
		if h.Length == 0 {
			return Header{}, nil, fmt.Errorf("%w: zero length", ErrMalformedChunk)
		}
	}
	if len(chunk) == headerSize {
		return Header{}, nil, fmt.Errorf("%w: empty payload", ErrMalformedChunk)
	}
	return h, chunk[headerSize:], nil
}

// EncodeChunk builds one data chunk. Length is written only for a first
// chunk.
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
	binary.BigEndian.PutUint16(out[1:3], h.Seq)
	binary.BigEndian.PutUint16(out[3:5], h.Tag)
	if h.First {
		binary.BigEndian.PutUint32(out[5:9], h.Length)
	}
	return append(out, payload...)
}

// EncodeAck builds the chunk that acknowledges every data chunk before next.
func EncodeAck(tag, next uint16) []byte {
	out := make([]byte, HeaderSize)
	out[0] = byte(ProtocolVersion)<<versionShift | flagAck
	binary.BigEndian.PutUint16(out[1:3], next)
	binary.BigEndian.PutUint16(out[3:5], tag)
	return out
}

// Chunker splits one side's messages into chunks for one connection. It
// carries the sequence counter, so a connection needs exactly one per
// direction and must not use it from two goroutines at once.
type Chunker struct {
	// Tag is stamped on every chunk.
	Tag uint16
	seq uint16
}

// Next is the sequence number the next data chunk will carry.
func (c *Chunker) Next() uint16 {
	return c.seq
}

// chunkPayload is how many payload bytes fit after a header of the given
// size. Anything below DefaultMTU is treated as DefaultMTU.
func chunkPayload(mtu, headerSize int) int {
	if mtu < DefaultMTU {
		mtu = DefaultMTU
	}
	return min(mtu-attHeaderSize, MaxChunkSize) - headerSize
}

// Split cuts msg into chunks that fit the link's ATT MTU, and never more
// than MaxChunkSize, and hands them to
// emit in transmission order, one at a time, so a large message is never
// held as chunks all at once. It stops at the first error from emit; the
// sequence counter has then moved past the chunks already emitted.
func (c *Chunker) Split(msg []byte, mtu int, emit func(chunk []byte) error) error {
	if len(msg) == 0 {
		return ErrEmptyMessage
	}
	if uint64(len(msg)) > math.MaxUint32 {
		return fmt.Errorf("%w: %d bytes", ErrMessageTooLarge, len(msg))
	}

	offset := 0
	for offset < len(msg) {
		first := offset == 0
		headerSize := HeaderSize
		if first {
			headerSize = FirstHeaderSize
		}
		n := min(chunkPayload(mtu, headerSize), len(msg)-offset)
		h := Header{
			Version: ProtocolVersion,
			Seq:     c.seq,
			Tag:     c.Tag,
			First:   first,
			Last:    offset+n == len(msg),
		}
		if first {
			h.Length = uint32(len(msg)) //nolint:gosec // bounded by the check above
		}
		c.seq++
		if err := emit(EncodeChunk(h, msg[offset:offset+n])); err != nil {
			return err
		}
		offset += n
	}
	return nil
}

// heldChunk is a chunk waiting for the ones before it.
type heldChunk struct {
	payload []byte
	header  Header
}

// Reassembler rebuilds one side's messages from chunks for one connection.
// Chunks may arrive in any order within ReorderWindow; messages come out in
// the order they were sent.
type Reassembler struct {
	held       map[uint16]heldChunk
	buf        []byte
	heldBytes  int
	errors     int
	maxMessage uint32
	expected   uint32
	nextSeq    uint16
	inProgress bool
}

// NewReassembler returns a reassembler accepting messages up to maxMessage
// bytes. Zero or less means no limit, which is what a client receiving from
// Core wants; Core always sets one.
func NewReassembler(maxMessage int) *Reassembler {
	r := &Reassembler{held: make(map[uint16]heldChunk)}
	r.SetMaxMessage(maxMessage)
	return r
}

// SetMaxMessage changes the largest message accepted from now on. Zero or
// less means no limit.
func (r *Reassembler) SetMaxMessage(maxMessage int) {
	if maxMessage <= 0 || uint64(maxMessage) > math.MaxUint32 {
		r.maxMessage = math.MaxUint32
		return
	}
	r.maxMessage = uint32(maxMessage)
}

// Next is the sequence number of the next chunk the reassembler is waiting
// for, which is what a client sends in an ACK.
func (r *Reassembler) Next() uint16 {
	return r.nextSeq
}

// Held is how many chunks are waiting for an earlier one that has not
// arrived. It staying above zero means a chunk was lost.
func (r *Reassembler) Held() int {
	return len(r.held)
}

// Push consumes one data chunk already decoded by ParseChunk and returns
// the messages it completed, oldest first: a chunk that fills a gap can
// release several. A non-nil error means the connection should be dropped;
// the reassembler is unusable afterwards.
func (r *Reassembler) Push(h Header, payload []byte) ([][]byte, error) {
	if h.Ack {
		return nil, fmt.Errorf("%w: acknowledgement is not data", ErrMalformedChunk)
	}
	distance := h.Seq - r.nextSeq
	switch {
	case distance == 0:
		return r.drain(h, payload)
	case distance < ReorderWindow:
		if _, dup := r.held[h.Seq]; dup {
			return nil, fmt.Errorf("%w: duplicate chunk %d", ErrSequence, h.Seq)
		}
		if r.heldBytes+len(payload) > maxHeldBytes {
			return nil, fmt.Errorf("%w: too much held waiting for chunk %d", ErrSequence, r.nextSeq)
		}
		r.held[h.Seq] = heldChunk{header: h, payload: append([]byte(nil), payload...)}
		r.heldBytes += len(payload)
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: chunk %d, expected %d", ErrSequence, h.Seq, r.nextSeq)
	}
}

// drain applies the expected chunk, then every held chunk that now follows
// in sequence.
func (r *Reassembler) drain(h Header, payload []byte) ([][]byte, error) {
	var msgs [][]byte
	for {
		msg, err := r.apply(h, payload)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			msgs = append(msgs, msg)
		}
		next, ok := r.held[r.nextSeq]
		if !ok {
			return msgs, nil
		}
		delete(r.held, r.nextSeq)
		r.heldBytes -= len(next.payload)
		h, payload = next.header, next.payload
	}
}

// apply appends the chunk the sequence was waiting for and returns the
// message if this chunk finished one.
func (r *Reassembler) apply(h Header, payload []byte) ([]byte, error) {
	r.nextSeq++
	switch {
	case h.First:
		if r.inProgress {
			// The peer started over mid-message: it gave up on the one
			// it was sending. Recoverable, but counted.
			if err := r.recoverable(); err != nil {
				return nil, err
			}
		}
		if h.Length > r.maxMessage {
			return nil, fmt.Errorf("%w: declared %d bytes, limit %d", ErrMessageTooLarge, h.Length, r.maxMessage)
		}
		r.buf = r.buf[:0]
		r.expected = h.Length
		r.inProgress = true
	case !r.inProgress:
		// A continuation with no message to continue.
		return nil, r.recoverable()
	}

	if uint64(len(r.buf)+len(payload)) > uint64(r.expected) {
		return nil, fmt.Errorf("%w: %d bytes exceeds declared %d",
			ErrLengthMismatch, len(r.buf)+len(payload), r.expected)
	}
	r.buf = append(r.buf, payload...)
	if !h.Last {
		return nil, nil
	}
	if uint64(len(r.buf)) != uint64(r.expected) {
		return nil, fmt.Errorf("%w: got %d bytes, declared %d", ErrLengthMismatch, len(r.buf), r.expected)
	}
	msg := r.buf
	// The buffer is handed to the caller rather than copied, and a fresh
	// one is grown for the next message, so a large message is not pinned
	// for the life of the connection.
	r.buf = nil
	r.expected = 0
	r.inProgress = false
	return msg, nil
}

// recoverable counts a protocol mistake and fails once the budget is used.
func (r *Reassembler) recoverable() error {
	r.errors++
	if r.errors >= MaxProtocolErrors {
		return ErrTooManyErrors
	}
	return nil
}
