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
	"strings"
	"unicode"
	"unicode/utf8"
)

// sanitize turns a tag from an untrusted file into display text: invalid
// UTF-8 is read as Latin-1, control and format characters are dropped,
// runs of space collapse to one and the result is trimmed and capped at
// maxTextBytes on a rune boundary.
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
