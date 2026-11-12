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

// Package fatimg reads files from FAT12, FAT16 and FAT32 disk images without
// loading them into memory. It is read-only and treats every image as
// untrusted input.
package fatimg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/afero"
)

var (
	ErrNoFilesystem   = errors.New("fatimg: no FAT filesystem found")
	ErrCorrupt        = errors.New("fatimg: corrupt filesystem")
	ErrNotFound       = errors.New("fatimg: not found")
	ErrNotDir         = errors.New("fatimg: not a directory")
	ErrIsDir          = errors.New("fatimg: is a directory")
	ErrTooLarge       = errors.New("fatimg: file too large")
	ErrTooManyEntries = errors.New("fatimg: too many entries")
	ErrInvalidPath    = errors.New("fatimg: invalid path")
)

const (
	mbrSectorSize   = 512
	vhdFooterSize   = 512
	maxExtendedHops = 64
)

var mbrFATTypes = map[byte]bool{
	0x01: true, 0x04: true, 0x06: true, 0x0B: true, 0x0C: true, 0x0E: true,
}

// Type is the FAT variant of a volume.
type Type int

const (
	FAT12 Type = 12
	FAT16 Type = 16
	FAT32 Type = 32
)

func (t Type) String() string {
	switch t {
	case FAT12:
		return "FAT12"
	case FAT16:
		return "FAT16"
	case FAT32:
		return "FAT32"
	default:
		return fmt.Sprintf("Type(%d)", int(t))
	}
}

// Volume is one FAT filesystem inside an image.
type Volume struct {
	v     *volume
	Label string
	Index int
	Type  Type
}

// Image is an open disk image.
type Image struct {
	file    afero.File
	volumes []Volume
}

// Open opens a disk image and detects its FAT volumes. The image is either a
// bare FAT filesystem or an MBR partition table with FAT partitions.
func Open(fs afero.Fs, path string) (*Image, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat image: %w", err)
	}
	size := st.Size()
	if size >= vhdFooterSize {
		var cookie [8]byte
		if _, err := f.ReadAt(cookie[:], size-vhdFooterSize); err == nil &&
			bytes.Equal(cookie[:], []byte("conectix")) {
			size -= vhdFooterSize
		}
	}

	img := &Image{file: f}
	if err := img.detect(size); err != nil {
		_ = f.Close()
		return nil, err
	}
	return img, nil
}

// Close closes the underlying file.
func (i *Image) Close() error {
	if err := i.file.Close(); err != nil {
		return fmt.Errorf("close image: %w", err)
	}
	return nil
}

// Volumes returns the FAT volumes found in the image.
func (i *Image) Volumes() []Volume {
	out := make([]Volume, len(i.volumes))
	copy(out, i.volumes)
	return out
}

func (i *Image) detect(size int64) error {
	if size < mbrSectorSize {
		return ErrNoFilesystem
	}
	if vol, err := newVolume(i.file, 0, size); err == nil {
		i.addVolume(vol)
		return nil
	}

	var sector [mbrSectorSize]byte
	if _, err := i.file.ReadAt(sector[:], 0); err != nil {
		return fmt.Errorf("read mbr: %w", err)
	}
	if sector[510] != 0x55 || sector[511] != 0xAA {
		return ErrNoFilesystem
	}

	var extStart int64 = -1
	for n := range 4 {
		start, typ := partEntry(sector[:], n)
		switch {
		case mbrFATTypes[typ]:
			i.tryPartition(start, size)
		case (typ == 0x05 || typ == 0x0F) && extStart < 0 && start > 0:
			extStart = start
		}
	}
	if extStart >= 0 {
		i.walkExtended(extStart, size)
	}
	if len(i.volumes) == 0 {
		return ErrNoFilesystem
	}
	return nil
}

// partEntry returns the start LBA and type of MBR partition entry n.
func partEntry(sector []byte, n int) (start int64, typ byte) {
	e := sector[446+n*16 : 446+(n+1)*16]
	if binary.LittleEndian.Uint32(e[12:16]) == 0 {
		return 0, 0
	}
	return int64(binary.LittleEndian.Uint32(e[8:12])), e[4]
}

func (i *Image) tryPartition(startLBA, size int64) {
	base := startLBA * mbrSectorSize
	if base <= 0 || base >= size {
		return
	}
	if vol, err := newVolume(i.file, base, size); err == nil {
		i.addVolume(vol)
	}
}

// walkExtended follows an extended partition's chain of EBRs.
func (i *Image) walkExtended(extStart, size int64) {
	cur := extStart
	for range maxExtendedHops {
		off := cur * mbrSectorSize
		if off <= 0 || off+mbrSectorSize > size {
			return
		}
		var ebr [mbrSectorSize]byte
		if _, err := i.file.ReadAt(ebr[:], off); err != nil ||
			ebr[510] != 0x55 || ebr[511] != 0xAA {
			return
		}
		rel, typ := partEntry(ebr[:], 0)
		if mbrFATTypes[typ] {
			i.tryPartition(cur+rel, size)
		}
		next, ntyp := partEntry(ebr[:], 1)
		if (ntyp != 0x05 && ntyp != 0x0F) || next == 0 {
			return
		}
		cur = extStart + next
	}
}

func (i *Image) addVolume(v *volume) {
	i.volumes = append(i.volumes, Volume{
		Index: len(i.volumes),
		Label: v.label,
		Type:  v.typ,
		v:     v,
	})
}

func readFull(r io.ReaderAt, buf []byte, off int64) error {
	n, err := r.ReadAt(buf, off)
	if n == len(buf) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("read at %d: %w", off, err)
}
