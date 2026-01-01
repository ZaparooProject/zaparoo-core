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

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCmdName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		char     rune
		expected bool
	}{
		// Valid characters
		{
			name:     "lowercase_a",
			char:     'a',
			expected: true,
		},
		{
			name:     "lowercase_z",
			char:     'z',
			expected: true,
		},
		{
			name:     "uppercase_A",
			char:     'A',
			expected: true,
		},
		{
			name:     "uppercase_Z",
			char:     'Z',
			expected: true,
		},
		{
			name:     "digit_0",
			char:     '0',
			expected: true,
		},
		{
			name:     "digit_9",
			char:     '9',
			expected: true,
		},
		{
			name:     "dot",
			char:     '.',
			expected: true,
		},
		// Invalid characters
		{
			name:     "underscore",
			char:     '_',
			expected: false,
		},
		{
			name:     "dash",
			char:     '-',
			expected: false,
		},
		{
			name:     "space",
			char:     ' ',
			expected: false,
		},
		{
			name:     "exclamation",
			char:     '!',
			expected: false,
		},
		{
			name:     "at_symbol",
			char:     '@',
			expected: false,
		},
		{
			name:     "hash",
			char:     '#',
			expected: false,
		},
		{
			name:     "unicode_letter",
			char:     'ñ',
			expected: false,
		},
		{
			name:     "tab",
			char:     '\t',
			expected: false,
		},
		{
			name:     "newline",
			char:     '\n',
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isCmdName(tt.char)
			assert.Equal(t, tt.expected, result, "isCmdName result mismatch for character '%c'", tt.char)
		})
	}
}

func TestIsAdvArgName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		char     rune
		expected bool
	}{
		// Valid characters
		{
			name:     "lowercase_a",
			char:     'a',
			expected: true,
		},
		{
			name:     "lowercase_z",
			char:     'z',
			expected: true,
		},
		{
			name:     "uppercase_A",
			char:     'A',
			expected: true,
		},
		{
			name:     "uppercase_Z",
			char:     'Z',
			expected: true,
		},
		{
			name:     "digit_0",
			char:     '0',
			expected: true,
		},
		{
			name:     "digit_9",
			char:     '9',
			expected: true,
		},
		{
			name:     "underscore",
			char:     '_',
			expected: true,
		},
		// Invalid characters
		{
			name:     "dot",
			char:     '.',
			expected: false,
		},
		{
			name:     "dash",
			char:     '-',
			expected: false,
		},
		{
			name:     "space",
			char:     ' ',
			expected: false,
		},
		{
			name:     "exclamation",
			char:     '!',
			expected: false,
		},
		{
			name:     "at_symbol",
			char:     '@',
			expected: false,
		},
		{
			name:     "hash",
			char:     '#',
			expected: false,
		},
		{
			name:     "unicode_letter",
			char:     'ñ',
			expected: false,
		},
		{
			name:     "tab",
			char:     '\t',
			expected: false,
		},
		{
			name:     "newline",
			char:     '\n',
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isAdvArgName(tt.char)
			assert.Equal(t, tt.expected, result, "isAdvArgName result mismatch for character '%c'", tt.char)
		})
	}
}

func TestIsWhitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		char     rune
		expected bool
	}{
		// Valid whitespace characters
		{
			name:     "space",
			char:     ' ',
			expected: true,
		},
		{
			name:     "tab",
			char:     '\t',
			expected: true,
		},
		{
			name:     "newline",
			char:     '\n',
			expected: true,
		},
		{
			name:     "carriage_return",
			char:     '\r',
			expected: true,
		},
		// Non-whitespace characters
		{
			name:     "letter_a",
			char:     'a',
			expected: false,
		},
		{
			name:     "letter_Z",
			char:     'Z',
			expected: false,
		},
		{
			name:     "digit_0",
			char:     '0',
			expected: false,
		},
		{
			name:     "underscore",
			char:     '_',
			expected: false,
		},
		{
			name:     "dot",
			char:     '.',
			expected: false,
		},
		{
			name:     "exclamation",
			char:     '!',
			expected: false,
		},
		{
			name:     "unicode_space",
			char:     '\u00A0', // Non-breaking space
			expected: false,    // Function only checks for specific ASCII whitespace
		},
		{
			name:     "form_feed",
			char:     '\f',
			expected: false, // Not included in the function
		},
		{
			name:     "vertical_tab",
			char:     '\v',
			expected: false, // Not included in the function
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isWhitespace(tt.char)
			assert.Equal(t, tt.expected, result,
				"isWhitespace result mismatch for character '%c' (0x%X)", tt.char, tt.char)
		})
	}
}
