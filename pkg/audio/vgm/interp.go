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
	"math"
	"math/bits"
)

// Engine write kinds, mirrored from engine.h.
const (
	wA8D8   uint8 = 0
	wA16D8  uint8 = 1
	wA8D16  uint8 = 2
	wA16D16 uint8 = 3
	wStereo uint8 = 4
)

// Engine memory regions, mirrored from engine.h.
const (
	memROM    = 0
	memDeltaT = 1
	memRAM    = 2
)

const (
	maxBankBytes   = 64 << 20 // all stream data banks together
	maxRegionROM   = 16 << 20 // one chip's ROM image
	maxTotalROM    = 96 << 20 // every ROM image together
	ramSizeAny     = 0xFFFFFFFF
	numBanks       = 0x40
	numStreams     = 0x100
	maxStreamBurst = 0x20 // items due at once before a stream skips ahead
)

// pcmBlock is one data block's place in its bank.
type pcmBlock struct {
	ofs, size uint32
}

// pcmBank is a stream data bank: data blocks of one type, appended.
type pcmBank struct {
	data   []byte
	blocks []pcmBlock
}

// playState is the interpreter's position in the command stream.
type playState struct {
	tables   decompTables
	romSizes map[int]uint32
	active   []*dacStream
	streams  [numStreams]*dacStream
	banks    [numBanks]pcmBank

	pos       int
	tick      int64 // VGM ticks elapsed, counting loop passes
	rendered  int64 // output frames rendered
	highWater int   // data blocks before this offset were already loaded
	bankBytes int
	pcmPos    uint32
	rfBank    [2]uint16 // RF5C68, RF5C164 wave bank
	hucChan   [2]uint8
	ended     bool
}

func newPlayState(h *header) *playState {
	return &playState{
		pos:      h.dataOfs,
		tables:   decompTables{},
		romSizes: map[int]uint32{},
	}
}

// cmdInfo returns the length of the command at pos and the ticks it waits.
// ok is false for an unknown command or one cut off by the end of b.
func cmdInfo(b []byte, pos int, version uint32) (n int, wait int64, ok bool) {
	c := b[pos]
	switch {
	case c == 0x00:
		n = 1
	case c >= 0x30 && c <= 0x3F:
		n = 2
	case c >= 0x40 && c <= 0x4E:
		n = 3
		if version < 0x160 {
			n = 2
		}
	case c == 0x4F || c == 0x50:
		n = 2
	case c >= 0x51 && c <= 0x5F:
		n = 3
	case c == 0x61:
		n = 3
		if pos+3 <= len(b) {
			wait = int64(binary.LittleEndian.Uint16(b[pos+1:]))
		}
	case c == 0x62:
		n, wait = 1, 735
	case c == 0x63:
		n, wait = 1, 882
	case c == 0x67:
		if pos+7 > len(b) {
			return 0, 0, false
		}
		size := int64(binary.LittleEndian.Uint32(b[pos+3:]) & 0x7FFFFFFF)
		if int64(pos)+7+size > int64(len(b)) {
			return 0, 0, false
		}
		n = 7 + int(size)
	case c == 0x68:
		n = 12
	case c >= 0x70 && c <= 0x7F:
		n, wait = 1, int64(c&0x0F)+1
	case c >= 0x80 && c <= 0x8F:
		n, wait = 1, int64(c&0x0F)
	case c == 0x90 || c == 0x91 || c == 0x95:
		n = 5
	case c == 0x92:
		n = 6
	case c == 0x93:
		n = 11
	case c == 0x94:
		n = 2
	case c >= 0xA0 && c <= 0xBF:
		n = 3
	case c >= 0xC0 && c <= 0xDF:
		n = 4
	case c >= 0xE0:
		n = 5
	default:
		return 0, 0, false
	}
	if pos+n > len(b) {
		return 0, 0, false
	}
	return n, wait, true
}

// prescan walks the command stream once to find its true length, where
// it ends and the loop position, and settles the pre-1.10 FM clock.
func prescan(h *header, data []byte) error {
	b := data[:h.dataEnd]
	pos := h.dataOfs
	var ticks int64
	loopTick := int64(-1)
	var firstFM byte
	for pos < len(b) {
		if pos == h.loopOfs {
			loopTick = ticks
		}
		c := b[pos]
		if c == 0x66 {
			break
		}
		n, wait, ok := cmdInfo(b, pos, h.version)
		if !ok {
			break
		}
		if firstFM == 0 && c >= 0x51 && c <= 0x54 {
			firstFM = c
		}
		if ticks+wait > maxTotalTicks {
			break
		}
		ticks += wait
		pos += n
	}
	h.cmdEnd = pos
	h.totalTicks = ticks
	if h.loopOfs != 0 && loopTick >= 0 && ticks > loopTick {
		h.loopTicks = ticks - loopTick
	} else {
		h.loopOfs = 0
		h.loopTicks = 0
	}
	if ticks == 0 {
		return errNoData
	}
	h.moveOldClock(firstFM)
	for _, s := range h.chips {
		if !omittedChips[s.typ] {
			return nil
		}
	}
	return errNoChips
}

// cmdFrame is the output frame at which the next command takes effect.
func (d *Decoder) cmdFrame() int64 {
	return d.frameOf(d.st.tick)
}

// advance runs commands and streams that take effect before frame end,
// queueing their writes. A command and a stream item due on the same frame
// apply in that order.
func (d *Decoder) advance(end int64) {
	st := d.st
	for !st.ended {
		f := d.cmdFrame()
		if f >= end {
			break
		}
		d.runStreams(f)
		d.step(f)
	}
	d.runStreams(end)
}

// step executes one command at frame f.
func (d *Decoder) step(f int64) {
	st := d.st
	h := d.hdr
	b := d.data[:h.cmdEnd]
	if st.pos >= len(b) || b[st.pos] == 0x66 {
		if h.loopOfs == 0 {
			st.ended = true
			return
		}
		st.pos = h.loopOfs
		return
	}
	n, wait, ok := cmdInfo(b, st.pos, h.version)
	if !ok {
		// prescan stopped cmdEnd before any such command.
		st.ended = true
		return
	}
	cmd := b[st.pos : st.pos+n]
	fresh := st.pos >= st.highWater
	d.exec(cmd, f, fresh)
	st.pos += n
	st.highWater = max(st.highWater, st.pos)
	st.tick += wait
}

// chipIdx returns the engine index of chip typ number num, or -1. typ can
// come straight from the file (DAC stream setup), so it is range-checked.
func (d *Decoder) chipIdx(typ int, num byte) int {
	if typ < 0 || typ >= numChips {
		return -1
	}
	return d.handle[typ][num&1]
}

// w queues a write to chip typ, number num.
func (d *Decoder) w(typ int, num byte, kind uint8, addr, data uint16, f int64) {
	if idx := d.chipIdx(typ, num); idx >= 0 {
		d.queue(idx, kind, addr, data, f)
	}
}

// sendYM queues an address write then a data write on an OPN-style port.
func (d *Decoder) sendYM(typ int, num, port, reg, data byte, f int64) {
	idx := d.chipIdx(typ, num)
	if idx < 0 {
		return
	}
	d.queue(idx, wA8D8, uint16(port)*2, uint16(reg), f)
	d.queue(idx, wA8D8, uint16(port)*2+1, uint16(data), f)
}

// ymCmd maps the 0x51-0x5F commands to their chip.
var ymCmd = [16]int{
	0x1: chipYM2413, 0x2: chipYM2612, 0x3: chipYM2612, 0x4: chipYM2151,
	0x5: chipYM2203, 0x6: chipYM2608, 0x7: chipYM2608, 0x8: chipYM2610,
	0x9: chipYM2610, 0xA: chipYM3812, 0xB: chipYM3526, 0xC: chipY8950,
	0xD: chipYMZ280B, 0xE: chipYMF262, 0xF: chipYMF262,
}

// regCmd maps 0xB0-0xBF commands that write one register to their chip;
// -1 marks commands with their own handling or an omitted chip.
var regCmd = [16]int{
	-1, -1, -1, chipGB, -1, chipMultiPCM, chipUPD7759, chipOKIM6258,
	-1, -1, chipK053260, chipPokey, -1, -1, -1, chipGA20,
}

// memCmd maps 0xC5-0xC8 and 0xD3-0xD4 to chips with a 16-bit address.
func memCmd(c byte) int {
	switch c {
	case 0xC5:
		return chipSCSP
	case 0xC7:
		return chipVSU
	case 0xC8:
		return chipX1010
	case 0xD3:
		return chipK054539
	case 0xD4:
		return chipC140
	}
	return -1
}

// exec applies one command. fresh is false on loop passes over commands
// that were already run once, so data blocks are not loaded twice.
//
//nolint:gocyclo,cyclop,funlen // one switch over the VGM command set
func (d *Decoder) exec(cmd []byte, f int64, fresh bool) {
	c := cmd[0]
	switch {
	case c == 0x30:
		d.w(chipSN76489, 1, wA8D8, 0, uint16(cmd[1]), f)
	case c == 0x3F:
		d.w(chipSN76489, 1, wA8D8, 1, uint16(cmd[1]), f)
	case c == 0x4F:
		d.w(chipSN76489, 0, wA8D8, 1, uint16(cmd[1]), f)
	case c == 0x50:
		d.w(chipSN76489, 0, wA8D8, 0, uint16(cmd[1]), f)
	case c == 0x31:
		v := cmd[1]
		num := v >> 7
		if v&0x40 != 0 {
			if idx := d.ssg[chipYM2203][num]; idx >= 0 {
				d.queue(idx, wStereo, 0, uint16(v&0x3F), f)
			}
			return
		}
		d.w(chipAY8910, num, wStereo, 0, uint16(v&0x3F), f)
	case c == 0x40 && len(cmd) == 3:
		d.w(chipMikey, cmd[1]>>7, wA8D8, uint16(cmd[1]&0x7F), uint16(cmd[2]), f)
	case c >= 0x51 && c <= 0x5F:
		d.ymWrite(ymCmd[c&0x0F], 0, c, cmd[1], cmd[2], f)
	case c >= 0xA1 && c <= 0xAF:
		d.ymWrite(ymCmd[c&0x0F], 1, c, cmd[1], cmd[2], f)
	case c == 0xA0:
		d.sendYM(chipAY8910, cmd[1]>>7, 0, cmd[1]&0x7F, cmd[2], f)
	case c == 0x67:
		if fresh {
			d.dataBlock(cmd, f)
		}
	case c == 0x68:
		d.pcmRAMWrite(cmd, f)
	case c >= 0x80 && c <= 0x8F:
		bank := d.st.banks[0].data
		if int64(d.st.pcmPos) < int64(len(bank)) {
			d.sendYM(chipYM2612, 0, 0, 0x2A, bank[d.st.pcmPos], f)
		}
		d.st.pcmPos++
	case c == 0xE0:
		d.st.pcmPos = binary.LittleEndian.Uint32(cmd[1:])
	case c >= 0x90 && c <= 0x95:
		d.streamCmd(cmd, f)
	case c == 0xB0 || c == 0xB1:
		typ, bi := chipRF5C68, 0
		if c == 0xB1 {
			typ, bi = chipRF5C164, 1
		}
		reg := cmd[1] & 0x7F
		if reg == 7 && cmd[2]&0x40 == 0 {
			d.st.rfBank[bi] = uint16(cmd[2] & 0x0F)
		}
		d.w(typ, cmd[1]>>7, wA8D8, uint16(reg), uint16(cmd[2]), f)
	case c == 0xB2:
		d.w(chipPWM, cmd[1]>>7, wA8D16, uint16(cmd[1]>>4&7),
			uint16(cmd[1]&0x0F)<<8|uint16(cmd[2]), f)
	case c == 0xB4:
		// Offsets from 0x20 belong to the FDS, which is not emulated.
		if reg := cmd[1] & 0x7F; reg < 0x20 {
			d.w(chipNES, cmd[1]>>7, wA8D8, uint16(reg), uint16(cmd[2]), f)
		}
	case c == 0xB8:
		reg, v := cmd[1]&0x7F, cmd[2]
		if reg == 0x0B {
			v &= 0x7F
		}
		d.w(chipOKIM6295, cmd[1]>>7, wA8D8, uint16(reg), uint16(v), f)
	case c == 0xB9:
		reg := cmd[1] & 0x7F
		if reg == 0 {
			d.st.hucChan[cmd[1]>>7] = cmd[2]
		}
		d.w(chipHuC6280, cmd[1]>>7, wA8D8, uint16(reg), uint16(cmd[2]), f)
	case c == 0xBD:
		num := cmd[1] >> 7
		d.w(chipSAA1099, num, wA8D8, 1, uint16(cmd[1]&0x7F), f)
		d.w(chipSAA1099, num, wA8D8, 0, uint16(cmd[2]), f)
	case c >= 0xB0 && c <= 0xBF:
		if typ := regCmd[c&0x0F]; typ >= 0 {
			d.w(typ, cmd[1]>>7, wA8D8, uint16(cmd[1]&0x7F), uint16(cmd[2]), f)
		}
	case c == 0xC0:
		v := binary.LittleEndian.Uint16(cmd[1:])
		d.w(chipSegaPCM, byte(v>>15), wA16D8, v&0x7FFF, uint16(cmd[3]), f)
	case c == 0xC1:
		d.w(chipRF5C68, 0, wA16D8, binary.LittleEndian.Uint16(cmd[1:]), uint16(cmd[3]), f)
	case c == 0xC2:
		d.w(chipRF5C164, 0, wA16D8, binary.LittleEndian.Uint16(cmd[1:]), uint16(cmd[3]), f)
	case c == 0xC3:
		d.multiPCMBank(cmd, f)
	case c == 0xC4:
		idx := d.chipIdx(chipQSound, 0)
		if idx >= 0 {
			d.queue(idx, wA8D8, 0, uint16(cmd[1]), f)
			d.queue(idx, wA8D8, 1, uint16(cmd[2]), f)
			d.queue(idx, wA8D8, 2, uint16(cmd[3]), f)
		}
	case c == 0xD1 || c == 0xD2:
		typ := chipYMF271
		if c == 0xD2 {
			typ = chipK051649
		}
		d.sendYM(typ, cmd[1]>>7, cmd[1]&0x7F, cmd[2], cmd[3], f)
	case c == 0xD5:
		d.w(chipES5503, cmd[1]>>7, wA8D8, uint16(cmd[2]), uint16(cmd[3]), f)
	case c >= 0xC5 && c <= 0xD4:
		if typ := memCmd(c); typ >= 0 {
			v := binary.BigEndian.Uint16(cmd[1:])
			d.w(typ, byte(v>>15), wA16D8, v&0x7FFF, uint16(cmd[3]), f)
		}
	case c == 0xE1:
		v := binary.BigEndian.Uint16(cmd[1:])
		d.w(chipC352, byte(v>>15), wA16D16, v&0x7FFF, binary.BigEndian.Uint16(cmd[3:]), f)
	}
	// Everything else is a wait, a reserved command or an omitted chip.
}

// ymWrite handles the 0x5n and 0xAn FM register writes.
func (d *Decoder) ymWrite(typ int, num, c, reg, data byte, f int64) {
	var port byte
	switch typ {
	case chipYM2612, chipYM2608, chipYM2610, chipYMF262:
		port = c & 1
	}
	// YM2608 rhythm samples come from a ROM that is not included; keep the
	// rhythm part silent rather than playing garbage.
	if typ == chipYM2608 && port == 0 && reg == 0x10 {
		return
	}
	d.sendYM(typ, num, port, reg, data, f)
}

// multiPCMBank handles 0xC3, the MultiPCM sample bank select.
func (d *Decoder) multiPCMBank(cmd []byte, f int64) {
	num := cmd[1] >> 7
	mask := cmd[1] & 3
	b := cmd[2]
	if mask == 3 && b&8 == 0 {
		d.w(chipMultiPCM, num, wA8D8, 0x10, uint16(b>>4), f)
		return
	}
	if mask&2 != 0 {
		d.w(chipMultiPCM, num, wA8D8, 0x11, uint16(b>>3), f)
	}
	if mask&1 != 0 {
		d.w(chipMultiPCM, num, wA8D8, 0x12, uint16(b>>3), f)
	}
}

// romTarget maps a ROM data block type to its chip and memory region.
func romTarget(t byte) (typ, region int) {
	switch t {
	case 0x80:
		return chipSegaPCM, memROM
	case 0x81:
		return chipYM2608, memDeltaT
	case 0x82:
		return chipYM2610, memROM
	case 0x83:
		return chipYM2610, memDeltaT
	case 0x85:
		return chipYMF271, memROM
	case 0x86:
		return chipYMZ280B, memROM
	case 0x88:
		return chipY8950, memDeltaT
	case 0x89:
		return chipMultiPCM, memROM
	case 0x8A:
		return chipUPD7759, memROM
	case 0x8B:
		return chipOKIM6295, memROM
	case 0x8C:
		return chipK054539, memROM
	case 0x8D:
		return chipC140, memROM
	case 0x8E:
		return chipK053260, memROM
	case 0x8F:
		return chipQSound, memROM
	case 0x91:
		return chipX1010, memROM
	case 0x92:
		return chipC352, memROM
	case 0x93:
		return chipGA20, memROM
	}
	return -1, 0
}

// ramTarget maps a RAM data block type, or a 0x68 bank type, to its chip.
func ramTarget(t byte) int {
	switch t {
	case 0xC0, 0x01:
		return chipRF5C68
	case 0xC1, 0x02:
		return chipRF5C164
	case 0xC2, 0x07:
		return chipNES
	case 0xE0, 0x06:
		return chipSCSP
	case 0xE1:
		return chipES5503
	}
	return -1
}

// rfBankOfs is the RF5C68/RF5C164 wave bank base for RAM loads.
func (d *Decoder) rfBankOfs(typ int) uint32 {
	switch typ {
	case chipRF5C68:
		return uint32(d.st.rfBank[0]) << 12
	case chipRF5C164:
		return uint32(d.st.rfBank[1]) << 12
	}
	return 0
}

// dataBlock handles 0x67.
func (d *Decoder) dataBlock(cmd []byte, f int64) {
	st := d.st
	t := cmd[2]
	num := cmd[6] >> 7
	p := cmd[7:]
	switch {
	case t < 0x40:
		d.addBank(t, p)
	case t < 0x7F:
		out, err := st.tables.decompress(p, maxBankBytes-st.bankBytes)
		if err == nil {
			d.addBank(t&0x3F, out)
		}
	case t == 0x7F:
		st.tables.addTable(p)
	case t < 0xC0:
		typ, region := romTarget(t)
		if typ < 0 || len(p) < 8 {
			return
		}
		d.loadROM(typ, num, region, binary.LittleEndian.Uint32(p), binary.LittleEndian.Uint32(p[4:]), p[8:], f)
	case t < 0xE0:
		typ := ramTarget(t)
		if typ < 0 || len(p) < 2 {
			return
		}
		start := uint32(binary.LittleEndian.Uint16(p)) | d.rfBankOfs(typ)
		d.loadRAM(typ, num, start, p[2:], f)
	default:
		typ := ramTarget(t)
		if typ < 0 || len(p) < 4 {
			return
		}
		d.loadRAM(typ, num, binary.LittleEndian.Uint32(p), p[4:], f)
	}
}

func (d *Decoder) addBank(t byte, p []byte) {
	st := d.st
	if len(p) == 0 || len(p) > maxBankBytes-st.bankBytes {
		return
	}
	bk := &st.banks[t]
	// Both fit: bank data is capped at maxBankBytes.
	blk := pcmBlock{ofs: uint32(len(bk.data)), size: uint32(len(p))} //nolint:gosec // see above
	bk.blocks = append(bk.blocks, blk)
	bk.data = append(bk.data, p...)
	st.bankBytes += len(p)
}

func (d *Decoder) loadROM(typ int, num byte, region int, romSize, start uint32, p []byte, f int64) {
	idx := d.chipIdx(typ, num)
	if idx < 0 || romSize == 0 {
		return
	}
	romSize = min(romSize, maxRegionROM)
	key := idx<<2 | region
	total := 0
	for k, v := range d.st.romSizes {
		if k != key {
			total += int(v)
		}
	}
	if total+int(romSize) > maxTotalROM {
		return
	}
	d.st.romSizes[key] = romSize
	if start >= romSize {
		return
	}
	p = p[:min(uint64(len(p)), uint64(romSize-start))]
	d.memWrite(idx, region, romSize, start, p, f)
}

func (d *Decoder) loadRAM(typ int, num byte, start uint32, p []byte, f int64) {
	if idx := d.chipIdx(typ, num); idx >= 0 {
		d.memWrite(idx, memRAM, ramSizeAny, start, p, f)
	}
}

// pcmRAMWrite handles 0x68, a copy from a data bank into chip RAM.
func (d *Decoder) pcmRAMWrite(cmd []byte, f int64) {
	t := cmd[2] & 0x7F
	num := cmd[2] >> 7
	le24 := func(o int) uint32 { return uint32(cmd[o]) | uint32(cmd[o+1])<<8 | uint32(cmd[o+2])<<16 }
	src, dst, n := le24(3), le24(6), le24(9)
	if n == 0 {
		n = 0x1000000
	}
	typ := ramTarget(t)
	if typ < 0 || t >= numBanks {
		return
	}
	bank := d.st.banks[t].data
	if uint64(src)+uint64(n) > uint64(len(bank)) {
		return
	}
	d.loadRAM(typ, num, dst|d.rfBankOfs(typ), bank[src:src+n], f)
}

// dacStream is one DAC stream from the 0x90-0x95 commands: it feeds
// bytes from a data bank to a chip register at a fixed frequency.
type dacStream struct {
	typ      int
	num      byte
	port     byte
	reg      byte
	bank     byte
	stepSize uint32
	stepBase uint32
	freq     uint32

	running  bool
	start    uint32 // byte offset of the first item
	step     uint32 // bytes between items
	cmdSize  uint32
	count    uint32 // items per pass
	reverse  bool
	loop     bool
	item     uint64 // items emitted since the stream started
	baseItem uint64
	baseF    int64 // frame the timing is measured from
}

func (d *Decoder) streamAt(id byte) *dacStream {
	s := d.st.streams[id]
	if s == nil {
		s = &dacStream{stepSize: 1}
		d.st.streams[id] = s
	}
	return s
}

func (d *Decoder) streamCmd(cmd []byte, f int64) {
	st := d.st
	id := cmd[1]
	switch cmd[0] {
	case 0x90:
		if id == 0xFF {
			return
		}
		s := d.streamAt(id)
		s.typ = int(cmd[2] & 0x7F)
		s.num = cmd[2] >> 7
		s.port, s.reg = cmd[3], cmd[4]
		s.running = false
	case 0x91:
		if id == 0xFF {
			return
		}
		s := d.streamAt(id)
		s.bank = cmd[2] & 0x3F
		s.stepSize = uint32(cmd[3])
		if s.stepSize == 0 {
			s.stepSize = 1
		}
		s.stepBase = uint32(cmd[4])
	case 0x92:
		if id == 0xFF {
			return
		}
		s := d.streamAt(id)
		s.freq = binary.LittleEndian.Uint32(cmd[2:])
		s.rebase(f)
	case 0x93:
		if id == 0xFF {
			return
		}
		s := d.streamAt(id)
		start := binary.LittleEndian.Uint32(cmd[2:])
		mode := cmd[6]
		length := binary.LittleEndian.Uint32(cmd[7:])
		d.startStream(s, start, mode, length, f)
	case 0x94:
		if id == 0xFF {
			for _, s := range &st.streams {
				if s != nil {
					s.running = false
				}
			}
			return
		}
		if s := st.streams[id]; s != nil {
			s.running = false
		}
	case 0x95:
		if id == 0xFF {
			return
		}
		s := d.streamAt(id)
		blk := int(binary.LittleEndian.Uint16(cmd[2:]))
		flags := cmd[4]
		blocks := st.banks[s.bank].blocks
		if blk >= len(blocks) {
			return
		}
		mode := byte(0x0F) | flags&0x10
		if flags&1 != 0 {
			mode |= 0x80
		}
		d.startStream(s, blocks[blk].ofs, mode, blocks[blk].size, f)
	}
	d.refreshActive()
}

func (s *dacStream) rebase(f int64) {
	s.baseItem = s.item
	s.baseF = f
}

// itemSize is the bytes one stream item takes for its target chip.
func itemSize(typ int, reg byte) uint32 {
	switch typ {
	case chipPWM, chipQSound:
		return 2
	case chipSN76489:
		if reg&0x10 == 0 {
			return 2
		}
	}
	return 1
}

func (d *Decoder) startStream(s *dacStream, start uint32, mode byte, length uint32, f int64) {
	bankLen := uint64(len(d.st.banks[s.bank].data))
	s.cmdSize = itemSize(s.typ, s.reg)
	s.step = s.cmdSize * s.stepSize
	if start != 0xFFFFFFFF {
		s.start = start
	}
	first := min(uint64(s.start)+uint64(s.cmdSize)*uint64(s.stepBase), bankLen)
	var count uint64
	switch mode & 0x0F {
	case 0:
		count = uint64(s.count)
	case 1:
		count = uint64(length)
	case 2:
		count = uint64(length) * uint64(s.freq) / 1000
	case 3:
		count = (bankLen - first) / uint64(s.step)
	case 0x0F:
		count = uint64(length) / uint64(s.step)
	}
	s.count = uint32(min(count, math.MaxUint32))
	s.start = uint32(first) //nolint:gosec // first <= bank length <= maxBankBytes
	s.reverse = mode&0x10 != 0
	s.loop = mode&0x80 != 0
	s.item = 0
	s.rebase(f)
	s.running = s.count > 0
}

func (d *Decoder) refreshActive() {
	st := d.st
	st.active = st.active[:0]
	for _, s := range &st.streams {
		if s != nil && s.running && s.freq != 0 {
			st.active = append(st.active, s)
		}
	}
}

// itemFrame is the frame item i of stream s plays on.
func (d *Decoder) itemFrame(s *dacStream, i uint64) int64 {
	hi, lo := bits.Mul64(i-s.baseItem, uint64(d.opts.SampleRate)) //nolint:gosec // rate is clamped positive
	if hi != 0 {
		return math.MaxInt64
	}
	q := lo / uint64(s.freq)
	if lo%uint64(s.freq) != 0 {
		q++
	}
	if q > math.MaxInt64/2 {
		return math.MaxInt64
	}
	return s.baseF + 1 + int64(q)
}

// lastItemBy is the last item of stream s due on or before frame f.
func (d *Decoder) lastItemBy(s *dacStream, f int64) uint64 {
	rel := f - s.baseF - 1
	if rel < 0 {
		return s.baseItem
	}
	hi, lo := bits.Mul64(uint64(rel), uint64(s.freq))
	rate := uint64(d.opts.SampleRate) //nolint:gosec // rate is clamped positive
	q, _ := bits.Div64(hi%rate, lo, rate)
	if hi >= rate {
		q = math.MaxUint64
	}
	return s.baseItem + q
}

// runStreams emits stream items due before frame end.
func (d *Decoder) runStreams(end int64) {
	st := d.st
	changed := false
	for _, s := range st.active {
		for s.running {
			f := d.itemFrame(s, s.item)
			if f >= end {
				break
			}
			f = max(f, st.rendered)
			n := d.lastItemBy(s, f) - s.item + 1
			if n > maxStreamBurst {
				s.item += n - maxStreamBurst/2
				n = maxStreamBurst / 2
			}
			for range n {
				if !d.streamItem(s, f) {
					changed = true
					break
				}
			}
		}
	}
	if changed {
		d.refreshActive()
	}
}

// streamItem writes the next item of s; false when the stream stopped.
func (d *Decoder) streamItem(s *dacStream, f int64) bool {
	i := s.item
	if i >= uint64(s.count) {
		if !s.loop {
			s.running = false
			return false
		}
		i %= uint64(s.count)
	}
	s.item++
	if s.reverse {
		i = uint64(s.count) - 1 - i
	}
	bank := d.st.banks[s.bank].data
	pos := uint64(s.start) + i*uint64(s.step)
	if pos+uint64(s.cmdSize) > uint64(len(bank)) {
		return true
	}
	b0 := bank[pos]
	var b1 byte
	if s.cmdSize > 1 {
		b1 = bank[pos+1]
	}
	d.streamWrite(s, b0, b1, f)
	return true
}

// streamWrite sends one stream item to its chip.
//
//nolint:gocyclo,cyclop // one case per chip family
func (d *Decoder) streamWrite(s *dacStream, b0, b1 byte, f int64) {
	idx := d.chipIdx(s.typ, s.num)
	if idx < 0 {
		return
	}
	w8 := func(a, v byte) { d.queue(idx, wA8D8, uint16(a), uint16(v), f) }
	switch s.typ {
	case chipPWM:
		d.queue(idx, wA8D16, uint16(s.reg&0x0F), uint16(b1&0x0F)<<8|uint16(b0), f)
	case chipSN76489:
		w8(0, s.reg&0xF0|b0&0x0F)
		if s.reg&0x10 == 0 {
			w8(0, (b1&3)<<4|b0>>4)
		}
	case chipOKIM6295:
		if s.reg != 0 {
			w8(s.reg, b0)
			return
		}
		ch := s.port & 0x0F
		if b0&0x80 != 0 {
			w8(0, b0)
			w8(0, ch<<4)
		} else {
			w8(0, ch<<3)
		}
	case chipYM2413, chipYM2151, chipYM2203, chipYM3812, chipYM3526, chipY8950,
		chipYMZ280B, chipAY8910:
		w8(0, s.reg)
		w8(1, b0)
	case chipGB, chipNES, chipUPD7759, chipOKIM6258, chipK053260, chipPokey,
		chipES5503, chipGA20, chipMikey:
		if s.typ == chipNES && s.reg >= 0x20 {
			return
		}
		w8(s.reg, b0)
	case chipYM2612, chipYM2608, chipYM2610, chipYMF262, chipYMF271, chipK051649:
		w8(s.port*2, s.reg)
		w8(s.port*2+1, b0)
	case chipK054539, chipC140, chipSCSP, chipVSU, chipX1010, chipC352:
		d.queue(idx, wA16D8, uint16(s.port)<<8|uint16(s.reg), uint16(b0), f)
	case chipRF5C68, chipRF5C164, chipHuC6280:
		if s.port == 0xFF {
			w8(s.reg&0x0F, b0)
			return
		}
		w8(s.reg>>4, s.port)
		w8(s.reg&0x0F, b0)
		if s.typ == chipHuC6280 {
			w8(0, d.st.hucChan[s.num&1])
		}
	case chipQSound:
		w8(0, b0)
		w8(1, b1)
		w8(2, s.reg)
	case chipSAA1099:
		w8(1, s.reg)
		w8(0, b0)
	}
}
