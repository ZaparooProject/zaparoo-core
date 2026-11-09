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
	"errors"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	maxGD3Bytes  = 64 << 10 // GD3 tags past this size are cut off
	maxTextBytes = 512      // per field, after sanitizing
	gd3Fields    = 11
)

var errBadGD3 = errors.New("invalid GD3 tag")

// Info is the GD3 tag of a VGM file. English fields are preferred and the
// Japanese ones fill in when the English one is empty.
type Info struct {
	Title      string // track title
	Game       string
	System     string
	Author     string
	Date       string // release date as written in the tag
	Year       string // first four-digit year in Date, if any
	Notes      string
	TrackTitle string // same as Title
}

// parseGD3 reads a GD3 tag starting at its "Gd3 " magic.
func parseGD3(b []byte) (Info, error) {
	if len(b) < 12 || string(b[:4]) != "Gd3 " {
		return Info{}, errBadGD3
	}
	n := binary.LittleEndian.Uint32(b[8:])
	body := b[12:]
	if uint64(n) < uint64(len(body)) {
		body = body[:n]
	}
	if len(body) > maxGD3Bytes {
		body = body[:maxGD3Bytes]
	}

	var f [gd3Fields]string
	for i := range f {
		var s []byte
		s, body = gd3String(body)
		f[i] = sanitize(s)
	}

	pick := func(en, jp string) string {
		if en != "" {
			return en
		}
		return jp
	}
	info := Info{
		Title:  pick(f[0], f[1]),
		Game:   pick(f[2], f[3]),
		System: pick(f[4], f[5]),
		Author: pick(f[6], f[7]),
		Date:   f[8],
		Notes:  f[10],
	}
	info.TrackTitle = info.Title
	info.Year = findYear(info.Date)
	return info, nil
}

// gd3String splits one NUL-terminated UTF-16LE string off b and returns it
// as UTF-8 along with the rest. A missing terminator ends at the tag's end.
func gd3String(b []byte) (s, rest []byte) {
	end := len(b) &^ 1
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			end = i
			break
		}
	}
	units := make([]uint16, 0, min(end/2, maxTextBytes*2))
	for i := 0; i+1 < end && len(units) < maxTextBytes*2; i += 2 {
		units = append(units, binary.LittleEndian.Uint16(b[i:]))
	}
	out := make([]byte, 0, len(units))
	for _, r := range utf16.Decode(units) {
		out = utf8.AppendRune(out, r)
	}
	if end+2 <= len(b) {
		return out, b[end+2:]
	}
	return out, nil
}

// sanitize turns tag text from an untrusted file into display text:
// control and format characters are dropped, runs of space collapse to one,
// and the result is trimmed and capped at maxTextBytes on a rune boundary.
func sanitize(b []byte) string {
	var sb strings.Builder
	space := false
	for _, r := range string(b) {
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

// findYear returns the first run of exactly four digits in s.
func findYear(s string) string {
	run := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] >= '0' && s[i] <= '9' {
			run++
			continue
		}
		if run == 4 {
			return s[i-4 : i]
		}
		run = 0
	}
	return ""
}
