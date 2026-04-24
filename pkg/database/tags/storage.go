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

package tags

import (
	"strings"
)

// Storage-only padding: tag values that are purely numeric (or whose terminal
// colon-segment is purely numeric) are zero-padded to width 4 in SQLite so
// that lexicographic ORDER BY produces correct numeric ordering. This detail
// is transparent to callers — the public API, NFC tokens, and ZapScript always
// use the natural (unpadded) form. PadTagValue is called at every DB write
// site; UnpadTagValue is called at every DB read site.

// PadTagValue left-pads the terminal colon-segment to width 4 when it is
// entirely composed of digits. All other values pass through unchanged.
// Values already 4+ digits pass through unchanged, so callers must ensure
// their numeric value space fits within 4 digits to preserve sort order.
//
// Examples: "1" → "0001", "prg:0" → "prg:0000", "joystick:2h" → "joystick:2h".
// Idempotent: "0001" → "0001", "1995" → "1995".
func PadTagValue(value string) string {
	lastColon := strings.LastIndex(value, ":")
	var prefix, segment string
	if lastColon >= 0 {
		prefix = value[:lastColon+1]
		segment = value[lastColon+1:]
	} else {
		prefix = ""
		segment = value
	}
	if !isAllDigits(segment) || len(segment) >= 4 {
		return value
	}
	return prefix + strings.Repeat("0", 4-len(segment)) + segment
}

// UnpadTagValue strips leading zeros from the terminal colon-segment when it
// is entirely composed of digits. All other values pass through unchanged.
//
// Examples: "0001" → "1", "prg:0000" → "prg:0", "joystick:2h" → "joystick:2h".
// Idempotent on already-natural input. Preserves "0" (does not collapse to "").
func UnpadTagValue(value string) string {
	lastColon := strings.LastIndex(value, ":")
	var prefix, segment string
	if lastColon >= 0 {
		prefix = value[:lastColon+1]
		segment = value[lastColon+1:]
	} else {
		prefix = ""
		segment = value
	}
	if !isAllDigits(segment) {
		return value
	}
	stripped := strings.TrimLeft(segment, "0")
	if stripped == "" {
		stripped = "0"
	}
	return prefix + stripped
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
