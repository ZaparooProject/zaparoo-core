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

package keyboardmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsShiftedKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantBaseCode int
		wantOk       bool
	}{
		// Shifted symbols
		{name: "asterisk", input: "*", wantBaseCode: 9, wantOk: true}, // Shift+8
		{name: "uppercase M", input: "M", wantBaseCode: 50, wantOk: true},
		{name: "uppercase E", input: "E", wantBaseCode: 18, wantOk: true},
		{name: "uppercase N", input: "N", wantBaseCode: 49, wantOk: true},
		{name: "uppercase U", input: "U", wantBaseCode: 22, wantOk: true},
		{name: "exclamation", input: "!", wantBaseCode: 2, wantOk: true}, // Shift+1
		{name: "at sign", input: "@", wantBaseCode: 3, wantOk: true},     // Shift+2
		{name: "uppercase A", input: "A", wantBaseCode: 30, wantOk: true},
		{name: "uppercase Z", input: "Z", wantBaseCode: 44, wantOk: true},
		{name: "plus (braced)", input: "{plus}", wantBaseCode: 13, wantOk: true}, // Shift+=
		{name: "colon", input: ":", wantBaseCode: 39, wantOk: true},              // Shift+;

		// Unshifted single chars (positive code)
		{name: "lowercase a", input: "a", wantBaseCode: 0, wantOk: false},
		{name: "lowercase m", input: "m", wantBaseCode: 0, wantOk: false},
		{name: "space", input: " ", wantBaseCode: 0, wantOk: false},
		{name: "digit 1", input: "1", wantBaseCode: 0, wantOk: false},

		// Braced specials with positive codes
		{name: "enter", input: "{enter}", wantBaseCode: 0, wantOk: false},
		{name: "f1", input: "{f1}", wantBaseCode: 0, wantOk: false},
		{name: "shift (explicit)", input: "{shift}", wantBaseCode: 0, wantOk: false},

		// Unknown names
		{name: "empty", input: "", wantBaseCode: 0, wantOk: false},
		{name: "unknown", input: "{unknown}", wantBaseCode: 0, wantOk: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotCode, gotOk := IsShiftedKey(tt.input)

			assert.Equal(t, tt.wantOk, gotOk, "ok flag should match")
			if tt.wantOk {
				assert.Equal(t, tt.wantBaseCode, gotCode, "base code should match")
			}
		})
	}
}
