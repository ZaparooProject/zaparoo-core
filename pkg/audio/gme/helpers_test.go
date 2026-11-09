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

package gme

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// The fixtures are tiny programs for each sound chip's CPU, assembled by
// hand here, so the tests carry no third-party music. Each starts one
// square-wave voice from its init routine and leaves it sounding.

const rate = DefaultSampleRate

// maxTestFrames bounds how much a test reads from one decoder. A decoder
// that runs past it fails the test instead of eating the host's memory.
const maxTestFrames = 20 * 60 * rate

// nesCode is a 6502 program at $8000: init plays the pulse 1 channel at
// the period in the table at $8020 for the track in A; play at $8040
// returns at once.
func nesCode(periods ...byte) []byte {
	code := make([]byte, 0x41)
	copy(code, []byte{
		0xAA,       // TAX
		0xA9, 0x01, // LDA #$01
		0x8D, 0x15, 0x40, // STA $4015   enable pulse 1
		0xA9, 0xBF, // LDA #$BF
		0x8D, 0x00, 0x40, // STA $4000   duty 50%, constant volume 15
		0xA9, 0x08, // LDA #$08
		0x8D, 0x01, 0x40, // STA $4001   sweep off
		0xBD, 0x20, 0x80, // LDA $8020,X
		0x8D, 0x02, 0x40, // STA $4002   period low
		0xA9, 0x00, // LDA #$00
		0x8D, 0x03, 0x40, // STA $4003   period high, start
		0x60, // RTS
	})
	copy(code[0x20:], periods)
	code[0x40] = 0x60 // play: RTS
	return code
}

func nsfFile(title, artist, copyright string, periods ...byte) []byte {
	code := nesCode(periods...)
	f := make([]byte, 0x80+len(code))
	copy(f[0x80:], code)
	h := f[:0x80]
	copy(h, "NESM\x1a")
	h[5] = 1
	h[6] = byte(len(periods)) //nolint:gosec // bounded test inputs
	h[7] = 1
	binary.LittleEndian.PutUint16(h[0x08:], 0x8000)
	binary.LittleEndian.PutUint16(h[0x0A:], 0x8000)
	binary.LittleEndian.PutUint16(h[0x0C:], 0x8040)
	copy(h[0x0E:0x2E], title)
	copy(h[0x2E:0x4E], artist)
	copy(h[0x4E:0x6E], copyright)
	binary.LittleEndian.PutUint16(h[0x6E:], 16639)
	binary.LittleEndian.PutUint16(h[0x78:], 19997)
	return f
}

func nsfeChunk(id string, data []byte) []byte {
	out := make([]byte, 0, 8+len(data))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data))) //nolint:gosec // bounded test inputs
	out = append(out, id...)
	return append(out, data...)
}

func nul(strs ...string) []byte {
	var out []byte
	for _, s := range strs {
		out = append(out, s...)
		out = append(out, 0)
	}
	return out
}

// nsfeFile has a track per period, with the given titles and lengths.
func nsfeFile(periods []byte, titles []string, ms []int32) []byte {
	info := make([]byte, 10)
	binary.LittleEndian.PutUint16(info[0:], 0x8000)
	binary.LittleEndian.PutUint16(info[2:], 0x8000)
	binary.LittleEndian.PutUint16(info[4:], 0x8040)
	info[8] = byte(len(periods)) //nolint:gosec // bounded test inputs
	out := []byte("NSFE")
	out = append(out, nsfeChunk("INFO", info)...)
	out = append(out, nsfeChunk("auth", nul("NSFE Game", "NSFE Author", "2001 NSFE Co", "Ripper"))...)
	out = append(out, nsfeChunk("tlbl", nul(titles...))...)
	var times []byte
	for _, t := range ms {
		times = binary.LittleEndian.AppendUint32(times, uint32(t)) //nolint:gosec // bounded test inputs
	}
	out = append(out, nsfeChunk("time", times)...)
	out = append(out, nsfeChunk("DATA", nesCode(periods...))...)
	return append(out, nsfeChunk("NEND", nil)...)
}

// gbsFile plays Game Boy channel 1 at the frequency low byte from the
// table at $0430 for the track in A.
func gbsFile(lows ...byte) []byte {
	h := make([]byte, 0x70)
	copy(h, "GBS")
	h[3] = 1
	h[4] = byte(len(lows)) //nolint:gosec // bounded test inputs
	h[5] = 1
	binary.LittleEndian.PutUint16(h[0x06:], 0x0400)
	binary.LittleEndian.PutUint16(h[0x08:], 0x0400)
	binary.LittleEndian.PutUint16(h[0x0A:], 0x0440)
	binary.LittleEndian.PutUint16(h[0x0C:], 0xFFFE)
	copy(h[0x10:0x30], "GBS Game")
	copy(h[0x30:0x50], "GBS Author")
	copy(h[0x50:0x70], "1999 GBS")
	code := make([]byte, 0x41)
	copy(code, []byte{
		0x5F,       // LD E,A
		0x16, 0x00, // LD D,0
		0x21, 0x30, 0x04, // LD HL,$0430
		0x19,                   // ADD HL,DE
		0x7E,                   // LD A,(HL)
		0x47,                   // LD B,A
		0x3E, 0x80, 0xE0, 0x26, // NR52: sound on
		0x3E, 0x77, 0xE0, 0x24, // NR50: full volume
		0x3E, 0x11, 0xE0, 0x25, // NR51: channel 1 to both sides
		0x3E, 0x80, 0xE0, 0x11, // NR11: duty 50%
		0x3E, 0xF0, 0xE0, 0x12, // NR12: volume 15, no envelope
		0x78, 0xE0, 0x13, // NR13: frequency low
		0x3E, 0x86, 0xE0, 0x14, // NR14: trigger, frequency high 6
		0xC9, // RET
	})
	copy(code[0x30:], lows)
	code[0x40] = 0xC9 // play: RET
	return slices.Concat(h, code)
}

// hesFile writes a square wave into PC Engine PSG channel 0 and starts it.
func hesFile() []byte {
	code := []byte{
		0xA9, 0xFF, 0x8D, 0x01, 0x08, // main balance
		0xA9, 0x00, 0x8D, 0x00, 0x08, // select channel 0
		0xA9, 0xFD, 0x8D, 0x02, 0x08, // frequency low
		0xA9, 0x00, 0x8D, 0x03, 0x08, // frequency high
		0xA9, 0xFF, 0x8D, 0x05, 0x08, // channel balance
		0xA9, 0x00, 0x8D, 0x04, 0x08, // channel off: waveform writes
		0xA2, 0x00, // LDX #0
		0x8A,       // TXA
		0x29, 0x10, // AND #$10
		0x8D, 0x06, 0x08, // STA $0806
		0xE8,       // INX
		0xE0, 0x20, // CPX #32
		0xD0, 0xF5, // BNE back to TXA
		0xA9, 0x9F, 0x8D, 0x04, 0x08, // channel on, volume 31
		0x60, // RTS
	}
	// Real rips are far larger; Game_Music_Emu's info-only reader wants
	// at least 0xD0 bytes, so pad the bank with BRK.
	code = slices.Concat(code, make([]byte, 0x100))
	h := make([]byte, 0x20)
	copy(h, "HESM")
	binary.LittleEndian.PutUint16(h[0x06:], 0x8000)
	copy(h[0x08:0x10], []byte{0xFF, 0xF8, 0, 0, 0, 0, 0, 0})
	copy(h[0x10:], "DATA")
	binary.LittleEndian.PutUint32(h[0x14:], uint32(len(code))) //nolint:gosec // bounded test inputs
	return slices.Concat(h, code)
}

// kssFile plays MSX PSG tone A from a Z80 init routine at $4000.
func kssFile() []byte {
	code := []byte{
		0x3E, 0x07, 0xD3, 0xA0, 0x3E, 0x3E, 0xD3, 0xA1, // mixer: tone A only
		0x3E, 0x08, 0xD3, 0xA0, 0x3E, 0x0F, 0xD3, 0xA1, // volume A 15
		0x3E, 0x00, 0xD3, 0xA0, 0x3E, 0xFE, 0xD3, 0xA1, // period low
		0x3E, 0x01, 0xD3, 0xA0, 0x3E, 0x00, 0xD3, 0xA1, // period high
		0xC9, // RET
	}
	h := make([]byte, 0x10)
	copy(h, "KSCC")
	binary.LittleEndian.PutUint16(h[0x04:], 0x4000)
	binary.LittleEndian.PutUint16(h[0x06:], uint16(len(code))) //nolint:gosec // bounded test inputs
	binary.LittleEndian.PutUint16(h[0x08:], 0x4000)
	binary.LittleEndian.PutUint16(h[0x0A:], 0x4020) // a RET in the code
	return slices.Concat(h, code)
}

const spcSeconds = 5

// spcFile is an SPC700 snapshot with a looping square BRR sample in the
// directory and a program that keys voice 0 on and spins.
func spcFile() []byte {
	f := make([]byte, 0x10200)
	copy(f, "SNES-SPC700 Sound File Data v0.30")
	f[0x21], f[0x22], f[0x23], f[0x24] = 0x1A, 0x1A, 0x1A, 30
	binary.LittleEndian.PutUint16(f[0x25:], 0x0400)
	f[0x2B] = 0xEF // SP
	copy(f[0x2E:], "SPC Song")
	copy(f[0x4E:], "SPC Game")
	copy(f[0x6E:], "Dumper")
	copy(f[0x7E:], "SPC Comment")
	copy(f[0x9E:], "01/02/2003")
	copy(f[0xA9:], "005")
	copy(f[0xAC:], "1000")
	copy(f[0xB1:], "SPC Artist")
	ram := f[0x100:0x10100]
	copy(ram[0x0200:], []byte{0x00, 0x03, 0x00, 0x03}) // directory: start, loop
	copy(ram[0x0300:], []byte{0xC3, 0x77, 0x77, 0x77, 0x77, 0x88, 0x88, 0x88, 0x88})
	copy(ram[0x0400:], []byte{
		0x8F, 0x4C, 0xF2, // MOV $F2,#KON
		0x8F, 0x01, 0xF3, // MOV $F3,#1
		0x2F, 0xFE, // BRA $
	})
	dsp := f[0x10100:0x10180]
	dsp[0x00], dsp[0x01] = 0x7F, 0x7F // voice 0 volume
	dsp[0x03] = 0x10                  // pitch $1000
	dsp[0x07] = 0x7F                  // direct gain
	dsp[0x0C], dsp[0x1C] = 0x7F, 0x7F // main volume
	dsp[0x6C] = 0x20                  // echo writes off
	dsp[0x5D] = 0x02                  // sample directory at $0200
	return f
}

func writeTemp(t testing.TB, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, data, 0o600))
	return p
}

func readFrames(t testing.TB, d *Decoder, frames int) [][2]float64 {
	t.Helper()
	frames = min(frames, maxTestFrames+1)
	out := make([][2]float64, 0, min(frames, 10*rate))
	buf := make([][2]float64, 1024)
	for len(out) < frames {
		n, ok := d.Stream(buf[:min(len(buf), frames-len(out))])
		out = append(out, buf[:n]...)
		if !ok {
			break
		}
	}
	require.NoError(t, d.Err())
	return out
}

func readAll(t testing.TB, d *Decoder) [][2]float64 {
	t.Helper()
	out := readFrames(t, d, maxTestFrames+1)
	require.LessOrEqual(t, len(out), maxTestFrames, "decoder did not end")
	return out
}

func rms(frames [][2]float64) float64 {
	if len(frames) == 0 {
		return 0
	}
	var sum float64
	for _, f := range frames {
		sum += f[0]*f[0] + f[1]*f[1]
	}
	return math.Sqrt(sum / float64(2*len(frames)))
}
