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

package filters

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTagFilters_ANDOperator(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []database.TagFilter
	}{
		{
			name:  "AND with + prefix",
			input: []string{"+region:usa"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "AND without prefix (default)",
			input: []string{"region:usa"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Multiple AND tags",
			input: []string{"region:usa", "+lang:en"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: database.TagOperatorAND},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestParseTagFilters_NOTOperator(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []database.TagFilter
	}{
		{
			name:  "NOT with - prefix",
			input: []string{"-unfinished:demo"},
			expected: []database.TagFilter{
				{Type: "unfinished", Value: "demo", Operator: database.TagOperatorNOT},
			},
		},
		{
			name:  "Multiple NOT tags",
			input: []string{"-unfinished:demo", "-unfinished:beta"},
			expected: []database.TagFilter{
				{Type: "unfinished", Value: "demo", Operator: database.TagOperatorNOT},
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestParseTagFilters_OROperator(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []database.TagFilter
	}{
		{
			name:  "OR with ~ prefix",
			input: []string{"~lang:en"},
			expected: []database.TagFilter{
				{Type: "lang", Value: "en", Operator: database.TagOperatorOR},
			},
		},
		{
			name:  "Multiple OR tags",
			input: []string{"~lang:en", "~lang:es", "~lang:fr"},
			expected: []database.TagFilter{
				{Type: "lang", Value: "en", Operator: database.TagOperatorOR},
				{Type: "lang", Value: "es", Operator: database.TagOperatorOR},
				{Type: "lang", Value: "fr", Operator: database.TagOperatorOR},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestParseTagFilters_MixedOperators(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []database.TagFilter
	}{
		{
			name:  "AND, NOT, and OR mixed",
			input: []string{"region:usa", "-unfinished:demo", "~lang:en", "~lang:es"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
				{Type: "unfinished", Value: "demo", Operator: database.TagOperatorNOT},
				{Type: "lang", Value: "en", Operator: database.TagOperatorOR},
				{Type: "lang", Value: "es", Operator: database.TagOperatorOR},
			},
		},
		{
			name:  "Complex mix with multiple types",
			input: []string{"+region:usa", "genre:action", "-unfinished:beta", "~players:2", "~players:4"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
				{Type: "genre", Value: "action", Operator: database.TagOperatorAND},
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
				{Type: "players", Value: "2", Operator: database.TagOperatorOR},
				{Type: "players", Value: "4", Operator: database.TagOperatorOR},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestParseTagFilters_Normalization(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []database.TagFilter
	}{
		{
			name:  "Uppercase to lowercase",
			input: []string{"Region:USA"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Whitespace trimming",
			input: []string{"  region : usa  "},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Mixed case with operator",
			input: []string{"-Unfinished:DEMO"},
			expected: []database.TagFilter{
				{Type: "unfinished", Value: "demo", Operator: database.TagOperatorNOT},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestParseTagFilters_Deduplication(t *testing.T) {
	tests := []struct {
		name          string
		input         []string
		expectedCount int
	}{
		{
			name:          "Duplicate tags removed",
			input:         []string{"region:usa", "region:usa"},
			expectedCount: 1,
		},
		{
			name:          "Case-insensitive deduplication",
			input:         []string{"region:USA", "Region:usa"},
			expectedCount: 1,
		},
		{
			name:          "Different operators not deduplicated",
			input:         []string{"region:usa", "-region:usa"},
			expectedCount: 2, // Different operators = different filters
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.Len(t, result, tt.expectedCount)
		})
	}
}

func TestParseTagFilters_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		errorMsg    string
		input       []string
		expectError bool
	}{
		{
			name:        "Empty input",
			input:       []string{},
			expectError: false,
		},
		{
			name:        "Empty string in slice",
			input:       []string{""},
			expectError: false,
		},
		{
			name:        "Whitespace only",
			input:       []string{"   "},
			expectError: false,
		},
		{
			name:        "Missing colon",
			input:       []string{"regionusa"},
			expectError: true,
			errorMsg:    "must be in 'type:value' format",
		},
		{
			name:        "Empty type",
			input:       []string{":usa"},
			expectError: true,
			errorMsg:    "type and value cannot be empty",
		},
		{
			name:        "Empty value",
			input:       []string{"region:"},
			expectError: true,
			errorMsg:    "type and value cannot be empty",
		},
		{
			name:        "Multiple colons",
			input:       []string{"region:usa:extra"},
			expectError: false, // Should parse as region:usa:extra (value contains colon)
		},
		{
			name:        "Operator only",
			input:       []string{"-"},
			expectError: true,
			errorMsg:    "must be in 'type:value' format",
		},
		{
			name:        "Operator with no tag",
			input:       []string{"-:"},
			expectError: true,
			errorMsg:    "type and value cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestParseTagFilters_MaxLimits(t *testing.T) {
	t.Run("Exceeds max tags count", func(t *testing.T) {
		input := make([]string, 51) // maxTagsCount is 50
		for i := range input {
			input[i] = "tag:value"
		}
		_, err := ParseTagFilters(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeded maximum number of tags")
	})

	t.Run("Exceeds max tag length", func(t *testing.T) {
		longTag := "type:" + string(make([]byte, 200)) // maxTagLength is 128
		_, err := ParseTagFilters([]string{longTag})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tag too long")
	})

	t.Run("At max tags count", func(t *testing.T) {
		input := make([]string, 50) // Exactly at limit
		for i := range input {
			input[i] = "tag:value"
		}
		result, err := ParseTagFilters(input)
		require.NoError(t, err)
		assert.Len(t, result, 1) // Should deduplicate to 1
	})

	t.Run("Exactly at max tag length boundary", func(t *testing.T) {
		// Create a tag that is exactly 128 characters (type:value format)
		// "type:" = 5 chars, so we need 123 chars in value
		longValue := string(make([]byte, 123))
		for i := range longValue {
			longValue = longValue[:i] + "a" + longValue[i+1:]
		}
		input := []string{"type:" + longValue}
		result, err := ParseTagFilters(input)
		require.NoError(t, err)
		assert.Len(t, result, 1)
	})
}

// TestParseTagFilters_Security tests for potentially malicious input patterns
func TestParseTagFilters_Security(t *testing.T) {
	tests := []struct {
		name        string
		description string
		input       []string
		expectError bool
	}{
		{
			name:        "SQL injection - single quote",
			input:       []string{"region:usa' OR '1'='1"},
			expectError: false, // Should be normalized, not cause SQL injection
			description: "SQL injection attempts should be safely normalized",
		},
		{
			name:        "SQL injection - comment",
			input:       []string{"region:usa--"},
			expectError: false,
			description: "SQL comments should be treated as normal text",
		},
		{
			name:        "SQL injection - semicolon",
			input:       []string{"region:usa; DROP TABLE tags;"},
			expectError: false,
			description: "SQL commands should be treated as normal text",
		},
		{
			name:        "Script injection - HTML tags",
			input:       []string{"region:<script>alert('xss')</script>"},
			expectError: false,
			description: "HTML/script tags should be normalized",
		},
		{
			name:        "Path traversal attempt",
			input:       []string{"region:../../../etc/passwd"},
			expectError: false,
			description: "Path traversal should be treated as normal text",
		},
		{
			name:        "Null byte injection",
			input:       []string{"region:usa\x00admin"},
			expectError: false,
			description: "Null bytes should be handled safely",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			if tt.expectError {
				require.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestParseTagFilters_Unicode tests Unicode and special character handling
// Note: Unicode characters are stripped by normalization (only a-z, 0-9, :,+,- allowed)
func TestParseTagFilters_Unicode(t *testing.T) {
	tests := []struct {
		name        string
		input       []string
		expected    []database.TagFilter
		expectError bool
	}{
		{
			name:  "Japanese characters only - rejected after normalization",
			input: []string{"lang:日本語"}, //nolint:gosmopolitan // Test requires non-ASCII
			// Normalization strips all chars, leaving empty value
			expectError: true,
		},
		{
			name:        "Emoji only - rejected after normalization",
			input:       []string{"mood:😀"},
			expectError: true, // Normalization strips emoji, leaving empty value
		},
		{
			name:        "Arabic characters only - rejected after normalization",
			input:       []string{"lang:العربية"},
			expectError: true, // Normalization strips all chars, leaving empty value
		},
		{
			name:        "Cyrillic only - rejected after normalization",
			input:       []string{"lang:Русский"},
			expectError: true, // Normalization strips all chars, leaving empty value
		},
		{
			name:  "Mixed scripts - only ASCII kept",
			input: []string{"game:ファイナルFantasy"},
			expected: []database.TagFilter{
				{Type: "game", Value: "fantasy", Operator: database.TagOperatorAND},
			},
			expectError: false,
		},
		{
			name:  "Combining diacritics removed",
			input: []string{"name:café"},
			expected: []database.TagFilter{
				{Type: "name", Value: "caf", Operator: database.TagOperatorAND},
			},
			expectError: false,
		},
		{
			name:  "Zero-width characters removed",
			input: []string{"text:hello\u200Bworld"}, // Zero-width space
			expected: []database.TagFilter{
				{Type: "text", Value: "helloworld", Operator: database.TagOperatorAND},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.ElementsMatch(t, tt.expected, result)
			}
		})
	}
}

// TestParseTagFilters_Whitespace tests various whitespace handling scenarios
func TestParseTagFilters_Whitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []database.TagFilter
	}{
		{
			name:  "Tab characters",
			input: []string{"region:\tusa"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Newline characters",
			input: []string{"region:\nusa"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Multiple spaces become multiple hyphens",
			input: []string{"name:super  mario  world"},
			expected: []database.TagFilter{
				{Type: "name", Value: "super--mario--world", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Leading and trailing spaces",
			input: []string{"  +region:usa  "},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Mixed whitespace around colon",
			input: []string{"region \t:\n usa"},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// TestParseTagFilters_ConflictingFilters tests handling of potentially conflicting filter combinations
func TestParseTagFilters_ConflictingFilters(t *testing.T) {
	tests := []struct {
		name          string
		description   string
		input         []string
		expectedCount int
	}{
		{
			name:          "Same tag with different operators",
			input:         []string{"region:usa", "-region:usa"},
			expectedCount: 2,
			description:   "AND and NOT on same tag should both be preserved",
		},
		{
			name:          "OR and NOT on same tag",
			input:         []string{"~region:usa", "-region:usa"},
			expectedCount: 2,
			description:   "OR and NOT on same tag should both be preserved",
		},
		{
			name:          "All three operators on same tag",
			input:         []string{"region:usa", "~region:usa", "-region:usa"},
			expectedCount: 3,
			description:   "All operators on same tag should be preserved",
		},
		{
			name:          "Multiple values with mixed operators",
			input:         []string{"region:usa", "region:jp", "-region:eu"},
			expectedCount: 3,
			description:   "Different values with different operators should be preserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err, tt.description)
			assert.Len(t, result, tt.expectedCount, tt.description)
		})
	}
}

// TestParseTagFilters_SpecialCharacters tests handling of special characters in type and value
// Note: Tags go through normalization which removes most special chars
// Keeps only: a-z, 0-9, colon, comma, plus, and hyphen
func TestParseTagFilters_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []database.TagFilter
	}{
		{
			name:  "Ampersand removed by normalization (spaces to hyphen first)",
			input: []string{"game:sonic & knuckles"},
			expected: []database.TagFilter{
				{Type: "game", Value: "sonic--knuckles", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Parentheses removed by normalization",
			input: []string{"game:zelda (usa)"},
			expected: []database.TagFilter{
				{Type: "game", Value: "zelda-usa", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Brackets removed by normalization",
			input: []string{"game:game [beta]"},
			expected: []database.TagFilter{
				{Type: "game", Value: "game-beta", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Underscore removed, hyphen preserved",
			input: []string{"system:retro_gaming-console"},
			expected: []database.TagFilter{
				{Type: "system", Value: "retrogaming-console", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Period converted to hyphen",
			input: []string{"version:v1.2.3"},
			expected: []database.TagFilter{
				{Type: "version", Value: "v1-2-3", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Plus sign preserved",
			input: []string{"game:c++"},
			expected: []database.TagFilter{
				{Type: "game", Value: "c++", Operator: database.TagOperatorAND},
			},
		},
		{
			name:  "Comma and colon preserved",
			input: []string{"list:a,b,c:d"},
			expected: []database.TagFilter{
				{Type: "list", Value: "a,b,c:d", Operator: database.TagOperatorAND},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// TestParseTagFilters_Regression tests for previously found bugs
func TestParseTagFilters_Regression(t *testing.T) {
	tests := []struct {
		name        string
		description string
		input       []string
		expected    []database.TagFilter
		expectError bool
	}{
		{
			name:  "Empty strings mixed with valid tags",
			input: []string{"", "region:usa", "", "lang:en", ""},
			expected: []database.TagFilter{
				{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: database.TagOperatorAND},
			},
			expectError: false,
			description: "Empty strings should be skipped",
		},
		{
			name:  "Only operator prefix with spaces",
			input: []string{"+   "},
			expected: []database.TagFilter{
				{Type: "", Value: "", Operator: database.TagOperatorAND},
			},
			expectError: true,
			description: "Operator with only spaces should error",
		},
		{
			name:  "Colon at end after operator",
			input: []string{"+region:"},
			expected: []database.TagFilter{
				{Type: "region", Value: "", Operator: database.TagOperatorAND},
			},
			expectError: true,
			description: "Operator with empty value should error",
		},
		{
			name:  "Multiple colons with operators - normalized",
			input: []string{"+url:https://example.com:8080"},
			expected: []database.TagFilter{
				{Type: "url", Value: "https:example-com:8080", Operator: database.TagOperatorAND},
			},
			expectError: false,
			description: "Multiple colons should parse correctly (slashes normalized to hyphens)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTagFilters(tt.input)
			if tt.expectError {
				require.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
				assert.ElementsMatch(t, tt.expected, result, tt.description)
			}
		})
	}
}
