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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/nativedec"
)

const (
	tsPacketSize = 188
	tsSync       = 0x47
)

// tsDemuxer pulls the first decodable audio elementary stream out of an
// MPEG transport stream: PAT, then PMT, then the PES packets of the first
// ADTS AAC or MPEG audio PID. The payload is handed on as it arrives; the
// decoders resync on their own, so a packet lost to a continuity error
// costs a frame, not the stream. It keeps its state across segments, as
// one transport stream spans them.
type tsDemuxer struct {
	out       func(codec nativedec.Kind, es []byte) error
	err       error
	pmtPID    int
	audioPID  int
	codec     nativedec.Kind
	cc        int
	pesSkip   int
	inPES     bool
	ccCorrupt bool
}

func newTSDemuxer(out func(codec nativedec.Kind, es []byte) error) *tsDemuxer {
	return &tsDemuxer{out: out, pmtPID: -1, audioPID: -1, codec: codecNone, cc: -1}
}

// reset forgets the program, as at a discontinuity.
func (d *tsDemuxer) reset() {
	out := d.out
	*d = *newTSDemuxer(out)
}

// looksLikeTS reports whether b starts with transport stream packets.
func looksLikeTS(b []byte) bool {
	if len(b) < tsPacketSize {
		return len(b) > 0 && b[0] == tsSync
	}
	if b[0] != tsSync {
		return false
	}
	return len(b) < 2*tsPacketSize || b[tsPacketSize] == tsSync
}

// write demuxes b, which should be whole packets; a partial packet at the
// end is dropped, and bytes out of sync are skipped to the next sync. It
// stops at the first error out returns and returns it.
func (d *tsDemuxer) write(b []byte) error {
	d.err = nil
	for len(b) >= tsPacketSize && d.err == nil {
		if b[0] != tsSync || (len(b) >= 2*tsPacketSize && b[tsPacketSize] != tsSync) {
			b = b[1:]
			continue
		}
		d.packet(b[:tsPacketSize])
		b = b[tsPacketSize:]
	}
	return d.err
}

func (d *tsDemuxer) packet(p []byte) {
	if p[1]&0x80 != 0 { // transport error
		if int(p[1]&0x1F)<<8|int(p[2]) == d.audioPID {
			d.ccCorrupt = true
		}
		return
	}
	pusi := p[1]&0x40 != 0
	pid := int(p[1]&0x1F)<<8 | int(p[2])
	afc := (p[3] >> 4) & 3
	cc := int(p[3] & 0x0F)
	payload := p[4:]
	if afc&2 != 0 {
		if len(payload) == 0 {
			return
		}
		alen := int(payload[0])
		if 1+alen > len(payload) {
			return
		}
		if alen > 0 && payload[1]&0x80 != 0 && pid == d.audioPID {
			// The discontinuity indicator: the counter restarts.
			d.cc = -1
		}
		payload = payload[1+alen:]
	}
	if afc&1 == 0 {
		return
	}
	switch pid {
	case 0:
		if pusi {
			d.pat(payload)
		}
	case d.pmtPID:
		if pusi {
			d.pmt(payload)
		}
	case d.audioPID:
		d.audio(payload, pusi, cc)
	}
}

// section returns the table section a PSI payload starts, after its
// pointer field, bounded by its section length.
func section(payload []byte) []byte {
	if len(payload) < 1 {
		return nil
	}
	ptr := int(payload[0])
	if 1+ptr+3 > len(payload) {
		return nil
	}
	s := payload[1+ptr:]
	length := int(s[1]&0x0F)<<8 | int(s[2])
	if 3+length > len(s) || length < 9 {
		return nil
	}
	// Without the 4-byte CRC.
	return s[:3+length-4]
}

func (d *tsDemuxer) pat(payload []byte) {
	s := section(payload)
	if len(s) < 8 || s[0] != 0x00 {
		return
	}
	for i := 8; i+4 <= len(s); i += 4 {
		program := int(s[i])<<8 | int(s[i+1])
		pid := int(s[i+2]&0x1F)<<8 | int(s[i+3])
		if program != 0 {
			if pid != d.pmtPID {
				d.pmtPID = pid
				d.audioPID = -1
			}
			return
		}
	}
}

func (d *tsDemuxer) pmt(payload []byte) {
	s := section(payload)
	if len(s) < 12 || s[0] != 0x02 {
		return
	}
	infoLen := int(s[10]&0x0F)<<8 | int(s[11])
	i := 12 + infoLen
	for i+5 <= len(s) {
		streamType := s[i]
		pid := int(s[i+1]&0x1F)<<8 | int(s[i+2])
		esLen := int(s[i+3]&0x0F)<<8 | int(s[i+4])
		i += 5 + esLen
		var codec nativedec.Kind
		switch streamType {
		case 0x0F:
			codec = nativedec.AAC
		case 0x03, 0x04:
			codec = nativedec.MP3
		default:
			continue
		}
		if pid != d.audioPID {
			d.audioPID = pid
			d.codec = codec
			d.cc = -1
			d.inPES = false
		}
		return
	}
}

func (d *tsDemuxer) audio(payload []byte, pusi bool, cc int) {
	if d.cc >= 0 && cc != (d.cc+1)&0x0F {
		if cc == d.cc {
			return // a duplicate packet
		}
		d.ccCorrupt = true
	}
	d.cc = cc
	if pusi {
		d.ccCorrupt = false
		d.inPES = false
		// PES header: start code, stream id, length, flags, header length.
		if len(payload) < 9 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
			return
		}
		d.inPES = true
		d.pesSkip = 9 + int(payload[8])
	}
	if !d.inPES || d.ccCorrupt {
		return
	}
	if d.pesSkip > 0 {
		n := min(d.pesSkip, len(payload))
		payload = payload[n:]
		d.pesSkip -= n
	}
	if len(payload) > 0 {
		d.err = d.out(d.codec, payload)
	}
}
