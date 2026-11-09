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
	"encoding/binary"
	"errors"
	"math"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
)

// fMP4 limits.
const (
	// maxTrunSamples bounds the samples one trun lists.
	maxTrunSamples = 1 << 16
	// maxADTSPayload is the most raw AAC an ADTS frame can carry.
	maxADTSPayload = 0x1FFF - 7
)

var errNoAudioTrack = errors.New("radio: fmp4 init segment has no AAC or MP3 track")

// mp4Track is what an fMP4 init segment says about its audio track.
type mp4Track struct {
	// frame is reused to build each ADTS frame.
	frame       []byte
	codec       nativedec.Kind
	trackID     uint32
	defaultSize uint32
	// ADTS header fields from the AudioSpecificConfig.
	profile  byte // audio object type - 1
	freqIdx  byte
	chanConf byte
}

// box is one ISO BMFF box: type and payload.
type box struct {
	typ     string
	payload []byte
	start   int // offset of the box header in the parent's buffer
}

// boxes splits b into its child boxes, stopping at the first malformed one.
func boxes(b []byte) []box {
	var out []box
	off := 0
	for len(b)-off >= 8 {
		size := uint64(binary.BigEndian.Uint32(b[off:]))
		typ := string(b[off+4 : off+8])
		hdr := uint64(8)
		switch size {
		case 0:
			size = uint64(len(b) - off)
		case 1:
			if len(b)-off < 16 {
				return out
			}
			size = binary.BigEndian.Uint64(b[off+8:])
			hdr = 16
		}
		if size < hdr || size > uint64(len(b)-off) {
			return out
		}
		end := off + int(size) //nolint:gosec // size is at most len(b)-off, checked above
		out = append(out, box{typ: typ, payload: b[off+int(hdr) : end], start: off})
		off = end
		if len(out) > 4096 {
			return out
		}
	}
	return out
}

func child(b []byte, typ string) []byte {
	for _, x := range boxes(b) {
		if x.typ == typ {
			return x.payload
		}
	}
	return nil
}

// boxPath follows a chain of first children of the given types.
func boxPath(b []byte, typs ...string) []byte {
	for _, t := range typs {
		b = child(b, t)
		if b == nil {
			return nil
		}
	}
	return b
}

// parseInit reads the first sound track of an fMP4 init segment.
func parseInit(b []byte) (*mp4Track, error) {
	moov := child(b, "moov")
	if moov == nil {
		return nil, errNoAudioTrack
	}
	defaults := map[uint32]uint32{}
	for _, x := range boxes(boxPath(moov, "mvex")) {
		if x.typ == "trex" && len(x.payload) >= 24 {
			id := binary.BigEndian.Uint32(x.payload[4:])
			defaults[id] = binary.BigEndian.Uint32(x.payload[16:])
		}
	}
	for _, x := range boxes(moov) {
		if x.typ != "trak" {
			continue
		}
		if hdlr := boxPath(x.payload, "mdia", "hdlr"); len(hdlr) < 12 || string(hdlr[8:12]) != "soun" {
			continue
		}
		tkhd := child(x.payload, "tkhd")
		if len(tkhd) < 24 {
			continue
		}
		var id uint32
		if tkhd[0] == 1 {
			if len(tkhd) < 24 {
				continue
			}
			id = binary.BigEndian.Uint32(tkhd[20:])
		} else {
			id = binary.BigEndian.Uint32(tkhd[12:])
		}
		stsd := boxPath(x.payload, "mdia", "minf", "stbl", "stsd")
		if len(stsd) < 8 {
			continue
		}
		for _, entry := range boxes(stsd[8:]) {
			if entry.typ != "mp4a" || len(entry.payload) < 28 {
				continue
			}
			channels := binary.BigEndian.Uint16(entry.payload[16:])
			// QuickTime sound description versions 1 and 2 are longer.
			fields := 28
			switch binary.BigEndian.Uint16(entry.payload[8:]) {
			case 1:
				fields += 16
			case 2:
				fields += 36
			}
			if len(entry.payload) < fields {
				continue
			}
			esds := child(entry.payload[fields:], "esds")
			t, ok := parseESDS(esds, channels)
			if !ok {
				continue
			}
			t.trackID = id
			t.defaultSize = defaults[id]
			return t, nil
		}
	}
	return nil, errNoAudioTrack
}

// descriptor reads one MPEG-4 descriptor: tag, then a length of up to
// four 7-bit groups.
func descriptor(b []byte) (tag byte, body []byte, ok bool) {
	if len(b) < 2 {
		return 0, nil, false
	}
	tag = b[0]
	n, i := 0, 1
	for ; i < 5 && i < len(b); i++ {
		n = n<<7 | int(b[i]&0x7F)
		if b[i]&0x80 == 0 {
			i++
			break
		}
	}
	if n > len(b)-i {
		return 0, nil, false
	}
	return tag, b[i : i+n], true
}

func parseESDS(esds []byte, channels uint16) (*mp4Track, bool) {
	if len(esds) < 4 {
		return nil, false
	}
	tag, es, ok := descriptor(esds[4:])
	if !ok || tag != 0x03 || len(es) < 3 {
		return nil, false
	}
	flags := es[2]
	es = es[3:]
	if flags&0x80 != 0 { // streamDependenceFlag
		if len(es) < 2 {
			return nil, false
		}
		es = es[2:]
	}
	if flags&0x40 != 0 { // URL_Flag
		if len(es) < 1 || int(es[0]) > len(es)-1 {
			return nil, false
		}
		es = es[1+int(es[0]):]
	}
	if flags&0x20 != 0 { // OCRstreamFlag
		if len(es) < 2 {
			return nil, false
		}
		es = es[2:]
	}
	tag, dcd, ok := descriptor(es)
	if !ok || tag != 0x04 || len(dcd) < 13 {
		return nil, false
	}
	switch dcd[0] {
	case 0x69, 0x6B: // MPEG-2 and MPEG-1 audio
		return &mp4Track{codec: nativedec.MP3}, true
	case 0x40, 0x66, 0x67, 0x68: // MPEG-4 and MPEG-2 AAC
	default:
		return nil, false
	}
	tag, asc, ok := descriptor(dcd[13:])
	if !ok || tag != 0x05 {
		return nil, false
	}
	t, ok := parseASC(asc, channels)
	return t, ok
}

// bitReader reads big-endian bit fields.
type bitReader struct {
	b   []byte
	pos int
}

func (r *bitReader) read(n int) (int, bool) {
	v := 0
	for range n {
		if r.pos >= len(r.b)*8 {
			return 0, false
		}
		bit := (r.b[r.pos/8] >> (7 - r.pos%8)) & 1
		v = v<<1 | int(bit)
		r.pos++
	}
	return v, true
}

func (r *bitReader) objectType() (int, bool) {
	aot, ok := r.read(5)
	if ok && aot == 31 {
		ext, ok2 := r.read(6)
		return 32 + ext, ok2
	}
	return aot, ok
}

// adtsRates are the sampling frequencies an ADTS index can name.
var adtsRates = []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

func nearestFreqIdx(rate int) int {
	best, bestDiff := 4, -1
	for i, r := range adtsRates {
		d := r - rate
		if d < 0 {
			d = -d
		}
		if bestDiff < 0 || d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return best
}

// parseASC reads an AudioSpecificConfig into the ADTS header fields. For
// explicit HE-AAC (object type 5 or 29) the header describes the AAC-LC
// core, and the decoder finds the SBR data itself.
func parseASC(asc []byte, channels uint16) (*mp4Track, bool) {
	r := &bitReader{b: asc}
	aot, ok := r.objectType()
	if !ok {
		return nil, false
	}
	freqIdx, ok := r.read(4)
	if !ok {
		return nil, false
	}
	if freqIdx == 15 {
		rate, ok2 := r.read(24)
		if !ok2 {
			return nil, false
		}
		freqIdx = nearestFreqIdx(rate)
	}
	chanConf, ok := r.read(4)
	if !ok {
		return nil, false
	}
	if aot >= 32 && aot <= 34 { // MPEG-1/2 layers I to III
		return &mp4Track{codec: nativedec.MP3}, true
	}
	if aot == 5 || aot == 29 {
		// The extension rate, then the core object type.
		ext, ok2 := r.read(4)
		if !ok2 {
			return nil, false
		}
		if ext == 15 {
			if _, ok2 = r.read(24); !ok2 {
				return nil, false
			}
		}
		if aot, ok = r.objectType(); !ok {
			return nil, false
		}
	}
	// ADTS has two profile bits: Main, LC, SSR and LTP.
	if aot < 1 || aot > 4 || freqIdx > 12 {
		return nil, false
	}
	if chanConf == 0 {
		// The layout is in a program config element ADTS cannot carry;
		// play the channel count the sample entry gives.
		chanConf = int(min(max(channels, 1), 2))
	}
	if chanConf > 7 {
		return nil, false
	}
	return &mp4Track{
		codec:    nativedec.AAC,
		profile:  byte(aot - 1),  //nolint:gosec // aot is 1 to 4, checked above
		freqIdx:  byte(freqIdx),  //nolint:gosec // at most 12, checked above
		chanConf: byte(chanConf), //nolint:gosec // at most 7, checked above
	}, true
}

// adtsHeader returns a 7-byte ADTS header for n bytes of raw AAC.
func (t *mp4Track) adtsHeader(n int) [7]byte {
	l := n + 7
	return [7]byte{
		0xFF,
		0xF1,
		t.profile<<6 | t.freqIdx<<2 | t.chanConf>>2,
		(t.chanConf&3)<<6 | byte((l>>11)&3),
		byte((l >> 3) & 0xFF),
		byte(l&7)<<5 | 0x1F,
		0xFC,
	}
}

// fmp4Samples calls emit with each sample of t in an fMP4 media segment,
// each AAC one framed as ADTS. It returns how many it emitted, stopping at
// the first error emit returns.
func fmp4Samples(seg []byte, t *mp4Track, emit func([]byte) error) (int, error) {
	count := 0
	top := boxes(seg)
	for i, x := range top {
		if x.typ != "moof" {
			continue
		}
		moofStart := x.start
		// The mdat that follows, for truns without a data offset.
		mdatStart, mdatEnd := -1, -1
		for _, y := range top[i+1:] {
			if y.typ == "mdat" {
				mdatEnd = y.start + 8 + len(y.payload)
				mdatStart = mdatEnd - len(y.payload)
				break
			}
			if y.typ == "moof" {
				break
			}
		}
		for _, traf := range boxes(x.payload) {
			if traf.typ != "traf" {
				continue
			}
			n, err := trafSamples(seg, traf.payload, moofStart, mdatStart, mdatEnd, t, emit)
			count += n
			if err != nil {
				return count, err
			}
		}
	}
	return count, nil
}

func trafSamples(
	seg, traf []byte, moofStart, mdatStart, mdatEnd int, t *mp4Track, emit func([]byte) error,
) (int, error) {
	tfhd := child(traf, "tfhd")
	if len(tfhd) < 8 {
		return 0, nil
	}
	flags := binary.BigEndian.Uint32(tfhd) & 0xFFFFFF
	if id := binary.BigEndian.Uint32(tfhd[4:]); t.trackID != 0 && id != t.trackID {
		return 0, nil
	}
	base := int64(moofStart)
	defSize := t.defaultSize
	p := tfhd[8:]
	if flags&0x01 != 0 {
		if len(p) < 8 {
			return 0, nil
		}
		v := binary.BigEndian.Uint64(p)
		if v > math.MaxInt64 {
			return 0, nil
		}
		base = int64(v) //nolint:gosec // at most MaxInt64, checked above
		p = p[8:]
	}
	for _, f := range []uint32{0x02, 0x08} {
		if flags&f != 0 {
			if len(p) < 4 {
				return 0, nil
			}
			p = p[4:]
		}
	}
	if flags&0x10 != 0 {
		if len(p) < 4 {
			return 0, nil
		}
		defSize = binary.BigEndian.Uint32(p)
	}
	count := 0
	next := int64(-1) // where a trun without a data offset starts
	for _, x := range boxes(traf) {
		if x.typ != "trun" {
			continue
		}
		n, end, err := trunSamples(seg, x.payload, base, next, mdatStart, mdatEnd, defSize, t, emit)
		count += n
		next = end
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

func trunSamples(
	seg, trun []byte, base, next int64, mdatStart, mdatEnd int, defSize uint32, t *mp4Track, emit func([]byte) error,
) (count int, end int64, err error) {
	if len(trun) < 8 {
		return 0, next, nil
	}
	flags := binary.BigEndian.Uint32(trun) & 0xFFFFFF
	samples := binary.BigEndian.Uint32(trun[4:])
	if samples > maxTrunSamples {
		return 0, next, nil
	}
	p := trun[8:]
	pos := next
	if flags&0x01 != 0 {
		if len(p) < 4 {
			return 0, next, nil
		}
		pos = base + int64(int32(binary.BigEndian.Uint32(p))) //nolint:gosec // a signed offset by spec
		p = p[4:]
	} else if pos < 0 {
		pos = int64(mdatStart)
	}
	if flags&0x04 != 0 {
		if len(p) < 4 {
			return 0, next, nil
		}
		p = p[4:]
	}
	perSample := 0
	sizeAt := -1
	for _, f := range []uint32{0x100, 0x200, 0x400, 0x800} {
		if flags&f != 0 {
			if f == 0x200 {
				sizeAt = perSample
			}
			perSample += 4
		}
	}
	limit := int64(len(seg))
	if mdatEnd >= 0 {
		limit = int64(mdatEnd)
	}
	for range samples {
		size := int64(defSize)
		if perSample > 0 {
			if len(p) < perSample {
				break
			}
			if sizeAt >= 0 {
				size = int64(binary.BigEndian.Uint32(p[sizeAt:]))
			}
			p = p[perSample:]
		}
		if pos < 0 || size <= 0 || pos+size > limit {
			break
		}
		sample := seg[pos : pos+size]
		pos += size
		if t.codec == nativedec.AAC {
			if len(sample) > maxADTSPayload {
				continue
			}
			h := t.adtsHeader(len(sample))
			t.frame = append(append(t.frame[:0], h[:]...), sample...)
			err = emit(t.frame)
		} else {
			err = emit(sample)
		}
		if err != nil {
			return count, pos, err
		}
		count++
	}
	return count, pos, nil
}
