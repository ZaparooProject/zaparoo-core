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
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pcm builds frames whose left sample counts up from first and whose right
// sample is its negation, so a test can tell where any frame came from.
func pcm(first, frames int) []byte {
	b := make([]byte, frames*bytesPerFrame)
	for i := range frames {
		v := (first + i) % 30000
		putSample(b[i*4:], v)
		putSample(b[i*4+2:], -v)
	}
	return b
}

// putSample writes v, which fits 16 bits, as a little-endian sample.
func putSample(b []byte, v int) {
	u := v & 0xffff
	b[0] = byte(u & 0xff) //nolint:gosec // masked to 8 bits
	b[1] = byte(u >> 8)   //nolint:gosec // masked to 8 bits
}

// leftAt is the left sample pcm wrote for a frame.
func leftAt(frame int) float64 {
	return float64(int16(frame%30000)) / 32768
}

const cueDir = "/music/Album"

func writeRip(t *testing.T, fsys afero.Fs, cue string, bins map[string]int) string {
	t.Helper()
	require.NoError(t, fsys.MkdirAll(cueDir, 0o755))
	cuePath := filepath.Join(cueDir, "album.cue")
	require.NoError(t, afero.WriteFile(fsys, cuePath, []byte(cue), 0o644))
	for name, sectors := range bins {
		require.NoError(t, afero.WriteFile(fsys, filepath.Join(cueDir, name), pcm(0, sectors*FramesPerSector), 0o644))
	}
	return cuePath
}

func streamAll(t *testing.T, d *Decoder) [][2]float64 {
	t.Helper()
	var out [][2]float64
	buf := make([][2]float64, 1000)
	for {
		n, ok := d.Stream(buf)
		out = append(out, buf[:n]...)
		if !ok {
			break
		}
	}
	require.NoError(t, d.Err())
	return out
}

func TestOpenCueSingleFileAlbum(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, `TITLE "Album"
PERFORMER "Band"
FILE "album.bin" BINARY
  TRACK 01 AUDIO
    TITLE "One"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Two"
    PERFORMER "Guest"
    INDEX 00 00:00:08
    INDEX 01 00:00:10
`, map[string]int{"album.bin": 25})

	d, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()

	assert.Equal(t, 25*FramesPerSector, d.Len())
	assert.Equal(t, []int{0, 10 * FramesPerSector}, d.TrackStarts())
	m := d.TrackMeta(1)
	require.NotNil(t, m)
	assert.Equal(t, "Two", m.Title)
	assert.Equal(t, "Guest", m.Artist)
	assert.Equal(t, "Album", m.Album)
	assert.Equal(t, "Band", m.AlbumArtist)
	assert.Equal(t, 2, m.TrackNumber)
	assert.Equal(t, 2, m.TrackTotal)

	out := streamAll(t, d)
	require.Len(t, out, 25*FramesPerSector)
	for _, f := range []int{0, 1, 5879, 5880, 14699} {
		assert.InDelta(t, leftAt(f), out[f][0], 1e-9, "frame %d", f)
		assert.InDelta(t, -leftAt(f), out[f][1], 1e-9, "frame %d", f)
	}

	require.NoError(t, d.Seek(12*FramesPerSector+3))
	buf := make([][2]float64, 4)
	n, ok := d.Stream(buf)
	require.True(t, ok)
	require.Equal(t, 4, n)
	assert.InDelta(t, leftAt(12*FramesPerSector+3), buf[0][0], 1e-9)
}

func TestOpenCueMultiFile(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, `FILE "01.bin" BINARY
  TRACK 01 AUDIO
    INDEX 01 00:00:00
FILE "02.bin" BINARY
  TRACK 02 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:00:02
`, map[string]int{"01.bin": 3, "02.bin": 5})

	d, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()

	assert.Equal(t, []int{0, 3 * FramesPerSector}, d.TrackStarts())
	out := streamAll(t, d)
	require.Len(t, out, 6*FramesPerSector)
	// Track 2 starts at its INDEX 01, two sectors into its own file.
	assert.InDelta(t, leftAt(2*FramesPerSector), out[3*FramesPerSector][0], 1e-9)
}

func TestOpenCueDataTrackFirst(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, `FILE "game.bin" BINARY
  TRACK 01 MODE1/2352
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    INDEX 00 00:00:10
    INDEX 01 00:00:12
  TRACK 03 AUDIO
    INDEX 01 00:00:20
`, map[string]int{"game.bin": 30})

	d, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()

	album := d.Album()
	require.Len(t, album.Tracks, 2)
	assert.Equal(t, 2, album.Tracks[0].Number)
	assert.Equal(t, 3, album.Total)
	assert.Equal(t, (20-12)*FramesPerSector+(30-20)*FramesPerSector, d.Len())
	assert.Equal(t, 2, d.TrackMeta(0).TrackNumber)
	assert.Equal(t, 3, d.TrackMeta(0).TrackTotal)
	out := streamAll(t, d)
	assert.InDelta(t, leftAt(12*FramesPerSector), out[0][0], 1e-9)
}

func TestOpenCueAudioBeforeDataEndsAtPregap(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, `FILE "cd.bin" BINARY
  TRACK 01 AUDIO
    INDEX 01 00:00:00
  TRACK 02 MODE2/2352
    INDEX 00 00:00:10
    INDEX 01 00:00:12
`, map[string]int{"cd.bin": 20})

	d, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	assert.Equal(t, 10*FramesPerSector, d.Len())
}

func TestOpenCueSingleTrack(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, `FILE "a.bin" BINARY
  TRACK 01 AUDIO
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Two"
    INDEX 01 00:00:04
`, map[string]int{"a.bin": 9})

	d, err := OpenCue(cuePath, CueOptions{Fs: fsys, Track: 2})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	assert.Equal(t, 5*FramesPerSector, d.Len())
	assert.Equal(t, "Two", d.TrackMeta(0).Title)

	_, err = OpenCue(cuePath, CueOptions{Fs: fsys, Track: 3})
	require.Error(t, err)
}

func TestOpenCueMalformedIndex(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cue := "FILE \"a.bin\" BINARY\nTRACK 01 AUDIO\nINDEX 01 00:61:00\n"
	cuePath := writeRip(t, fsys, cue, map[string]int{"a.bin": 1})
	_, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.ErrorIs(t, err, ErrMalformedCue)
}

func TestOpenCueRejectsEscapingFile(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"../secret.bin", "sub/../../x.bin", "/etc/passwd", `..\x.bin`, `C:\x.bin`, "./a.bin", "a//b.bin",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fsys := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fsys, "/music/secret.bin", pcm(0, 588), 0o644))
			cuePath := writeRip(t, fsys, "FILE \""+name+"\" BINARY\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n", nil)
			_, err := OpenCue(cuePath, CueOptions{Fs: fsys})
			require.ErrorIs(t, err, ErrBadFileName)
		})
	}
}

func TestOpenCueSubfolderAndCaseFold(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, `FILE "Track.BIN" BINARY
  TRACK 01 AUDIO
    INDEX 01 00:00:00
FILE "sub/b.bin" BINARY
  TRACK 02 AUDIO
    INDEX 01 00:00:00
`, map[string]int{"track.bin": 2})
	require.NoError(t, afero.WriteFile(fsys, filepath.Join(cueDir, "sub", "b.bin"), pcm(0, 588), 0o644))
	d, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.NoError(t, err)
	require.NoError(t, d.Close())
	assert.Equal(t, 3*FramesPerSector, d.Len())
}

func TestOpenCueMissingBin(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, "FILE \"gone.bin\" BINARY\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n", nil)
	_, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gone.bin")
}

func TestOpenCueRejectsWaveFile(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cuePath := writeRip(t, fsys, "FILE \"a.wav\" WAVE\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n", map[string]int{"a.wav": 1})
	_, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.Error(t, err)
}

func TestOpenCueOnlyDataTracks(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cue := "FILE \"a.bin\" BINARY\nTRACK 01 MODE1/2352\nINDEX 01 00:00:00\n"
	cuePath := writeRip(t, fsys, cue, map[string]int{"a.bin": 1})
	_, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.ErrorIs(t, err, ErrNoAudioTracks)
}

func TestDecoderTruncatedFileEndsCleanly(t *testing.T) {
	t.Parallel()
	fsys := afero.NewMemMapFs()
	cue := "FILE \"a.bin\" BINARY\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n"
	cuePath := writeRip(t, fsys, cue, map[string]int{"a.bin": 4})
	d, err := OpenCue(cuePath, CueOptions{Fs: fsys})
	require.NoError(t, err)
	defer func() { require.NoError(t, d.Close()) }()
	require.NoError(t, fsys.Remove(filepath.Join(cueDir, "a.bin")))
	require.NoError(t, afero.WriteFile(fsys, filepath.Join(cueDir, "a.bin"), pcm(0, 588), 0o644))
	out := streamAll(t, d)
	assert.Less(t, len(out), 4*FramesPerSector)
}
