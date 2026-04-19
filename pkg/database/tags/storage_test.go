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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPadTagValue(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// flat numeric values
		{"1", "0001"},
		{"2", "0002"},
		{"10", "0010"},
		{"12", "0012"},
		{"100", "0100"},
		// already 4 digits — no-op
		{"0001", "0001"},
		{"0010", "0010"},
		{"1995", "1995"},
		{"2500", "2500"},
		// more than 4 digits — no-op
		{"10000", "10000"},
		// hierarchical with digit terminal segment
		{"prg:0", "prg:0000"},
		{"prg:1", "prg:0001"},
		{"prg:3", "prg:0003"},
		{"beta:1", "beta:0001"},
		{"beta:5", "beta:0005"},
		{"vol:1", "vol:0001"},
		{"vol:9", "vol:0009"},
		{"3dfukkoku:1", "3dfukkoku:0001"},
		{"3dfukkoku:2", "3dfukkoku:0002"},
		{"mdmini:1", "mdmini:0001"},
		// hierarchical with non-digit terminal — no-op
		{"joystick:2h", "joystick:2h"},
		{"joystick:2v", "joystick:2v"},
		{"rev:a", "rev:a"},
		{"rockmanclassic:x2", "rockmanclassic:x2"},
		{"demo:playable", "demo:playable"},
		{"set:f1", "set:f1"},
		{"alt", "alt"},
		// non-digit flat values — no-op
		{"mmo", "mmo"},
		{"coop", "coop"},
		{"english", "english"},
		// special: single zero
		{"0", "0000"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, PadTagValue(tt.in))
		})
	}
}

func TestUnpadTagValue(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// padded → natural
		{"0001", "1"},
		{"0002", "2"},
		{"0010", "10"},
		{"0012", "12"},
		{"0100", "100"},
		// already natural — no-op
		{"1", "1"},
		{"10", "10"},
		{"1995", "1995"},
		// special: all zeros → single zero (regardless of count)
		{"00", "0"},
		{"000", "0"},
		{"0000", "0"},
		{"0", "0"},
		// hierarchical padded → natural
		{"prg:0000", "prg:0"},
		{"prg:0001", "prg:1"},
		{"beta:0001", "beta:1"},
		{"vol:0009", "vol:9"},
		{"3dfukkoku:0001", "3dfukkoku:1"},
		// hierarchical non-digit terminal — no-op
		{"joystick:2h", "joystick:2h"},
		{"rev:a", "rev:a"},
		{"set:f1", "set:f1"},
		// flat non-digit — no-op
		{"mmo", "mmo"},
		{"coop", "coop"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, UnpadTagValue(tt.in))
		})
	}
}

// TestPadUnpadRoundTrip verifies pad then unpad returns the original value.
func TestPadUnpadRoundTrip(t *testing.T) {
	originals := []string{
		"1", "2", "10", "12", "100",
		"0", // zero stays zero through round-trip
		"prg:0", "prg:1", "prg:3",
		"beta:1", "beta:5",
		"vol:1", "vol:9",
		"3dfukkoku:1", "mdmini:2",
		"1995", "2500", // already 4 digits, pad is no-op
		"joystick:2h", "rev:a", "mmo", "set:f1",
	}
	for _, orig := range originals {
		t.Run(orig, func(t *testing.T) {
			assert.Equal(t, orig, UnpadTagValue(PadTagValue(orig)))
		})
	}
}
