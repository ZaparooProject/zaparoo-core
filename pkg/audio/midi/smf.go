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
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxTextBytes caps every metadata string the package hands out.
const maxTextBytes = 256

var (
	errNotMIDI = errors.New("midi: not a Standard MIDI File")
	errBadRMI  = errors.New("midi: malformed RMI file")
)

// unwrapRMI returns the Standard MIDI File inside a RIFF RMID container,
// or data unchanged when it is not one. With lenient set a data chunk cut
// short by a read cap is returned as far as it goes.
func unwrapRMI(data []byte, lenient bool) ([]byte, error) {
	if len(data) < 12 || !bytes.Equal(data[0:4], []byte("RIFF")) {
		return data, nil
	}
	if !bytes.Equal(data[8:12], []byte("RMID")) {
		return nil, errBadRMI
	}
	end := len(data)
	if riff := uint64(binary.LittleEndian.Uint32(data[4:8])) + 8; riff < uint64(end) {
		end = int(riff) //nolint:gosec // G115: riff < end, an int
	}
	off := 12
	for off+8 <= end {
		id := data[off : off+4]
		size := uint64(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		body := off + 8
		if size > uint64(end-body) { //nolint:gosec // G115: end >= body
			if lenient && bytes.Equal(id, []byte("data")) {
				return data[body:end], nil
			}
			return nil, errBadRMI
		}
		if bytes.Equal(id, []byte("data")) {
			return data[body : body+int(size)], nil //nolint:gosec // G115: size <= end-body
		}
		// RIFF chunks are padded to an even size.
		next := uint64(body) + size + size&1
		if next > uint64(end) {
			break
		}
		off = int(next) //nolint:gosec // G115: next <= end, an int
	}
	return nil, errBadRMI
}

// isSMF reports whether data starts with a Standard MIDI File header.
func isSMF(data []byte) bool {
	return len(data) >= 14 && bytes.Equal(data[0:4], []byte("MThd"))
}

// smfTitle finds a song title in a Standard MIDI File: a Soft Karaoke
// "@T" text event first, then the first track's name, then the first
// track's first plain text event, then the first name in a later track
// (some converters leave track 0 with only a copyright). A track cut short ends the scan of that
// track without an error, so a truncated prefix of a file still works.
func smfTitle(data []byte) (string, error) {
	if !isSMF(data) {
		return "", errNotMIDI
	}
	hdrLen := uint64(binary.BigEndian.Uint32(data[4:8]))
	if hdrLen < 6 || hdrLen > uint64(len(data)-8) { //nolint:gosec // G115: isSMF means len(data) >= 14
		return "", errNotMIDI
	}
	var karaoke, name, text, laterName string
	off := 8 + int(hdrLen) //nolint:gosec // G115: hdrLen <= len(data)-8
	for track := 0; off+8 <= len(data); track++ {
		size := uint64(binary.BigEndian.Uint32(data[off+4 : off+8]))
		body := off + 8
		end := len(data)
		if size < uint64(end-body) { //nolint:gosec // G115: off+8 <= len(data)
			end = body + int(size) //nolint:gosec // G115: size < end-body
		}
		if bytes.Equal(data[off:off+4], []byte("MTrk")) {
			scanTrackText(data[body:end], func(kind byte, b []byte) {
				switch {
				case kind == 0x01 && bytes.HasPrefix(b, []byte("@T")):
					if karaoke == "" {
						karaoke = sanitize(b[2:])
					}
				case track != 0:
					if kind == 0x03 && laterName == "" {
						laterName = sanitize(b)
					}
				case kind == 0x03 && name == "":
					name = sanitize(b)
				case kind == 0x01 && text == "" && !bytes.HasPrefix(b, []byte("@")):
					text = sanitize(b)
				}
			})
		}
		off = end
	}
	for _, s := range []string{karaoke, name, text, laterName} {
		if s != "" {
			return s, nil
		}
	}
	return "", nil
}

// scanTrackText walks the events of one MTrk body and calls fn with the
// type and payload of every text-like meta event (types 0x01 to 0x0F).
func scanTrackText(trk []byte, fn func(kind byte, b []byte)) {
	var running byte
	i := 0
	for i < len(trk) {
		_, n := readVLQ(trk[i:])
		if n == 0 {
			return
		}
		i += n
		if i >= len(trk) {
			return
		}
		status := trk[i]
		switch {
		case status == 0xFF:
			if i+2 > len(trk) {
				return
			}
			kind := trk[i+1]
			l, n := readVLQ(trk[i+2:])
			if n == 0 {
				return
			}
			start := i + 2 + n
			if l > uint64(len(trk)-start) { //nolint:gosec // G115: start <= len(trk): readVLQ read n bytes there
				return
			}
			stop := start + int(l) //nolint:gosec // G115: l <= len(trk)-start
			if kind == 0x2F {
				return
			}
			if kind >= 0x01 && kind <= 0x0F {
				fn(kind, trk[start:stop])
			}
			i = stop
			running = 0
		case status == 0xF0 || status == 0xF7:
			l, n := readVLQ(trk[i+1:])
			if n == 0 {
				return
			}
			start := i + 1 + n
			if l > uint64(len(trk)-start) { //nolint:gosec // G115: start <= len(trk): readVLQ read n bytes there
				return
			}
			i = start + int(l) //nolint:gosec // G115: l <= len(trk)-start
			running = 0
		case status&0x80 != 0:
			running = status
			i++
			i += channelDataLen(running)
		default:
			if running == 0 {
				return
			}
			i += channelDataLen(running)
		}
	}
}

// channelDataLen is the number of data bytes a channel message carries.
func channelDataLen(status byte) int {
	switch status & 0xF0 {
	case 0xC0, 0xD0:
		return 1
	case 0x80, 0x90, 0xA0, 0xB0, 0xE0:
		return 2
	default:
		// System common messages have no place in a file; skip the byte.
		return 0
	}
}

// readVLQ reads a MIDI variable-length quantity of at most four bytes. It
// returns n == 0 when the input ends first or the value is too long.
func readVLQ(b []byte) (v uint64, n int) {
	for n < 4 && n < len(b) {
		c := b[n]
		n++
		v = v<<7 | uint64(c&0x7F)
		if c&0x80 == 0 {
			return v, n
		}
	}
	return 0, 0
}

// sanitize turns untrusted metadata bytes into a clean string: text that
// is not valid UTF-8 is read as Latin-1, which is what most MIDI tools
// wrote; control characters become spaces; runs of space collapse; the
// result is trimmed and capped at maxTextBytes on a rune boundary.
func sanitize(b []byte) string {
	var raw string
	if utf8.Valid(b) {
		raw = string(b)
	} else {
		rs := make([]rune, len(b))
		for i, c := range b {
			rs[i] = rune(c)
		}
		raw = string(rs)
	}
	var sb strings.Builder
	space := false
	for _, r := range raw {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == utf8.RuneError {
			space = true
			continue
		}
		if space && sb.Len() > 0 {
			if sb.Len()+1 > maxTextBytes {
				break
			}
			sb.WriteByte(' ')
		}
		space = false
		if sb.Len()+utf8.RuneLen(r) > maxTextBytes {
			break
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
