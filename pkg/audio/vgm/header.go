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

// vgmRate is the fixed rate of VGM command timing.
const vgmRate = 44100

// Chip indexes, in VGM header order.
const (
	chipSN76489  = 0x00
	chipYM2413   = 0x01
	chipYM2612   = 0x02
	chipYM2151   = 0x03
	chipSegaPCM  = 0x04
	chipRF5C68   = 0x05
	chipYM2203   = 0x06
	chipYM2608   = 0x07
	chipYM2610   = 0x08
	chipYM3812   = 0x09
	chipYM3526   = 0x0A
	chipY8950    = 0x0B
	chipYMF262   = 0x0C
	chipYMF278B  = 0x0D
	chipYMF271   = 0x0E
	chipYMZ280B  = 0x0F
	chipRF5C164  = 0x10
	chipPWM      = 0x11
	chipAY8910   = 0x12
	chipGB       = 0x13
	chipNES      = 0x14
	chipMultiPCM = 0x15
	chipUPD7759  = 0x16
	chipOKIM6258 = 0x17
	chipOKIM6295 = 0x18
	chipK051649  = 0x19
	chipK054539  = 0x1A
	chipHuC6280  = 0x1B
	chipC140     = 0x1C
	chipK053260  = 0x1D
	chipPokey    = 0x1E
	chipQSound   = 0x1F
	chipSCSP     = 0x20
	chipWSwan    = 0x21
	chipVSU      = 0x22
	chipSAA1099  = 0x23
	chipES5503   = 0x24
	chipES5506   = 0x25
	chipX1010    = 0x26
	chipC352     = 0x27
	chipGA20     = 0x28
	chipMikey    = 0x29

	numChips = 0x2A
)

// clockOffsets maps each chip index to its header clock field.
var clockOffsets = [numChips]int{
	0x0C, 0x10, 0x2C, 0x30, 0x38, 0x40, 0x44, 0x48,
	0x4C, 0x50, 0x54, 0x58, 0x5C, 0x60, 0x64, 0x68,
	0x6C, 0x70, 0x74, 0x80, 0x84, 0x88, 0x8C, 0x90,
	0x98, 0x9C, 0xA0, 0xA4, 0xA8, 0xAC, 0xB0, 0xB4,
	0xB8, 0xC0, 0xC4, 0xC8, 0xCC, 0xD0, 0xD8, 0xDC,
	0xE0, 0xE4,
}

// omittedChips play silent: their cores are not vendored.
var omittedChips = [numChips]bool{
	chipYMF278B: true,
	chipWSwan:   true,
	chipES5506:  true,
}

// Relative chip volumes (8.8 fixed point) and the playback amount used to
// normalize the mix, by chip index. These follow VGMPlay's tables so files
// sound balanced the way their authors heard them.
var (
	baseVolume = [numChips]int32{
		0x80, 0x200, 0x100, 0x100, 0x180, 0xB0, 0x100, 0x80,
		0x80, 0x100, 0x100, 0x100, 0x100, 0x100, 0x100, 0x98,
		0x80, 0xE0, 0x100, 0xC0, 0x100, 0x40, 0x11E, 0x1C0,
		0x100, 0xA0, 0x100, 0x100, 0x100, 0x100, 0x100, 0x100,
		0x20, 0x100, 0x100, 0x100, 0x40, 0x20, 0x100, 0x40,
		0x280, 0x100,
	}
	mixAmount = [numChips]int32{
		0x100, 0x80, 0x100, 0x100, 0x100, 0x100, 0x100, 0x100,
		0x100, 0x200, 0x200, 0x200, 0x200, 0x100, 0x100, 0x1AF,
		0x200, 0x100, 0x200, 0x400, 0x200, 0x400, 0x100, 0x200,
		0x200, 0x100, 0x100, 0x100, 0x180, 0x100, 0x100, 0x100,
		0x800, 0x100, 0x100, 0x100, 0x800, 0x1000, 0x100, 0x800,
		0x100, 0x200,
	}
)

const (
	minHeaderLen  = 0x40
	headerBufLen  = 0x100
	maxTotalTicks = int64(vgmRate) * 60 * 60 * 6 // 6 hours of VGM time
)

var (
	errNotVGM    = errors.New("not a VGM file")
	errTruncated = errors.New("truncated VGM file")
	errNoChips   = errors.New("VGM file uses no supported sound chip")
)

// chipSpec is one chip instance the file asks for.
type chipSpec struct {
	clock  uint32 // clock with the flag bits removed
	flag   bool   // bit 31 of the header clock
	typ    uint8
	num    uint8
	paired bool // the file uses two chips of this type
}

// header is the parsed, bounds-checked VGM header.
type header struct {
	chips      []chipSpec
	xVols      []extraVol
	xClocks    []extraClock
	totalTicks int64
	loopTicks  int64
	dataOfs    int
	dataEnd    int
	cmdEnd     int // end of the playable commands, set by prescan
	gd3Ofs     int // 0 when absent
	loopOfs    int // 0 when absent or invalid
	volMod     int
	version    uint32
	loopBase   int8
	loopMod    uint8
	raw        [headerBufLen]byte
}

// extraVol is one entry of the extra header's volume table.
type extraVol struct {
	typ   uint8
	flags uint8
	vol   uint16
}

func le32(b []byte, o int) uint32 { return binary.LittleEndian.Uint32(b[o:]) }

func (h *header) u8(o int) uint8   { return h.raw[o] }
func (h *header) u16(o int) uint16 { return binary.LittleEndian.Uint16(h.raw[o:]) }
func (h *header) u32(o int) uint32 { return binary.LittleEndian.Uint32(h.raw[o:]) }

// relOfs resolves a header offset field that is relative to its own
// position, returning 0 for an unset or out-of-range value.
func relOfs(field int, v uint32, size int) int {
	if v == 0 {
		return 0
	}
	o := int64(field) + int64(v)
	if o <= int64(field) || o >= int64(size) {
		return 0
	}
	return int(o)
}

// parseHeader validates the fixed header of a VGM file. Command-stream
// facts (true length, loop position) are filled in by prescan.
func parseHeader(data []byte) (*header, error) {
	if len(data) < 4 || string(data[:4]) != "Vgm " {
		return nil, errNotVGM
	}
	if len(data) < minHeaderLen {
		return nil, errTruncated
	}
	h := &header{version: le32(data, 0x08)}

	h.dataOfs = 0x40
	if h.version >= 0x150 {
		if v := le32(data, 0x34); v != 0 {
			o := int64(0x34) + int64(v)
			if o < 0x38 {
				o = 0x38
			}
			if o > int64(len(data)) {
				return nil, errTruncated
			}
			h.dataOfs = int(o)
		}
	}
	if h.dataOfs > len(data) {
		return nil, errTruncated
	}

	// Header bytes at or past the data start belong to the command stream
	// and read as zero, as does anything past the extra header.
	hdrLen := min(h.dataOfs, headerBufLen, len(data))
	xhOfs := 0
	if hdrLen >= 0xC0 {
		xhOfs = relOfs(0xBC, le32(data, 0xBC), len(data))
		if xhOfs != 0 && xhOfs < hdrLen {
			hdrLen = xhOfs
		}
	}
	copy(h.raw[:hdrLen], data[:hdrLen])

	h.dataEnd = len(data)
	if eof := relOfs(0x04, h.u32(0x04), len(data)+1); eof > h.dataOfs && eof < h.dataEnd {
		h.dataEnd = eof
	}
	h.gd3Ofs = relOfs(0x14, h.u32(0x14), len(data))
	if h.gd3Ofs >= h.dataOfs && h.gd3Ofs < h.dataEnd {
		h.dataEnd = h.gd3Ofs
	}
	h.loopOfs = relOfs(0x1C, h.u32(0x1C), len(data))
	if h.loopOfs < h.dataOfs || h.loopOfs >= h.dataEnd || h.u32(0x20) == 0 {
		h.loopOfs = 0
	}

	h.volMod = volumeModifier(h.u8(0x7C))
	h.loopBase = int8(h.u8(0x7E)) //nolint:gosec // the spec stores loop base as a signed byte
	h.loopMod = h.u8(0x7F)

	if xhOfs != 0 {
		h.parseExtraHeader(data, xhOfs)
	}
	h.collectChips()
	return h, nil
}

func volumeModifier(v uint8) int {
	switch {
	case v <= 0xC0:
		return int(v)
	case v == 0xC1:
		return -0x40
	default:
		return int(v) - 0x100
	}
}

// extraClock holds a second-chip clocks from the extra header.
type extraClock struct {
	typ   uint8
	clock uint32
}

func (h *header) parseExtraHeader(data []byte, xh int) {
	if xh+4 > len(data) {
		return
	}
	xhLen := le32(data, xh)
	if xhLen >= 0x0C && xh+0x0C <= len(data) {
		if vt := relOfs(xh+8, le32(data, xh+8), len(data)); vt != 0 {
			n := int(data[vt])
			for i := range n {
				o := vt + 1 + i*4
				if o+4 > len(data) {
					break
				}
				h.xVols = append(h.xVols, extraVol{
					typ: data[o], flags: data[o+1],
					vol: binary.LittleEndian.Uint16(data[o+2:]),
				})
			}
		}
	}
	if xhLen >= 0x08 && xh+0x08 <= len(data) {
		if ct := relOfs(xh+4, le32(data, xh+4), len(data)); ct != 0 {
			n := int(data[ct])
			for i := range n {
				o := ct + 1 + i*5
				if o+5 > len(data) {
					break
				}
				h.xClocks = append(h.xClocks, extraClock{typ: data[o], clock: le32(data, o+1)})
			}
		}
	}
}

func (h *header) collectChips() {
	h.chips = h.chips[:0]
	for typ := range numChips {
		raw := h.u32(clockOffsets[typ])
		clock := raw & 0x3FFFFFFF
		if clock == 0 {
			continue
		}
		flag := raw&0x80000000 != 0
		dual := raw&0x40000000 != 0
		// T6W28 is one chip made of two SN76489 halves.
		if typ == chipSN76489 && flag {
			dual = true
		}
		h.chips = append(h.chips, chipSpec{typ: uint8(typ), clock: clock, flag: flag, paired: dual})
		if dual {
			c2 := clock
			for _, xc := range h.xClocks {
				if int(xc.typ) == typ {
					c2 = xc.clock & 0x3FFFFFFF
				}
			}
			h.chips = append(h.chips, chipSpec{typ: uint8(typ), num: 1, clock: c2, flag: flag, paired: true})
		}
	}
}

// moveOldClock handles files before 1.10, which kept the YM2612 and YM2151
// clocks in the YM2413 field. The first FM command decides the chip.
func (h *header) moveOldClock(firstFM byte) {
	if h.version >= 0x110 || h.u32(0x10) == 0 {
		return
	}
	var dst int
	switch firstFM {
	case 0x52, 0x53:
		dst = 0x2C
	case 0x54:
		dst = 0x30
	default:
		return
	}
	binary.LittleEndian.PutUint32(h.raw[dst:], h.u32(0x10))
	binary.LittleEndian.PutUint32(h.raw[0x10:], 0)
	h.collectChips()
}

// loopCount is the number of times the loop body plays, after the header's
// loop modifier and loop base.
func (h *header) loopCount(loops int) int {
	m := loops
	if h.loopMod != 0 {
		m = (loops*int(h.loopMod) + 8) / 16
	}
	if m <= int(h.loopBase) {
		return 1
	}
	return m - int(h.loopBase)
}
