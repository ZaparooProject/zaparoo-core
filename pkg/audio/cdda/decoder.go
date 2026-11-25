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

package cdda

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/rs/zerolog/log"
)

// errMediaGone is returned by a frame reader whose disc was ejected. It ends
// playback as if the album had finished.
var errMediaGone = errors.New("disc removed")

// errDecoderClosed is returned by a seek on a closed decoder.
var errDecoderClosed = errors.New("decoder closed")

// readChunkFrames is how many frames one read pulls from a source.
const readChunkFrames = 27 * FramesPerSector

// frameReader reads PCM from an album's sources.
type frameReader interface {
	// readFrames fills p, a whole number of frames, with the frames of
	// track t starting at frame off within its source. On an error it
	// returns how many bytes of whole frames it filled first.
	readFrames(t *Track, off int64, p []byte) (int, error)
	close() error
}

// Decoder plays an album's tracks back to back as one stream of 44.1 kHz
// frames. It implements beep.StreamSeekCloser. Stream blocks until it has
// filled its buffer or reached the end, as the resampler takes a short read
// for the end of the stream.
type Decoder struct {
	reader frameReader
	err    error
	album  *Album
	// starts is the stream frame each track begins at; starts[len] is the
	// stream's length.
	starts []int
	buf    []byte
	pos    int
	// ended is set when a rip file ended early; a seek clears it, as the
	// file may still hold what the seek asks for.
	ended bool
	// gone is set when the disc was removed, which no seek undoes.
	gone   bool
	closed bool
}

// trackStarts lays an album's tracks out one after another, failing when the
// stream would be too long to address, as an 8 GiB rip is on 32-bit builds.
func trackStarts(album *Album) ([]int, error) {
	starts := make([]int, len(album.Tracks)+1)
	for i := range album.Tracks {
		frames := album.Tracks[i].Frames()
		if frames < 0 || frames > int64(math.MaxInt-starts[i]) {
			return nil, fmt.Errorf("album is too long: track %d", album.Tracks[i].Number)
		}
		starts[i+1] = starts[i] + int(frames)
	}
	return starts, nil
}

func newDecoder(album *Album, starts []int, r frameReader) *Decoder {
	return &Decoder{
		reader: r,
		album:  album,
		starts: starts,
		buf:    make([]byte, readChunkFrames*bytesPerFrame),
	}
}

// Album is what the decoder plays.
func (d *Decoder) Album() *Album { return d.album }

// TrackStarts is the stream frame each track begins at.
func (d *Decoder) TrackStarts() []int { return slices.Clone(d.starts[:len(d.starts)-1]) }

// TrackMeta is the tags of track i.
func (d *Decoder) TrackMeta(i int) *trackmeta.Meta { return d.album.TrackMeta(i) }

// SampleRate is the stream's sample rate.
func (*Decoder) SampleRate() int { return SampleRate }

// Stream implements beep.Streamer.
func (d *Decoder) Stream(samples [][2]float64) (int, bool) {
	total := d.Len()
	n := 0
	for n < len(samples) && d.pos < total && !d.ended && !d.gone && !d.closed && d.err == nil {
		i := sort.SearchInts(d.starts, d.pos+1) - 1
		t := &d.album.Tracks[i]
		off := int64(d.pos - d.starts[i])
		want := min(len(samples)-n, d.starts[i+1]-d.pos, readChunkFrames)
		p := d.buf[:want*bytesPerFrame]
		filled, err := d.reader.readFrames(t, t.Start+off, p)
		got := want
		if err != nil {
			got = min(filled/bytesPerFrame, want)
		}
		for j := range got {
			b := p[j*bytesPerFrame:]
			samples[n+j][0] = pcmSample(b[0], b[1])
			samples[n+j][1] = pcmSample(b[2], b[3])
		}
		n += got
		d.pos += got
		switch {
		case err == nil:
		case errors.Is(err, errMediaGone):
			log.Warn().Err(err).Int("track", t.Number).Msg("cd audio ended early")
			d.gone = true
		case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
			log.Warn().Err(err).Int("track", t.Number).Msg("cd audio ended early")
			d.ended = true
		default:
			d.err = fmt.Errorf("read track %d: %w", t.Number, err)
		}
	}
	return n, n > 0
}

// pcmSample is a signed 16-bit little-endian sample scaled to -1..1.
func pcmSample(lo, hi byte) float64 {
	v := int(lo) | int(hi)<<8
	if v >= 0x8000 {
		v -= 0x10000
	}
	return float64(v) / 32768
}

// Err implements beep.Streamer. A removed disc is not an error.
func (d *Decoder) Err() error { return d.err }

// Len implements beep.StreamSeeker.
func (d *Decoder) Len() int { return d.starts[len(d.starts)-1] }

// Position implements beep.StreamSeeker.
func (d *Decoder) Position() int { return d.pos }

// Seek implements beep.StreamSeeker.
func (d *Decoder) Seek(p int) error {
	if d.closed {
		return errDecoderClosed
	}
	if p < 0 || p > d.Len() {
		return fmt.Errorf("seek %d out of range [0, %d]", p, d.Len())
	}
	d.pos = p
	d.ended = false
	return nil
}

// Close releases the decoder's files or drive. A closed decoder streams
// nothing more.
func (d *Decoder) Close() error {
	d.closed = true
	return d.reader.close()
}
