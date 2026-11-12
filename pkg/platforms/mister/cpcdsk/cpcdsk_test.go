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

package cpcdsk

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dirEnt struct {
	name   string
	ext    string
	user   byte
	count  byte
	extent byte
	sys    bool
}

type spec struct {
	entries  []dirEnt
	tracks   int
	sides    int
	firstID  byte
	extended bool
}

const infoSizeTable = 0x34

var physical = []byte{0, 5, 1, 6, 2, 7, 3, 8, 4}

func buildDSK(sp spec) []byte {
	const secs, secSize = 9, 512
	trackLen := trackHeaderSize + secs*secSize
	hdr := make([]byte, infoSize)
	if sp.extended {
		copy(hdr, "EXTENDED CPC DSK File\r\nDisk-Info\r\n")
	} else {
		copy(hdr, "MV - CPCEMU Disk-File\r\nDisk-Info\r\n")
		hdr[0x32] = byte(trackLen & 0xFF)
		hdr[0x33] = byte(trackLen >> 8)
	}
	hdr[0x30] = byte(sp.tracks & 0xFF)
	hdr[0x31] = byte(sp.sides & 0xFF)
	if sp.extended {
		sizes := make([]byte, sp.tracks*sp.sides)
		for i := range sizes {
			sizes[i] = byte(trackLen >> 8)
		}
		copy(hdr[infoSizeTable:], sizes)
	}

	dir := make([]byte, 0, dirBytes)
	for _, e := range sp.entries {
		b := make([]byte, dirEntrySize)
		b[0] = e.user
		for i := 1; i < 12; i++ {
			b[i] = ' '
		}
		copy(b[1:9], e.name)
		copy(b[9:12], e.ext)
		if e.sys {
			b[10] |= 0x80
		}
		b[12] = e.extent
		b[15] = e.count
		dir = append(dir, b...)
	}
	for len(dir) < dirBytes {
		e := make([]byte, dirEntrySize)
		for i := range e {
			e[i] = 0xE5
		}
		dir = append(dir, e...)
	}

	reserved := map[byte]int{0xC0: 0, 0x40: 2, 0x00: 1}[sp.firstID&0xC0]
	out := hdr
	for t := range sp.tracks {
		for s := range sp.sides {
			th := make([]byte, trackHeaderSize)
			copy(th, "Track-Info\r\n")
			th[0x10], th[0x11], th[0x14], th[0x15] = byte(t), byte(s), 2, secs
			data := make([]byte, 0, secs*secSize)
			for i, k := range physical {
				e := th[sectorListStart+i*sectorListEntry:]
				e[0], e[1], e[2], e[3] = byte(t), byte(s), sp.firstID+k, 2
				e[6], e[7] = byte(secSize&0xFF), byte(secSize>>8)
				sec := make([]byte, secSize)
				if t == reserved && s == 0 && int(k) < 4 {
					copy(sec, dir[int(k)*secSize:])
				}
				data = append(data, sec...)
			}
			out = append(out, th...)
			out = append(out, data...)
		}
	}
	return out
}

func write(t *testing.T, data []byte) (fs afero.Fs, path string) {
	t.Helper()
	fs = afero.NewMemMapFs()
	path = filepath.Join("cpc", "disk.dsk")
	require.NoError(t, afero.WriteFile(fs, path, data, 0o644))
	return fs, path
}

func names(l Listing) []string {
	out := make([]string, 0, len(l.Files))
	for _, f := range l.Files {
		out = append(out, f.Name)
	}
	return out
}

func TestPrograms(t *testing.T) {
	t.Parallel()
	entries := []dirEnt{
		{name: "GAME", ext: "BAS", count: 0x80},
		{name: "GAME", ext: "BAS", extent: 1, count: 0x10},
		{name: "LOADER", ext: "BIN", count: 4},
		{name: "OLD", ext: "BAS", user: deletedUser, count: 9},
		{name: "USER1", ext: "BAS", user: 1, count: 9},
		{name: "DISC", count: 2},
		{name: "SECRET", ext: "BIN", sys: true, count: 1},
	}
	wantNames := []string{"GAME.BAS", "LOADER.BIN", "DISC", "SECRET.BIN"}
	tests := []struct {
		name string
		sp   spec
		cpm  bool
	}{
		{"standard data", spec{entries: entries, firstID: 0xC1, tracks: 3, sides: 1}, false},
		{"extended data", spec{entries: entries, firstID: 0xC1, tracks: 3, sides: 1, extended: true}, false},
		{"standard double sided", spec{entries: entries, firstID: 0xC1, tracks: 3, sides: 2}, false},
		{"extended double sided", spec{entries: entries, firstID: 0xC1, tracks: 3, sides: 2, extended: true}, false},
		{"system format", spec{entries: entries, firstID: 0x41, tracks: 4, sides: 1}, true},
		{"system format extended", spec{entries: entries, firstID: 0x41, tracks: 4, sides: 2, extended: true}, true},
		{"ibm format", spec{entries: entries, firstID: 0x01, tracks: 3, sides: 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs, p := write(t, buildDSK(tt.sp))
			l, err := Programs(fs, p)
			require.NoError(t, err)
			assert.Equal(t, wantNames, names(l))
			assert.Equal(t, tt.cpm, l.CPM)
			assert.Equal(t, (0x80+0x10)*128, l.Files[0].Size)
			assert.Equal(t, 4*128, l.Files[1].Size)
			assert.True(t, l.Files[3].Hidden)
		})
	}
}

func TestDirectoryAfterReservedTracks(t *testing.T) {
	t.Parallel()
	entries := []dirEnt{{name: "ALICE", ext: "COM", count: 8}, {name: "ALICE", ext: "DAT", count: 2}}
	fs, p := write(t, buildDSK(spec{entries: entries, firstID: 0x41, tracks: 4, sides: 1, extended: true}))

	files, err := Directory(fs, p, 2)

	require.NoError(t, err)
	assert.Equal(t, []string{"ALICE.COM", "ALICE.DAT"}, names(Listing{Files: files}))
}

func TestProgramsMasksHighBits(t *testing.T) {
	t.Parallel()
	img := buildDSK(spec{entries: []dirEnt{{name: "AB", ext: "BAS", count: 1}}, firstID: 0xC1, tracks: 1, sides: 1})
	idx := -1
	for i := infoSize + trackHeaderSize; i+1 < len(img); i++ {
		if img[i] == 'A' && img[i+1] == 'B' {
			idx = i
			break
		}
	}
	require.Positive(t, idx)
	img[idx] |= 0x80
	fs, p := write(t, img)
	l, err := Programs(fs, p)
	require.NoError(t, err)
	assert.Equal(t, []string{"AB.BAS"}, names(l))
}

func TestProgramsErrors(t *testing.T) {
	t.Parallel()
	good := buildDSK(spec{firstID: 0xC1, tracks: 2, sides: 1})
	trunc := good[:infoSize+10]
	badTrack := append([]byte(nil), good...)
	badTrack[infoSize] = 'X'
	zeroTracks := append([]byte(nil), good...)
	zeroTracks[0x30] = 0
	threeSides := append([]byte(nil), good...)
	threeSides[0x31] = 3
	badFormat := buildDSK(spec{firstID: 0x81, tracks: 2, sides: 1})
	shortFile := good[:infoSize+trackHeaderSize+600]

	tests := []struct {
		err  error
		name string
		data []byte
	}{
		{name: "empty", data: nil, err: ErrNotDSK},
		{name: "garbage", data: make([]byte, 1000), err: ErrNotDSK},
		{name: "truncated", data: trunc, err: ErrNoDirectory},
		{name: "bad track signature", data: badTrack, err: ErrNotDSK},
		{name: "zero tracks", data: zeroTracks, err: ErrNotDSK},
		{name: "three sides", data: threeSides, err: ErrNotDSK},
		{name: "unknown format", data: badFormat, err: ErrNoDirectory},
		{name: "short directory data", data: shortFile, err: ErrNoDirectory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs, p := write(t, tt.data)
			_, err := Programs(fs, p)
			require.ErrorIs(t, err, tt.err)
		})
	}

	_, err := Programs(afero.NewMemMapFs(), filepath.Join("cpc", "none.dsk"))
	require.Error(t, err)
}

func TestRunnable(t *testing.T) {
	t.Parallel()
	f := func(name string, hidden bool) File { return File{Name: name, Hidden: hidden} }
	tests := []struct {
		name  string
		want  string
		files []File
		ok    bool
	}{
		{name: "disc wins", files: []File{f("A.BAS", false), f("DISC", false)}, want: "DISC", ok: true},
		{name: "disk wins", files: []File{f("A.BAS", false), f("DISK", false)}, want: "DISK", ok: true},
		{name: "first bas", files: []File{f("A.BIN", false), f("B.BAS", false)}, want: "B.BAS", ok: true},
		{name: "bin without bas", files: []File{f("A.BAK", false), f("C.BIN", false)}, want: "C.BIN", ok: true},
		{name: "no extension", files: []File{f("A.DAT", false), f("GAME", false)}, want: "GAME", ok: true},
		{name: "bak and dat only", files: []File{f("A.BAK", false), f("B.DAT", false)}, want: "", ok: false},
		{name: "hidden skipped", files: []File{f("DISC", true), f("X.BAS", false)}, want: "X.BAS", ok: true},
		{name: "hidden only", files: []File{f("SECRET.BAS", true)}, want: "SECRET.BAS", ok: true},
		{name: "lowercase ext", files: []File{f("a.bas", false)}, want: "a.bas", ok: true},
		{name: "none", files: nil, want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := Listing{Files: tt.files}.Runnable()
		assert.Equal(t, tt.ok, ok, tt.name)
		assert.Equal(t, tt.want, got, tt.name)
	}
}
