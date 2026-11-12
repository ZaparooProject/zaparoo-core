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

package vgm

import (
	"encoding/binary"
	"errors"
)

var errBadCompression = errors.New("invalid compressed data block")

// decompTable is a decompression table from a 0x7F data block.
type decompTable struct {
	vals []uint16
	bd   uint8
	bc   uint8
}

// decompTables holds one table per compression type and sub-type.
type decompTables map[uint16]*decompTable

// addTable parses a 0x7F data block.
func (t decompTables) addTable(b []byte) {
	if len(b) < 6 {
		return
	}
	ctype, sub, bd, bc := b[0], b[1], b[2], b[3]
	if bd == 0 || bd > 16 {
		return
	}
	count := int(binary.LittleEndian.Uint16(b[4:]))
	size := (int(bd) + 7) / 8
	data := b[6:]
	count = min(count, len(data)/size)
	vals := make([]uint16, count)
	for i := range vals {
		if size == 1 {
			vals[i] = uint16(data[i])
		} else {
			vals[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
	}
	t[uint16(ctype)<<8|uint16(sub)] = &decompTable{vals: vals, bd: bd, bc: bc}
}

// bitReader reads values MSB-first from a byte slice.
type bitReader struct {
	b   []byte
	pos int // bit position
}

func (r *bitReader) read(n uint8) uint32 {
	var v uint32
	for range n {
		byteIdx := r.pos >> 3
		bit := (r.b[byteIdx] >> (7 - uint(r.pos&7))) & 1
		v = v<<1 | uint32(bit)
		r.pos++
	}
	return v
}

// decompress expands a compressed stream block (types 0x40-0x7E, without
// the type byte of the block itself). The output is capped at limit bytes.
func (t decompTables) decompress(b []byte, limit int) ([]byte, error) {
	if len(b) < 10 {
		return nil, errBadCompression
	}
	ctype := b[0]
	outSize := int64(binary.LittleEndian.Uint32(b[1:]))
	bd, bc, sub := b[5], b[6], b[7]
	base := uint32(binary.LittleEndian.Uint16(b[8:]))
	in := b[10:]
	if bd == 0 || bd > 16 || bc == 0 || bc > 16 {
		return nil, errBadCompression
	}
	size := (int64(bd) + 7) / 8
	count := min(outSize/size, int64(len(in))*8/int64(bc), int64(limit)/size)
	if count <= 0 {
		return nil, nil
	}

	var tbl *decompTable
	useTable := ctype == 1 || (ctype == 0 && sub == 2)
	switch {
	case ctype > 1:
		return nil, errBadCompression
	case useTable:
		tbl = t[uint16(ctype)<<8|uint16(sub)]
		if tbl == nil || tbl.bd != bd || tbl.bc != bc {
			return nil, errBadCompression
		}
	case sub > 1:
		return nil, errBadCompression
	}

	out := make([]byte, 0, count*size)
	r := bitReader{b: in}
	mask := uint32(1)<<bd - 1
	state := base
	for range count {
		var v uint32
		if bc <= 8 {
			v = r.read(bc)
		} else {
			lo := r.read(8)
			v = lo | r.read(bc-8)<<8
		}
		var o uint32
		switch {
		case ctype == 1:
			if int(v) < len(tbl.vals) {
				state = (state + uint32(tbl.vals[v])) & mask
			}
			o = state
		case sub == 0:
			o = v + base
		case sub == 1:
			if bd > bc {
				v <<= bd - bc
			}
			o = v + base
		default:
			if int(v) < len(tbl.vals) {
				o = uint32(tbl.vals[v])
			}
		}
		if size == 1 {
			out = append(out, byte(o))
		} else {
			out = append(out, byte(o), byte(o>>8))
		}
	}
	return out, nil
}
