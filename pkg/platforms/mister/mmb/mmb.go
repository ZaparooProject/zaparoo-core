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
//

// Package mmb reads the disc catalog of a BBC/Acorn MMFS MMB container.
package mmb

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/afero"
)

const (
	catalogSize   = 0x2000
	entrySize     = 16
	slotsPerChunk = 511
	chunkSize     = 0x63D0000
	headerFlagOff = 8
	maxChunks     = 16

	statusReadOnly  = 0x00
	statusReadWrite = 0x0F
)

// ErrNotMMB is returned when a file is not a plausible MMB container.
var ErrNotMMB = errors.New("not a valid MMB file")

// Disk is a single formatted disc in an MMB container.
type Disk struct {
	Title    string
	Number   int
	ReadOnly bool
}

// ReadCatalog returns the formatted discs listed in the MMB at path. Only the
// catalog regions of the file are read.
func ReadCatalog(fs afero.Fs, path string) ([]Disk, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mmb: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat mmb: %w", err)
	}
	size := info.Size()
	if size < catalogSize {
		return nil, fmt.Errorf("%w: file too short", ErrNotMMB)
	}

	cat := make([]byte, catalogSize)
	chunks := 1
	var disks []Disk
	for k := 0; k < chunks; k++ {
		off := int64(k) * chunkSize
		if off+catalogSize > size {
			break
		}
		if _, err := f.ReadAt(cat, off); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read mmb catalog: %w", err)
		}
		if k == 0 {
			if h := cat[headerFlagOff]; h >= 0xA0 && h <= 0xAF {
				chunks = int(h-0xA0) + 1
				if chunks > maxChunks {
					chunks = maxChunks
				}
			}
		}
		for slot := range slotsPerChunk {
			e := cat[(slot+1)*entrySize : (slot+2)*entrySize]
			if e[15] != statusReadOnly && e[15] != statusReadWrite {
				continue
			}
			if !isTitleText(e[:12]) {
				continue
			}
			title := sanitizeTitle(e[:12])
			if title == "" {
				continue
			}
			disks = append(disks, Disk{
				Title:    title,
				Number:   k*slotsPerChunk + slot,
				ReadOnly: e[15] == statusReadOnly,
			})
		}
	}

	if len(disks) == 0 {
		return nil, fmt.Errorf("%w: no valid disc entries", ErrNotMMB)
	}
	return disks, nil
}

// isTitleText reports whether a catalog title holds only the padding and
// printable ASCII a disc title is written in. A disk image that is not an MMB
// has bytes outside that in nearly every 12-byte window, so this is what tells
// the two apart.
func isTitleText(b []byte) bool {
	for _, c := range b {
		if c != 0 && (c < 0x20 || c > 0x7E) {
			return false
		}
	}
	return true
}

func sanitizeTitle(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c < 0x20 || c > 0x7E || c == '/' || c == '\\' {
			c = ' '
		}
		out[i] = c
	}
	return strings.Join(strings.Fields(string(out)), " ")
}
