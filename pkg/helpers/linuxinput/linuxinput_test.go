// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

//go:build linux

package linuxinput

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKeyCombo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCodes []int
		wantCombo bool
		wantErr   bool
	}{
		// Single key tests
		{
			name:      "single lowercase letter",
			input:     "a",
			wantCodes: []int{30}, // KEY_A
			wantCombo: false,
			wantErr:   false,
		},
		{
			name:      "single uppercase letter",
			input:     "A",
			wantCodes: []int{-30}, // Shifted KEY_A (negative indicates shift)
			wantCombo: false,
			wantErr:   false,
		},
		{
			name:      "single digit",
			input:     "1",
			wantCodes: []int{2}, // KEY_1
			wantCombo: false,
			wantErr:   false,
		},
		{
			name:      "single function key",
			input:     "{f9}",
			wantCodes: []int{67}, // KEY_F9
			wantCombo: false,
			wantErr:   false,
		},
		{
			name:      "single escape key",
			input:     "{esc}",
			wantCodes: []int{1}, // KEY_ESC
			wantCombo: false,
			wantErr:   false,
		},
		{
			name:      "single enter key",
			input:     "{enter}",
			wantCodes: []int{28}, // KEY_ENTER
			wantCombo: false,
			wantErr:   false,
		},

		// Combo key tests
		{
			name:      "ctrl+q combo",
			input:     "{ctrl+q}",
			wantCodes: []int{29, 16}, // KEY_LEFTCTRL, KEY_Q
			wantCombo: true,
			wantErr:   false,
		},
		{
			name:      "shift+a combo",
			input:     "{shift+a}",
			wantCodes: []int{42, 30}, // KEY_LEFTSHIFT, KEY_A
			wantCombo: true,
			wantErr:   false,
		},
		{
			name:      "alt+f4 combo",
			input:     "{alt+f4}",
			wantCodes: []int{56, 62}, // KEY_LEFTALT, KEY_F4
			wantCombo: true,
			wantErr:   false,
		},
		{
			name:      "ctrl+shift+esc combo (3 keys)",
			input:     "{ctrl+shift+esc}",
			wantCodes: []int{29, 42, 1}, // KEY_LEFTCTRL, KEY_LEFTSHIFT, KEY_ESC
			wantCombo: true,
			wantErr:   false,
		},
		{
			name:      "ctrl+alt+delete combo (3 keys)",
			input:     "{ctrl+alt+delete}",
			wantCodes: []int{29, 56, 111}, // KEY_LEFTCTRL, KEY_LEFTALT, KEY_DELETE
			wantCombo: true,
			wantErr:   false,
		},

		// Special character tests
		{
			name:      "space key",
			input:     " ",
			wantCodes: []int{57}, // KEY_SPACE
			wantCombo: false,
			wantErr:   false,
		},
		{
			name:      "tab key",
			input:     "{tab}",
			wantCodes: []int{15}, // KEY_TAB
			wantCombo: false,
			wantErr:   false,
		},

		// Shifted character tests
		{
			name:      "shifted character tilde",
			input:     "~",
			wantCodes: []int{-41}, // Shifted backtick
			wantCombo: false,
			wantErr:   false,
		},
		{
			name:      "shifted character exclamation",
			input:     "!",
			wantCodes: []int{-2}, // Shifted 1
			wantCombo: false,
			wantErr:   false,
		},

		// Error cases
		{
			name:      "unknown single key",
			input:     "{unknown}",
			wantCodes: nil,
			wantCombo: false,
			wantErr:   true,
		},
		{
			name:      "unknown key in combo",
			input:     "{ctrl+unknown}",
			wantCodes: nil,
			wantCombo: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			codes, isCombo, err := ParseKeyCombo(tt.input)

			if tt.wantErr {
				require.Error(t, err, "ParseKeyCombo() should return error")
				assert.Nil(t, codes, "codes should be nil on error")
				return
			}

			require.NoError(t, err, "ParseKeyCombo() should not return error")
			assert.Equal(t, tt.wantCodes, codes, "key codes should match")
			assert.Equal(t, tt.wantCombo, isCombo, "isCombo flag should match")
		})
	}
}

func TestParseKeyCombo_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantCodes []int
		wantCombo bool
		wantErr   bool
	}{
		{
			name:      "empty braces",
			input:     "{}",
			wantCodes: nil,
			wantCombo: false,
			wantErr:   true,
		},
		{
			name:      "single char in braces (invalid format)",
			input:     "{a}",
			wantCodes: nil,
			wantCombo: false,
			wantErr:   true, // {a} is not a valid key name
		},
		{
			name:      "plus at start",
			input:     "{+ctrl}",
			wantCodes: nil,
			wantCombo: false,
			wantErr:   true,
		},
		{
			name:      "plus at end",
			input:     "{ctrl+}",
			wantCodes: nil,
			wantCombo: false,
			wantErr:   true,
		},
		{
			name:      "double plus",
			input:     "{ctrl++q}",
			wantCodes: nil,
			wantCombo: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			codes, isCombo, err := ParseKeyCombo(tt.input)

			if tt.wantErr {
				require.Error(t, err, "ParseKeyCombo() should return error")
				assert.Nil(t, codes, "codes should be nil on error")
				return
			}

			require.NoError(t, err, "ParseKeyCombo() should not return error")
			assert.Equal(t, tt.wantCodes, codes, "key codes should match")
			assert.Equal(t, tt.wantCombo, isCombo, "isCombo flag should match")
		})
	}
}
