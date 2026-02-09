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
)

func TestNormalizeTag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic lowercase",
			input:    "Genre",
			expected: "genre",
		},
		{
			name:     "trim whitespace",
			input:    "  genre  ",
			expected: "genre",
		},
		{
			name:     "spaces to dashes",
			input:    "multi player",
			expected: "multi-player",
		},
		{
			name:     "periods to dashes",
			input:    "year.1991",
			expected: "year-1991",
		},
		{
			name:     "remove special chars except colon and dash",
			input:    "compatibility@A500!",
			expected: "compatibilitya500",
		},
		{
			name:     "keep colon and dash",
			input:    "disc:1-2",
			expected: "disc:1-2",
		},
		{
			name:     "complex case",
			input:    "  Lang: EN  ",
			expected: "lang:en",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special chars",
			input:    "!@#$%^&*()",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeTag(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeTag(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
