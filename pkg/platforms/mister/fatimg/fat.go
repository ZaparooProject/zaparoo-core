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

package fatimg

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const (
	fatCacheSectors = 8
	maxRootEntries  = 65535
	maxSectorsPerCl = 128
)

type volume struct {
	r     io.ReaderAt
	label string

	cache     [fatCacheSectors]cachedSector
	cacheNext int
	mu        syncutil.Mutex
	base      int64
	fatStart  int64 // bytes, absolute
	rootStart int64 // bytes, absolute; FAT12/16 fixed root
	dataStart int64 // bytes, absolute
	fatBytes  int64
	bps       int64
	clBytes   int64
	clusters  uint32 // number of data clusters
	rootClus  uint32 // FAT32 root cluster
	rootBytes int64
	typ       Type
}

type cachedSector struct {
	data []byte
	idx  int64
	ok   bool
}

// newVolume parses and validates the boot sector at base. size is the usable
// image size in bytes; the volume must fit entirely inside it.
func newVolume(r io.ReaderAt, base, size int64) (*volume, error) {
	var bs [mbrSectorSize]byte
	if err := readFull(r, bs[:], base); err != nil {
		return nil, err
	}
	if bs[510] != 0x55 || bs[511] != 0xAA {
		return nil, ErrCorrupt
	}
	le := binary.LittleEndian
	bps := int64(le.Uint16(bs[11:13]))
	spc := int64(bs[13])
	reserved := int64(le.Uint16(bs[14:16]))
	nfats := int64(bs[16])
	rootEnt := int64(le.Uint16(bs[17:19]))
	total := int64(le.Uint16(bs[19:21]))
	if total == 0 {
		total = int64(le.Uint32(bs[32:36]))
	}
	fatSz := int64(le.Uint16(bs[22:24]))
	if fatSz == 0 {
		fatSz = int64(le.Uint32(bs[36:40]))
	}

	switch bps {
	case 512, 1024, 2048, 4096:
	default:
		return nil, ErrCorrupt
	}
	if spc == 0 || spc > maxSectorsPerCl || spc&(spc-1) != 0 ||
		reserved == 0 || nfats == 0 || nfats > 4 ||
		fatSz == 0 || total == 0 || rootEnt > maxRootEntries {
		return nil, ErrCorrupt
	}
	if base+total*bps > size {
		return nil, fmt.Errorf("%w: volume exceeds image", ErrCorrupt)
	}

	rootSectors := (rootEnt*32 + bps - 1) / bps
	dataSectors := total - reserved - nfats*fatSz - rootSectors
	if dataSectors <= 0 {
		return nil, ErrCorrupt
	}
	clusters := dataSectors / spc
	if clusters == 0 || clusters > 0x0FFFFFF5-2 {
		return nil, ErrCorrupt
	}

	v := &volume{
		r:         r,
		base:      base,
		bps:       bps,
		clBytes:   spc * bps,
		clusters:  uint32(clusters & 0xFFFFFFFF),
		fatStart:  base + reserved*bps,
		fatBytes:  fatSz * bps,
		rootStart: base + (reserved+nfats*fatSz)*bps,
		rootBytes: rootEnt * 32,
		dataStart: base + (reserved+nfats*fatSz+rootSectors)*bps,
	}
	for i := range v.cache {
		v.cache[i].data = make([]byte, bps)
	}

	var need int64
	switch {
	case clusters < 4085:
		v.typ = FAT12
		need = ((clusters+2)*3 + 1) / 2
	case clusters < 65525:
		v.typ = FAT16
		need = (clusters + 2) * 2
	default:
		v.typ = FAT32
		need = (clusters + 2) * 4
	}
	if v.fatBytes < need {
		return nil, fmt.Errorf("%w: FAT too small", ErrCorrupt)
	}

	if v.typ == FAT32 {
		if rootEnt != 0 {
			return nil, ErrCorrupt
		}
		v.rootClus = le.Uint32(bs[44:48])
		if !v.validCluster(v.rootClus) {
			return nil, fmt.Errorf("%w: bad root cluster", ErrCorrupt)
		}
		v.label = bootLabel(bs[71:82])
	} else {
		if rootEnt == 0 {
			return nil, ErrCorrupt
		}
		v.label = bootLabel(bs[43:54])
	}

	budget := maxDirSlots
	if _, label, err := v.readDirRaw(v.rootEntry(), &budget); err == nil && label != "" {
		v.label = label
	}
	return v, nil
}

func bootLabel(b []byte) string {
	s := strings.TrimRight(oemString(b), " ")
	if s == "NO NAME" {
		return ""
	}
	return s
}

func (v *volume) validCluster(c uint32) bool {
	return c >= 2 && c < v.clusters+2
}

func (v *volume) clusterOffset(c uint32) int64 {
	return v.dataStart + int64(c-2)*v.clBytes
}

// fatBytesAt reads n (1-4) bytes from the first FAT through the sector cache.
func (v *volume) fatBytesAt(off int64, dst []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i := range dst {
		o := off + int64(i)
		if o >= v.fatBytes {
			return ErrCorrupt
		}
		sec := o / v.bps
		cs, err := v.fatSector(sec)
		if err != nil {
			return err
		}
		dst[i] = cs.data[o%v.bps]
	}
	return nil
}

func (v *volume) fatSector(sec int64) (*cachedSector, error) {
	for i := range v.cache {
		if v.cache[i].ok && v.cache[i].idx == sec {
			return &v.cache[i], nil
		}
	}
	cs := &v.cache[v.cacheNext]
	v.cacheNext = (v.cacheNext + 1) % fatCacheSectors
	cs.ok = false
	if err := readFull(v.r, cs.data, v.fatStart+sec*v.bps); err != nil {
		return nil, err
	}
	cs.idx = sec
	cs.ok = true
	return cs, nil
}

// next returns the cluster following c. last is true at end of chain.
func (v *volume) next(c uint32) (next uint32, last bool, err error) {
	var buf [4]byte
	var val uint32
	switch v.typ {
	case FAT12:
		off := int64(c) * 3 / 2
		if err := v.fatBytesAt(off, buf[:2]); err != nil {
			return 0, false, err
		}
		val = uint32(binary.LittleEndian.Uint16(buf[:2]))
		if c&1 == 1 {
			val >>= 4
		} else {
			val &= 0xFFF
		}
		if val >= 0xFF8 {
			return 0, true, nil
		}
	case FAT16:
		if err := v.fatBytesAt(int64(c)*2, buf[:2]); err != nil {
			return 0, false, err
		}
		val = uint32(binary.LittleEndian.Uint16(buf[:2]))
		if val >= 0xFFF8 {
			return 0, true, nil
		}
	default:
		if err := v.fatBytesAt(int64(c)*4, buf[:4]); err != nil {
			return 0, false, err
		}
		val = binary.LittleEndian.Uint32(buf[:4]) & 0x0FFFFFFF
		if val >= 0x0FFFFFF8 {
			return 0, true, nil
		}
	}
	if !v.validCluster(val) {
		return 0, false, fmt.Errorf("%w: bad cluster link %d -> %d", ErrCorrupt, c, val)
	}
	return val, false, nil
}

// walkChain calls fn for each cluster of the chain starting at start. A loop
// fails with ErrCorrupt, found with Brent's algorithm so no per-cluster state
// is kept; the step count is also capped at the number of clusters.
func (v *volume) walkChain(start uint32, fn func(c uint32) (bool, error)) error {
	if !v.validCluster(start) {
		return fmt.Errorf("%w: bad start cluster %d", ErrCorrupt, start)
	}
	c := start
	mark, power, lam := start, uint32(1), uint32(0)
	for range v.clusters {
		stop, err := fn(c)
		if err != nil || stop {
			return err
		}
		n, last, err := v.next(c)
		if err != nil {
			return err
		}
		if last {
			return nil
		}
		c = n
		if c == mark {
			return fmt.Errorf("%w: cluster chain loop", ErrCorrupt)
		}
		lam++
		if lam == power {
			mark, power, lam = c, power*2, 0
		}
	}
	return fmt.Errorf("%w: cluster chain loop", ErrCorrupt)
}
