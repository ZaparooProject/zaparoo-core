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

package decb

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type entry struct {
	base, ext string
	typ       byte
}

func buildDisk(tracks, sides int, entries []entry, terminate bool) []byte {
	img := make([]byte, tracks*sides*trackSize)
	for i := range img {
		img[i] = 0xFF
	}
	off := dirTrack*sides*trackSize + (dirFirstSector-1)*sectorSize
	for i, e := range entries {
		b := img[off+i*entrySize : off+(i+1)*entrySize]
		for j := range b {
			b[j] = 0
		}
		copy(b[0:8], "        ")
		copy(b[0:8], e.base)
		copy(b[8:11], "   ")
		copy(b[8:11], e.ext)
		b[11] = e.typ
	}
	if terminate {
		o := off + len(entries)*entrySize
		img[o] = 0xFF
	}
	return img
}

func write(t *testing.T, data []byte) (fs afero.Fs, path string) {
	t.Helper()
	fs = afero.NewMemMapFs()
	path = filepath.Join("games", "disk.dsk")
	require.NoError(t, afero.WriteFile(fs, path, data, 0o644))
	return fs, path
}

func TestFiles(t *testing.T) {
	t.Parallel()
	entries := []entry{
		{"AUTO", "BAS", 0},
		{"GAME", "BIN", 2},
		{"DATA", "DAT", 1},
		{"NOTES", "TXT", 3},
	}
	all := []string{"AUTO.BAS", "GAME.BIN", "DATA.DAT", "NOTES.TXT"}
	tests := []struct {
		errIs error
		name  string
		img   []byte
		want  []string
	}{
		{name: "35 track", img: buildDisk(35, 1, entries, true), want: all},
		{name: "40 track", img: buildDisk(40, 1, entries, true), want: all},
		{name: "double sided", img: buildDisk(35, 2, entries[:2], true), want: []string{"AUTO.BAS", "GAME.BIN"}},
		{name: "unterminated", img: buildDisk(35, 1, entries, false), want: all},
		{name: "too small", img: make([]byte, 1000), errIs: ErrBadImage},
		{name: "empty", img: nil, errIs: ErrBadImage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs, p := write(t, tt.img)
			files, err := Files(fs, p)
			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
				return
			}
			require.NoError(t, err)
			var got []string
			for _, f := range files {
				got = append(got, f.Name)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilesKilledAndMissing(t *testing.T) {
	t.Parallel()
	img := buildDisk(35, 1, []entry{{"OLD", "BAS", 0}, {"NEW", "BAS", 0}}, true)
	img[dirTrack*trackSize+(dirFirstSector-1)*sectorSize] = 0x00
	fs, p := write(t, img)
	files, err := Files(fs, p)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "NEW.BAS", files[0].Name)

	_, err = Files(fs, filepath.Join("games", "missing.dsk"))
	require.Error(t, err)
}

func TestRunCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		want string
		file File
	}{
		{file: File{Name: "A.BAS", Base: "A", Ext: "BAS", Type: TypeBASIC}, want: `RUN"A"`},
		{file: File{Name: "A.XYZ", Base: "A", Ext: "XYZ", Type: TypeBASIC}, want: `RUN"A.XYZ"`},
		{file: File{Name: "G.BIN", Base: "G", Ext: "BIN", Type: TypeMachine}, want: `LOADM"G":EXEC`},
		{file: File{Name: "G.OBJ", Base: "G", Ext: "OBJ", Type: TypeMachine}, want: `LOADM"G.OBJ":EXEC`},
		{file: File{Name: "D.DAT", Base: "D", Ext: "DAT", Type: TypeData}, want: ""},
		{file: File{Name: "T.TXT", Base: "T", Ext: "TXT", Type: TypeText}, want: ""},
		{file: File{Name: "X", Base: "X", Type: 9}, want: ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.file.RunCommand(), tt.file.Name)
	}
}

func TestRunnable(t *testing.T) {
	t.Parallel()
	bas := func(b string) File { return File{Name: b + ".BAS", Base: b, Ext: "BAS", Type: TypeBASIC} }
	bin := func(b string) File { return File{Name: b + ".BIN", Base: b, Ext: "BIN", Type: TypeMachine} }
	tests := []struct {
		name  string
		want  string
		files []File
		ok    bool
	}{
		{name: "auto", files: []File{bas("A"), bas("AUTO")}, want: "AUTO.BAS", ok: true},
		{name: "autoexec", files: []File{bin("G"), bas("AUTOEXEC")}, want: "AUTOEXEC.BAS", ok: true},
		{name: "single basic", files: []File{bin("G"), bas("A")}, want: "A.BAS", ok: true},
		{name: "many basic then machine", files: []File{bas("A"), bas("B"), bin("G")}, want: "G.BIN", ok: true},
		{name: "many basic only", files: []File{bas("A"), bas("B")}, want: "", ok: false},
		{name: "data only", files: []File{{Name: "D", Base: "D", Type: TypeData}}, want: "", ok: false},
		{name: "none", files: nil, want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := Runnable(tt.files)
		assert.Equal(t, tt.ok, ok, tt.name)
		assert.Equal(t, tt.want, got.Name, tt.name)
	}
}
