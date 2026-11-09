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
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Stream fixtures. tone.aac is one second of a 440 Hz tone, AAC-LC in
// ADTS, made with
//
//	ffmpeg -f lavfi -i sine=frequency=440:sample_rate=44100:duration=1 \
//	  -ac 2 -c:a aac -b:a 96k -f adts tone.aac
//
// tone-opus.ogg is 0.2 s of the same as Opus in Ogg (libopus, 24 kbps).
// he-aac-v1.aac and he-aac-v2.aac are Chromium's
// media/test/data/bear-audio-implicit-he-aac-v1.aac and -v2.aac: ADTS
// HE-AAC with implicit SBR (and PS for v2), 48 kHz output, about 2.8 s.
// They are under Chromium's BSD license, copied in LICENSE-chromium. They
// are quiet: ffmpeg measures their mean volume at -41 dB, an RMS of about
// 0.0088, which the decoder matches.
var streamFixtures = []struct {
	name      string
	kind      Kind
	rate      int
	channels  int
	minFrames int
	minRMS    float64
}{
	{name: "tone.mp3", kind: MP3, rate: 44100, channels: 2, minFrames: 44100, minRMS: 0.05},
	{name: "tone.ogg", kind: Vorbis, rate: 48000, channels: 2, minFrames: 47000, minRMS: 0.05},
	{name: "tone.aac", kind: AAC, rate: 44100, channels: 2, minFrames: 44100, minRMS: 0.05},
	{name: "he-aac-v1.aac", kind: AAC, rate: 48000, channels: 2, minFrames: 130000, minRMS: 0.007},
	{name: "he-aac-v2.aac", kind: AAC, rate: 48000, channels: 2, minFrames: 130000, minRMS: 0.007},
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed test fixture
	require.NoError(t, err)
	return data
}

// maxTestFrames caps how many frames one test decode may collect, far
// above any fixture, so a decoder that never stops fails instead of
// growing without bound.
const maxTestFrames = 1 << 20

// drain reads until the decoder wants more input or stops, returning the
// frames and the error that ended the reading.
func drain(t testing.TB, d *StreamDecoder, out *[][2]float64) error {
	t.Helper()
	buf := make([][2]float64, 1500)
	for len(*out) <= maxTestFrames {
		n, err := d.Read(buf)
		*out = append(*out, buf[:n]...)
		if err != nil {
			return err
		}
		require.Positive(t, n, "a nil error comes with frames")
	}
	t.Fatalf("decoder produced more than %d frames", maxTestFrames)
	return nil
}

// decodeChunked feeds data in chunks sized by next and decodes it all.
func decodeChunked(t testing.TB, kind Kind, data []byte, next func() int) [][2]float64 {
	t.Helper()
	d, err := NewStreamDecoder(kind)
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	var out [][2]float64
	for len(data) > 0 {
		n := min(next(), len(data), d.Space())
		require.Positive(t, n, "the decoder takes more input after a drain")
		require.NoError(t, d.Feed(data[:n]))
		data = data[n:]
		err := drain(t, d, &out)
		require.ErrorIs(t, err, ErrNeedMore)
	}
	d.Flush()
	require.ErrorIs(t, drain(t, d, &out), io.EOF)
	return out
}

func TestStreamDecodesEachFormat(t *testing.T) {
	t.Parallel()
	for _, fx := range streamFixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			out := decodeChunked(t, fx.kind, readFixture(t, fx.name), func() int { return 1 << 20 })
			assert.GreaterOrEqual(t, len(out), fx.minFrames)
			assert.Less(t, len(out), fx.minFrames*3/2)
			assert.Greater(t, rms(out), fx.minRMS, "decoded audio is not silence")
		})
	}
}

func TestStreamFormatIsKnownFromTheHeaders(t *testing.T) {
	t.Parallel()
	for _, fx := range streamFixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			d, err := NewStreamDecoder(fx.kind)
			require.NoError(t, err)
			defer func() { _ = d.Close() }()
			_, _, ok := d.Format()
			assert.False(t, ok, "nothing is known before any input")

			data := readFixture(t, fx.name)
			require.NoError(t, d.Feed(data[:min(len(data), d.Space())]))
			buf := make([][2]float64, 64)
			n, err := d.Read(buf)
			require.NoError(t, err)
			require.Positive(t, n)
			rate, channels, ok := d.Format()
			require.True(t, ok)
			assert.Equal(t, fx.rate, rate, "HE-AAC reports the SBR output rate")
			assert.Equal(t, fx.channels, channels)
		})
	}
}

func TestStreamTinyChunksMatchOneFeed(t *testing.T) {
	t.Parallel()
	for _, fx := range streamFixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			data := readFixture(t, fx.name)
			whole := decodeChunked(t, fx.kind, data, func() int { return len(data) })
			rng := rand.New(rand.NewPCG(1, uint64(len(fx.name)))) //nolint:gosec // deterministic test chunking
			chunked := decodeChunked(t, fx.kind, data, func() int { return 1 + rng.IntN(700) })
			require.Len(t, chunked, len(whole))
			assert.Equal(t, whole, chunked)
		})
	}
}

func TestStreamResyncsPastGarbage(t *testing.T) {
	t.Parallel()
	for _, fx := range streamFixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			data := readFixture(t, fx.name)
			rng := rand.New(rand.NewPCG(2, 3)) //nolint:gosec // deterministic test noise
			noise := func(n int) []byte {
				b := make([]byte, n)
				for i := range b {
					b[i] = byte(rng.UintN(256)) //nolint:gosec // UintN(256) fits a byte
				}
				return b
			}
			clean := decodeChunked(t, fx.kind, data, func() int { return 4096 })
			var dirty []byte
			if fx.kind == Vorbis {
				// Ogg resyncs on the next page; garbage before the headers
				// is skipped while looking for them.
				dirty = append(noise(3000), data...)
			} else {
				dirty = append(dirty, noise(3000)...)
				dirty = append(dirty, data[:len(data)/2]...)
				dirty = append(dirty, noise(1500)...)
				dirty = append(dirty, data[len(data)/2:]...)
			}
			out := decodeChunked(t, fx.kind, dirty, func() int { return 4096 })
			assert.Greater(t, len(out), len(clean)*8/10)
			assert.Greater(t, rms(out), fx.minRMS)
		})
	}
}

func TestStreamResetResyncsMidStream(t *testing.T) {
	t.Parallel()
	for _, fx := range streamFixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			data := readFixture(t, fx.name)
			d, err := NewStreamDecoder(fx.kind)
			require.NoError(t, err)
			defer func() { _ = d.Close() }()
			// Most of the file, as MP3 holds back 16 KiB until it trusts
			// the frame sync.
			require.NoError(t, d.Feed(data[:len(data)-100]))
			var out [][2]float64
			require.ErrorIs(t, drain(t, d, &out), ErrNeedMore)
			require.NotEmpty(t, out)

			d.Reset()
			// Vorbis needs its headers again after a reset, as a new
			// connection to the stream would send them.
			rest := data[len(data)/2+3:]
			if fx.kind == Vorbis {
				rest = data
			}
			out = out[:0]
			for len(rest) > 0 {
				n := min(4096, len(rest), d.Space())
				require.NoError(t, d.Feed(rest[:n]))
				rest = rest[n:]
				require.ErrorIs(t, drain(t, d, &out), ErrNeedMore)
			}
			d.Flush()
			require.ErrorIs(t, drain(t, d, &out), io.EOF)
			assert.NotEmpty(t, out, "decoding resumes after a reset")
		})
	}
}

func TestStreamFollowsChainedOgg(t *testing.T) {
	t.Parallel()
	// Concatenated Ogg files are a chained stream, as Icecast sends a new
	// logical stream with fresh headers for each song.
	data := append(readFixture(t, "tone.ogg"), readFixture(t, "chirp.ogg")...)
	out := decodeChunked(t, Vorbis, data, func() int { return 3000 })
	assert.Greater(t, len(out), 94000, "both songs decode")
}

func TestStreamRejectsOpus(t *testing.T) {
	t.Parallel()
	d, err := NewStreamDecoder(Vorbis)
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	require.NoError(t, d.Feed(readFixture(t, "tone-opus.ogg")))
	_, err = d.Read(make([][2]float64, 100))
	require.ErrorIs(t, err, ErrUnsupportedCodec)
	_, err = d.Read(make([][2]float64, 100))
	require.ErrorIs(t, err, ErrUnsupportedCodec, "the failure sticks")
	d.Reset()
	_, err = d.Read(make([][2]float64, 100))
	require.ErrorIs(t, err, ErrNeedMore, "a reset clears it")
}

func TestStreamGivesUpOnEndlessGarbage(t *testing.T) {
	t.Parallel()
	for _, kind := range []Kind{MP3, Vorbis, AAC} {
		d, err := NewStreamDecoder(kind)
		require.NoError(t, err)
		rng := rand.New(rand.NewPCG(4, uint64(kind))) //nolint:gosec // deterministic test noise
		chunk := make([]byte, 4096)
		var last error
		for range 1000 {
			for i := range chunk {
				chunk[i] = byte(rng.UintN(256)) //nolint:gosec // UintN(256) fits a byte
			}
			if err := d.Feed(chunk[:min(len(chunk), d.Space())]); err != nil {
				last = err
				break
			}
			var out [][2]float64
			last = drain(t, d, &out)
			if !errors.Is(last, ErrNeedMore) {
				break
			}
		}
		require.ErrorIs(t, last, ErrUndecodable, "kind %d", kind)
		require.NoError(t, d.Close())
	}
}

func TestStreamFeedIsBounded(t *testing.T) {
	t.Parallel()
	d, err := NewStreamDecoder(MP3)
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	space := d.Space()
	require.Positive(t, space)
	require.ErrorIs(t, d.Feed(make([]byte, space+1)), ErrStreamFull)
	assert.Equal(t, space, d.Space(), "a rejected feed takes nothing")
	require.NoError(t, d.Feed(make([]byte, space)))
	assert.Zero(t, d.Space())
}

func TestStreamDecoderRejectsFLACAndUnknownKinds(t *testing.T) {
	t.Parallel()
	_, err := NewStreamDecoder(FLAC)
	require.Error(t, err)
	_, err = NewStreamDecoder(Kind(99))
	require.Error(t, err)
	_, ok := KindForExt(".aac")
	assert.False(t, ok, ".aac files are not mapped to the stream-only kind")
}

func TestClosedStreamDecoderFails(t *testing.T) {
	t.Parallel()
	d, err := NewStreamDecoder(AAC)
	require.NoError(t, err)
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
	require.ErrorIs(t, d.Feed([]byte{1}), ErrClosed)
	_, err = d.Read(make([][2]float64, 1))
	require.ErrorIs(t, err, ErrClosed)
	_, _, ok := d.Format()
	assert.False(t, ok)
	assert.Zero(t, d.Space())
	d.Reset()
	d.Flush()
}

func FuzzStreamDecoder(f *testing.F) {
	for i, fx := range streamFixtures {
		data := readFixture(f, fx.name)
		f.Add(uint8(i), uint16(700), data[:min(len(data), 6000)])
	}
	f.Add(uint8(1), uint16(31), readFixture(f, "tone-opus.ogg"))
	f.Add(uint8(1), uint16(100), bytes.Repeat([]byte("OggS\x00\x02"), 50))
	f.Add(uint8(2), uint16(9), bytes.Repeat([]byte{0xFF, 0xF1, 0x50, 0x80, 0x02, 0x1F, 0xFC}, 40))
	f.Add(uint8(0), uint16(5), bytes.Repeat([]byte{0xFF, 0xFB, 0x90, 0x64}, 100))
	kinds := []Kind{MP3, Vorbis, AAC}
	const maxInput = 64 << 10
	const maxFrames = 1 << 20
	f.Fuzz(func(t *testing.T, which uint8, chunk uint16, data []byte) {
		if len(data) > maxInput {
			data = data[:maxInput]
		}
		d, err := NewStreamDecoder(kinds[int(which)%len(kinds)])
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Close() }()
		step := int(chunk)%4096 + 1
		buf := make([][2]float64, 777)
		total := 0
		feed := func() bool {
			for total <= maxFrames {
				n, err := d.Read(buf)
				total += n
				for _, s := range buf[:n] {
					if math.IsNaN(s[0]) || math.IsNaN(s[1]) {
						t.Fatal("NaN sample")
					}
				}
				if err != nil {
					return errors.Is(err, ErrNeedMore) || errors.Is(err, io.EOF)
				}
			}
			t.Fatal("unbounded output")
			return false
		}
		for len(data) > 0 {
			n := min(step, len(data), d.Space())
			if n == 0 {
				return
			}
			if err := d.Feed(data[:n]); err != nil {
				t.Fatal(err)
			}
			data = data[n:]
			if !feed() {
				return
			}
		}
		d.Flush()
		feed()
	})
}

// oggCRC is the Ogg page checksum: CRC-32, polynomial 0x04c11db7, no
// reflection, zero initial value.
func oggCRC(b []byte) uint32 {
	var crc uint32
	for _, c := range b {
		crc ^= uint32(c) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// oggPage builds one Ogg page holding whole packets.
func oggPage(serial, seq uint32, packets ...[]byte) []byte {
	var lacing, body []byte
	for _, packet := range packets {
		n := len(packet)
		for n >= 255 {
			lacing = append(lacing, 255)
			n -= 255
		}
		lacing = append(lacing, byte(n)) //nolint:gosec // n is below 255 here
		body = append(body, packet...)
	}
	h := []byte("OggS")
	h = append(h, 0, 0)
	h = append(h, make([]byte, 8)...) // granule position
	h = binary.LittleEndian.AppendUint32(h, serial)
	h = binary.LittleEndian.AppendUint32(h, seq)
	h = append(h, 0, 0, 0, 0, byte(len(lacing))) //nolint:gosec // test pages hold well under 255 segments
	page := append(append(h, lacing...), body...)
	binary.LittleEndian.PutUint32(page[22:], oggCRC(page))
	return page
}

// oggPageAt splits off the page at the start of b: its lacing values,
// its body and the bytes after it.
func oggPageAt(t *testing.T, b []byte) (lacing, body, rest []byte) {
	t.Helper()
	require.GreaterOrEqual(t, len(b), 27)
	require.Equal(t, "OggS", string(b[:4]))
	segs := int(b[26])
	lacing = b[27 : 27+segs]
	end := 27 + segs
	for _, l := range lacing {
		end += int(l)
	}
	return lacing, b[27+segs : end], b[end:]
}

func TestStreamRejectsHostileVorbisComments(t *testing.T) {
	t.Parallel()
	// tone.ogg has the identification header alone on its first page,
	// then the comment and setup headers together on the second.
	ogg := readFixture(t, "tone.ogg")
	_, _, afterID := oggPageAt(t, ogg)
	idPage := ogg[:len(ogg)-len(afterID)]
	lacing, body, audio := oggPageAt(t, afterID)
	commentLen := 0
	for _, l := range lacing {
		commentLen += int(l)
		if l < 255 {
			break
		}
	}
	setup := body[commentLen:]
	require.Equal(t, "\x05vorbis", string(setup[:7]))
	serial := binary.LittleEndian.Uint32(ogg[14:])

	for _, tt := range []struct {
		name   string
		header []byte
		valid  bool
	}{
		{name: "control", header: []byte{0, 0, 0, 0, 0, 0, 0, 0}, valid: true},
		{name: "vendor length 4 GiB", header: []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{name: "vendor length 2 GiB", header: []byte{0x00, 0x00, 0x00, 0x80}},
		{name: "comment count 4 G", header: []byte{0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}},
		{name: "comment count above cap", header: []byte{0, 0, 0, 0, 0x01, 0x00, 0x01, 0x00}},
		{name: "comment length 4 GiB", header: []byte{0, 0, 0, 0, 1, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			comment := append([]byte("\x03vorbis"), tt.header...)
			comment = append(comment, 1) // framing bit
			stream := append(append([]byte{}, idPage...), oggPage(serial, 1, comment, setup)...)
			stream = append(stream, audio...)
			d, err := NewStreamDecoder(Vorbis)
			require.NoError(t, err)
			defer func() { _ = d.Close() }()
			require.NoError(t, d.Feed(stream))
			d.Flush()
			var out [][2]float64
			require.ErrorIs(t, drain(t, d, &out), io.EOF)
			if tt.valid {
				assert.Greater(t, len(out), 40000, "a sane comment header decodes")
				return
			}
			assert.Empty(t, out, "the hostile header is refused")
		})
	}
}
