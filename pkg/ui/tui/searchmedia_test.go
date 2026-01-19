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

package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateSystemName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short name unchanged",
			input:    "NES",
			expected: "NES",
		},
		{
			name:     "exact max length",
			input:    "123456789012345678", // exactly 18 chars
			expected: "123456789012345678",
		},
		{
			name:     "one over max length",
			input:    "1234567890123456789", // 19 chars
			expected: "123456789012345...",
		},
		{
			name:     "long name truncated",
			input:    "Nintendo Entertainment System",
			expected: "Nintendo Entert...",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "medium length unchanged",
			input:    "Super Nintendo",
			expected: "Super Nintendo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := truncateSystemName(tt.input)
			assert.Equal(t, tt.expected, result)
			// Verify truncated results are at most 18 chars
			assert.LessOrEqual(t, len(result), 18)
		})
	}
}
