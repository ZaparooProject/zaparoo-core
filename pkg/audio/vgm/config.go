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

// #include "engine.h"
import "C"

// chipCfg is one chip to start, with its engine settings.
type chipCfg struct {
	c      C.zvgm_chip_cfg
	typ    int
	num    int
	hasSSG bool
}

const maxClock = 0x3FFFFFFF

// chipConfigs turns the header's chip list into engine settings: clocks
// with their per-chip quirks, chip options and balanced volumes.
func chipConfigs(h *header, opts Options) []chipCfg {
	nYM2612 := 0
	for _, s := range h.chips {
		if s.typ == chipYM2612 {
			nYM2612++
		}
	}
	cfgs := make([]chipCfg, 0, len(h.chips))
	for _, s := range h.chips {
		if omittedChips[s.typ] {
			continue
		}
		clock, ok := fixClock(h, s)
		if !ok {
			continue
		}
		cc := chipCfg{typ: int(s.typ), num: int(s.num)}
		c := &cc.c
		c._type = C.uint8_t(s.typ)
		c.clock = C.uint32_t(clock)
		if s.flag {
			c.flags = 1
		}
		switch s.typ {
		case chipSN76489:
			taps := h.u16(0x28)
			if taps == 0 {
				taps = 9
			}
			width := h.u8(0x2A)
			if width == 0 {
				width = 16
			}
			c.p[0] = C.uint8_t(taps)
			c.p[1] = C.uint8_t(taps >> 8)
			c.p[2] = C.uint8_t(width)
			c.p[3] = C.uint8_t(h.u8(0x2B))
			if s.flag && s.num == 1 {
				c.p[4] = 1 // T6W28 noise half
			}
		case chipYM2612:
			if opts.AccurateYM2612 {
				c.accurate = 1
			}
			if h.version <= 0x150 && nYM2612 == 1 {
				c.p[0] = 1
			}
		case chipYM2203:
			c.p[0] = C.uint8_t(h.u8(0x7A))
			cc.hasSSG = true
		case chipYM2608:
			c.p[0] = C.uint8_t(h.u8(0x7B))
			cc.hasSSG = true
		case chipYM2610:
			cc.hasSSG = true
		case chipSegaPCM:
			c.p[0] = C.uint8_t(h.u8(0x3C))
			c.p[1] = C.uint8_t(h.u8(0x3E))
		case chipAY8910:
			c.p[0] = C.uint8_t(h.u8(0x78))
			c.p[1] = C.uint8_t(h.u8(0x79))
		case chipOKIM6258:
			c.p[0] = C.uint8_t(h.u8(0x94))
		case chipK054539:
			c.flags = C.uint32_t(h.u8(0x95))
		case chipC140:
			c.p[0] = C.uint8_t(h.u8(0x96))
			c.flags = C.uint32_t(h.u8(0x96))
		case chipES5503:
			c.flags = C.uint32_t(h.u8(0xD4))
		}
		cfgs = append(cfgs, cc)
	}
	setVolumes(h, cfgs)
	return cfgs
}

// fixClock applies the header clock conventions some chips use.
func fixClock(h *header, s chipSpec) (uint32, bool) {
	clk := uint64(s.clock)
	switch s.typ {
	case chipMultiPCM:
		clk = clk * 224 / 180
	case chipK054539:
		if clk < 1000000 {
			clk *= 384
		}
	case chipC140:
		if h.u8(0x96) == 2 {
			if clk == 44100 {
				clk = 25056500
			}
		} else if clk == 21390 {
			clk = 12288000
		}
		if clk < 1000000 {
			clk *= 576
		}
	case chipC352:
		if div := uint64(h.u8(0xD6)); div != 0 {
			clk = clk * 72 / div
		}
	case chipQSound:
		if clk < 5000000 {
			clk *= 15
		}
	case chipSCSP:
		if clk < 1000000 {
			clk *= 512
		}
	}
	if clk == 0 || clk > maxClock {
		return 0, false
	}
	return uint32(clk), true
}

// setVolumes sets each chip's mix volume the way VGMPlay balances chips,
// then scales them all so the mix sits at a sensible level.
func setVolumes(h *header, cfgs []chipCfg) {
	for i := range cfgs {
		cc := &cfgs[i]
		paired := false
		for _, s := range h.chips {
			if int(s.typ) == cc.typ && s.paired {
				paired = true
			}
		}
		// T6W28 is one chip made of two halves, not a pair.
		if cc.typ == chipSN76489 && h.u32(0x0C)&0x80000000 != 0 {
			paired = false
		}
		vol := chipVolume(h, cc.typ, cc.num, false, paired)
		volL, volR := vol, vol
		if cc.typ == chipYM3812 && paired && h.u32(0x50)&0x80000000 != 0 {
			// Two YM3812s flagged for stereo: one per side.
			if cc.num == 0 {
				volL, volR = vol*2, 0
			} else {
				volL, volR = 0, vol*2
			}
		}
		cc.c.volL, cc.c.volR = C.int32_t(volL), C.int32_t(volR)
		if cc.hasSSG {
			sv := chipVolume(h, cc.typ, cc.num, true, paired)
			cc.c.ssgVolL, cc.c.ssgVolR = C.int32_t(sv), C.int32_t(sv)
		}
	}

	var sum int64
	for i := range cfgs {
		cc := &cfgs[i]
		sum += (int64(cc.c.volL) + int64(cc.c.volR)) * int64(mixAmount[cc.typ]) / 2 >> 8
		if cc.hasSSG {
			sum += (int64(cc.c.ssgVolL) + int64(cc.c.ssgVolR)) * int64(mixAmount[chipAY8910]) / 2 >> 8
		}
	}
	shift := 0
	for s := sum; s > 0 && s <= 0x180 && shift < 16; s <<= 1 {
		shift++
	}
	for s := sum; s > 0x300 && shift > -16; s >>= 1 {
		shift--
	}
	for i := range cfgs {
		c := &cfgs[i].c
		c.volL = scaleVol(c.volL, shift)
		c.volR = scaleVol(c.volR, shift)
		c.ssgVolL = scaleVol(c.ssgVolL, shift)
		c.ssgVolR = scaleVol(c.ssgVolR, shift)
	}
}

func scaleVol(v C.int32_t, shift int) C.int32_t {
	if shift >= 0 {
		return v << shift
	}
	return v >> -shift
}

// chipVolume is one chip's volume in 8.8 fixed point.
func chipVolume(h *header, typ, num int, ssg, paired bool) int32 {
	vol := baseVolume[typ]
	if ssg && typ == chipYM2203 {
		vol /= 2
	}
	if paired {
		vol /= 2
	}
	want := uint8(typ)
	if ssg {
		want |= 0x80
	}
	for _, xv := range h.xVols {
		if xv.typ != want || int(xv.flags&1) != num {
			continue
		}
		if xv.vol&0x8000 != 0 {
			vol = (vol*int32(xv.vol&0x7FFF) + 0x80) >> 8
		} else {
			vol = int32(xv.vol)
		}
	}
	switch typ {
	case chipK051649:
		vol = vol * 8 / 5
	case chipC140:
		vol = (vol*2 + 1) / 3
	}
	return vol
}
