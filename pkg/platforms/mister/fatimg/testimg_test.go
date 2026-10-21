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

package fatimg

import (
	"encoding/binary"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// tNode is a file or directory for the test formatter.
type tNode struct {
	name    string // long name; empty means no LFN entries
	short   string // 8.3 name, NAME.EXT
	data    []byte
	kids    []*tNode
	nt      byte
	dir     bool
	deleted bool
	badSum  bool
}

type tFS struct {
	img                                 []byte
	typ                                 Type
	bps, spc, clusters, reserved, nfats int
	rootEnt, fatSz                      int
	fatOff, rootOff, dataOff            int
	next, rootClus                      uint32
}

func newTFS(typ Type, clusters, spc, rootEnt int) *tFS {
	t := &tFS{typ: typ, bps: 512, spc: spc, clusters: clusters, reserved: 1, nfats: 2, rootEnt: rootEnt, next: 2}
	var need int
	switch typ {
	case FAT12:
		need = ((clusters+2)*3 + 1) / 2
	case FAT16:
		need = (clusters + 2) * 2
	default:
		need = (clusters + 2) * 4
		t.reserved = 32
		t.rootEnt = 0
	}
	t.fatSz = (need + t.bps - 1) / t.bps
	rootSectors := (t.rootEnt*32 + t.bps - 1) / t.bps
	total := t.reserved + t.nfats*t.fatSz + rootSectors + clusters*spc
	t.img = make([]byte, total*t.bps)
	t.fatOff = t.reserved * t.bps
	t.rootOff = t.fatOff + t.nfats*t.fatSz*t.bps
	t.dataOff = t.rootOff + rootSectors*t.bps
	switch typ {
	case FAT12:
		t.setFAT(0, 0xFF8)
		t.setFAT(1, 0xFFF)
	case FAT16:
		t.setFAT(0, 0xFFF8)
		t.setFAT(1, 0xFFFF)
	default:
		t.setFAT(0, 0x0FFFFFF8)
		t.setFAT(1, 0x0FFFFFFF)
	}
	return t
}

func (t *tFS) eoc() uint32 {
	switch t.typ {
	case FAT12:
		return 0xFFF
	case FAT16:
		return 0xFFFF
	default:
		return 0x0FFFFFFF
	}
}

func (t *tFS) setFAT(c, v uint32) {
	for i := range t.nfats {
		base := t.fatOff + i*t.fatSz*t.bps
		switch t.typ {
		case FAT12:
			off := base + int(c)*3/2
			old := uint32(binary.LittleEndian.Uint16(t.img[off:]))
			if c&1 == 1 {
				old = old&0x000F | v<<4
			} else {
				old = old&0xF000 | v&0xFFF
			}
			binary.LittleEndian.PutUint16(t.img[off:], u16(int(old)))
		case FAT16:
			binary.LittleEndian.PutUint16(t.img[base+int(c)*2:], u16(int(v)))
		default:
			binary.LittleEndian.PutUint32(t.img[base+int(c)*4:], v)
		}
	}
}

func (t *tFS) clusterOff(c uint32) int { return t.dataOff + int(c-2)*t.spc*t.bps }

func (t *tFS) clBytes() int { return t.spc * t.bps }

func (t *tFS) alloc(n int) []uint32 {
	out := make([]uint32, n)
	for i := range out {
		if int(t.next) >= t.clusters+2 {
			panic("test image full")
		}
		out[i] = t.next
		t.next++
	}
	for i, c := range out {
		if i+1 < len(out) {
			t.setFAT(c, out[i+1])
		} else {
			t.setFAT(c, t.eoc())
		}
	}
	return out
}

func (t *tFS) clustersFor(n int) int {
	return max(1, (n+t.clBytes()-1)/t.clBytes())
}

func (t *tFS) writeChain(chain []uint32, data []byte) {
	for i, c := range chain {
		lo := i * t.clBytes()
		if lo >= len(data) {
			return
		}
		copy(t.img[t.clusterOff(c):t.clusterOff(c)+t.clBytes()], data[lo:min(lo+t.clBytes(), len(data))])
	}
}

func lfnCount(k *tNode) int {
	if k.name == "" {
		return 0
	}
	return (len(utf16.Encode([]rune(k.name))) + 12) / 13
}

func slotCount(kids []*tNode) int {
	n := 0
	for _, k := range kids {
		n += lfnCount(k) + 1
	}
	return n
}

func to83(s string) []byte {
	out := []byte("           ")
	base, ext, _ := strings.Cut(strings.ToUpper(s), ".")
	copy(out[0:8], base)
	copy(out[8:11], ext)
	return out
}

func (t *tFS) encodeKid(k *tNode, cluster, size uint32) []byte {
	short := to83(k.short)
	var out []byte
	if k.name != "" {
		units := utf16.Encode([]rune(k.name))
		n := lfnCount(k)
		padded := make([]uint16, n*13)
		for i := range padded {
			switch {
			case i < len(units):
				padded[i] = units[i]
			case i == len(units):
				padded[i] = 0
			default:
				padded[i] = 0xFFFF
			}
		}
		sum := lfnChecksum(short)
		if k.badSum {
			sum ^= 0xFF
		}
		for i := n; i >= 1; i-- {
			slot := make([]byte, 32)
			slot[0] = u8(i)
			if i == n {
				slot[0] |= lfnLast
			}
			slot[11] = attrLFN
			slot[13] = sum
			chars := padded[(i-1)*13 : i*13]
			for j := range 5 {
				binary.LittleEndian.PutUint16(slot[1+j*2:], chars[j])
			}
			for j := range 6 {
				binary.LittleEndian.PutUint16(slot[14+j*2:], chars[5+j])
			}
			for j := range 2 {
				binary.LittleEndian.PutUint16(slot[28+j*2:], chars[11+j])
			}
			out = append(out, slot...)
		}
	}
	slot := make([]byte, 32)
	copy(slot, short)
	slot[11] = 0x20
	if k.dir {
		slot[11] = attrDirectory
	}
	slot[12] = k.nt
	if t.typ == FAT32 {
		binary.LittleEndian.PutUint16(slot[20:], uint16(cluster>>16))
	}
	binary.LittleEndian.PutUint16(slot[26:], u16(int(cluster)))
	binary.LittleEndian.PutUint32(slot[28:], size)
	out = append(out, slot...)
	if k.deleted {
		for i := 0; i < len(out); i += 32 {
			out[i] = 0xE5
		}
	}
	return out
}

func dotEntries(self, parent uint32) []byte {
	out := make([]byte, 0, 64)
	for i, name := range []string{".", ".."} {
		slot := make([]byte, 32)
		copy(slot, "           ")
		copy(slot, name)
		slot[11] = attrDirectory
		c := self
		if i == 1 {
			c = parent
		}
		binary.LittleEndian.PutUint16(slot[26:], u16(int(c)))
		out = append(out, slot...)
	}
	return out
}

func (t *tFS) buildDir(kids []*tNode, self, parent uint32, root bool) []byte {
	var out []byte
	if !root {
		out = dotEntries(self, parent)
	}
	for _, k := range kids {
		var cl, size uint32
		switch {
		case k.dir:
			chain := t.alloc(t.clustersFor((slotCount(k.kids) + 2) * 32))
			cl = chain[0]
			own := cl
			par := self
			if root {
				par = 0
			}
			t.writeChain(chain, t.buildDir(k.kids, own, par, false))
		case len(k.data) > 0:
			chain := t.alloc(t.clustersFor(len(k.data)))
			cl = chain[0]
			size = u32(len(k.data))
			t.writeChain(chain, k.data)
		}
		out = append(out, t.encodeKid(k, cl, size)...)
	}
	return out
}

func (t *tFS) format(label string, kids []*tNode) []byte {
	var labelSlot []byte
	if label != "" {
		labelSlot = make([]byte, 32)
		copy(labelSlot, "           ")
		copy(labelSlot, label)
		labelSlot[11] = attrVolumeLabel
	}
	if t.typ == FAT32 {
		chain := t.alloc(t.clustersFor((slotCount(kids) + 1) * 32))
		t.rootClus = chain[0]
		t.writeChain(chain, slices.Concat(labelSlot, t.buildDir(kids, t.rootClus, 0, true)))
	} else {
		content := slices.Concat(labelSlot, t.buildDir(kids, 0, 0, true))
		if len(content) > t.rootEnt*32 {
			panic("root directory full")
		}
		copy(t.img[t.rootOff:], content)
	}
	t.writeBoot()
	return t.img
}

func (t *tFS) writeBoot() {
	b := t.img[:512]
	le := binary.LittleEndian
	copy(b, []byte{0xEB, 0x3C, 0x90})
	copy(b[3:], "TESTFMT ")
	le.PutUint16(b[11:], u16(t.bps))
	b[13] = u8(t.spc)
	le.PutUint16(b[14:], u16(t.reserved))
	b[16] = u8(t.nfats)
	le.PutUint16(b[17:], u16(t.rootEnt))
	total := len(t.img) / t.bps
	if total < 65536 {
		le.PutUint16(b[19:], u16(total))
	} else {
		le.PutUint32(b[32:], u32(total))
	}
	b[21] = 0xF8
	if t.typ == FAT32 {
		le.PutUint32(b[36:], u32(t.fatSz))
		le.PutUint32(b[44:], t.rootClus)
		copy(b[71:], "NO NAME    ")
		copy(b[82:], "FAT32   ")
	} else {
		le.PutUint16(b[22:], u16(t.fatSz))
		copy(b[43:], "NO NAME    ")
		copy(b[54:], "FAT16   ")
	}
	b[510], b[511] = 0x55, 0xAA
}

func pattern(n, seed int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = u8(i*7 + seed)
	}
	return out
}

func stdTree() []*tNode {
	big := make([]*tNode, 0, 40)
	for i := range 40 {
		name := "F" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ".TXT"
		big = append(big, &tNode{short: name, data: []byte(name)})
	}
	return []*tNode{
		{name: "Games", short: "GAMES", dir: true, kids: []*tNode{
			{name: "Super Mario Bros.nes", short: "SUPERM~1.NES", data: pattern(1500, 1)},
			{short: "zelda.nes", nt: 0x18, data: pattern(700, 2)},
			{name: "Old Game.nes", short: "OLDGAM~1.NES", data: pattern(100, 3), deleted: true},
			{name: "Bad Sum.nes", short: "BADSUM~1.NES", data: pattern(10, 4), badSum: true},
			{name: "ünï\U0001F600.bin", short: "N~1.BIN", data: pattern(20, 5)},
		}},
		{short: "BIG", dir: true, kids: big},
		{name: "ReadMe.txt", short: "README.TXT", data: []byte("hello fat")},
		{short: "MULTI.BIN", data: pattern(3000, 9)},
		{short: "EMPTY.DAT"},
	}
}

type variant struct {
	name     string
	typ      Type
	clusters int
	spc      int
	rootEnt  int
}

var variants = []variant{
	{"fat12", FAT12, 300, 2, 512},
	{"fat16", FAT16, 4200, 1, 512},
	{"fat32", FAT32, 66000, 1, 0},
}

var (
	stdMu    syncutil.Mutex
	stdCache = map[string][]byte{}
)

// stdImage returns a shared standard image; callers must not modify it.
func stdImage(v variant) []byte {
	stdMu.Lock()
	defer stdMu.Unlock()
	if img, ok := stdCache[v.name]; ok {
		return img
	}
	t := newTFS(v.typ, v.clusters, v.spc, v.rootEnt)
	img := t.format("TESTVOL", stdTree())
	stdCache[v.name] = img
	return img
}

type part struct {
	data []byte
	typ  byte
}

// mbrImage lays partitions out on 1 MiB boundaries behind an MBR.
func mbrImage(parts ...part) []byte {
	const align = 2048
	mbr := make([]byte, 512)
	start := align
	var img []byte
	img = append(img, mbr...)
	for i, p := range parts {
		img = append(img, make([]byte, start*512-len(img))...)
		img = append(img, p.data...)
		e := mbr[446+i*16:]
		e[4] = p.typ
		binary.LittleEndian.PutUint32(e[8:], uint32(start))
		binary.LittleEndian.PutUint32(e[12:], u32(len(p.data)/512))
		start += (len(p.data)/512 + align) / align * align
	}
	mbr[510], mbr[511] = 0x55, 0xAA
	copy(img, mbr)
	return img
}

func withVHDFooter(img []byte) []byte {
	footer := make([]byte, 512)
	copy(footer, "conectix")
	return append(append([]byte{}, img...), footer...)
}

func u8(n int) byte    { return byte(n & 0xFF) }
func u16(n int) uint16 { return uint16(n & 0xFFFF) }
func u32(n int) uint32 { return uint32(n & 0xFFFFFFFF) }
