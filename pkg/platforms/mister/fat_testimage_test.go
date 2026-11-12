//go:build linux && !android

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

package mister

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// testFATFile is a file placed in a test FAT image. dir is a single top-level
// folder name, or empty for the root; short is the 8.3 name as stored and long,
// when set, the long name stored with it.
type testFATFile struct {
	dir, short, long string
	data             []byte
}

const (
	testFATSectors      = 4167
	testFATSize         = 17
	testFATRootSectors  = 32
	testFATReserved     = 1
	testFATFats         = 2
	testFATBytesPerSect = 512
)

// buildTestFAT16 builds a small FAT16 superfloppy image whose files are one
// cluster each, with LFN entries for files that have a long name.
func buildTestFAT16(files []testFATFile) []byte {
	img := make([]byte, testFATSectors*testFATBytesPerSect)
	boot := img[:testFATBytesPerSect]
	copy(boot, []byte{0xEB, 0x3C, 0x90})
	copy(boot[3:], "MSDOS5.0")
	binary.LittleEndian.PutUint16(boot[11:], testFATBytesPerSect)
	boot[13] = 1
	binary.LittleEndian.PutUint16(boot[14:], testFATReserved)
	boot[16] = testFATFats
	binary.LittleEndian.PutUint16(boot[17:], 512)
	binary.LittleEndian.PutUint16(boot[19:], testFATSectors)
	boot[21] = 0xF8
	binary.LittleEndian.PutUint16(boot[22:], testFATSize)
	copy(boot[43:], "TESTCARD   ")
	copy(boot[54:], "FAT16   ")
	boot[510], boot[511] = 0x55, 0xAA

	fatOff := testFATReserved * testFATBytesPerSect
	rootOff := fatOff + testFATFats*testFATSize*testFATBytesPerSect
	dataOff := rootOff + testFATRootSectors*testFATBytesPerSect
	fat := img[fatOff:]
	binary.LittleEndian.PutUint16(fat[0:], 0xFFF8)
	binary.LittleEndian.PutUint16(fat[2:], 0xFFFF)

	next := 2
	alloc := func() int {
		c := next
		next++
		binary.LittleEndian.PutUint16(fat[c*2:], 0xFFFF)
		return c
	}
	clusterOff := func(c int) int { return dataOff + (c-2)*testFATBytesPerSect }

	rootSlots := 0
	dirSlots := map[string]int{}
	dirCluster := map[string]int{}
	putEntries := func(dir string, entries [][]byte) {
		var base, slot int
		if dir == "" {
			base, slot = rootOff, rootSlots
			rootSlots += len(entries)
		} else {
			base, slot = clusterOff(dirCluster[dir]), dirSlots[dir]
			dirSlots[dir] += len(entries)
		}
		for i, e := range entries {
			copy(img[base+(slot+i)*32:], e)
		}
	}

	for _, f := range files {
		if f.dir == "" {
			continue
		}
		if _, ok := dirCluster[f.dir]; ok {
			continue
		}
		dirCluster[f.dir] = alloc()
		short := strings.ToUpper(f.dir)
		putEntries("", [][]byte{testShortEntry(short, "", 0x10, dirCluster[f.dir], 0)})
		putEntries(f.dir, [][]byte{
			testShortEntry(".", "", 0x10, dirCluster[f.dir], 0),
			testShortEntry("..", "", 0x10, 0, 0),
		})
	}

	for _, f := range files {
		cluster := alloc()
		copy(img[clusterOff(cluster):], f.data)
		name, ext, _ := strings.Cut(f.short, ".")
		var entries [][]byte
		if f.long != "" {
			entries = testLFNEntries(f.long, testShortEntry(name, ext, 0x20, cluster, len(f.data)))
		}
		entries = append(entries, testShortEntry(name, ext, 0x20, cluster, len(f.data)))
		putEntries(f.dir, entries)
	}
	return img
}

func testShortEntry(name, ext string, attr byte, cluster, size int) []byte {
	e := make([]byte, 32)
	field := make([]byte, 11)
	for i := range field {
		field[i] = ' '
	}
	copy(field, name)
	copy(field[8:], ext)
	copy(e, field)
	e[11] = attr
	binary.LittleEndian.PutUint16(e[26:], uint16(cluster)) //nolint:gosec // test clusters are small
	binary.LittleEndian.PutUint32(e[28:], uint32(size))    //nolint:gosec // test sizes are small
	return e
}

func testLFNChecksum(short []byte) byte {
	var sum byte
	for _, b := range short[:11] {
		sum = (sum>>1 | sum<<7) + b
	}
	return sum
}

func testLFNEntries(long string, short []byte) [][]byte {
	units := utf16.Encode([]rune(long))
	units = append(units, 0)
	for len(units)%13 != 0 {
		units = append(units, 0xFFFF)
	}
	count := len(units) / 13
	sum := testLFNChecksum(short)
	entries := make([][]byte, 0, count)
	for n := count; n >= 1; n-- {
		e := make([]byte, 32)
		e[0] = byte(n) //nolint:gosec // a name is a few entries
		if n == count {
			e[0] |= 0x40
		}
		e[11], e[13] = 0x0F, sum
		chunk := units[(n-1)*13 : n*13]
		offsets := []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30}
		for i, off := range offsets {
			binary.LittleEndian.PutUint16(e[off:], chunk[i])
		}
		entries = append(entries, e)
	}
	return entries
}
