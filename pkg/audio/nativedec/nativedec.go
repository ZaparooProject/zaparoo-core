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

// Package nativedec decodes MP3, FLAC and Ogg Vorbis with dr_mp3, dr_flac
// and stb_vorbis, single-file C decoders vendored in third_party, and AAC
// with libfaad (faad2) for network streams; see StreamDecoder. The pure
// Go decoders cost about half of a MiSTer CPU core to play one MP3, which
// the background music slot spent for as long as it played; these decode
// the same audio for a fraction of that. A Decoder is a beep
// StreamSeekCloser, so it slots in wherever the Go decoders were.
package nativedec

/*
#cgo CFLAGS: -O2 -I${SRCDIR}/third_party -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -I${SRCDIR}/third_party/faad2/include -I${SRCDIR}/third_party/faad2/libfaad
#cgo linux LDFLAGS: -lm
#include <stdlib.h>
#include "decoders.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unsafe"
)

// Kind is an encoded audio format a Decoder reads.
type Kind int

const (
	MP3    Kind = C.ND_MP3
	FLAC   Kind = C.ND_FLAC
	Vorbis Kind = C.ND_VORBIS
)

// KindForExt returns the Kind for a file extension (".mp3", ".flac",
// ".ogg"), case-insensitively.
func KindForExt(ext string) (Kind, bool) {
	switch strings.ToLower(ext) {
	case ".mp3":
		return MP3, true
	case ".flac":
		return FLAC, true
	case ".ogg":
		return Vorbis, true
	default:
		return 0, false
	}
}

// ErrClosed is returned by a Decoder used after Close.
var ErrClosed = errors.New("nativedec: decoder closed")

// Decoder streams one decoded file as stereo frames. It is not safe for
// concurrent use; the playback code drives it from one goroutine.
type Decoder struct {
	d        *C.nd_decoder
	err      error
	buf      []float32
	rate     int
	channels int
	length   int
	pos      int
}

// Open decodes the file at path.
func Open(path string, kind Kind) (*Decoder, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var channels, rate C.uint
	var frames C.longlong
	d := C.nd_open(cpath, C.int(kind), &channels, &rate, &frames)
	if d == nil {
		return nil, fmt.Errorf("nativedec: cannot decode %s", path)
	}
	return newDecoder(d, channels, rate, frames)
}

// OpenBytes decodes data held in memory.
func OpenBytes(data []byte, kind Kind) (*Decoder, error) {
	if len(data) == 0 {
		return nil, errors.New("nativedec: no data")
	}
	// The C side keeps reading the encoded data for as long as the decoder
	// lives, so it gets its own copy, which it frees.
	cdata := C.CBytes(data)
	var channels, rate C.uint
	var frames C.longlong
	d := C.nd_open_memory(cdata, C.size_t(len(data)), C.int(kind), &channels, &rate, &frames)
	if d == nil {
		return nil, errors.New("nativedec: cannot decode data")
	}
	return newDecoder(d, channels, rate, frames)
}

func newDecoder(d *C.nd_decoder, channels, rate C.uint, frames C.longlong) (*Decoder, error) {
	if channels == 0 || rate == 0 {
		C.nd_close(d)
		return nil, errors.New("nativedec: no audio channels")
	}
	return &Decoder{
		d:        d,
		channels: int(channels),
		rate:     int(rate),
		length:   lengthInFrames(frames),
	}, nil
}

// lengthInFrames is a decoder's frame count as an int, or -1 when it is
// unknown or does not fit: int is 32 bits on the ARM32 MiSTer, where a long
// file's count would otherwise wrap to a wrong, positive length.
func lengthInFrames(frames C.longlong) int {
	return boundedLength(int64(frames), int64(maxInt))
}

const maxInt = int(^uint(0) >> 1)

// boundedLength is frames, or -1 when it is negative or above limit.
func boundedLength(frames, limit int64) int {
	if frames < 0 || frames > limit {
		return -1
	}
	return int(frames)
}

// SampleRate is the decoded audio's sample rate.
func (d *Decoder) SampleRate() int { return d.rate }

// Channels is how many channels the file has. Stream always produces two:
// mono is copied to both, and only the first two of more are kept.
func (d *Decoder) Channels() int { return d.channels }

// Stream fills samples with the next frames. It returns how many it wrote,
// and false once the audio has ended or failed.
func (d *Decoder) Stream(samples [][2]float64) (int, bool) {
	if d.d == nil {
		d.err = ErrClosed
		return 0, false
	}
	if len(samples) == 0 {
		return 0, true
	}
	need := len(samples) * d.channels
	if cap(d.buf) < need {
		d.buf = make([]float32, need)
	}
	buf := d.buf[:need]
	n := int(C.nd_read(d.d, (*C.float)(unsafe.Pointer(&buf[0])), C.longlong(len(samples))))
	if n <= 0 {
		if C.nd_failed(d.d) != 0 {
			d.err = fmt.Errorf("nativedec: decode failed at frame %d", d.pos)
		}
		return 0, false
	}
	switch d.channels {
	case 1:
		for i := range n {
			v := finite(buf[i])
			samples[i] = [2]float64{v, v}
		}
	default:
		for i := range n {
			at := i * d.channels
			samples[i] = [2]float64{finite(buf[at]), finite(buf[at+1])}
		}
	}
	d.pos += n
	return n, true
}

// Err returns the error that stopped the stream, if any.
func (d *Decoder) Err() error { return d.err }

// finite widens a decoded sample, turning NaN and infinity into silence.
// A hostile file can make the decoders emit them, and one non-finite
// sample would poison every sound it is mixed with.
func finite(v float32) float64 {
	f := float64(v)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

// Len is the length in frames, or -1 when the file does not say.
func (d *Decoder) Len() int { return d.length }

// Position is the next frame Stream returns.
func (d *Decoder) Position() int { return d.pos }

// Seek moves to frame p.
func (d *Decoder) Seek(p int) error {
	if d.d == nil {
		return ErrClosed
	}
	if p < 0 || (d.length >= 0 && p > d.length) {
		return fmt.Errorf("nativedec: seek to %d outside 0..%d", p, d.length)
	}
	if C.nd_seek(d.d, C.longlong(p)) == 0 {
		return fmt.Errorf("nativedec: seek to %d failed", p)
	}
	d.pos = p
	return nil
}

// Close frees the decoder. It is safe to call more than once.
func (d *Decoder) Close() error {
	if d.d != nil {
		C.nd_close(d.d)
		d.d = nil
	}
	return nil
}
