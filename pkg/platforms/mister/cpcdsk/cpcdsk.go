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

// Package cpcdsk reads the CP/M directory of Amstrad CPC .dsk images in both
// the standard and extended formats.
package cpcdsk

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

const (
	infoSize        = 0x100
	trackHeaderSize = 0x100
	sectorListStart = 0x18
	sectorListEntry = 8
	maxSectors      = (trackHeaderSize - sectorListStart) / sectorListEntry
	maxSizeCode     = 6
	maxTableTracks  = infoSize - 0x34
	dirEntrySize    = 32
	dirEntries      = 64
	dirBytes        = dirEntrySize * dirEntries
	maxDirTracks    = 4
	recordSize      = 128
	deletedUser     = 0xE5

	formatData   = 0xC0
	formatSystem = 0x40
	formatIBM    = 0x00
)

var (
	// ErrNotDSK is returned when the file is not a CPC disk image.
	ErrNotDSK = errors.New("cpcdsk: not a CPC DSK image")
	// ErrNoDirectory is returned when the directory cannot be located.
	ErrNoDirectory = errors.New("cpcdsk: directory not found")
)

// File is a user 0 directory entry, merged across extents.
type File struct {
	Name   string
	Size   int
	Hidden bool
}

// Listing is the parsed directory of a disk.
type Listing struct {
	Files []File
	// CPM is true when the disk uses the system data format, which reserves
	// tracks for CP/M.
	CPM bool
}

type sector struct {
	off    int64
	length int
	id     byte
	size   byte
	st1    byte
	st2    byte
}

type image struct {
	f        afero.File
	sizes    []byte
	size     int64
	trackLen int64
	tracks   int
	sides    int
	extended bool
}

// Programs reads the directory of the DSK image at path.
func Programs(fs afero.Fs, path string) (Listing, error) {
	f, err := fs.Open(path)
	if err != nil {
		return Listing{}, fmt.Errorf("open disk image: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return Listing{}, fmt.Errorf("stat disk image: %w", err)
	}
	img, err := openImage(f, info.Size())
	if err != nil {
		return Listing{}, err
	}

	first, err := img.track(0)
	if err != nil {
		return Listing{}, err
	}
	if len(first) == 0 {
		return Listing{}, ErrNoDirectory
	}
	lowest := first[0].id
	for _, s := range first {
		lowest = min(lowest, s.id)
	}
	format := lowest & 0xC0

	var reserved int
	switch format {
	case formatSystem:
		reserved = 2
	case formatIBM:
		reserved = 1
	case formatData:
	default:
		return Listing{}, ErrNoDirectory
	}

	dir, err := img.directory(reserved)
	if err != nil {
		return Listing{}, err
	}
	return Listing{Files: parseDirectory(dir), CPM: format == formatSystem}, nil
}

// Directory reads the CP/M directory of a DSK image whose directory follows
// the given number of reserved tracks, for machines other than the CPC that
// share its image format, such as the Tatung Einstein.
func Directory(fs afero.Fs, path string, reserved int) ([]File, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open disk image: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat disk image: %w", err)
	}
	img, err := openImage(f, info.Size())
	if err != nil {
		return nil, err
	}
	dir, err := img.directory(reserved)
	if err != nil {
		return nil, err
	}
	return parseDirectory(dir), nil
}

func openImage(f afero.File, size int64) (*image, error) {
	hdr := make([]byte, infoSize)
	if n, err := f.ReadAt(hdr, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read disc info: %w", err)
	} else if n < infoSize {
		return nil, ErrNotDSK
	}
	img := &image{f: f, size: size, tracks: int(hdr[0x30]), sides: int(hdr[0x31])}
	switch {
	case bytes.HasPrefix(hdr, []byte("EXTENDED")):
		img.extended = true
		img.sizes = hdr[0x34:]
	case bytes.HasPrefix(hdr, []byte("MV - CPC")):
		img.trackLen = int64(hdr[0x32]) | int64(hdr[0x33])<<8
	default:
		return nil, ErrNotDSK
	}
	if img.tracks == 0 || img.sides < 1 || img.sides > 2 {
		return nil, ErrNotDSK
	}
	if img.extended && img.tracks*img.sides > maxTableTracks {
		return nil, ErrNotDSK
	}
	if !img.extended && img.trackLen <= trackHeaderSize {
		return nil, ErrNotDSK
	}
	return img, nil
}

// trackOffset returns the file offset of the track block for side 0 of the
// given track, or false if the track is absent or unformatted.
func (img *image) trackOffset(track int) (int64, bool) {
	if track >= img.tracks {
		return 0, false
	}
	idx := track * img.sides
	if !img.extended {
		return infoSize + int64(idx)*img.trackLen, true
	}
	off := int64(infoSize)
	for i := range idx {
		off += int64(img.sizes[i]) << 8
	}
	if img.sizes[idx] == 0 {
		return 0, false
	}
	return off, true
}

// track returns the sectors of side 0 of the given track.
func (img *image) track(track int) ([]sector, error) {
	off, ok := img.trackOffset(track)
	if !ok || off+trackHeaderSize > img.size {
		return nil, nil
	}
	hdr := make([]byte, trackHeaderSize)
	if _, err := img.f.ReadAt(hdr, off); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read track info: %w", err)
	}
	if !bytes.HasPrefix(hdr, []byte("Track-Info")) {
		return nil, ErrNotDSK
	}
	count := min(int(hdr[0x15]), maxSectors)
	data := off + trackHeaderSize
	sectors := make([]sector, 0, count)
	for i := range count {
		e := hdr[sectorListStart+i*sectorListEntry : sectorListStart+(i+1)*sectorListEntry]
		length := 128 << min(e[3], maxSizeCode)
		if img.extended {
			length = int(e[6]) | int(e[7])<<8
		}
		sectors = append(sectors, sector{
			id: e[2], size: e[3], st1: e[4], st2: e[5],
			off: data, length: length,
		})
		data += int64(length)
	}
	return sectors, nil
}

func (s sector) readable() bool {
	// Missing address mark or no data.
	return s.length > 0 && s.st1&0x05 == 0
}

func (img *image) directory(reserved int) ([]byte, error) {
	var dir []byte
	for t := reserved; t < reserved+maxDirTracks && len(dir) < dirBytes; t++ {
		sectors, err := img.track(t)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(sectors, func(i, j int) bool { return sectors[i].id < sectors[j].id })
		lastID := -1
		for _, s := range sectors {
			if len(dir) >= dirBytes {
				break
			}
			if int(s.id) == lastID {
				continue
			}
			lastID = int(s.id)
			if !s.readable() {
				return nil, ErrNoDirectory
			}
			n := min(s.length, 128<<min(s.size, maxSizeCode), dirBytes-len(dir))
			if s.off+int64(n) > img.size {
				return nil, ErrNoDirectory
			}
			buf := make([]byte, n)
			if _, err := img.f.ReadAt(buf, s.off); err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("read directory sector: %w", err)
			}
			dir = append(dir, buf...)
		}
	}
	if len(dir) < dirEntrySize {
		return nil, ErrNoDirectory
	}
	return dir, nil
}

func parseDirectory(dir []byte) []File {
	var files []File
	index := make(map[string]int)
	for off := 0; off+dirEntrySize <= len(dir); off += dirEntrySize {
		e := dir[off : off+dirEntrySize]
		if e[0] != 0 {
			continue
		}
		name := cleanName(e[1:9])
		ext := cleanName(e[9:12])
		if name == "" {
			continue
		}
		if ext != "" {
			name += "." + ext
		}
		size := int(e[15]) * recordSize
		if i, ok := index[name]; ok {
			files[i].Size += size
			continue
		}
		index[name] = len(files)
		files = append(files, File{Name: name, Size: size, Hidden: e[10]&0x80 != 0})
	}
	return files
}

func cleanName(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		c &= 0x7F
		if c < 0x20 || c == 0x7F {
			c = '?'
		}
		sb.WriteByte(c)
	}
	return strings.TrimRight(sb.String(), " ")
}

// Runnable returns the file to start with RUN", preferring visible files.
func (l Listing) Runnable() (string, bool) {
	var visible, hidden []File
	for _, f := range l.Files {
		if f.Hidden {
			hidden = append(hidden, f)
		} else {
			visible = append(visible, f)
		}
	}
	if name, ok := pick(visible); ok {
		return name, true
	}
	return pick(hidden)
}

func pick(files []File) (string, bool) {
	ext := func(name string) string {
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			return strings.ToUpper(name[i+1:])
		}
		return ""
	}
	for _, f := range files {
		if n := strings.ToUpper(f.Name); n == "DISC" || n == "DISK" {
			return f.Name, true
		}
	}
	for _, f := range files {
		if ext(f.Name) == "BAS" {
			return f.Name, true
		}
	}
	for _, f := range files {
		if e := ext(f.Name); e == "BIN" || (e == "" && !strings.Contains(f.Name, ".")) {
			return f.Name, true
		}
	}
	return "", false
}
