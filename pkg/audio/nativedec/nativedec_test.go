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

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gopxl/beep/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures are one second of a 440 Hz tone: tone.mp3 (stereo, 44.1
// kHz), tone-mono.flac (mono, 44.1 kHz) and tone.ogg (stereo, 48 kHz).
var fixtures = []struct {
	name     string
	kind     Kind
	rate     int
	channels int
}{
	{name: "tone.mp3", kind: MP3, rate: 44100, channels: 2},
	{name: "tone-mono.flac", kind: FLAC, rate: 44100, channels: 1},
	{name: "tone.ogg", kind: Vorbis, rate: 48000, channels: 2},
}

var _ beep.StreamSeekCloser = (*Decoder)(nil)

// readAll streams a decoder to its end in chunks the playback code uses.
func readAll(t *testing.T, d *Decoder) [][2]float64 {
	t.Helper()
	var out [][2]float64
	chunk := make([][2]float64, 4410)
	for {
		n, ok := d.Stream(chunk)
		out = append(out, chunk[:n]...)
		if !ok {
			return out
		}
	}
}

// rms is the root mean square of the left channel.
func rms(frames [][2]float64) float64 {
	var sum float64
	for _, f := range frames {
		sum += f[0] * f[0]
	}
	return math.Sqrt(sum / float64(len(frames)))
}

func TestDecodesEachFormatToItsWholeLength(t *testing.T) {
	t.Parallel()

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()

			d, err := Open(filepath.Join("testdata", fx.name), fx.kind)
			require.NoError(t, err)
			defer func() { require.NoError(t, d.Close()) }()

			assert.Equal(t, fx.rate, d.SampleRate())
			assert.Equal(t, fx.channels, d.Channels())
			require.Positive(t, d.Len())
			frames := readAll(t, d)
			assert.Len(t, frames, d.Len(), "every frame the length promises is decoded")
			assert.InDelta(t, fx.rate, len(frames), float64(fx.rate)/20, "about one second")
			assert.Equal(t, len(frames), d.Position())
			// A full-scale sine has an RMS near 0.7 of its amplitude; ffmpeg's
			// tone is at 1/8 scale, so anything clearly above silence will do.
			assert.Greater(t, rms(frames), 0.02, "the tone is heard")
			assert.NoError(t, d.Err())
		})
	}
}

func TestMonoPlaysOnBothChannels(t *testing.T) {
	t.Parallel()

	d, err := Open(filepath.Join("testdata", "tone-mono.flac"), FLAC)
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	for _, f := range readAll(t, d) {
		require.InDelta(t, f[0], f[1], 0)
	}
}

// The seek fixtures are one second of a rising chirp, so no two stretches of
// them sound alike and a seek that lands in the wrong place shows.
var chirpFixtures = []struct {
	name string
	kind Kind
}{
	{name: "chirp.mp3", kind: MP3},
	{name: "chirp.flac", kind: FLAC},
	{name: "chirp.ogg", kind: Vorbis},
}

// maxSeekSlack is how far, in frames, a seek may land from the frame asked
// for: a codec frame or so.
const maxSeekSlack = 2048

func TestSeekResumesFromTheFrame(t *testing.T) {
	t.Parallel()

	for _, fx := range chirpFixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()

			d, err := Open(filepath.Join("testdata", fx.name), fx.kind)
			require.NoError(t, err)
			defer func() { require.NoError(t, d.Close()) }()

			whole := readAll(t, d)
			require.Greater(t, len(whole), 2*maxSeekSlack)
			for _, at := range []int{d.Len() / 2, d.Len() / 4, 0} {
				require.NoError(t, d.Seek(at))
				assert.Equal(t, at, d.Position())
				rest := readAll(t, d)
				require.NotEmpty(t, rest)

				// Find where the decoded samples sit in the whole track.
				shift := -1
				for s := max(at-maxSeekSlack, 0); s <= at+maxSeekSlack && s+len(rest) <= len(whole); s++ {
					if framesMatch(whole[s:s+len(rest)], rest) {
						shift = s
						break
					}
				}
				require.NotEqual(t, -1, shift, "seek to %d: the samples are the track's own, from one place", at)
				assert.InDelta(t, at, shift, maxSeekSlack, "seek to %d lands within a codec frame", at)
			}
			require.Error(t, d.Seek(-1))
			require.Error(t, d.Seek(d.Len()+1))
		})
	}
}

// framesMatch reports whether two stretches of audio are the same samples.
func framesMatch(a, b [][2]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i][0]-b[i][0]) > 1e-3 || math.Abs(a[i][1]-b[i][1]) > 1e-3 {
			return false
		}
	}
	return true
}

func TestLengthTooLongForIntIsUnknown(t *testing.T) {
	t.Parallel()

	const limit = math.MaxInt32 // what an int holds on a 32-bit build
	assert.Equal(t, 1000, boundedLength(1000, limit))
	assert.Equal(t, limit, boundedLength(limit, limit))
	assert.Equal(t, -1, boundedLength(limit+1, limit), "would wrap negative or to a wrong length")
	assert.Equal(t, -1, boundedLength(1<<33+5, limit), "would wrap to a small positive length")
	assert.Equal(t, -1, boundedLength(-1, limit))
}

func TestACutShortStreamIsAFailureNotTheEnd(t *testing.T) {
	t.Parallel()

	for _, fx := range []struct {
		name string
		kind Kind
	}{{"chirp.flac", FLAC}, {"chirp.ogg", Vorbis}} {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("testdata", fx.name)) //nolint:gosec // fixed test fixture
			require.NoError(t, err)
			d, err := OpenBytes(data[:len(data)*2/3], fx.kind)
			require.NoError(t, err)
			defer func() { require.NoError(t, d.Close()) }()

			frames := readAll(t, d)
			assert.NotEmpty(t, frames, "what was there still plays")
			require.Error(t, d.Err())
			assert.Contains(t, d.Err().Error(), "decode failed")
		})
	}
}

func TestOpenBytesMatchesOpen(t *testing.T) {
	t.Parallel()

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", fx.name)
			data, err := os.ReadFile(path) //nolint:gosec // fixed test fixture
			require.NoError(t, err)
			fromBytes, err := OpenBytes(data, fx.kind)
			require.NoError(t, err)
			defer func() { require.NoError(t, fromBytes.Close()) }()
			fromFile, err := Open(path, fx.kind)
			require.NoError(t, err)
			defer func() { require.NoError(t, fromFile.Close()) }()

			assert.Equal(t, readAll(t, fromFile), readAll(t, fromBytes))
		})
	}
}

func TestRejectsWhatItCannotDecode(t *testing.T) {
	t.Parallel()

	_, err := Open(filepath.Join("testdata", "missing.mp3"), MP3)
	require.Error(t, err)
	_, err = OpenBytes([]byte("not audio at all"), FLAC)
	require.Error(t, err)
	_, err = OpenBytes(nil, MP3)
	require.Error(t, err)
	_, err = Open(filepath.Join("testdata", "tone.mp3"), Vorbis)
	require.Error(t, err, "an MP3 is not Vorbis")

	kind, ok := KindForExt(".FLAC")
	assert.True(t, ok)
	assert.Equal(t, FLAC, kind)
	_, ok = KindForExt(".wav")
	assert.False(t, ok, "WAV stays with the Go decoder")
}

func TestAClosedDecoderStops(t *testing.T) {
	t.Parallel()

	d, err := Open(filepath.Join("testdata", "tone.ogg"), Vorbis)
	require.NoError(t, err)
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
	n, ok := d.Stream(make([][2]float64, 16))
	assert.Zero(t, n)
	assert.False(t, ok)
	require.ErrorIs(t, d.Err(), ErrClosed)
	require.ErrorIs(t, d.Seek(0), ErrClosed)
}
