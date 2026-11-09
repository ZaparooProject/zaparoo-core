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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ntscClock = 3579545
	oneSecond = 44100
	testRate  = 48000
)

// snTone is an SN76489 file playing a 440 Hz tone on channel 0 (or on the
// second chip when num is 1).
func snTone(num byte, seconds int64) *vgmBuilder {
	b := newVGM(0x171).clock(chipSN76489, ntscClock)
	b.set16(0x28, 9)
	b.set8(0x2A, 16)
	op := byte(0x50)
	if num == 1 {
		op = 0x30
	}
	const n = 254 // 3579545 / (32 * 254) = 440.4 Hz
	b.cmd(op, 0x80|n&0x0F, op, n>>4&0x3F, op, 0x90)
	return b.wait(seconds * oneSecond)
}

func TestSN76489Tone(t *testing.T) {
	t.Parallel()
	d := openTest(t, snTone(0, 1).bytes(), Options{})
	assert.Equal(t, testRate, d.SampleRate())
	assert.Equal(t, testRate, d.Len())
	s := renderAll(t, d)
	require.Len(t, s, testRate)
	assert.Greater(t, rms(s), 0.02)
	requirePitch(t, s[testRate/10:], testRate, 440.4, 0.02)
}

func ym2612Note() *vgmBuilder {
	b := newVGM(0x171).clock(chipYM2612, 7670453)
	w := func(port byte, reg, v byte) { b.cmd(0x52+port, reg, v) }
	w(0, 0x22, 0)
	w(0, 0x27, 0)
	w(0, 0x2B, 0)
	w(0, 0xB0, 0x07) // algorithm 7, no feedback
	w(0, 0xB4, 0xC0)
	for i, tl := range []byte{0, 0x7F, 0x7F, 0x7F} {
		o := byte(i * 4)
		w(0, 0x30+o, 0x01)
		w(0, 0x40+o, tl)
		w(0, 0x50+o, 0x1F)
		w(0, 0x60+o, 0)
		w(0, 0x70+o, 0)
		w(0, 0x80+o, 0x0F)
		w(0, 0x90+o, 0)
	}
	// 440 Hz: fnum = 440 * 2^20 / (7670453/144) / 2^(4-1)
	const fnum = 1083
	w(0, 0xA4, 4<<3|fnum>>8)
	w(0, 0xA0, fnum&0xFF)
	w(0, 0x28, 0xF0)
	return b.wait(oneSecond)
}

func TestYM2612Note(t *testing.T) {
	t.Parallel()
	for _, accurate := range []bool{false, true} {
		d := openTest(t, ym2612Note().bytes(), Options{AccurateYM2612: accurate})
		s := renderAll(t, d)
		assert.Greater(t, rms(s), 0.02, "accurate=%v", accurate)
		requirePitch(t, s[testRate/10:], testRate, 440, 0.03)
	}
}

// ym2151Note plays a 440 Hz sine on one YM2151 operator.
func ym2151Note() *vgmBuilder {
	b := newVGM(0x171).clock(chipYM2151, ntscClock)
	w := func(reg, v byte) { b.cmd(0x54, reg, v) }
	w(0x20, 0xC7) // both sides, algorithm 7
	w(0x28, 0x4A) // octave 4, note A
	w(0x30, 0)
	for i, tl := range []byte{0, 0x7F, 0x7F, 0x7F} {
		o := byte(i * 8)
		w(0x40+o, 0x01)
		w(0x60+o, tl)
		w(0x80+o, 0x1F)
		w(0xA0+o, 0)
		w(0xC0+o, 0)
		w(0xE0+o, 0x0F)
	}
	w(0x08, 0x78)
	return b.wait(oneSecond)
}

func TestYM2151Note(t *testing.T) {
	t.Parallel()
	d := openTest(t, ym2151Note().bytes(), Options{})
	s := renderAll(t, d)
	assert.Greater(t, rms(s), 0.02)
	requirePitch(t, s[testRate/10:], testRate, 440, 0.03)
}

// opl2Note plays a 440 Hz sine on one YM3812 operator.
func opl2Note() *vgmBuilder {
	b := newVGM(0x171).clock(chipYM3812, ntscClock)
	w := func(reg, v byte) { b.cmd(0x5A, reg, v) }
	w(0x01, 0x20)
	w(0x20, 0x21) // sustained envelope, multiplier 1
	w(0x40, 0x3F) // modulator silent
	w(0x60, 0xF0)
	w(0x80, 0x0F)
	w(0x23, 0x21)
	w(0x43, 0x00)
	w(0x63, 0xF0)
	w(0x83, 0x0F)
	w(0xC0, 0x01)
	// 440 Hz: fnum = 440 * 2^(20-4) / 49716
	const fnum = 580
	w(0xA0, fnum&0xFF)
	w(0xB0, 0x20|4<<2|fnum>>8)
	return b.wait(oneSecond)
}

func TestOPL2Note(t *testing.T) {
	t.Parallel()
	d := openTest(t, opl2Note().bytes(), Options{})
	s := renderAll(t, d)
	assert.Greater(t, rms(s), 0.02)
	requirePitch(t, s[testRate/10:], testRate, 440, 0.03)
}

func ayTone() *vgmBuilder {
	b := newVGM(0x171).clock(chipAY8910, 1789773)
	w := func(reg, v byte) { b.cmd(0xA0, reg, v) }
	const period = 254 // 1789773 / (16 * 254) = 440.4 Hz
	w(0, period&0xFF)
	w(1, period>>8)
	w(7, 0x3E)
	w(8, 0x0F)
	return b.wait(oneSecond)
}

func TestAY8910Tone(t *testing.T) {
	t.Parallel()
	d := openTest(t, ayTone().bytes(), Options{})
	s := renderAll(t, d)
	assert.Greater(t, rms(s), 0.02)
	requirePitch(t, s[testRate/10:], testRate, 440.4, 0.02)
}

// okiFile plays phrase 1 of a small ADPCM ROM on an OKIM6295.
func okiFile() *vgmBuilder {
	rom := make([]byte, 0x2000)
	const start, end = 0x400, 0x1400
	copy(rom[8:], []byte{0, start >> 8, start & 0xFF, 0, end >> 8, end & 0xFF})
	for i := start; i < end; i++ {
		// A square-ish wave: runs of maximum up then maximum down steps.
		if (i/16)%2 == 0 {
			rom[i] = 0x77
		} else {
			rom[i] = 0xFF
		}
	}
	b := newVGM(0x171).clock(chipOKIM6295, 1000000|0x80000000)
	b.romBlock(0x8B, 0x40000, 0, rom)
	b.cmd(0xB8, 0x00, 0x81, 0xB8, 0x00, 0x10)
	return b.wait(oneSecond / 2)
}

func TestOKIM6295DataBlock(t *testing.T) {
	t.Parallel()
	d := openTest(t, okiFile().bytes(), Options{})
	s := renderAll(t, d)
	assert.Greater(t, rms(s[:testRate/4]), 0.005)
}

// segaPCMFile loops a square wave on SegaPCM channel 0.
func segaPCMFile() *vgmBuilder {
	rom := make([]byte, 0x1000)
	for i := range rom {
		if (i/32)%2 == 0 {
			rom[i] = 0xE0
		} else {
			rom[i] = 0x20
		}
	}
	b := newVGM(0x171).clock(chipSegaPCM, 4000000)
	b.set32(0x3C, 0x000D) // bank shift 13 (chip settings used by most dumps)
	b.set8(0x3E, 0x70)
	b.romBlock(0x80, 0x10000, 0, rom)
	w := func(a uint16, v byte) { b.cmd(0xC0, byte(a), byte(a>>8), v) } //nolint:gosec // bounded test inputs
	w(0x02, 0x7F)                                                       // left volume
	w(0x03, 0x7F)                                                       // right volume
	w(0x04, 0x00)                                                       // loop address low
	w(0x05, 0x00)                                                       // loop address high
	w(0x06, 0x0F)                                                       // end address high
	w(0x07, 0x80)                                                       // delta
	w(0x84, 0x00)                                                       // start address low
	w(0x85, 0x00)                                                       // start address high
	w(0x86, 0x00)                                                       // key on, looping
	return b.wait(oneSecond / 2)
}

func TestSegaPCMDataBlock(t *testing.T) {
	t.Parallel()
	d := openTest(t, segaPCMFile().bytes(), Options{})
	s := renderAll(t, d)
	assert.Greater(t, rms(s), 0.005)
}

// dacStreamFile feeds a square wave to the YM2612 DAC through stream
// control commands. With 100 bytes per period at 8000 Hz it plays 80 Hz.
func dacStreamFile(useBlockStart bool) *vgmBuilder {
	pcm := make([]byte, 1000)
	for i := range pcm {
		if i%100 < 50 {
			pcm[i] = 0xE0
		} else {
			pcm[i] = 0x20
		}
	}
	b := newVGM(0x171).clock(chipYM2612, 7670453)
	b.dataBlock(0x00, pcm)
	b.cmd(0x52, 0x2B, 0x80, 0x53, 0xB6, 0xC0)
	b.cmd(0x90, 0x00, chipYM2612, 0x00, 0x2A)
	b.cmd(0x91, 0x00, 0x00, 0x01, 0x00)
	b.cmd(0x92, 0x00)
	b.cmd(binary.LittleEndian.AppendUint32(nil, 8000)...)
	if useBlockStart {
		b.cmd(0x95, 0x00, 0x00, 0x00, 0x01)
	} else {
		b.cmd(0x93, 0x00, 0, 0, 0, 0, 0x83, 0, 0, 0, 0)
	}
	return b.wait(oneSecond)
}

func TestDACStreamControl(t *testing.T) {
	t.Parallel()
	for _, blockStart := range []bool{false, true} {
		d := openTest(t, dacStreamFile(blockStart).bytes(), Options{})
		s := renderAll(t, d)
		assert.Greater(t, rms(s), 0.02, "0x95=%v", blockStart)
		requirePitch(t, s[testRate/10:], testRate, 80, 0.03)
	}
}

// A 0x93 restart with no start of its own keeps the stream's start, rather
// than moving it on by the step base each time.
func TestDACStreamRestartKeepsStart(t *testing.T) {
	t.Parallel()
	b := newVGM(0x171).clock(chipYM2612, 7670453)
	b.dataBlock(0x00, make([]byte, 1000))
	b.cmd(0x90, 0x00, chipYM2612, 0x00, 0x2A)
	b.cmd(0x91, 0x00, 0x00, 0x01, 0x0A)
	b.cmd(0x92, 0x00)
	b.cmd(binary.LittleEndian.AppendUint32(nil, 8000)...)
	b.cmd(0x93, 0x00, 0x14, 0, 0, 0, 0x01, 0x10, 0, 0, 0)
	b.wait(100)
	for range 2 {
		b.cmd(0x93, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x10, 0, 0, 0)
		b.wait(100)
	}
	d := openTest(t, b.bytes(), Options{})
	renderAll(t, d)
	s := d.st.streams[0]
	require.NotNil(t, s)
	assert.Equal(t, uint32(0x14+0x0A), s.start)
}

func TestDACStreamStop(t *testing.T) {
	t.Parallel()
	b := dacStreamFile(false)
	b.cmd(0x94, 0x00)
	b.wait(oneSecond)
	d := openTest(t, b.bytes(), Options{})
	s := renderAll(t, d)
	require.Len(t, s, 2*testRate)
	// After the stop the DAC holds its last value.
	assert.Greater(t, peakToPeak(s[testRate/2:testRate]), 0.1)
	assert.Less(t, peakToPeak(s[testRate+testRate/10:]), 0.01)
}

func TestYM2612DACDirect(t *testing.T) {
	t.Parallel()
	pcm := make([]byte, 4000)
	for i := range pcm {
		if i%40 < 20 {
			pcm[i] = 0xF0
		} else {
			pcm[i] = 0x10
		}
	}
	b := newVGM(0x171).clock(chipYM2612, 7670453)
	b.dataBlock(0x00, pcm)
	b.cmd(0x52, 0x2B, 0x80, 0x53, 0xB6, 0xC0, 0xE0, 0, 0, 0, 0)
	for range pcm {
		b.cmd(0x81) // one byte per VGM sample: 44100/40 = 1102.5 Hz
		b.ticks++
	}
	d := openTest(t, b.bytes(), Options{})
	s := renderAll(t, d)
	requirePitch(t, s[testRate/100:], testRate, 1102.5, 0.03)
}

func TestDualChip(t *testing.T) {
	t.Parallel()
	b := snTone(1, 1)
	b.set32(0x0C, ntscClock|0x40000000)
	d := openTest(t, b.bytes(), Options{})
	s := renderAll(t, d)
	assert.Greater(t, rms(s), 0.01)
	requirePitch(t, s[testRate/10:], testRate, 440.4, 0.02)

	// The same commands with no second chip play nothing.
	b = snTone(1, 1)
	d = openTest(t, b.bytes(), Options{})
	assert.InDelta(t, 0, rms(renderAll(t, d)), 1e-6)
}

func loopFile() *vgmBuilder {
	b := snTone(0, 1)
	b.markLoop()
	return b.wait(oneSecond / 2)
}

func TestLengthAndLoops(t *testing.T) {
	t.Parallel()
	// No loop: exactly the file's length.
	d := openTest(t, snTone(0, 2).bytes(), Options{SampleRate: 44100})
	assert.Equal(t, 2*44100, d.Len())

	// Loop: intro + loop body twice + 8 s fade by default.
	d = openTest(t, loopFile().bytes(), Options{})
	assert.Equal(t, 96000+8*testRate, d.Len())

	d = openTest(t, loopFile().bytes(), Options{SampleRate: 44100, Loops: 3, FadeSeconds: 1})
	assert.Equal(t, 66150+22050*2+44100, d.Len())

	// The header's loop modifier and loop base adjust the count.
	b := loopFile()
	b.set8(0x7F, 0x20) // loops * 2
	d = openTest(t, b.bytes(), Options{SampleRate: 44100, FadeSeconds: 1})
	assert.Equal(t, 66150+22050*3+44100, d.Len())
	b = loopFile()
	b.set8(0x7E, 5) // more than the loops asked for: play it once
	d = openTest(t, b.bytes(), Options{SampleRate: 44100, FadeSeconds: 1})
	assert.Equal(t, 66150+44100, d.Len())

	// The stream really is that long, and the loop keeps playing.
	d = openTest(t, loopFile().bytes(), Options{SampleRate: 44100, FadeSeconds: 1})
	s := renderAll(t, d)
	require.Len(t, s, d.Len())
	assert.Equal(t, d.Len(), d.Position())
	assert.Greater(t, rms(s[88200:110250]), 0.02)
	n, ok := d.Stream(make([][2]float64, 16))
	assert.Equal(t, 0, n)
	assert.False(t, ok)
}

func TestLoopOffsetMustBeACommand(t *testing.T) {
	t.Parallel()
	b := loopFile()
	b.loopPos++ // inside the 0x61 wait command
	d := openTest(t, b.bytes(), Options{})
	assert.Equal(t, 72000, d.Len())
}

func TestFade(t *testing.T) {
	t.Parallel()
	d := openTest(t, loopFile().bytes(), Options{Loops: 1, FadeSeconds: 2})
	s := renderAll(t, d)
	fadeStart := 72000
	require.Len(t, s, fadeStart+2*testRate)
	full := rms(s[fadeStart-4800 : fadeStart])
	mid := rms(s[fadeStart+testRate-2400 : fadeStart+testRate+2400])
	end := rms(s[len(s)-480:])
	require.Greater(t, full, 0.02)
	// The fade gain is (1 - t)^2, so a quarter halfway through.
	assert.InDelta(t, 0.25, mid/full, 0.05)
	assert.Less(t, end/full, 0.001)
}

func TestVolumeModifier(t *testing.T) {
	t.Parallel()
	base := rms(renderAll(t, openTest(t, snTone(0, 1).bytes(), Options{})))
	b := snTone(0, 1)
	b.set8(0x7C, 0x20) // +32: double
	louder := rms(renderAll(t, openTest(t, b.bytes(), Options{})))
	assert.InDelta(t, 2, louder/base, 0.05)
}

func TestGD3(t *testing.T) {
	t.Parallel()
	b := snTone(0, 1).setGD3(
		"Green Hill\x01 Zone", "グリーンヒル", "", "ソニック", "Mega Drive", "",
		"Masato  Nakamura", "", "1991/06/23", "someone", "line one\nline two",
	)
	data := b.bytes()
	d := openTest(t, data, Options{})
	want := Info{
		Title:      "Green Hill Zone",
		TrackTitle: "Green Hill Zone",
		Game:       "ソニック",
		System:     "Mega Drive",
		Author:     "Masato Nakamura",
		Date:       "1991/06/23",
		Year:       "1991",
		Notes:      "line one line two",
	}
	assert.Equal(t, want, d.Info())

	path := filepath.Join(t.TempDir(), "song.vgm")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	info, err := ReadInfo(path, 0)
	require.NoError(t, err)
	assert.Equal(t, want, info)

	// No tag at all.
	info, err = ReadInfo(writeTemp(t, "plain.vgm", snTone(0, 1).bytes()), 0)
	require.NoError(t, err)
	assert.Equal(t, Info{}, info)
}

func TestGD3Bounds(t *testing.T) {
	t.Parallel()
	long := make([]rune, 5000)
	for i := range long {
		long[i] = 'é'
	}
	info, err := parseGD3(snTone(0, 1).setGD3(string(long)).gd3)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(info.Title), maxTextBytes)
	assert.NotEmpty(t, info.Title)

	// A length field past the end, and an unterminated string.
	g := []byte("Gd3 \x00\x01\x00\x00\xff\xff\xff\x7fA\x00B\x00C")
	info, err = parseGD3(g)
	require.NoError(t, err)
	assert.Equal(t, "AB", info.Title)

	// Unpaired surrogates become nothing printable.
	g = []byte("Gd3 \x00\x01\x00\x00\x06\x00\x00\x00\x00\xd8x\x00\x00\x00")
	info, err = parseGD3(g)
	require.NoError(t, err)
	assert.Equal(t, "x", info.Title)

	_, err = parseGD3([]byte("Gd3"))
	require.Error(t, err)
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestVGZ(t *testing.T) {
	t.Parallel()
	raw := ym2612Note().setGD3("Zipped").bytes()
	gz := gzipBytes(t, raw)

	d1, err := Open(writeTemp(t, "song.vgz", gz), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d1.Close() })
	d2 := openTest(t, raw, Options{})
	d3 := openTest(t, gz, Options{})
	assert.Equal(t, "Zipped", d1.Info().Title)
	a, b, c := renderAll(t, d1), renderAll(t, d2), renderAll(t, d3)
	assert.Equal(t, b, a)
	assert.Equal(t, b, c)

	info, err := ReadInfo(writeTemp(t, "info.vgz", gz), 0)
	require.NoError(t, err)
	assert.Equal(t, "Zipped", info.Title)
}

func TestDecompressionCap(t *testing.T) {
	t.Parallel()
	_, err := unpack(gzipBytes(t, make([]byte, 4096)), 1024)
	require.ErrorIs(t, err, errTooLarge)
	_, err = ReadInfo(writeTemp(t, "big.vgz", gzipBytes(t, snTone(0, 1).bytes())), 64)
	require.Error(t, err)
}

// mixedFile drives several chips and a DAC stream, for state-heavy tests.
func mixedFile() []byte {
	b := ym2612Note()
	b.clock(chipSN76489, ntscClock)
	b.cmd(0x50, 0x80|0x0E, 0x50, 0x0F, 0x50, 0x92)
	pcm := make([]byte, 2000)
	for i := range pcm {
		pcm[i] = byte(i * 7)
	}
	b.dataBlock(0x00, pcm)
	b.cmd(0x90, 0x00, chipYM2612, 0x00, 0x2A)
	b.cmd(0x91, 0x00, 0x00, 0x01, 0x00)
	b.cmd(0x92, 0x00, 0x40, 0x1F, 0x00, 0x00) // 8000 Hz
	b.cmd(0x93, 0x00, 0, 0, 0, 0, 0x83, 0, 0, 0, 0)
	b.markLoop()
	for i := range 40 {
		b.cmd(0x50, 0x80|byte(i&0x0F), 0x52, 0xA0, byte(i*13))
		b.wait(1103)
	}
	return b.bytes()
}

func TestSeekMatchesContinuous(t *testing.T) {
	t.Parallel()
	data := mixedFile()
	opts := Options{Loops: 2, FadeSeconds: 1}
	ref := renderAll(t, openTest(t, data, opts))

	d := openTest(t, data, opts)
	for _, p := range []int{50000, 123457, 10, len(ref) - 100, 77777} {
		require.NoError(t, d.Seek(p))
		assert.Equal(t, p, d.Position())
		got := renderN(t, d, 3000)
		want := ref[p:min(p+3000, len(ref))]
		require.Len(t, got, len(want))
		for i := range want {
			require.InDelta(t, want[i][0], got[i][0], 1e-9, "seek %d frame %d", p, i)
			require.InDelta(t, want[i][1], got[i][1], 1e-9, "seek %d frame %d", p, i)
		}
	}
	require.NoError(t, d.Seek(d.Len()))
	require.Error(t, d.Seek(-1))
	require.Error(t, d.Seek(d.Len()+1))
}

func TestChunkSizeDoesNotMatter(t *testing.T) {
	t.Parallel()
	data := mixedFile()
	ref := renderAll(t, openTest(t, data, Options{}))
	d := openTest(t, data, Options{})
	var got [][2]float64
	buf := make([][2]float64, 9000)
	for i := 1; len(got) < len(ref) && i < 100000; i++ {
		n, ok := d.Stream(buf[:1+(i*7919)%len(buf)])
		got = append(got, buf[:n]...)
		if !ok {
			break
		}
	}
	require.Len(t, got, len(ref))
	assert.Equal(t, ref, got)
}

func TestBadFiles(t *testing.T) {
	t.Parallel()
	good := snTone(0, 1).bytes()
	noChips := newVGM(0x171).wait(oneSecond).bytes()
	omittedOnly := newVGM(0x171).clock(chipYMF278B, 33868800).wait(oneSecond).bytes()
	noWaits := newVGM(0x171).clock(chipSN76489, ntscClock).cmd(0x50, 0x90).bytes()
	badOfs := append([]byte{}, good...)
	binary.LittleEndian.PutUint32(badOfs[0x34:], 0x7FFFFFF0)
	cases := map[string][]byte{
		"empty":        nil,
		"magic only":   []byte("Vgm "),
		"short header": good[:0x30],
		"garbage":      []byte("this is not a vgm file at all, not even close"),
		"bad gzip":     {0x1f, 0x8b, 1, 2, 3},
		"no chips":     noChips,
		"omitted only": omittedOnly,
		"no waits":     noWaits,
		"data offset":  badOfs,
		"truncated":    good[:0x100],
	}
	for name, data := range cases {
		_, err := OpenBytes(data, Options{})
		require.Error(t, err, name)
	}
	_, err := Open(filepath.Join(t.TempDir(), "missing.vgm"), Options{})
	require.Error(t, err)

	// A file cut off mid-stream plays what is there.
	long := snTone(0, 2).bytes()
	d := openTest(t, long[:len(long)-2], Options{})
	assert.Positive(t, d.Len())
}

func TestUnknownCommandsSkipped(t *testing.T) {
	t.Parallel()
	ref := renderAll(t, openTest(t, snTone(0, 1).bytes(), Options{}))

	b := newVGM(0x171).clock(chipSN76489, ntscClock)
	b.set16(0x28, 9)
	b.set8(0x2A, 16)
	b.cmd(0x33, 0x12)                   // reserved, one operand
	b.cmd(0x4A, 0x12, 0x34)             // reserved, two operands
	b.cmd(0xD0, 0x00, 0x01, 0x02)       // YMF278B, omitted
	b.cmd(0xBC, 0x01, 0x02)             // WonderSwan, omitted
	b.cmd(0xC9, 1, 2, 3)                // reserved, three operands
	b.cmd(0xE5, 1, 2, 3, 4)             // reserved, four operands
	b.cmd(0x5E, 0x01, 0x02)             // a chip the file does not use
	b.cmd(0x67, 0x66, 0x84, 4, 0, 0, 0) // ROM for an omitted chip...
	b.cmd(1, 2, 3, 4)                   // ...and its payload
	b.cmd(0x50, 0x80|0x0E, 0x50, 0x0F, 0x50, 0x90)
	b.wait(oneSecond)
	got := renderAll(t, openTest(t, b.bytes(), Options{}))
	assert.Equal(t, ref, got)

	// An undefined command ends the stream there.
	b = snTone(0, 1)
	b.cmd(0x01)
	b.wait(oneSecond)
	d := openTest(t, b.bytes(), Options{})
	assert.Equal(t, testRate, d.Len())
}

func TestOldVersions(t *testing.T) {
	t.Parallel()
	// 1.01: 0x40-byte header, YM2612 clock kept in the YM2413 field.
	b := ym2612Note()
	b.set32(0x08, 0x101)
	b.set32(0x10, 7670453)
	b.set32(0x2C, 0)
	data := b.bytes()
	data = append(data[:0x40:0x40], data[0x100:]...)
	binary.LittleEndian.PutUint32(data[0x04:], uint32(len(data)-4)) //nolint:gosec // bounded test inputs
	d := openTest(t, data, Options{})
	s := renderAll(t, d)
	requirePitch(t, s[testRate/10:], testRate, 440, 0.03)
}

func TestCloseIdempotent(t *testing.T) {
	t.Parallel()
	d, err := OpenBytes(snTone(0, 1).bytes(), Options{})
	require.NoError(t, err)
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
	n, ok := d.Stream(make([][2]float64, 8))
	assert.Zero(t, n)
	assert.False(t, ok)
	require.ErrorIs(t, d.Seek(0), ErrClosed)
	assert.NoError(t, d.Err())
}

func TestOptionsDefaults(t *testing.T) {
	t.Parallel()
	o := Options{}.withDefaults()
	assert.Equal(t, Options{SampleRate: 48000, Loops: 2, FadeSeconds: 8}, o)
	o = Options{SampleRate: 1, Loops: 1 << 30, FadeSeconds: math.NaN()}.withDefaults()
	assert.Equal(t, minSampleRate, o.SampleRate)
	assert.Equal(t, maxLoops, o.Loops)
	assert.InDelta(t, defaultFade, o.FadeSeconds, 0)
	d := openTest(t, snTone(0, 1).bytes(), Options{SampleRate: 22050})
	assert.Equal(t, 22050, d.SampleRate())
	assert.Equal(t, 22050, d.Len())
}

func TestOutputBounded(t *testing.T) {
	t.Parallel()
	b := snTone(0, 1)
	b.set8(0x7C, 0xC0) // the loudest volume modifier
	for _, f := range renderAll(t, openTest(t, b.bytes(), Options{})) {
		require.LessOrEqual(t, math.Abs(f[0]), 1.0)
		require.LessOrEqual(t, math.Abs(f[1]), 1.0)
	}
}

// A clock far below any real chip's must be refused before the core starts:
// Mikey divides by clock/16 in its start and faulted on a fuzzed header.
func TestOpenRefusesTinyClock(t *testing.T) {
	t.Parallel()
	for _, clk := range []uint32{1, 15, 99999} {
		_, err := OpenBytes(newVGM(0x171).clock(chipMikey, clk).wait(oneSecond).bytes(), Options{})
		require.Error(t, err, "clock %d", clk)
	}
}

// A DAC stream may name a chip type past the known ones; it plays nothing
// instead of indexing past the chip table.
func TestDACStreamUnknownChipType(t *testing.T) {
	t.Parallel()
	b := newVGM(0x171).clock(chipYM2612, 7670453)
	b.dataBlock(0x00, make([]byte, 1000))
	b.cmd(0x90, 0x00, 0x7F, 0x00, 0x2A)
	b.cmd(0x91, 0x00, 0x00, 0x01, 0x00)
	b.cmd(0x92, 0x00)
	b.cmd(binary.LittleEndian.AppendUint32(nil, 8000)...)
	b.cmd(0x93, 0x00, 0, 0, 0, 0, 0x83, 0, 0, 0, 0)
	d := openTest(t, b.wait(oneSecond).bytes(), Options{})
	renderAll(t, d)
}
