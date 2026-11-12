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

// Package decb reads the file directory of Tandy CoCo Disk Extended Color
// BASIC disk images.
package decb

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/afero"
)

const (
	sectorSize      = 256
	sectorsPerTrack = 18
	trackSize       = sectorSize * sectorsPerTrack
	dirTrack        = 17
	dirFirstSector  = 3
	dirSectors      = 9
	entrySize       = 32
	minImageSize    = 35 * trackSize
	maxImageSize    = 64 << 20
	endOfDirectory  = 0xFF
	killedEntry     = 0x00
)

// ErrBadImage is returned when the file is not a plausible DECB disk image.
var ErrBadImage = errors.New("decb: not a DECB disk image")

// FileType is the DECB file type byte.
type FileType uint8

const (
	TypeBASIC   FileType = 0
	TypeData    FileType = 1
	TypeMachine FileType = 2
	TypeText    FileType = 3
)

// File is a single directory entry.
type File struct {
	Name string
	Base string
	Ext  string
	Type FileType
}

// Files lists the files in the directory of the DECB disk image at path.
func Files(fs afero.Fs, path string) ([]File, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open disk image: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat disk image: %w", err)
	}
	size := info.Size()
	if size < minImageSize || size > maxImageSize {
		return nil, ErrBadImage
	}

	// Double-sided images interleave the two sides track by track.
	sides := int64(1)
	if size == 2*minImageSize || size == 2*40*trackSize {
		sides = 2
	}
	dirOffset := dirTrack*sides*trackSize + (dirFirstSector-1)*sectorSize
	if dirOffset+dirSectors*sectorSize > size {
		return nil, ErrBadImage
	}

	buf := make([]byte, dirSectors*sectorSize)
	if _, err := f.ReadAt(buf, dirOffset); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var files []File
	for off := 0; off+entrySize <= len(buf); off += entrySize {
		e := buf[off : off+entrySize]
		if e[0] == endOfDirectory {
			break
		}
		if e[0] == killedEntry {
			continue
		}
		// Disks of other formats, such as OS-9, land arbitrary bytes here.
		// One entry that is not a DECB file means the directory is not DECB.
		if !validEntry(e) {
			return nil, ErrBadImage
		}
		base := cleanName(e[0:8])
		ext := cleanName(e[8:11])
		if base == "" {
			continue
		}
		name := base
		if ext != "" {
			name = base + "." + ext
		}
		files = append(files, File{Name: name, Base: base, Ext: ext, Type: FileType(e[11])})
	}
	return files, nil
}

// validEntry reports whether a directory entry is a DECB file: a printable
// name, a known file type, and an ASCII flag of 0x00 or 0xFF.
func validEntry(e []byte) bool {
	for _, c := range e[0:11] {
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	return FileType(e[11]) <= TypeText && (e[12] == 0x00 || e[12] == 0xFF)
}

func cleanName(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c < 0x20 || c > 0x7E {
			c = '?'
		}
		sb.WriteByte(c)
	}
	return strings.TrimRight(sb.String(), " ")
}

// RunCommand returns the DECB command that starts the file, or an empty
// string for data and text files.
func (f File) RunCommand() string {
	name := f.Base
	switch f.Type {
	case TypeBASIC:
		if f.Ext != "" && f.Ext != "BAS" {
			name = f.Name
		}
		return `RUN"` + name + `"`
	case TypeMachine:
		if f.Ext != "" && f.Ext != "BIN" {
			name = f.Name
		}
		return `LOADM"` + name + `":EXEC`
	case TypeData, TypeText:
		return ""
	}
	return ""
}

// Runnable picks the file to start: an AUTO program first, then the only
// BASIC program, then the first machine-language file.
func Runnable(files []File) (File, bool) {
	var basic []File
	var machine []File
	for _, f := range files {
		switch f.Type {
		case TypeBASIC:
			if f.Ext == "BAS" && (f.Base == "AUTO" || f.Base == "AUTOEXEC") {
				return f, true
			}
			basic = append(basic, f)
		case TypeMachine:
			machine = append(machine, f)
		case TypeData, TypeText:
		}
	}
	if len(basic) == 1 {
		return basic[0], true
	}
	if len(machine) > 0 {
		return machine[0], true
	}
	return File{}, false
}
