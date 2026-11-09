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

package gme

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	name, ext string
	data      []byte
}

func fixtures() []fixture {
	return []fixture{
		{name: "nsf", ext: ".nsf", data: nsfFile("NSF Song", "NSF Author", "1990 NSF", 0xFD, 0x7E)},
		{name: "nsfe", ext: ".nsfe", data: nsfeFile([]byte{0xFD, 0x7E}, []string{"One", "Two"}, []int32{2000, 3000})},
		{name: "gbs", ext: ".gbs", data: gbsFile(0x00, 0x80)},
		{name: "hes", ext: ".hes", data: hesFile()},
		{name: "kss", ext: ".kss", data: kssFile()},
		{name: "spc", ext: ".spc", data: spcFile()},
	}
}

func TestSupported(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{".nsf", ".NSFE", ".gbs", ".hes", ".kss", ".Spc"} {
		assert.True(t, Supported(ext), ext)
	}
	for _, ext := range []string{".vgm", ".gym", ".ay", ".sap", ".mid", "", "nsf"} {
		assert.False(t, Supported(ext), ext)
	}
}

func TestEachFormatSounds(t *testing.T) {
	t.Parallel()
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			d, err := Open(writeTemp(t, "song"+f.ext, f.data), 0, Options{})
			require.NoError(t, err)
			defer func() { _ = d.Close() }()
			assert.Equal(t, DefaultSampleRate, d.SampleRate())
			frames := readFrames(t, d, rate)
			require.Len(t, frames, rate)
			assert.Greater(t, rms(frames[rate/4:]), 0.01, "track 0 is silent")
			assert.Equal(t, rate, d.Position())
		})
	}
}

func TestOpenBytesMatchesOpen(t *testing.T) {
	t.Parallel()
	f := fixtures()[0]
	a, err := Open(writeTemp(t, "a.nsf", f.data), 1, Options{})
	require.NoError(t, err)
	defer func() { _ = a.Close() }()
	b, err := OpenBytes(f.data, ".nsf", 1, Options{})
	require.NoError(t, err)
	defer func() { _ = b.Close() }()
	assert.Equal(t, readFrames(t, a, rate/2), readFrames(t, b, rate/2))
}

func TestOpenBytesExtensionMismatch(t *testing.T) {
	t.Parallel()
	_, err := OpenBytes(fixtures()[0].data, ".vgm", 0, Options{})
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestNSFInfo(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(nsfFile("NSF Song", "NSF Author", "1990 NSF", 0xFD, 0x7E, 0x3F), "", 2, Options{})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	info := d.Info()
	assert.Equal(t, "NSF Song", info.Game)
	assert.Equal(t, "NSF Author", info.Author)
	assert.Equal(t, "1990 NSF", info.Copyright)
	assert.Equal(t, "Nintendo NES", info.System)
	require.Len(t, info.Tracks, 3)
	assert.Zero(t, d.CurrentTrack(), "index into TrackStarts, not the file")
	// No length in an NSF: the default play time plus the default fade.
	want := (DefaultTrackSeconds + DefaultFadeSeconds) * rate
	assert.Equal(t, want, d.Len())
	assert.Equal(t, want, info.Tracks[0].LengthFrames)
	assert.Equal(t, []int{0}, d.TrackStarts())
}

func TestNSFEInfoAndLengths(t *testing.T) {
	t.Parallel()
	data := nsfeFile([]byte{0xFD, 0x7E, 0x3F}, []string{"One", "Two", "Three"}, []int32{2000, 3000, 1000})
	d, err := OpenBytes(data, "", -1, Options{FadeSeconds: 1})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	info := d.Info()
	assert.Empty(t, info.Title, "all-tracks mode has no single title")
	assert.Equal(t, "NSFE Game", info.Game)
	assert.Equal(t, "NSFE Author", info.Author)
	assert.Equal(t, "2001 NSFE Co", info.Copyright)
	require.Len(t, info.Tracks, 3)
	titles := []string{info.Tracks[0].Title, info.Tracks[1].Title, info.Tracks[2].Title}
	assert.Equal(t, []string{"One", "Two", "Three"}, titles)

	lens := []int{3 * rate, 4 * rate, 2 * rate} // time tag + 1 s fade
	for i, l := range lens {
		assert.Equal(t, l, info.Tracks[i].LengthFrames, "track %d", i)
	}
	assert.Equal(t, 9*rate, d.Len())
	assert.Equal(t, []int{0, 3 * rate, 7 * rate}, d.TrackStarts())

	all := readAll(t, d)
	assert.Len(t, all, d.Len())
	assert.Equal(t, 2, d.CurrentTrack())
	// Each track fades out to silence before the next starts.
	assert.Less(t, rms(all[3*rate-rate/20:3*rate]), 0.002)
	assert.Greater(t, rms(all[3*rate+rate/4:3*rate+rate/2]), 0.01)
	n, ok := d.Stream(make([][2]float64, 16))
	assert.Zero(t, n)
	assert.False(t, ok)
}

func TestSingleTrackTitle(t *testing.T) {
	t.Parallel()
	data := nsfeFile([]byte{0xFD, 0x7E}, []string{"One", "Two"}, []int32{2000, 3000})
	d, err := OpenBytes(data, "", 1, Options{})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	assert.Equal(t, "Two", d.Info().Title)
	assert.Zero(t, d.CurrentTrack())
	assert.Equal(t, (3+DefaultFadeSeconds)*rate, d.Len())
}

func TestSeekToTrackMatchesOpen(t *testing.T) {
	t.Parallel()
	data := nsfeFile([]byte{0xFD, 0x7E}, []string{"One", "Two"}, []int32{2000, 3000})
	all, err := OpenBytes(data, "", -1, Options{})
	require.NoError(t, err)
	defer func() { _ = all.Close() }()
	one, err := OpenBytes(data, "", 1, Options{})
	require.NoError(t, err)
	defer func() { _ = one.Close() }()

	readFrames(t, all, rate/3) // play some of track 0 first
	start := all.TrackStarts()[1]
	require.NoError(t, all.Seek(start))
	assert.Equal(t, start, all.Position())
	assert.Equal(t, 1, all.CurrentTrack())
	assert.Equal(t, readFrames(t, one, rate/2), readFrames(t, all, rate/2))
}

func TestSeekWithinTrack(t *testing.T) {
	t.Parallel()
	f := fixtures()[0]
	a, err := OpenBytes(f.data, "", 0, Options{})
	require.NoError(t, err)
	defer func() { _ = a.Close() }()
	b, err := OpenBytes(f.data, "", 0, Options{})
	require.NoError(t, err)
	defer func() { _ = b.Close() }()

	readFrames(t, a, rate)
	ref := readFrames(t, a, rate/4)
	require.NoError(t, b.Seek(rate))
	got := readFrames(t, b, rate/4)
	require.Len(t, got, len(ref))
	// The seek is emulated to the sample, so both runs line up.
	assert.InDelta(t, rms(ref), rms(got), 0.01)

	require.NoError(t, b.Seek(0))
	assert.Zero(t, b.Position())
	require.NoError(t, b.Seek(b.Len()))
	n, ok := b.Stream(make([][2]float64, 8))
	assert.Zero(t, n)
	assert.False(t, ok)

	require.Error(t, b.Seek(-1))
	require.Error(t, b.Seek(b.Len()+1))
}

func TestSPCInfo(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(spcFile(), "", 0, Options{})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	info := d.Info()
	assert.Equal(t, "SPC Song", info.Title)
	assert.Equal(t, "SPC Game", info.Game)
	assert.Equal(t, "SPC Artist", info.Author)
	assert.Equal(t, "Super Nintendo", info.System)
	// id666 gives 5 s of play and a 1000 ms fade.
	assert.Equal(t, spcSeconds*rate+rate, d.Len())
	all := readAll(t, d)
	assert.Len(t, all, d.Len())
	assert.Less(t, rms(all[len(all)-rate/50:]), rms(all[rate:2*rate])/20, "track faded out")
}

func TestInfoSanitized(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(nsfFile("Bad\x1b[31mName\x07", "  Auth\tor ", "", 0xFD), "", 0, Options{})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	assert.Equal(t, "Bad [31mName", d.Info().Game)
	assert.Equal(t, "Auth or", d.Info().Author)
}

func TestSanitize(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a b", sanitize([]byte(" a\x00\n b ")))
	long := make([]byte, 1000)
	for i := range long {
		long[i] = 'x'
	}
	assert.Len(t, sanitize(long), maxTextBytes)
	assert.Equal(t, "ok\u00ff", sanitize([]byte("ok\xff")), "invalid UTF-8 is Latin-1")
	assert.Equal(t, "ab", sanitize([]byte("a\u200bb")), "format characters dropped")
}

func TestBadTrack(t *testing.T) {
	t.Parallel()
	data := fixtures()[0].data
	for _, track := range []int{-2, 2, 1000} {
		_, err := OpenBytes(data, "", track, Options{})
		require.ErrorIs(t, err, ErrNoTrack, "track %d", track)
	}
}

func TestBadOptions(t *testing.T) {
	t.Parallel()
	data := fixtures()[0].data
	for _, o := range []Options{{SampleRate: 1000}, {SampleRate: 1 << 20}, {SampleRate: -1}} {
		_, err := OpenBytes(data, "", 0, o)
		require.Error(t, err, "%+v", o)
	}
	// Out-of-range times fall back to the defaults.
	d, err := OpenBytes(data, "", 0, Options{DefaultTrackSeconds: -1, FadeSeconds: -1})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	assert.Equal(t, (DefaultTrackSeconds+DefaultFadeSeconds)*rate, d.Len())
}

func TestLongTrackCapped(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(fixtures()[0].data, "", 0, Options{DefaultTrackSeconds: 1e9, FadeSeconds: 1e9})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	assert.LessOrEqual(t, d.Len(), maxTrackSeconds*rate)
}

func TestSampleRate(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(fixtures()[0].data, "", 0, Options{SampleRate: 22050, DefaultTrackSeconds: 2, FadeSeconds: 1})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	assert.Equal(t, 22050, d.SampleRate())
	assert.Equal(t, 3*22050, d.Len())
	assert.Len(t, readAll(t, d), 3*22050)
}

func TestGarbage(t *testing.T) {
	t.Parallel()
	inputs := map[string][]byte{
		"empty":   nil,
		"text":    []byte("hello, this is not music at all"),
		"nsf hdr": []byte("NESM\x1a"),
		"spc hdr": spcFile()[:0x100],
		"vgm":     append([]byte("Vgm "), make([]byte, 256)...),
	}
	for _, f := range fixtures() {
		for _, n := range []int{1, 4, 16, len(f.data) / 2} {
			inputs[f.name+"/"+string(rune('0'+n%10))] = f.data[:min(n, len(f.data))]
		}
	}
	for name, data := range inputs {
		d, err := OpenBytes(data, "", 0, Options{})
		if err != nil {
			continue
		}
		// A truncated file may still load; it must play without fault.
		readFrames(t, d, rate/4)
		require.NoError(t, d.Close(), name)
	}
	_, err := OpenBytes([]byte("hello, this is not music at all"), "", 0, Options{})
	require.ErrorIs(t, err, ErrUnsupported)
}

// TestGBSStackOnAPU covers a stack pointer that makes the play routine's
// return address land on APU registers. The vendored emulator used to stamp
// those writes with a time far before the frame and draw outside the sound
// buffer.
func TestGBSStackOnAPU(t *testing.T) {
	t.Parallel()
	data := gbsFile(0x80)
	binary.LittleEndian.PutUint16(data[0x0C:], 0xFF15)
	d, err := OpenBytes(data, ".gbs", 0, Options{DefaultTrackSeconds: 2})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	frames := readAll(t, d)
	assert.Len(t, frames, d.Len())
}

// TestNSFEOversizedChunk covers a chunk claiming more bytes than the file
// holds, which the vendored loader used to allocate before reading.
func TestNSFEOversizedChunk(t *testing.T) {
	t.Parallel()
	good := nsfeFile([]byte{0xFD}, []string{"One"}, []int32{2000})
	data := append([]byte(nil), good[:4+8+10]...) // NSFE and INFO
	data = binary.LittleEndian.AppendUint32(data, 64<<20)
	data = append(data, "time"...)
	data = append(data, good[4+8+10:]...)
	_, err := OpenBytes(data, ".nsfe", 0, Options{})
	require.Error(t, err)
}

func TestTooLarge(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "big.nsf")
	f, err := os.Create(p) //nolint:gosec // test temp dir
	require.NoError(t, err)
	require.NoError(t, f.Truncate(MaxFileBytes+1))
	require.NoError(t, f.Close())
	_, err = Open(p, 0, Options{})
	require.ErrorIs(t, err, ErrTooLarge)
	_, err = TrackCount(p)
	require.ErrorIs(t, err, ErrTooLarge)
	_, err = ReadInfo(p, 0)
	require.ErrorIs(t, err, ErrTooLarge)
	_, err = ReadInfo(writeTemp(t, "s.nsf", fixtures()[0].data), 64)
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestMissingFile(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "nope.nsf")
	_, err := Open(p, 0, Options{})
	require.Error(t, err)
	_, err = TrackCount(p)
	require.Error(t, err)
	_, err = ReadInfo(p, 0)
	require.Error(t, err)
}

func TestTrackCountAndReadInfo(t *testing.T) {
	t.Parallel()
	nsf := writeTemp(t, "a.nsf", nsfFile("G", "A", "C", 1, 2, 3, 4))
	n, err := TrackCount(nsf)
	require.NoError(t, err)
	assert.Equal(t, 4, n)

	nsfe := writeTemp(t, "b.nsfe", nsfeFile([]byte{0xFD, 0x7E}, []string{"One", "Two"}, []int32{2000, 3000}))
	info, err := ReadInfo(nsfe, 0)
	require.NoError(t, err)
	assert.Empty(t, info.Title)
	assert.Equal(t, "NSFE Game", info.Game)
	require.Len(t, info.Tracks, 2)
	assert.Equal(t, "Two", info.Tracks[1].Title)
	assert.Equal(t, (3+DefaultFadeSeconds)*rate, info.Tracks[1].LengthFrames)

	for _, f := range fixtures() {
		n, err := TrackCount(writeTemp(t, "c"+f.ext, f.data))
		require.NoError(t, err, f.name)
		assert.Positive(t, n, f.name)
	}
}

func TestClose(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(fixtures()[0].data, "", 0, Options{})
	require.NoError(t, err)
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
	n, ok := d.Stream(make([][2]float64, 8))
	assert.Zero(t, n)
	assert.False(t, ok)
	require.ErrorIs(t, d.Seek(0), ErrClosed)
}
