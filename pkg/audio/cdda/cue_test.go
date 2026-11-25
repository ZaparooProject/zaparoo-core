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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCueSingleFile(t *testing.T) {
	t.Parallel()
	sheet, err := ParseCue([]byte(`REM GENRE Rock
PERFORMER "The Band"
TITLE "The Album"
FILE "album.bin" BINARY
  TRACK 01 AUDIO
    TITLE "First"
    PERFORMER "Singer"
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    TITLE "Second"
    INDEX 00 03:58:10
    INDEX 01 04:00:00
`))
	require.NoError(t, err)
	assert.Equal(t, "The Album", sheet.Title)
	assert.Equal(t, "The Band", sheet.Performer)
	require.Len(t, sheet.Files, 1)
	f := sheet.Files[0]
	assert.Equal(t, "album.bin", f.Name)
	assert.True(t, f.Binary)
	require.Len(t, f.Tracks, 2)
	assert.Equal(t, CueTrack{Title: "First", Performer: "Singer", Number: 1, Index0: -1, Index1: 0, Audio: true},
		f.Tracks[0])
	assert.Equal(t, (3*60+58)*75+10, f.Tracks[1].Index0)
	assert.Equal(t, 4*60*75, f.Tracks[1].Index1)
}

func TestParseCueUnquotedNameAndCRLF(t *testing.T) {
	t.Parallel()
	sheet, err := ParseCue([]byte("\xef\xbb\xbfFILE My Album.bin BINARY\r\n  TRACK 1 AUDIO\r\n    INDEX 1 0:2:0\r\n"))
	require.NoError(t, err)
	assert.Equal(t, "My Album.bin", sheet.Files[0].Name)
	assert.Equal(t, 150, sheet.Files[0].Tracks[0].Index1)
}

func TestParseCueLatin1Text(t *testing.T) {
	t.Parallel()
	sheet, err := ParseCue([]byte("TITLE \"Caf\xe9\"\nFILE \"a.bin\" BINARY\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n"))
	require.NoError(t, err)
	assert.Equal(t, "Café", sheet.Title)
}

func TestParseCueRejects(t *testing.T) {
	t.Parallel()
	head := "FILE \"a.bin\" BINARY\n"
	track := "TRACK 01 AUDIO\nINDEX 01 00:00:00\n"
	tests := []struct {
		name string
		cue  string
	}{
		{"no tracks", head},
		{"track before file", "TRACK 01 AUDIO\nINDEX 01 00:00:00\n"},
		{"index before track", head + "INDEX 01 00:00:00\n"},
		{"seconds out of range", head + "TRACK 01 AUDIO\nINDEX 01 00:60:00\n"},
		{"frames out of range", head + "TRACK 01 AUDIO\nINDEX 01 00:00:75\n"},
		{"not a time", head + "TRACK 01 AUDIO\nINDEX 01 aa:00:00\n"},
		{"negative time", head + "TRACK 01 AUDIO\nINDEX 01 00:-1:00\n"},
		{"two fields", head + "TRACK 01 AUDIO\nINDEX 01 00:00\n"},
		{"missing index 01", head + "TRACK 01 AUDIO\nINDEX 00 00:00:00\nTRACK 02 AUDIO\nINDEX 01 00:10:00\n"},
		{"missing last index 01", head + "TRACK 01 AUDIO\n"},
		{"repeated index 01", head + "TRACK 01 AUDIO\nINDEX 01 00:00:00\nINDEX 01 00:01:00\n"},
		{"index 01 before 00", head + "TRACK 01 AUDIO\nINDEX 00 00:02:00\nINDEX 01 00:01:00\n"},
		{"backwards tracks", head + "TRACK 01 AUDIO\nINDEX 01 00:10:00\nTRACK 02 AUDIO\nINDEX 01 00:05:00\n"},
		{"track out of order", head + "TRACK 02 AUDIO\nINDEX 01 00:00:00\nTRACK 01 AUDIO\nINDEX 01 00:01:00\n"},
		{"track zero", head + "TRACK 00 AUDIO\nINDEX 01 00:00:00\n"},
		{"track 100", head + "TRACK 100 AUDIO\nINDEX 01 00:00:00\n"},
		{"cooked mode", head + "TRACK 01 MODE1/2048\nINDEX 01 00:00:00\n"},
		{"file without type", "FILE \"a.bin\"\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n"},
		{"long line", head + "TITLE \"" + strings.Repeat("x", maxLineBytes) + "\"\n" + track},
		{"too large", strings.Repeat("REM\n", MaxCueBytes/4+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseCue([]byte(tt.cue))
			require.ErrorIs(t, err, ErrMalformedCue)
		})
	}
}

func TestParseCueBounds(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := range MaxFiles + 1 {
		b.WriteString("FILE \"a.bin\" BINARY\n")
		if i < MaxTracks {
			b.WriteString("TRACK " + itoa2(i+1) + " AUDIO\nINDEX 01 00:00:00\n")
		}
	}
	_, err := ParseCue([]byte(b.String()))
	require.ErrorIs(t, err, ErrMalformedCue)
}

func itoa2(n int) string {
	return fmt.Sprintf("%02d", n)
}

func FuzzParseCue(f *testing.F) {
	f.Add([]byte("FILE \"a.bin\" BINARY\nTRACK 01 AUDIO\nINDEX 01 00:00:00\n"))
	f.Add([]byte("TITLE x\nFILE \"d.bin\" BINARY\nTRACK 01 MODE1/2352\nINDEX 01 00:00:00\n" +
		"TRACK 02 AUDIO\nINDEX 00 10:00:00\nINDEX 01 10:02:00\nFILE b.bin WAVE\nTRACK 03 AUDIO\nINDEX 01 00:00:00\n"))
	f.Add([]byte("FILE \"../x.bin\" BINARY\nTRACK 01 AUDIO\nINDEX 01 999:59:74\nINDEX 02 999:59:74\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		sheet, err := ParseCue(data)
		if err != nil {
			return
		}
		require.LessOrEqual(t, len(sheet.Files), MaxFiles)
		tracks, last := 0, 0
		for _, file := range sheet.Files {
			prev := 0
			for _, tr := range file.Tracks {
				tracks++
				require.Greater(t, tr.Number, last)
				require.LessOrEqual(t, tr.Number, MaxTracks)
				require.GreaterOrEqual(t, tr.Index1, prev)
				require.GreaterOrEqual(t, tr.Index1, tr.Index0)
				last, prev = tr.Number, tr.Index1
			}
		}
		require.Positive(t, tracks)
		require.LessOrEqual(t, tracks, MaxTracks)
	})
}
