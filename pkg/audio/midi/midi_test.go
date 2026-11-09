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

package midi

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rate = DefaultSampleRate

// expectedLen is the stream length for a song of seconds: the song plus
// the release tail. Sequencers round event times, so a few milliseconds
// of slack are allowed.
func assertLen(t *testing.T, d *Decoder, seconds float64) {
	t.Helper()
	want := int(seconds*float64(d.SampleRate())) + tailSeconds*d.SampleRate()
	assert.InDelta(t, want, d.Len(), float64(d.SampleRate())/100, "Len")
}

func TestSupported(t *testing.T) {
	t.Parallel()
	for _, ext := range []string{".mid", ".MIDI", ".kar", ".Rmi"} {
		assert.True(t, Supported(ext), ext)
	}
	for _, ext := range []string{"", ".mp3", ".xmi", "mid", ".sf2"} {
		assert.False(t, Supported(ext), ext)
	}
}

func TestFMRendersEachSongToItsLength(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		title   string
		data    []byte
		seconds float64
	}{
		"type 0": {"Type Zero Song", type0Song(), type0Seconds},
		"type 1": {"Type One Song", type1Song(), type1Seconds},
		"kar":    {"Karaoke Title", karSong(), 1},
		"rmi":    {"Type One Song", rmi(type1Song()), type1Seconds},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d, err := OpenBytes(tc.data, Options{})
			require.NoError(t, err)
			defer func() { _ = d.Close() }()
			assert.Equal(t, Info{Title: tc.title, Renderer: RendererFM}, d.Info())
			assert.Equal(t, rate, d.SampleRate())
			assertLen(t, d, tc.seconds)

			frames := readAll(t, d)
			require.Len(t, frames, d.Len())
			assert.Equal(t, d.Len(), d.Position())
			assert.Greater(t, rms(frames[:rate/2]), 0.01, "song start is not silent")
			for _, f := range frames {
				require.False(t, math.IsNaN(f[0]) || math.IsNaN(f[1]))
			}
			n, ok := d.Stream(make([][2]float64, 16))
			assert.Equal(t, 0, n)
			assert.False(t, ok)
		})
	}
}

func TestTempoChangeStretchesTheSong(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(type0Song(), Options{SampleRate: 22050})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	// Without the change to 60 BPM the third quarter would end at 1.5 s.
	assertLen(t, d, type0Seconds)
	assert.Equal(t, 22050, d.SampleRate())
}

func TestTailRingsOutAndThenFadesToSilence(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(type1Song(), Options{})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	frames := readAll(t, d)
	end := frames[len(frames)-rate/10:]
	assert.Less(t, rms(end), 0.01, "the end of the tail is close to silent")
}

func TestFMSeek(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(type0Song(), Options{})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()

	// The 60 BPM note starts at 1 s and plays to 2 s.
	at := rate - rate/10
	require.NoError(t, d.Seek(at))
	assert.Equal(t, at, d.Position())
	before := readFrames(t, d, rate/20)
	after := readFrames(t, d, rate/2)
	assert.Less(t, rms(before), rms(after), "the note after the seek point plays")
	assert.Greater(t, rms(after), 0.01)
	assert.Equal(t, at+rate/20+rate/2, d.Position())

	// Seeking back to the start plays the song from the top again.
	require.NoError(t, d.Seek(0))
	assert.Greater(t, rms(readFrames(t, d, rate/4)), 0.01)

	// Into the tail, with nothing sounding.
	quiet, err := OpenBytes(type0Song(), Options{})
	require.NoError(t, err)
	defer func() { _ = quiet.Close() }()
	require.NoError(t, quiet.Seek(quiet.Len()-rate/10))
	rest := readAll(t, quiet)
	assert.Len(t, rest, rate/10)
	assert.Less(t, rms(rest), 0.001)

	require.NoError(t, d.Seek(d.Len()))
	n, ok := d.Stream(make([][2]float64, 8))
	assert.Zero(t, n)
	assert.False(t, ok)

	require.Error(t, d.Seek(-1))
	require.Error(t, d.Seek(d.Len()+1))
}

func TestOpenReadsFilesAndUnwrapsRMI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"song.mid": type1Song(),
		"song.rmi": rmi(type1Song()),
	} {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, data, 0o600))
		d, err := Open(p, Options{})
		require.NoError(t, err, name)
		assert.Equal(t, "Type One Song", d.Info().Title, name)
		assertLen(t, d, type1Seconds)
		require.NoError(t, d.Close())
	}
}

func TestOpenRejectsBadInput(t *testing.T) {
	t.Parallel()
	song := type1Song()
	cases := map[string][]byte{
		"empty":            nil,
		"garbage":          []byte(strings.Repeat("not a midi file ", 64)),
		"header only":      song[:14],
		"rmi without data": rmi(nil)[:20],
		"riff not rmid":    append([]byte("RIFF\x04\x00\x00\x00WAVE"), make([]byte, 32)...),
		"rmi data past end": func() []byte {
			b := rmi(song)
			b[16] = 0xFF
			b[17] = 0xFF
			return b
		}(),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d, err := OpenBytes(data, Options{})
			if err == nil {
				_ = d.Close()
			}
			require.Error(t, err)
		})
	}
}

func TestTruncatedSongsDoNotCrash(t *testing.T) {
	t.Parallel()
	song := type1Song()
	for cut := 14; cut < len(song); cut += 7 {
		d, err := OpenBytes(song[:cut], Options{})
		if err != nil {
			continue
		}
		readFrames(t, d, rate/4)
		require.NoError(t, d.Close())
	}
}

func TestOpenRefusesOversizedFiles(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "big.mid")
	f, err := os.Create(p) //nolint:gosec // test temp file
	require.NoError(t, err)
	require.NoError(t, f.Truncate(MaxFileBytes+1))
	require.NoError(t, f.Close())
	_, err = Open(p, Options{})
	require.ErrorIs(t, err, ErrTooLarge)
	_, err = OpenBytes(make([]byte, MaxFileBytes+1), Options{})
	require.ErrorIs(t, err, ErrTooLarge)
}

func TestOpenRejectsBadSampleRate(t *testing.T) {
	t.Parallel()
	for _, r := range []int{-1, 100, maxSampleRate + 1} {
		_, err := OpenBytes(type0Song(), Options{SampleRate: r})
		require.Error(t, err, r)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(type0Song(), Options{})
	require.NoError(t, err)
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
	n, ok := d.Stream(make([][2]float64, 8))
	assert.Zero(t, n)
	assert.False(t, ok)
	require.ErrorIs(t, d.Seek(0), ErrClosed)
}

func TestSoundFontRendersTheSample(t *testing.T) {
	t.Parallel()
	sf := writeTemp(t, "sine.sf2", tinySF2())
	d, err := OpenBytes(type0Song(), Options{SoundFontPath: sf, MaxVoices: 8})
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	assert.Equal(t, Info{Title: "Type Zero Song", Renderer: RendererSoundFont}, d.Info())
	assertLen(t, d, type0Seconds)

	// The first note is A4, the sample's root key: a 440 Hz sine.
	first := readFrames(t, d, rate/4)
	assert.Greater(t, rms(first), 0.01)
	assert.InDelta(t, 440, zeroCrossingHz(first[rate/20:], rate), 20)

	rest := readAll(t, d)
	assert.Len(t, rest, d.Len()-rate/4)
	assert.Less(t, rms(rest[len(rest)-rate/10:]), 0.001, "the tail ends silent")
}

func TestSoundFontSeekMatchesPlayingThrough(t *testing.T) {
	t.Parallel()
	sf := writeTemp(t, "sine.sf2", tinySF2())
	open := func() *Decoder {
		d, err := OpenBytes(type1Song(), Options{SoundFontPath: sf})
		require.NoError(t, err)
		t.Cleanup(func() { _ = d.Close() })
		return d
	}
	// A note starts exactly at 1 s on both channels; seeking there and
	// playing must sound like playing through to it (the note that was
	// ringing is not carried over by a seek, so compare levels).
	at := rate
	through := open()
	readFrames(t, through, at)
	want := readFrames(t, through, rate/4)

	seeked := open()
	require.NoError(t, seeked.Seek(at))
	got := readFrames(t, seeked, rate/4)
	assert.InDelta(t, rms(want), rms(got), rms(want)*0.5)
	assert.Greater(t, rms(got), 0.01)
	assert.Equal(t, at+rate/4, seeked.Position())

	// Seeking into silence after the song.
	require.NoError(t, seeked.Seek(seeked.Len()-rate/10))
	assert.Less(t, rms(readAll(t, seeked)), 0.001)
}

func TestBadSoundFontFallsBackToFM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := tinySF2()
	cases := map[string]string{
		"missing":   filepath.Join(dir, "missing.sf2"),
		"garbage":   writeTemp(t, "garbage.sf2", []byte(strings.Repeat("junk", 100))),
		"truncated": writeTemp(t, "truncated.sf2", good[:len(good)/2]),
		"empty":     writeTemp(t, "empty.sf2", nil),
		"directory": dir,
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d, err := OpenBytes(type0Song(), Options{SoundFontPath: path})
			require.NoError(t, err)
			defer func() { _ = d.Close() }()
			assert.Equal(t, RendererFM, d.Info().Renderer)
			assert.Greater(t, rms(readFrames(t, d, rate/4)), 0.01)

			_, sfErr := loadSoundFont(path)
			require.ErrorIs(t, sfErr, ErrSoundFont)
		})
	}
}

func TestOversizedSoundFontIsRefused(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "huge.sf2")
	f, err := os.Create(p) //nolint:gosec // test temp file
	require.NoError(t, err)
	require.NoError(t, f.Truncate(MaxSoundFontBytes+1))
	require.NoError(t, f.Close())
	_, err = loadSoundFont(p)
	require.ErrorIs(t, err, ErrSoundFont)
	assert.Contains(t, err.Error(), "limit")
}

func TestReadInfo(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		title string
		data  []byte
	}{
		"type 0":   {"Type Zero Song", type0Song()},
		"kar":      {"Karaoke Title", karSong()},
		"rmi":      {"Type One Song", rmi(type1Song())},
		"untitled": {"", smf(0, (&track{}).end())},
	} {
		p := writeTemp(t, "song", tc.data)
		info, err := ReadInfo(p, 0)
		require.NoError(t, err, name)
		assert.Equal(t, Info{Title: tc.title}, info, name)
	}

	// A prefix that stops inside a track still finds the title.
	p := writeTemp(t, "prefix.rmi", rmi(type1Song()))
	info, err := ReadInfo(p, 60)
	require.NoError(t, err)
	assert.Equal(t, "Type One Song", info.Title)

	_, err = ReadInfo(writeTemp(t, "bad", []byte("nope nope nope nope")), 0)
	require.ErrorIs(t, err, errNotMIDI)
	_, err = ReadInfo(filepath.Join(t.TempDir(), "missing.mid"), 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestSanitize(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a [31m b c", sanitize([]byte("  a\x00\x1b[31m b\r\n\tc  ")[:15]))
	assert.Equal(t, "Café", sanitize([]byte("Caf\xe9")), "Latin-1 is decoded")
	assert.Equal(t, "\u65e5\u672c", sanitize([]byte("\u65e5\u672c")))
	assert.Equal(t, "ab", sanitize([]byte("a\u202eb")), "format characters are dropped")
	long := sanitize([]byte(strings.Repeat("é", 300)))
	assert.LessOrEqual(t, len(long), maxTextBytes)
	assert.True(t, strings.HasPrefix(long, "éé"))
	assert.Equal(t, strings.Repeat("é", maxTextBytes/2), long)
}

func TestTitleFallsBackToTrackZeroText(t *testing.T) {
	t.Parallel()
	t0 := &track{}
	t0.meta(0, 0x01, []byte("@ignored"))
	t0.meta(0, 0x01, []byte("A Text Title"))
	title, err := smfTitle(smf(0, t0.end()))
	require.NoError(t, err)
	assert.Equal(t, "A Text Title", title)
}

func TestTitleFallsBackToLaterTrackName(t *testing.T) {
	t.Parallel()
	t0 := &track{}
	t0.meta(0, 0x02, []byte("(C) Someone"))
	t1 := &track{}
	t1.meta(0, 0x03, []byte("Second Track Name"))
	t2 := &track{}
	t2.meta(0, 0x03, []byte("Third Track Name"))
	title, err := smfTitle(smf(1, t0.end(), t1.end(), t2.end()))
	require.NoError(t, err)
	assert.Equal(t, "Second Track Name", title)
}

func TestRunningStatusIsFollowed(t *testing.T) {
	t.Parallel()
	t0 := &track{}
	t0.ev(0, 0x90, 60, 100)
	t0.ev(10, 62, 100) // running status
	t0.ev(0, 0xF0, 0x02, 0x7E, 0xF7)
	t0.meta(0, 0x03, []byte("After Running Status"))
	title, err := smfTitle(smf(0, t0.end()))
	require.NoError(t, err)
	assert.Equal(t, "After Running Status", title)
}
