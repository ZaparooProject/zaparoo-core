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

package userdb

import "testing"

func TestNormalizeID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal UID with colons",
			input:    "AB:CD:EF",
			expected: "abcdef",
		},
		{
			name:     "mixed case with colons",
			input:    "Ab:Cd:Ef",
			expected: "abcdef",
		},
		{
			name:     "lowercase with colons",
			input:    "ab:cd:ef",
			expected: "abcdef",
		},
		{
			name:     "uppercase with colons",
			input:    "AB:CD:EF:12:34:56",
			expected: "abcdef123456",
		},
		{
			name:     "no colons mixed case",
			input:    "AbCdEf",
			expected: "abcdef",
		},
		{
			name:     "no colons lowercase",
			input:    "abcdef",
			expected: "abcdef",
		},
		{
			name:     "no colons uppercase",
			input:    "ABCDEF",
			expected: "abcdef",
		},
		{
			name:     "leading whitespace",
			input:    "  AB:CD:EF",
			expected: "abcdef",
		},
		{
			name:     "trailing whitespace",
			input:    "AB:CD:EF  ",
			expected: "abcdef",
		},
		{
			name:     "leading and trailing whitespace",
			input:    "  AB:CD:EF  ",
			expected: "abcdef",
		},
		{
			name:     "whitespace around colons",
			input:    "AB : CD : EF",
			expected: "ab  cd  ef",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "only colons",
			input:    ":::",
			expected: "",
		},
		{
			name:     "tabs and newlines",
			input:    "\t\nAB:CD:EF\t\n",
			expected: "abcdef",
		},
		{
			name:     "alphanumeric with colons",
			input:    "12:AB:34:CD:56:EF",
			expected: "12ab34cd56ef",
		},
		{
			name:     "single character",
			input:    "A",
			expected: "a",
		},
		{
			name:     "single character with colon",
			input:    "A:",
			expected: "a",
		},
		{
			name:     "colon only",
			input:    ":",
			expected: "",
		},
		{
			name:     "multiple consecutive colons",
			input:    "AB::CD::EF",
			expected: "abcdef",
		},
		{
			name:     "real NFC UID example",
			input:    "04:52:7C:A2:6B:5D:80",
			expected: "04527ca26b5d80",
		},
		{
			name:     "serial card example",
			input:    "CARD001",
			expected: "card001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeID(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeID(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeID_Consistency(t *testing.T) {
	t.Parallel()
	// Test that the same logical ID is normalized consistently regardless of format
	inputs := []string{
		"AB:CD:EF",
		"ab:cd:ef",
		"Ab:Cd:Ef",
		"  AB:CD:EF  ",
		"\tAB:CD:EF\n",
		"ABCDEF",
		"abcdef",
	}

	expected := "abcdef"
	for _, input := range inputs {
		result := NormalizeID(input)
		if result != expected {
			t.Errorf("NormalizeID(%q) = %q, expected consistent result %q", input, result, expected)
		}
	}
}

func TestNormalizeID_Idempotent(t *testing.T) {
	t.Parallel()
	// Test that normalizing an already normalized ID returns the same result
	testCases := []string{
		"abcdef",
		"123456",
		"abc123",
		"",
		"a",
	}

	for _, input := range testCases {
		first := NormalizeID(input)
		second := NormalizeID(first)
		if first != second {
			t.Errorf("NormalizeID is not idempotent: first=%q, second=%q", first, second)
		}
	}
}
