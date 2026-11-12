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
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"math"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

const testHeaderLen = 0x100

// vgmBuilder writes small VGM files for tests.
type vgmBuilder struct {
	cmds     []byte
	gd3      []byte
	extra    []byte // extra header, placed right after the main header
	ticks    int64
	loopPos  int // command offset of the loop point, -1 for none
	loopTick int64
	hdr      [testHeaderLen]byte
}

func newVGM(version uint32) *vgmBuilder {
	b := &vgmBuilder{loopPos: -1}
	copy(b.hdr[:], "Vgm ")
	binary.LittleEndian.PutUint32(b.hdr[0x08:], version)
	return b
}

func (b *vgmBuilder) set8(o int, v uint8)   { b.hdr[o] = v }
func (b *vgmBuilder) set16(o int, v uint16) { binary.LittleEndian.PutUint16(b.hdr[o:], v) }
func (b *vgmBuilder) set32(o int, v uint32) { binary.LittleEndian.PutUint32(b.hdr[o:], v) }

func (b *vgmBuilder) clock(typ int, clk uint32) *vgmBuilder {
	b.set32(clockOffsets[typ], clk)
	return b
}

func (b *vgmBuilder) cmd(p ...byte) *vgmBuilder {
	b.cmds = append(b.cmds, p...)
	return b
}

func (b *vgmBuilder) wait(n int64) *vgmBuilder {
	b.ticks += n
	for n > 0 {
		w := min(n, 0xFFFF)
		b.cmds = append(b.cmds, 0x61, byte(w), byte(w>>8)) //nolint:gosec // bounded test inputs
		n -= w
	}
	return b
}

func (b *vgmBuilder) markLoop() *vgmBuilder {
	b.loopPos = len(b.cmds)
	b.loopTick = b.ticks
	return b
}

// dataBlock appends a 0x67 data block.
func (b *vgmBuilder) dataBlock(typ byte, payload []byte) *vgmBuilder {
	b.cmds = append(b.cmds, 0x67, 0x66, typ)
	b.cmds = binary.LittleEndian.AppendUint32(b.cmds, uint32(len(payload))) //nolint:gosec // bounded test inputs
	b.cmds = append(b.cmds, payload...)
	return b
}

// romBlock appends a ROM image data block.
func (b *vgmBuilder) romBlock(typ byte, romSize, start uint32, data []byte) *vgmBuilder {
	p := binary.LittleEndian.AppendUint32(nil, romSize)
	p = binary.LittleEndian.AppendUint32(p, start)
	return b.dataBlock(typ, append(p, data...))
}

func (b *vgmBuilder) setGD3(fields ...string) *vgmBuilder {
	var body []byte
	for i := range gd3Fields {
		s := ""
		if i < len(fields) {
			s = fields[i]
		}
		for _, u := range utf16.Encode([]rune(s)) {
			body = binary.LittleEndian.AppendUint16(body, u)
		}
		body = append(body, 0, 0)
	}
	g := []byte("Gd3 \x00\x01\x00\x00")
	g = binary.LittleEndian.AppendUint32(g, uint32(len(body))) //nolint:gosec // bounded test inputs
	g = append(g, body...)
	b.gd3 = g
	return b
}

func (b *vgmBuilder) bytes() []byte {
	hdr := b.hdr
	dataOfs := testHeaderLen + len(b.extra)
	binary.LittleEndian.PutUint32(hdr[0x34:], uint32(dataOfs-0x34)) //nolint:gosec // bounded test inputs
	binary.LittleEndian.PutUint32(hdr[0x18:], uint32(b.ticks))      //nolint:gosec // bounded test inputs
	if len(b.extra) > 0 {
		binary.LittleEndian.PutUint32(hdr[0xBC:], uint32(testHeaderLen-0xBC))
	}
	if b.loopPos >= 0 {
		binary.LittleEndian.PutUint32(hdr[0x1C:], uint32(dataOfs+b.loopPos-0x1C)) //nolint:gosec // bounded test inputs
		binary.LittleEndian.PutUint32(hdr[0x20:], uint32(b.ticks-b.loopTick))     //nolint:gosec // bounded test inputs
	}
	out := append([]byte{}, hdr[:]...)
	out = append(out, b.extra...)
	out = append(out, b.cmds...)
	out = append(out, 0x66)
	if b.gd3 != nil {
		binary.LittleEndian.PutUint32(out[0x14:], uint32(len(out)-0x14)) //nolint:gosec // bounded test inputs
		out = append(out, b.gd3...)
	}
	binary.LittleEndian.PutUint32(out[0x04:], uint32(len(out)-0x04)) //nolint:gosec // bounded test inputs
	return out
}

func gzipBytes(t testing.TB, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(p)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// maxTestFrames caps every render loop in the tests.
const maxTestFrames = 2_000_000

func openTest(t testing.TB, data []byte, opts Options) *Decoder {
	t.Helper()
	d, err := OpenBytes(data, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// renderN renders up to n frames (capped) and returns them.
func renderN(t testing.TB, d *Decoder, n int) [][2]float64 {
	t.Helper()
	n = min(n, maxTestFrames)
	out := make([][2]float64, 0, n)
	buf := make([][2]float64, 1024)
	for len(out) < n {
		k, ok := d.Stream(buf[:min(len(buf), n-len(out))])
		out = append(out, buf[:k]...)
		if !ok {
			break
		}
	}
	return out
}

// renderAll renders the whole stream, capped at maxTestFrames.
func renderAll(t testing.TB, d *Decoder) [][2]float64 {
	t.Helper()
	return renderN(t, d, d.Len())
}

func rms(s [][2]float64) float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, f := range s {
		sum += f[0]*f[0] + f[1]*f[1]
	}
	return math.Sqrt(sum / float64(2*len(s)))
}

// pitch estimates the fundamental of s (left+right) by counting upward
// zero crossings of the signal with its mean removed.
func pitch(s [][2]float64, rate int) float64 {
	if len(s) < 2 {
		return 0
	}
	var mean float64
	for _, f := range s {
		mean += f[0] + f[1]
	}
	mean /= float64(len(s))
	crossings := 0
	first, last := -1, -1
	prev := s[0][0] + s[0][1] - mean
	for i := 1; i < len(s); i++ {
		v := s[i][0] + s[i][1] - mean
		if prev < 0 && v >= 0 {
			if first < 0 {
				first = i
			}
			last = i
			crossings++
		}
		prev = v
	}
	if crossings < 2 {
		return 0
	}
	return float64(crossings-1) * float64(rate) / float64(last-first)
}

func requirePitch(t *testing.T, s [][2]float64, rate int, want, tolerance float64) {
	t.Helper()
	got := pitch(s, rate)
	require.InDelta(t, want, got, want*tolerance, "pitch")
}

func peakToPeak(s [][2]float64) float64 {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, f := range s {
		lo = min(lo, f[0], f[1])
		hi = max(hi, f[0], f[1])
	}
	return hi - lo
}
