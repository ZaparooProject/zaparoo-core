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

package nativedec

/*
#include <stdlib.h>
#include "decoders.h"
#include "streamdec.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

// AAC is ADTS-framed AAC (LC, HE-AAC v1 and v2). Only a StreamDecoder
// reads it; KindForExt does not map ".aac" to it.
const AAC Kind = C.ND_AAC

var (
	// ErrNeedMore is returned by StreamDecoder.Read when the buffered input
	// holds no further complete frame: Feed more and read again.
	ErrNeedMore = errors.New("nativedec: decoder needs more input")
	// ErrStreamFull is returned by StreamDecoder.Feed when the bytes do not
	// fit in the bounded input buffer. Space says how many would.
	ErrStreamFull = errors.New("nativedec: stream input buffer full")
	// ErrUndecodable is returned once a stream yields no audio for too long:
	// a long run of garbage or of frames that fail to decode.
	ErrUndecodable = errors.New("nativedec: stream cannot be decoded")
	// ErrUnsupportedCodec is returned for an Ogg stream that carries another
	// codec than Vorbis, such as Opus or FLAC.
	ErrUnsupportedCodec = errors.New("nativedec: unsupported codec in Ogg stream")
)

// Input buffer bounds. A Vorbis decoder needs all three header packets at
// once, and the comment header can carry cover art, so it gets more room;
// MP3 and ADTS frames are at most a few KiB.
const (
	streamCapFrames = 64 << 10
	streamCapVorbis = 1 << 20
	// streamReadChunk is the most frames one C read produces.
	streamReadChunk = 4096
)

// StreamDecoder decodes an endless, unseekable stream of MP3, Ogg Vorbis
// or ADTS AAC pushed to it in arbitrary chunks, as a network stream
// arrives. The input is untrusted: garbage is skipped while resyncing,
// memory is bounded, and a long run without audio is an error.
//
// It is not safe for concurrent use.
type StreamDecoder struct {
	s    *C.nd_stream
	buf  []float32
	kind Kind
}

// NewStreamDecoder returns a decoder for kind: MP3, Vorbis or AAC.
func NewStreamDecoder(kind Kind) (*StreamDecoder, error) {
	limit := streamCapFrames
	switch kind {
	case MP3, AAC:
	case Vorbis:
		limit = streamCapVorbis
	default:
		return nil, fmt.Errorf("nativedec: no stream decoder for kind %d", kind)
	}
	s := C.nd_stream_open(C.int(kind), C.size_t(limit))
	if s == nil {
		return nil, errors.New("nativedec: cannot allocate stream decoder")
	}
	return &StreamDecoder{s: s, kind: kind}, nil
}

// Kind is the format the decoder reads.
func (d *StreamDecoder) Kind() Kind { return d.kind }

// Space is how many more bytes Feed accepts before Read consumes some.
func (d *StreamDecoder) Space() int {
	if d.s == nil {
		return 0
	}
	return int(C.nd_stream_space(d.s))
}

// Feed appends encoded bytes. It fails with ErrStreamFull, taking none of
// them, when they do not fit; read first, or feed less.
func (d *StreamDecoder) Feed(p []byte) error {
	if d.s == nil {
		return ErrClosed
	}
	if len(p) == 0 {
		return nil
	}
	if len(p) > d.Space() {
		return ErrStreamFull
	}
	if C.nd_stream_feed(d.s, (*C.uchar)(unsafe.Pointer(&p[0])), C.size_t(len(p))) != 0 {
		return ErrStreamFull
	}
	return nil
}

// Flush marks the input finished: Read decodes the last buffered bytes,
// which it otherwise holds back until more data confirms the frame sync,
// and then returns io.EOF. Reset clears it.
func (d *StreamDecoder) Flush() {
	if d.s != nil {
		C.nd_stream_end(d.s)
	}
}

// Read decodes into out as stereo frames: mono is copied to both channels
// and only the first two of more are kept. It returns ErrNeedMore when the
// buffered input is used up, and every frame of one call has the format
// Format reports after it. A stream that changes rate mid-way does so
// between calls.
func (d *StreamDecoder) Read(out [][2]float64) (int, error) {
	if d.s == nil {
		return 0, ErrClosed
	}
	if len(out) == 0 {
		return 0, nil
	}
	frames := min(len(out), streamReadChunk)
	if cap(d.buf) < frames*2 {
		d.buf = make([]float32, streamReadChunk*2)
	}
	buf := d.buf[:frames*2]
	var got C.int
	status := C.nd_stream_read(d.s, (*C.float)(unsafe.Pointer(&buf[0])), C.int(frames), &got)
	n := int(got)
	for i := range n {
		out[i] = [2]float64{finite(buf[i*2]), finite(buf[i*2+1])}
	}
	switch status {
	case C.NDS_OK:
		return n, nil
	case C.NDS_NEED_MORE:
		return n, ErrNeedMore
	case C.NDS_EOF:
		return n, io.EOF
	case C.NDS_UNSUPPORTED:
		return n, ErrUnsupportedCodec
	default:
		return n, ErrUndecodable
	}
}

// Format is the sample rate and channel count of the frames the last Read
// returned, or of the stream's headers before any were read. ok is false
// until they are known. HE-AAC reports the SBR output rate.
func (d *StreamDecoder) Format() (rate, channels int, ok bool) {
	if d.s == nil {
		return 0, 0, false
	}
	var r, c C.uint
	C.nd_stream_format(d.s, &r, &c)
	if r == 0 || c == 0 {
		return 0, 0, false
	}
	return int(r), int(c), true
}

// Reset drops the buffered input, decoded audio and decoder state, as for
// a discontinuity in the stream; the decoder resyncs on what is fed next.
func (d *StreamDecoder) Reset() {
	if d.s != nil {
		C.nd_stream_reset(d.s)
	}
}

// Close frees the decoder. It is safe to call more than once.
func (d *StreamDecoder) Close() error {
	if d.s != nil {
		C.nd_stream_close(d.s)
		d.s = nil
	}
	return nil
}
