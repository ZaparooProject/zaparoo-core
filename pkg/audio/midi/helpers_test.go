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

package midi

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// These helpers synthesize the MIDI, RMI and SoundFont files the tests
// play, so no binary fixtures are needed.

const testPPQ = 480

func vlq(v uint32) []byte {
	out := []byte{byte(v & 0x7F)}
	for v >>= 7; v > 0; v >>= 7 {
		out = append([]byte{byte(v&0x7F) | 0x80}, out...)
	}
	return out
}

// track builds MTrk event bytes.
type track struct{ b []byte }

func (t *track) ev(delta uint32, data ...byte) *track {
	t.b = append(t.b, vlq(delta)...)
	t.b = append(t.b, data...)
	return t
}

func (t *track) meta(delta uint32, kind byte, payload []byte) *track {
	t.b = append(t.b, vlq(delta)...)
	t.b = append(t.b, 0xFF, kind)
	t.b = append(t.b, vlq(uint32(len(payload)))...) //nolint:gosec // bounded test inputs
	t.b = append(t.b, payload...)
	return t
}

func (t *track) tempo(delta, usPerQuarter uint32) *track {
	//nolint:gosec // bounded test inputs
	return t.meta(delta, 0x51, []byte{byte(usPerQuarter >> 16), byte(usPerQuarter >> 8), byte(usPerQuarter)})
}

func (t *track) end() []byte {
	t.meta(0, 0x2F, nil)
	return t.b
}

func smf(format uint16, tracks ...[]byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("MThd")
	_ = binary.Write(&buf, binary.BigEndian, uint32(6))
	_ = binary.Write(&buf, binary.BigEndian, format)
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(tracks))) //nolint:gosec // bounded test inputs
	_ = binary.Write(&buf, binary.BigEndian, uint16(testPPQ))
	for _, trk := range tracks {
		buf.WriteString("MTrk")
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(trk))) //nolint:gosec // bounded test inputs
		buf.Write(trk)
	}
	return buf.Bytes()
}

// notes adds n quarter notes of key on channel ch, each held a quarter.
func notes(t *track, ch, key byte, n int) {
	for range n {
		t.ev(0, 0x90|ch, key, 100)
		t.ev(testPPQ, 0x80|ch, key, 0)
	}
}

// type0Song lasts 2.0 s: two quarters at 120 BPM (1 s), then a tempo
// change and one quarter at 60 BPM (1 s).
func type0Song() []byte {
	t := &track{}
	t.meta(0, 0x03, []byte("Type Zero Song"))
	t.tempo(0, 500000)
	t.ev(0, 0xC0, 0)
	notes(t, 0, 69, 2)
	t.tempo(0, 1000000)
	notes(t, 0, 72, 1)
	return smf(0, t.end())
}

const type0Seconds = 2.0

// type1Song has a tempo track and two instrument tracks, 2 s at 120 BPM.
func type1Song() []byte {
	t0 := &track{}
	t0.meta(0, 0x03, []byte("Type One Song"))
	t0.tempo(0, 500000)
	t1 := &track{}
	t1.ev(0, 0xC0, 0)
	notes(t1, 0, 60, 4)
	t2 := &track{}
	t2.ev(0, 0xC1, 40)
	notes(t2, 1, 67, 4)
	return smf(1, t0.end(), t1.end(), t2.end())
}

const type1Seconds = 2.0

// karSong is a Soft Karaoke file: the title is in a text event of the
// words track, and the first track's name is the generic "Soft Karaoke".
func karSong() []byte {
	t0 := &track{}
	t0.meta(0, 0x03, []byte("Soft Karaoke"))
	t0.meta(0, 0x01, []byte("@KMIDI KARAOKE FILE"))
	t0.tempo(0, 500000)
	t1 := &track{}
	t1.meta(0, 0x03, []byte("Words"))
	t1.meta(0, 0x01, []byte("@TKaraoke\x07 Title"))
	t1.meta(0, 0x01, []byte("@TSome Artist"))
	t1.meta(0, 0x01, []byte("\\Hel"))
	t1.meta(0, 0x01, []byte("lo"))
	t2 := &track{}
	t2.ev(0, 0xC0, 0)
	notes(t2, 0, 64, 2)
	return smf(1, t0.end(), t1.end(), t2.end())
}

// rmi wraps a Standard MIDI File in a RIFF RMID container, with an INFO
// list after the data chunk as real RMI files carry.
func rmi(song []byte) []byte {
	var body bytes.Buffer
	body.WriteString("RMID")
	body.WriteString("data")
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(song))) //nolint:gosec // bounded test inputs
	body.Write(song)
	if len(song)%2 == 1 {
		body.WriteByte(0)
	}
	info := []byte("INFOINAM\x05\x00\x00\x00Name\x00\x00")
	body.WriteString("LIST")
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(info))) //nolint:gosec // bounded test inputs
	body.Write(info)
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len())) //nolint:gosec // bounded test inputs
	out.Write(body.Bytes())
	return out.Bytes()
}

// denseSong plays four-note chords on eight channels for seconds seconds,
// to give the benchmarks something closer to real music.
func denseSong(seconds int) []byte {
	t0 := &track{}
	t0.tempo(0, 500000)
	t := &track{}
	for ch := range byte(8) {
		t.ev(0, 0xC0|ch, ch*8)
	}
	for beat := range seconds * 2 {
		for ch := range byte(8) {
			for k := range byte(4) {
				t.ev(0, 0x90|ch, 48+ch*3+k*4+byte(beat%5), 90)
			}
		}
		first := true
		for ch := range byte(8) {
			for k := range byte(4) {
				d := uint32(0)
				if first {
					d = testPPQ
					first = false
				}
				t.ev(d, 0x80|ch, 48+ch*3+k*4+byte(beat%5), 0)
			}
		}
	}
	return smf(1, t0.end(), t.end())
}

func riffChunk(id string, data []byte) []byte {
	out := []byte(id)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data))) //nolint:gosec // bounded test inputs
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func riffList(kind string, chunks ...[]byte) []byte {
	data := []byte(kind)
	for _, c := range chunks {
		data = append(data, c...)
	}
	return riffChunk("LIST", data)
}

func name20(s string) []byte {
	b := make([]byte, 20)
	copy(b, s)
	return b
}

// tinySF2 builds a SoundFont with one looped sine sample at 440 Hz under
// preset 0 of bank 0, with one instrument and one zone each.
func tinySF2() []byte {
	const (
		cycle  = 100
		cycles = 20
		rate   = 44000 // 100 samples a cycle -> 440 Hz
	)
	n := cycle * cycles
	smpl := make([]byte, 0, (n+46)*2)
	for i := range n {
		v := int16(math.Round(12000 * math.Sin(2*math.Pi*float64(i)/cycle)))
		smpl = binary.LittleEndian.AppendUint16(smpl, uint16(v)) //nolint:gosec // bounded test inputs
	}
	smpl = append(smpl, make([]byte, 46*2)...)

	u16 := func(b []byte, v uint16) []byte { return binary.LittleEndian.AppendUint16(b, v) }
	u32 := func(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }
	phdr := func(name string, preset, bank, bag uint16) []byte {
		b := name20(name)
		b = u16(b, preset)
		b = u16(b, bank)
		b = u16(b, bag)
		return append(b, make([]byte, 12)...)
	}
	bag := func(gen, mod uint16) []byte { return u16(u16(nil, gen), mod) }
	gen := func(op, amount uint16) []byte { return u16(u16(nil, op), amount) }
	inst := func(name string, bag uint16) []byte { return u16(name20(name), bag) }
	shdr := func(name string, start, end, loopStart, loopEnd, sampleRate uint32, pitch byte, typ uint16) []byte {
		b := name20(name)
		b = u32(b, start)
		b = u32(b, end)
		b = u32(b, loopStart)
		b = u32(b, loopEnd)
		b = u32(b, sampleRate)
		b = append(b, pitch, 0)
		b = u16(b, 0)
		return u16(b, typ)
	}
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	info := riffList("INFO",
		riffChunk("ifil", u16(u16(nil, 2), 1)),
		riffChunk("isng", []byte("EMU8000\x00")),
		riffChunk("INAM", []byte("Test Sine\x00")),
	)
	sdta := riffList("sdta", riffChunk("smpl", smpl))
	pdta := riffList("pdta",
		riffChunk("phdr", cat(phdr("Sine", 0, 0, 0), phdr("EOP", 0, 0, 1))),
		riffChunk("pbag", cat(bag(0, 0), bag(1, 0))),
		riffChunk("pmod", make([]byte, 10)),
		riffChunk("pgen", cat(gen(41, 0), gen(0, 0))), // instrument 0
		riffChunk("inst", cat(inst("Sine", 0), inst("EOI", 1))),
		riffChunk("ibag", cat(bag(0, 0), bag(2, 0))),
		riffChunk("imod", make([]byte, 10)),
		riffChunk("igen", cat(gen(54, 1), gen(53, 0), gen(0, 0))), // loop; sample 0
		riffChunk("shdr", cat(
			shdr("Sine", 0, uint32(n), 0, uint32(n), rate, 69, 1),
			shdr("EOS", 0, 0, 0, 0, 0, 0, 0),
		)),
	)
	body := cat([]byte("sfbk"), info, sdta, pdta)
	return riffChunk("RIFF", body)
}

func writeTemp(t testing.TB, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, data, 0o600))
	return p
}

func readFrames(t testing.TB, d *Decoder, frames int) [][2]float64 {
	t.Helper()
	out := make([][2]float64, 0, min(frames, maxTestFrames))
	buf := make([][2]float64, 1024)
	for len(out) < frames {
		want := min(len(buf), frames-len(out))
		n, ok := d.Stream(buf[:want])
		out = append(out, buf[:n]...)
		if !ok {
			break
		}
	}
	require.NoError(t, d.Err())
	return out
}

// maxTestFrames bounds how much a test reads from one decoder: ten minutes
// of audio. A decoder that never ends fails the test instead of eating the
// host's memory.
const maxTestFrames = 10 * 60 * rate

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

// zeroCrossingHz estimates the dominant frequency of the left channel.
func zeroCrossingHz(frames [][2]float64, rate int) float64 {
	crossings := 0
	for i := 1; i < len(frames); i++ {
		if (frames[i-1][0] < 0) != (frames[i][0] < 0) {
			crossings++
		}
	}
	return float64(crossings) / 2 / (float64(len(frames)) / float64(rate))
}
