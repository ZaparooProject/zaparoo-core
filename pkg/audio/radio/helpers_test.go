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

package radio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// maxTestReads caps every read-until loop in the tests.
const maxTestReads = 20000

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "nativedec", "testdata", name)) //nolint:gosec // fixed test fixture
	require.NoError(t, err)
	return b
}

// fastTiming makes reconnects, reloads and the open wait quick.
func fastTiming() timing {
	return timing{
		retry:       []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
		open:        10 * time.Second,
		idle:        10 * time.Second,
		reloadScale: 0.01,
	}
}

// adtsSplit splits an ADTS stream into its frames.
func adtsSplit(t testing.TB, b []byte) [][]byte {
	t.Helper()
	var frames [][]byte
	for len(b) >= 7 && len(frames) < 10000 {
		require.True(t, isADTS(b), "ADTS sync")
		n := adtsFrameLen(b)
		require.GreaterOrEqual(t, n, 7)
		require.LessOrEqual(t, n, len(b))
		frames = append(frames, b[:n])
		b = b[n:]
	}
	require.NotEmpty(t, frames)
	return frames
}

// readFrames pulls from s until it has want frames, the stream ends, or
// the deadline passes, and returns how many it got and whether it ended.
func readFrames(t testing.TB, s *Stream, want int) (got int, ended bool) {
	t.Helper()
	buf := make([][2]float64, 4096)
	deadline := time.Now().Add(15 * time.Second)
	for range maxTestReads {
		n, ok := s.Stream(buf)
		got += n
		if !ok {
			return got, true
		}
		if got >= want || time.Now().After(deadline) {
			return got, false
		}
		if n == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	return got, false
}

// tsBuilder packs ADTS frames into an MPEG transport stream with one AAC
// program, keeping continuity counters across segments.
type tsBuilder struct {
	cc map[int]byte
}

func newTSBuilder() *tsBuilder {
	return &tsBuilder{cc: map[int]byte{}}
}

const (
	testPMTPID   = 0x100
	testAudioPID = 0x101
)

func (b *tsBuilder) packet(out *bytes.Buffer, pid int, pusi bool, payload []byte) []byte {
	n := min(len(payload), 184)
	hdr := []byte{0x47, u8(pid >> 8 & 0x1F), u8(pid & 0xFF), 0}
	if pusi {
		hdr[1] |= 0x40
	}
	cc := b.cc[pid]
	b.cc[pid] = (cc + 1) & 0x0F
	if n == 184 {
		hdr[3] = 0x10 | cc
		out.Write(hdr)
		out.Write(payload[:n])
		return payload[n:]
	}
	// Fill the packet with an adaptation field.
	hdr[3] = 0x30 | cc
	out.Write(hdr)
	alen := 183 - n
	out.WriteByte(u8(alen))
	if alen > 0 {
		out.WriteByte(0)
		out.Write(bytes.Repeat([]byte{0xFF}, alen-1))
	}
	out.Write(payload[:n])
	return payload[n:]
}

func (b *tsBuilder) psi(out *bytes.Buffer, pid int, section []byte) {
	// A CRC the demuxer does not check.
	section = append(section, 0, 0, 0, 0)
	b.packet(out, pid, true, append([]byte{0}, padTo(section, 183)...))
}

func padTo(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	return append(b, bytes.Repeat([]byte{0xFF}, n-len(b))...)
}

// segment returns PAT, PMT and one PES of the frames.
func (b *tsBuilder) segment(frames [][]byte, streamType byte) []byte {
	var out bytes.Buffer
	b.psi(&out, 0, []byte{0x00, 0xB0, 13, 0, 1, 0xC1, 0, 0, 0, 1, 0xE0 | testPMTPID>>8, testPMTPID & 0xFF})
	b.psi(&out, testPMTPID, []byte{
		0x02, 0xB0, 18, 0, 1, 0xC1, 0, 0,
		0xE0 | testAudioPID>>8, testAudioPID & 0xFF, 0xF0, 0,
		streamType, 0xE0 | testAudioPID>>8, testAudioPID & 0xFF, 0xF0, 0,
	})
	pes := []byte{0, 0, 1, 0xC0, 0, 0, 0x80, 0x80, 5, 0x21, 0, 1, 0, 1}
	for _, f := range frames {
		pes = append(pes, f...)
	}
	first := true
	for len(pes) > 0 {
		pes = b.packet(&out, testAudioPID, first, pes)
		first = false
	}
	return out.Bytes()
}

// mp4Box builds an ISO BMFF box.
func mp4Box(typ string, parts ...[]byte) []byte {
	var body []byte
	for _, p := range parts {
		body = append(body, p...)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body))) //nolint:gosec // test boxes are small
	out = append(out, typ...)
	return append(out, body...)
}

// u8 narrows a fixture value known to fit a byte, failing loudly if not.
func u8(n int) byte {
	if n < 0 || n > 0xFF {
		panic("radio test: value does not fit a byte")
	}
	return byte(n)
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// ascFromADTS builds the AudioSpecificConfig an ADTS header describes.
func ascFromADTS(h []byte) []byte {
	aot := h[2]>>6 + 1
	freq := (h[2] >> 2) & 0x0F
	ch := (h[2]&1)<<2 | h[3]>>6
	v := uint16(aot)<<11 | uint16(freq)<<7 | uint16(ch)<<3
	return []byte{byte(v >> 8), byte(v & 0xFF)}
}

// fmp4Init builds an init segment for one AAC track.
func fmp4Init(asc []byte) []byte {
	dsi := append([]byte{0x05, u8(len(asc))}, asc...)
	dcd := append([]byte{0x40, 0x15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, dsi...)
	dcd = append([]byte{0x04, u8(len(dcd))}, dcd...)
	es := append([]byte{0, 1, 0}, dcd...)
	es = append([]byte{0x03, u8(len(es))}, es...)
	esds := mp4Box("esds", u32(0), es)
	mp4a := mp4Box("mp4a", make([]byte, 16), []byte{0, 2, 0, 16, 0, 0, 0, 0}, u32(44100<<16), esds)
	stsd := mp4Box("stsd", u32(0), u32(1), mp4a)
	tkhd := mp4Box("tkhd", u32(0), u32(0), u32(0), u32(1), make([]byte, 68))
	hdlr := mp4Box("hdlr", u32(0), u32(0), []byte("soun"), make([]byte, 13))
	trak := mp4Box("trak", tkhd, mp4Box("mdia", hdlr, mp4Box("minf", mp4Box("stbl", stsd))))
	trex := mp4Box("trex", u32(0), u32(1), u32(1), u32(0), u32(0), u32(0))
	return append(mp4Box("ftyp", []byte("iso6"), u32(0)), mp4Box("moov", trak, mp4Box("mvex", trex))...)
}

// fmp4Media builds a media segment of raw AAC frames (ADTS headers off).
func fmp4Media(frames [][]byte) []byte {
	sizes := make([]byte, 0, 4*len(frames))
	var data []byte
	for _, f := range frames {
		raw := f[7:]
		sizes = append(sizes, u32(uint32(len(raw)))...) //nolint:gosec // test frames are small
		data = append(data, raw...)
	}
	tfhd := mp4Box("tfhd", u32(0x020000), u32(1))
	trunHead := []byte{0, 0, 0x02, 0x01}
	build := func(offset uint32) []byte {
		trun := mp4Box("trun", trunHead, u32(uint32(len(frames))), u32(offset), sizes) //nolint:gosec // small
		return mp4Box("moof", mp4Box("mfhd", u32(0), u32(1)), mp4Box("traf", tfhd, trun))
	}
	moof := build(0)
	moof = build(uint32(len(moof) + 8)) //nolint:gosec // small
	return append(append(mp4Box("styp", []byte("msdh"), u32(0)), moof...), mp4Box("mdat", data)...)
}

// id3Tag builds an ID3v2.4 tag with a title and artist.
func id3Tag(title, artist string) []byte {
	frame := func(id, text string) []byte {
		body := append([]byte{3}, text...)
		return append(append([]byte(id), syncsafeBytes(len(body))...), append([]byte{0, 0}, body...)...)
	}
	body := append(frame("TIT2", title), frame("TPE1", artist)...)
	return append(append([]byte{'I', 'D', '3', 4, 0, 0}, syncsafeBytes(len(body))...), body...)
}

func syncsafeBytes(n int) []byte {
	return []byte{u8(n >> 21 & 0x7F), u8(n >> 14 & 0x7F), u8(n >> 7 & 0x7F), u8(n & 0x7F)}
}
