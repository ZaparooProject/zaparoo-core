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

package cdrom

import (
	"strings"
	"unicode"
)

const (
	cdTextPackSize = 18
	cdTextTextSize = 12
	cdTextTitle    = 0x80
	cdTextArtist   = 0x81
	// maxCDTextRunes bounds one decoded string.
	maxCDTextRunes = 160
)

// CDText is the block 0 text on an audio CD. Index 0 of Titles and Artists
// is the album; index n is track n. Missing entries are empty.
type CDText struct {
	Titles  []string
	Artists []string
}

// Title returns the title of track n, or the album for n == 0.
func (c CDText) Title(n int) string {
	if n < 0 || n >= len(c.Titles) {
		return ""
	}
	return c.Titles[n]
}

// Artist returns the artist of track n, or the album artist for n == 0.
func (c CDText) Artist(n int) string {
	if n < 0 || n >= len(c.Artists) {
		return ""
	}
	return c.Artists[n]
}

// Empty reports whether no text was found.
func (c CDText) Empty() bool {
	for _, s := range c.Titles {
		if s != "" {
			return false
		}
	}
	for _, s := range c.Artists {
		if s != "" {
			return false
		}
	}
	return true
}

// ParseCDText decodes the packs returned by READ TOC format 5, without the
// 4-byte response header. Only block 0 in a single-byte character set is
// read; other blocks and double-byte text are skipped.
func ParseCDText(packs []byte) CDText {
	var out CDText
	out.Titles = parseCDTextField(packs, cdTextTitle)
	out.Artists = parseCDTextField(packs, cdTextArtist)
	return out
}

func parseCDTextField(packs []byte, packType byte) []string {
	var raw []byte
	firstTrack := -1
	for off := 0; off+cdTextPackSize <= len(packs); off += cdTextPackSize {
		pack := packs[off : off+cdTextPackSize]
		if pack[0] != packType {
			continue
		}
		block := (pack[3] >> 4) & 0x07
		doubleByte := pack[3]&0x80 != 0
		if block != 0 || doubleByte {
			continue
		}
		if firstTrack < 0 {
			firstTrack = int(pack[1] & 0x7f)
		}
		raw = append(raw, pack[4:4+cdTextTextSize]...)
	}
	if firstTrack < 0 || firstTrack > MaxTracks {
		return nil
	}

	out := make([]string, firstTrack, MaxTracks+1)
	prev := ""
	for _, part := range strings.Split(string(raw), "\x00") {
		if len(out) > MaxTracks {
			break
		}
		s := part
		if s == "\t" {
			s = prev
		} else {
			s = cleanCDText(latin1ToUTF8(s))
		}
		out = append(out, s)
		prev = s
	}
	// The pack stream pads the last string with NULs, leaving empty tails.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func latin1ToUTF8(s string) string {
	runes := make([]rune, 0, len(s))
	for i := range len(s) {
		runes = append(runes, rune(s[i]))
	}
	return string(runes)
}

func cleanCDText(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		if n >= maxCDTextRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
