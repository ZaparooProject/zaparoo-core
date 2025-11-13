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

package helpers

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokensEqual(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		a        *tokens.Token
		b        *tokens.Token
		name     string
		expected bool
	}{
		{
			name:     "both_nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "first_nil",
			a:        nil,
			b:        &tokens.Token{UID: "123", Text: "test"},
			expected: false,
		},
		{
			name:     "second_nil",
			a:        &tokens.Token{UID: "123", Text: "test"},
			b:        nil,
			expected: false,
		},
		{
			name: "identical_tokens",
			a: &tokens.Token{
				UID:  "123456",
				Text: "Mario Bros",
			},
			b: &tokens.Token{
				UID:  "123456",
				Text: "Mario Bros",
			},
			expected: true,
		},
		{
			name: "different_uids",
			a: &tokens.Token{
				UID:  "123456",
				Text: "Mario Bros",
			},
			b: &tokens.Token{
				UID:  "789012",
				Text: "Mario Bros",
			},
			expected: false,
		},
		{
			name: "different_text",
			a: &tokens.Token{
				UID:  "123456",
				Text: "Mario Bros",
			},
			b: &tokens.Token{
				UID:  "123456",
				Text: "Zelda",
			},
			expected: false,
		},
		{
			name: "different_uid_and_text",
			a: &tokens.Token{
				UID:  "123456",
				Text: "Mario Bros",
			},
			b: &tokens.Token{
				UID:  "789012",
				Text: "Zelda",
			},
			expected: false,
		},
		{
			name: "empty_uid_and_text",
			a: &tokens.Token{
				UID:  "",
				Text: "",
			},
			b: &tokens.Token{
				UID:  "",
				Text: "",
			},
			expected: true,
		},
		{
			name: "ignores_other_fields",
			a: &tokens.Token{
				ScanTime: baseTime,
				Type:     "nfc",
				UID:      "123456",
				Text:     "Mario Bros",
				Data:     "extra_data_a",
				Source:   "reader_a",
				FromAPI:  true,
				Unsafe:   false,
			},
			b: &tokens.Token{
				ScanTime: baseTime.Add(time.Hour), // Different time
				Type:     "optical",               // Different type
				UID:      "123456",                // Same UID
				Text:     "Mario Bros",            // Same Text
				Data:     "extra_data_b",          // Different data
				Source:   "reader_b",              // Different source
				FromAPI:  false,                   // Different FromAPI
				Unsafe:   true,                    // Different Unsafe
			},
			expected: true, // Only UID and Text matter
		},
		{
			name: "empty_vs_space_text",
			a: &tokens.Token{
				UID:  "123456",
				Text: "",
			},
			b: &tokens.Token{
				UID:  "123456",
				Text: " ",
			},
			expected: false,
		},
		{
			name: "whitespace_differences",
			a: &tokens.Token{
				UID:  "123456",
				Text: "Mario Bros",
			},
			b: &tokens.Token{
				UID:  "123456",
				Text: " Mario Bros ",
			},
			expected: false, // No trimming performed by TokensEqual
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := TokensEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result, "TokensEqual result mismatch")
		})
	}
}

func TestFilenameFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_filename",
			input:    "/games/mario.sfc",
			expected: "mario",
		},
		{
			name:     "no_extension",
			input:    "/games/mario",
			expected: "mario",
		},
		{
			name:     "multiple_dots",
			input:    "/games/mario.v1.2.rom",
			expected: "mario.v1.2",
		},
		{
			name:     "extension_with_space",
			input:    "/games/mario. sfc",
			expected: "mario. sfc", // HasSpace in extension means no extension removal
		},
		{
			name:     "filename_with_spaces",
			input:    "/games/Super Mario Bros.sfc",
			expected: "Super Mario Bros",
		},
		{
			name:     "current_directory",
			input:    "./mario.sfc",
			expected: "mario",
		},
		{
			name:     "nested_path",
			input:    "/home/user/roms/nes/mario.nes",
			expected: "mario",
		},
		{
			name:     "windows_style_path",
			input:    "C:/Games/mario.sfc",
			expected: "mario",
		},
		{
			name:     "empty_path",
			input:    "",
			expected: "",
		},
		{
			name:     "only_extension",
			input:    ".sfc",
			expected: "", // Base is ".sfc", extension is ".sfc", so result is empty
		},
		{
			name:     "double_extension",
			input:    "/games/backup.tar.gz",
			expected: "backup.tar", // Only removes .gz
		},
		{
			name:     "path_with_dots",
			input:    "/home/user.name/game.rom",
			expected: "game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FilenameFromPath(tt.input)
			assert.Equal(t, tt.expected, result, "FilenameFromPath result mismatch")
		})
	}
}

func TestIsTruthy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "true_lowercase",
			input:    "true",
			expected: true,
		},
		{
			name:     "true_uppercase",
			input:    "TRUE",
			expected: true,
		},
		{
			name:     "true_mixed_case",
			input:    "TrUe",
			expected: true,
		},
		{
			name:     "yes_lowercase",
			input:    "yes",
			expected: true,
		},
		{
			name:     "yes_uppercase",
			input:    "YES",
			expected: true,
		},
		{
			name:     "yes_mixed_case",
			input:    "YeS",
			expected: true,
		},
		{
			name:     "false_string",
			input:    "false",
			expected: false,
		},
		{
			name:     "no_string",
			input:    "no",
			expected: false,
		},
		{
			name:     "empty_string",
			input:    "",
			expected: false,
		},
		{
			name:     "random_string",
			input:    "maybe",
			expected: false,
		},
		{
			name:     "numeric_one",
			input:    "1",
			expected: false,
		},
		{
			name:     "numeric_zero",
			input:    "0",
			expected: false,
		},
		{
			name:     "whitespace_around_true",
			input:    " true ",
			expected: false, // No trimming in function
		},
		{
			name:     "partial_match",
			input:    "truthy",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsTruthy(tt.input)
			assert.Equal(t, tt.expected, result, "IsTruthy result mismatch")
		})
	}
}

func TestIsFalsey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "false_lowercase",
			input:    "false",
			expected: true,
		},
		{
			name:     "false_uppercase",
			input:    "FALSE",
			expected: true,
		},
		{
			name:     "false_mixed_case",
			input:    "FaLsE",
			expected: true,
		},
		{
			name:     "no_lowercase",
			input:    "no",
			expected: true,
		},
		{
			name:     "no_uppercase",
			input:    "NO",
			expected: true,
		},
		{
			name:     "no_mixed_case",
			input:    "No",
			expected: true,
		},
		{
			name:     "true_string",
			input:    "true",
			expected: false,
		},
		{
			name:     "yes_string",
			input:    "yes",
			expected: false,
		},
		{
			name:     "empty_string",
			input:    "",
			expected: false,
		},
		{
			name:     "random_string",
			input:    "maybe",
			expected: false,
		},
		{
			name:     "numeric_zero",
			input:    "0",
			expected: false,
		},
		{
			name:     "numeric_one",
			input:    "1",
			expected: false,
		},
		{
			name:     "whitespace_around_false",
			input:    " false ",
			expected: false, // No trimming in function
		},
		{
			name:     "partial_match",
			input:    "falsey",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsFalsey(tt.input)
			assert.Equal(t, tt.expected, result, "IsFalsey result mismatch")
		})
	}
}

func TestMaybeJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected bool
	}{
		{
			name:     "valid_json_object",
			input:    []byte(`{"key": "value"}`),
			expected: true,
		},
		{
			name:     "json_with_leading_spaces",
			input:    []byte(`   {"key": "value"}`),
			expected: true,
		},
		{
			name:     "json_with_leading_newline",
			input:    []byte("\n{\"key\": \"value\"}"),
			expected: true,
		},
		{
			name:     "json_with_leading_tab",
			input:    []byte("\t{\"key\": \"value\"}"),
			expected: true,
		},
		{
			name:     "json_with_leading_carriage_return",
			input:    []byte("\r{\"key\": \"value\"}"),
			expected: true,
		},
		{
			name:     "json_with_mixed_whitespace",
			input:    []byte(" \n\t\r {\"key\": \"value\"}"),
			expected: true,
		},
		{
			name:     "json_array_start",
			input:    []byte(`["item1", "item2"]`),
			expected: false, // Only checks for { start
		},
		{
			name:     "plain_text",
			input:    []byte("hello world"),
			expected: false,
		},
		{
			name:     "number_string",
			input:    []byte("123"),
			expected: false,
		},
		{
			name:     "empty_data",
			input:    []byte{},
			expected: false,
		},
		{
			name:     "nil_data",
			input:    nil,
			expected: false,
		},
		{
			name:     "only_whitespace",
			input:    []byte("   \n\t  "),
			expected: false,
		},
		{
			name:     "brace_in_middle",
			input:    []byte("text{json}"),
			expected: false,
		},
		{
			name:     "invalid_first_char",
			input:    []byte("x{\"key\": \"value\"}"),
			expected: false,
		},
		{
			name:     "single_brace",
			input:    []byte("{"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := MaybeJSON(tt.input)
			assert.Equal(t, tt.expected, result, "MaybeJSON result mismatch")
		})
	}
}

func TestContains(t *testing.T) {
	t.Parallel()

	t.Run("string_slice", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			item     string
			slice    []string
			expected bool
		}{
			{
				name:     "found_item",
				slice:    []string{"apple", "banana", "cherry"},
				item:     "banana",
				expected: true,
			},
			{
				name:     "not_found_item",
				slice:    []string{"apple", "banana", "cherry"},
				item:     "grape",
				expected: false,
			},
			{
				name:     "empty_slice",
				slice:    []string{},
				item:     "apple",
				expected: false,
			},
			{
				name:     "single_item_match",
				slice:    []string{"apple"},
				item:     "apple",
				expected: true,
			},
			{
				name:     "single_item_no_match",
				slice:    []string{"apple"},
				item:     "banana",
				expected: false,
			},
			{
				name:     "case_sensitive",
				slice:    []string{"Apple", "Banana"},
				item:     "apple",
				expected: false,
			},
			{
				name:     "empty_string_item",
				slice:    []string{"apple", "", "banana"},
				item:     "",
				expected: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := Contains(tt.slice, tt.item)
				assert.Equal(t, tt.expected, result, "Contains result mismatch")
			})
		}
	})

	t.Run("int_slice", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			slice    []int
			item     int
			expected bool
		}{
			{
				name:     "found_number",
				slice:    []int{1, 2, 3, 4, 5},
				item:     3,
				expected: true,
			},
			{
				name:     "not_found_number",
				slice:    []int{1, 2, 3, 4, 5},
				item:     6,
				expected: false,
			},
			{
				name:     "zero_value",
				slice:    []int{0, 1, 2},
				item:     0,
				expected: true,
			},
			{
				name:     "negative_numbers",
				slice:    []int{-3, -2, -1, 0, 1},
				item:     -2,
				expected: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result := Contains(tt.slice, tt.item)
				assert.Equal(t, tt.expected, result, "Contains result mismatch")
			})
		}
	})
}

func TestMapKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string]int
		expected []string
	}{
		{
			name:     "normal_map",
			input:    map[string]int{"apple": 1, "banana": 2, "cherry": 3},
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "empty_map",
			input:    map[string]int{},
			expected: []string{},
		},
		{
			name:     "single_key",
			input:    map[string]int{"single": 42},
			expected: []string{"single"},
		},
		{
			name:     "numeric_keys",
			input:    map[string]int{"1": 1, "2": 2, "10": 10},
			expected: []string{"1", "2", "10"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := MapKeys(tt.input)
			// Sort both slices for comparison since map iteration order is not guaranteed
			expected := make([]string, len(tt.expected))
			copy(expected, tt.expected)
			sort.Strings(expected)
			sort.Strings(result)
			assert.Equal(t, expected, result, "MapKeys result mismatch")
		})
	}
}

func TestAlphaMapKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string]int
		expected []string
	}{
		{
			name:     "unordered_keys",
			input:    map[string]int{"zebra": 1, "apple": 2, "banana": 3},
			expected: []string{"apple", "banana", "zebra"},
		},
		{
			name:     "already_sorted",
			input:    map[string]int{"apple": 1, "banana": 2, "cherry": 3},
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "empty_map",
			input:    map[string]int{},
			expected: []string{},
		},
		{
			name:     "single_key",
			input:    map[string]int{"single": 42},
			expected: []string{"single"},
		},
		{
			name:     "numeric_string_keys",
			input:    map[string]int{"10": 10, "2": 2, "1": 1},
			expected: []string{"1", "10", "2"}, // Lexicographic sort
		},
		{
			name:     "mixed_case",
			input:    map[string]int{"Zebra": 1, "apple": 2, "Banana": 3},
			expected: []string{"Banana", "Zebra", "apple"}, // ASCII sort
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := AlphaMapKeys(tt.input)
			assert.Equal(t, tt.expected, result, "AlphaMapKeys result mismatch")
		})
	}
}

func TestIsZip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "lowercase_zip",
			input:    "game.zip",
			expected: true,
		},
		{
			name:     "uppercase_zip",
			input:    "GAME.ZIP",
			expected: true,
		},
		{
			name:     "mixed_case_zip",
			input:    "Game.ZiP",
			expected: true,
		},
		{
			name:     "full_path_zip",
			input:    "/games/roms/mario.zip",
			expected: true,
		},
		{
			name:     "windows_path_zip",
			input:    "C:\\Games\\mario.zip",
			expected: true,
		},
		{
			name:     "not_zip_extension",
			input:    "game.rom",
			expected: false,
		},
		{
			name:     "no_extension",
			input:    "game",
			expected: false,
		},
		{
			name:     "empty_string",
			input:    "",
			expected: false,
		},
		{
			name:     "zip_in_filename_but_different_ext",
			input:    "zipfile.rom",
			expected: false,
		},
		{
			name:     "partial_zip_extension",
			input:    "game.zi",
			expected: false,
		},
		{
			name:     "multiple_dots",
			input:    "game.backup.zip",
			expected: true,
		},
		{
			name:     "just_zip_extension",
			input:    ".zip",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsZip(tt.input)
			assert.Equal(t, tt.expected, result, "IsZip result mismatch")
		})
	}
}

func TestIsValidExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Valid extensions
		{
			name:     "simple_extension",
			input:    ".zip",
			expected: true,
		},
		{
			name:     "extension_with_numbers",
			input:    ".mp3",
			expected: true,
		},
		{
			name:     "uppercase_extension",
			input:    ".ZIP",
			expected: true,
		},
		{
			name:     "mixed_case",
			input:    ".TaR",
			expected: true,
		},
		{
			name:     "all_numbers",
			input:    ".264",
			expected: true,
		},
		{
			name:     "long_extension",
			input:    ".jpeg",
			expected: true,
		},

		// Invalid extensions
		{
			name:     "extension_with_space",
			input:    ".tar gz",
			expected: false,
		},
		{
			name:     "extension_with_hyphen",
			input:    ".tar-gz",
			expected: false,
		},
		{
			name:     "extension_with_underscore",
			input:    ".tar_gz",
			expected: false,
		},
		{
			name:     "extension_with_special_char",
			input:    ".tar!gz",
			expected: false,
		},
		{
			name:     "just_dot",
			input:    ".",
			expected: false,
		},
		{
			name:     "empty_string",
			input:    "",
			expected: false,
		},
		{
			name:     "no_leading_dot",
			input:    "zip",
			expected: true, // Still valid, just checks alphanumeric
		},
		{
			name:     "space_at_start",
			input:    ". zip",
			expected: false,
		},
		{
			name:     "space_at_end",
			input:    ".zip ",
			expected: false,
		},
		{
			name:     "multiple_dots",
			input:    ".tar.gz", // path.Ext returns ".gz", which is valid
			expected: false,     // But this has a dot in the middle
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsValidExtension(tt.input)
			assert.Equal(t, tt.expected, result, "IsValidExtension result mismatch")
		})
	}
}

func TestRandSeq(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		length int
	}{
		{
			name:   "zero_length",
			length: 0,
		},
		{
			name:   "single_character",
			length: 1,
		},
		{
			name:   "small_string",
			length: 5,
		},
		{
			name:   "medium_string",
			length: 20,
		},
		{
			name:   "large_string",
			length: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := RandSeq(tt.length)
			require.NoError(t, err, "RandSeq should not return error")

			// Check length
			assert.Len(t, result, tt.length, "RandSeq length mismatch")

			// Check all characters are letters
			for _, ch := range result {
				assert.True(t, (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z'),
					"RandSeq contains non-letter character: %c", ch)
			}
		})
	}

	// Test randomness by checking multiple calls produce different results
	t.Run("randomness_test", func(t *testing.T) {
		t.Parallel()
		const iterations = 10
		const length = 10
		results := make(map[string]bool)

		for range iterations {
			result, err := RandSeq(length)
			require.NoError(t, err, "RandSeq should not return error")
			results[result] = true
		}

		// Should have multiple unique results (not all the same)
		assert.Greater(t, len(results), 1, "RandSeq should produce different results")
	})
}

func TestSlugifyPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_path",
			input:    "/games/mario.sfc",
			expected: "mario",
		},
		{
			name:     "path_with_spaces",
			input:    "/games/Super Mario Bros.sfc",
			expected: "supermariobrothers",
		},
		{
			name:     "path_with_parentheses",
			input:    "/roms/Mario Bros (USA).nes",
			expected: "mariobrothers",
		},
		{
			name:     "path_with_brackets",
			input:    "/games/Zelda [Rev 1].sfc",
			expected: "zelda",
		},
		{
			name:     "windows_path",
			input:    "C:\\Games\\Street Fighter II.rom",
			expected: "streetfighter2",
		},
		{
			name:     "path_with_multiple_extensions",
			input:    "/backup/game.backup.tar.gz",
			expected: "gamebackuptar",
		},
		{
			name:     "path_with_special_chars",
			input:    "/games/Final-Fantasy_VII!.iso",
			expected: "finalfantasy7",
		},
		{
			name:     "empty_path",
			input:    "",
			expected: "",
		},
		{
			name:     "path_with_numbers",
			input:    "/games/Mega Man 2.nes",
			expected: "megaman2",
		},
		{
			name:     "extension_with_space",
			input:    "/games/test. ext",
			expected: "testext", // Space is removed by SlugifyString
		},
		{
			name:     "hidden_file",
			input:    "/home/user/.hidden",
			expected: "",
		},
		{
			name:     "complex_nested_path",
			input:    "/media/user/drive/games/nintendo/snes/Super Mario World (USA) [!].smc",
			expected: "supermarioworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SlugifyPath(tt.input)
			assert.Equal(t, tt.expected, result, "SlugifyPath result mismatch")
		})
	}
}

func TestRandomElem(t *testing.T) {
	t.Parallel()

	t.Run("string_slice", func(t *testing.T) {
		t.Parallel()

		// Test with various slice sizes
		tests := []struct {
			name    string
			slice   []string
			wantErr bool
		}{
			{
				name:    "empty_slice",
				slice:   []string{},
				wantErr: true,
			},
			{
				name:    "single_element",
				slice:   []string{"only"},
				wantErr: false,
			},
			{
				name:    "multiple_elements",
				slice:   []string{"apple", "banana", "cherry", "date"},
				wantErr: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				result, err := RandomElem(tt.slice)

				if tt.wantErr {
					require.Error(t, err, "RandomElem should return error for empty slice")
					assert.Equal(t, "empty slice", err.Error())
				} else {
					require.NoError(t, err)
					assert.Contains(t, tt.slice, result, "RandomElem result should be from the slice")
				}
			})
		}
	})

	t.Run("int_slice", func(t *testing.T) {
		t.Parallel()

		slice := []int{10, 20, 30, 40, 50}
		result, err := RandomElem(slice)
		require.NoError(t, err)
		assert.Contains(t, slice, result, "RandomElem result should be from the slice")
	})

	t.Run("struct_slice", func(t *testing.T) {
		t.Parallel()

		type testStruct struct {
			Name string
			ID   int
		}

		slice := []testStruct{
			{"first", 1},
			{"second", 2},
			{"third", 3},
		}

		result, err := RandomElem(slice)
		require.NoError(t, err)

		found := false
		for _, item := range slice {
			if item.ID == result.ID && item.Name == result.Name {
				found = true
				break
			}
		}
		assert.True(t, found, "RandomElem result should be from the slice")
	})

	t.Run("distribution_test", func(t *testing.T) {
		t.Parallel()

		// Test that all elements can be selected (not always the same)
		slice := []string{"a", "b", "c", "d", "e"}
		selected := make(map[string]bool)

		// Run multiple times to ensure different elements are selected
		for range 50 {
			result, err := RandomElem(slice)
			require.NoError(t, err)
			selected[result] = true

			// If we've seen at least 2 different results, that's good enough
			if len(selected) >= 2 {
				break
			}
		}

		assert.Greater(t, len(selected), 1, "RandomElem should select different elements over multiple calls")
	})
}

func TestGetMd5Hash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		expectedHash string
		wantErr      bool
	}{
		{
			name:    "regular_file",
			path:    "testdata/test.txt",
			wantErr: false,
		},
		{
			name:         "empty_file",
			path:         "testdata/empty.txt",
			expectedHash: "d41d8cd98f00b204e9800998ecf8427e", // MD5 of empty file
			wantErr:      false,
		},
		{
			name:         "non_existent_file",
			path:         "testdata/nonexistent.txt",
			expectedHash: "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := GetMd5Hash(tt.path)

			if tt.wantErr {
				require.Error(t, err, "GetMd5Hash should return error for non-existent file")
				assert.Contains(t, err.Error(), "failed to open file for MD5 hash")
			} else {
				require.NoError(t, err)
				if tt.name == "regular_file" {
					// For regular file, expect consistent LF line endings due to .gitattributes
					// MD5 of "Hello, World!\nThis is a test file." (34 bytes, LF)
					expectedHash := "371514d9ec1b09c42d7c924ccb009c0d"
					assert.Equal(t, expectedHash, result, "GetMd5Hash result mismatch")
				} else {
					assert.Equal(t, tt.expectedHash, result, "GetMd5Hash result mismatch")
				}
			}
		})
	}
}

func TestGetFileSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		expectedSize int64
		wantErr      bool
	}{
		{
			name:         "regular_file",
			path:         "testdata/test.txt",
			expectedSize: 34, // Length of "Hello, World!\nThis is a test file." with LF due to .gitattributes
			wantErr:      false,
		},
		{
			name:         "empty_file",
			path:         "testdata/empty.txt",
			expectedSize: 0,
			wantErr:      false,
		},
		{
			name:         "non_existent_file",
			path:         "testdata/nonexistent.txt",
			expectedSize: 0,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := GetFileSize(tt.path)

			if tt.wantErr {
				require.Error(t, err, "GetFileSize should return error for non-existent file")
				assert.Contains(t, err.Error(), "failed to open file for size check")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedSize, result, "GetFileSize result mismatch")
			}
		})
	}
}

func TestListZip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		expectedFiles []string
		wantErr       bool
	}{
		{
			name: "regular_zip",
			path: "testdata/test.zip",
			expectedFiles: []string{
				"file1.txt",
				"file2.txt",
				"subdir/",
				"subdir/file3.txt",
			},
			wantErr: false,
		},
		{
			name:          "empty_zip",
			path:          "testdata/empty.zip",
			expectedFiles: []string{},
			wantErr:       false,
		},
		{
			name:          "non_existent_zip",
			path:          "testdata/nonexistent.zip",
			expectedFiles: nil,
			wantErr:       true,
		},
		{
			name:          "non_zip_file",
			path:          "testdata/test.txt",
			expectedFiles: nil,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ListZip(tt.path)

			if tt.wantErr {
				require.Error(t, err, "ListZip should return error for invalid zip file")
				assert.Contains(t, err.Error(), "failed to open zip file")
			} else {
				require.NoError(t, err)
				assert.ElementsMatch(t, tt.expectedFiles, result, "ListZip result mismatch")
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test outputs
	tempDir := t.TempDir()

	tests := []struct {
		checkFunc  func(t *testing.T, destPath string)
		name       string
		sourcePath string
		destPath   string
		wantErr    bool
	}{
		{
			checkFunc: func(t *testing.T, destPath string) {
				// Verify file exists and content matches
				content, err := os.ReadFile(destPath) //nolint:gosec // Test file with controlled path
				require.NoError(t, err)
				// Normalize line endings for cross-platform compatibility
				normalizedContent := strings.ReplaceAll(string(content), "\r\n", "\n")
				assert.Equal(t, "Hello, World!\nThis is a test file.", normalizedContent)
			},
			name:       "copy_regular_file",
			sourcePath: "testdata/test.txt",
			destPath:   tempDir + "/copy_test.txt",
			wantErr:    false,
		},
		{
			checkFunc: func(t *testing.T, destPath string) {
				// Verify empty file exists
				info, err := os.Stat(destPath)
				require.NoError(t, err)
				assert.Equal(t, int64(0), info.Size())
			},
			name:       "copy_empty_file",
			sourcePath: "testdata/empty.txt",
			destPath:   tempDir + "/copy_empty.txt",
			wantErr:    false,
		},
		{
			checkFunc: func(t *testing.T, destPath string) {
				// First create a file to overwrite
				err := os.WriteFile(destPath, []byte("old content"), 0o600)
				require.NoError(t, err)

				// Copy should overwrite
				err = CopyFile("testdata/test.txt", destPath)
				require.NoError(t, err)

				// Verify new content
				content, err := os.ReadFile(destPath) //nolint:gosec // Test file with controlled path
				require.NoError(t, err)
				// Normalize line endings for cross-platform compatibility
				normalizedContent := strings.ReplaceAll(string(content), "\r\n", "\n")
				assert.Equal(t, "Hello, World!\nThis is a test file.", normalizedContent)
			},
			name:       "overwrite_existing_file",
			sourcePath: "testdata/test.txt",
			destPath:   tempDir + "/overwrite.txt",
			wantErr:    false,
		},
		{
			checkFunc:  nil,
			name:       "source_file_not_exist",
			sourcePath: "testdata/nonexistent.txt",
			destPath:   tempDir + "/dest.txt",
			wantErr:    true,
		},
		{
			checkFunc:  nil,
			name:       "dest_directory_not_exist",
			sourcePath: "testdata/test.txt",
			destPath:   tempDir + "/nonexistent/dest.txt",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.checkFunc != nil && tt.name == "overwrite_existing_file" {
				// Special handling for overwrite test
				tt.checkFunc(t, tt.destPath)
				return
			}

			err := CopyFile(tt.sourcePath, tt.destPath)

			if tt.wantErr {
				require.Error(t, err, "CopyFile should return error")
				assert.Contains(t, err.Error(), "failed to")
			} else {
				assert.NoError(t, err)
				if tt.checkFunc != nil {
					tt.checkFunc(t, tt.destPath)
				}
			}
		})
	}
}

func TestCopyFileWithPermissions(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	tests := []struct {
		name       string
		sourcePath string
		destPath   string
		perm       os.FileMode
		wantErr    bool
	}{
		{
			name:       "set_executable_permissions",
			sourcePath: "testdata/test.txt",
			destPath:   tempDir + "/executable.txt",
			perm:       0o755,
			wantErr:    false,
		},
		{
			name:       "set_readonly_permissions",
			sourcePath: "testdata/test.txt",
			destPath:   tempDir + "/readonly.txt",
			perm:       0o444,
			wantErr:    false,
		},
		{
			name:       "default_permissions_when_not_specified",
			sourcePath: "testdata/test.txt",
			destPath:   tempDir + "/default.txt",
			perm:       0, // No permission specified
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var err error
			if tt.perm == 0 {
				// Test default permissions
				err = CopyFile(tt.sourcePath, tt.destPath)
			} else {
				// Test with specified permissions
				err = CopyFile(tt.sourcePath, tt.destPath, tt.perm)
			}

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify file exists
				info, err := os.Stat(tt.destPath)
				require.NoError(t, err)

				// Check permissions match expected
				if tt.perm != 0 {
					assert.Equal(t, tt.perm, info.Mode().Perm(), "permissions should match")
				} else {
					// Default should be 0644
					assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "default permissions should be 0644")
				}
			}
		})
	}
}

func TestGetAllLocalIPs(t *testing.T) {
	t.Parallel()

	// Test GetAllLocalIPs returns all IPs
	ips := GetAllLocalIPs()

	// Should return a slice (could be empty on systems with no private IPs)
	assert.NotNil(t, ips, "GetAllLocalIPs should never return nil")

	// If GetLocalIP returns something, GetAllLocalIPs should include it
	singleIP := GetLocalIP()
	if singleIP != "" {
		assert.Contains(t, ips, singleIP, "GetAllLocalIPs should contain the IP returned by GetLocalIP")
		assert.GreaterOrEqual(t, len(ips), 1, "GetAllLocalIPs should return at least one IP if GetLocalIP works")
	}

	// All returned IPs should be valid private IPs
	for _, ip := range ips {
		assert.NotEmpty(t, ip, "No IP in the list should be empty")
		// Basic IP format check
		parts := strings.Split(ip, ".")
		assert.Len(t, parts, 4, "IP should have 4 octets: %s", ip)
	}

	// Should not have duplicates
	unique := make(map[string]bool)
	for _, ip := range ips {
		assert.False(t, unique[ip], "GetAllLocalIPs should not return duplicate IPs")
		unique[ip] = true
	}
}

func TestCreateVirtualPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   string
		id       string
		pathName string
		expected string
	}{
		{
			name:     "simple_name",
			scheme:   "kodi-movie",
			id:       "123",
			pathName: "The Matrix",
			expected: "kodi-movie://123/The%20Matrix",
		},
		{
			name:     "name_with_slash",
			scheme:   "kodi-show",
			id:       "456",
			pathName: "Some Hot/Cold",
			expected: "kodi-show://456/Some%20Hot%2FCold",
		},
		{
			name:     "alphanumeric_id",
			scheme:   "scummvm",
			id:       "monkey1",
			pathName: "Monkey Island",
			expected: "scummvm://monkey1/Monkey%20Island",
		},
		{
			name:     "id_with_special_chars",
			scheme:   "launchbox",
			id:       "game-id_123",
			pathName: "Game Title",
			expected: "launchbox://game-id_123/Game%20Title",
		},
		{
			name:     "id_with_space",
			scheme:   "steam",
			id:       "space id",
			pathName: "Game Name",
			expected: "steam://space%20id/Game%20Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := CreateVirtualPath(tt.scheme, tt.id, tt.pathName)
			assert.Equal(t, tt.expected, result)

			// Verify round-trip: create path, parse it back
			parsed, err := ParseVirtualPathStr(result)
			require.NoError(t, err, "Should parse created path without error")
			assert.Equal(t, tt.scheme, parsed.Scheme, "Scheme should match")
			assert.Equal(t, tt.id, parsed.ID, "ID should match after round-trip")
			assert.Equal(t, tt.pathName, parsed.Name, "Name should match after round-trip")
		})
	}
}

func TestFilenameFromPath_VirtualPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "kodi_show_with_slash",
			path:     "kodi-show://456/Some%20Hot%2FCold",
			expected: "Some Hot/Cold",
		},
		{
			name:     "regular_file_path",
			path:     "/home/user/Games/Super Mario Bros.nes",
			expected: "Super Mario Bros",
		},
		{
			name:     "empty_path",
			path:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FilenameFromPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEqualStringSlices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "equal_slices",
			a:        []string{"apple", "banana", "cherry"},
			b:        []string{"apple", "banana", "cherry"},
			expected: true,
		},
		{
			name:     "equal_slices_different_order",
			a:        []string{"apple", "banana", "cherry"},
			b:        []string{"cherry", "apple", "banana"},
			expected: true,
		},
		{
			name:     "different_length",
			a:        []string{"apple", "banana"},
			b:        []string{"apple", "banana", "cherry"},
			expected: false,
		},
		{
			name:     "different_content",
			a:        []string{"apple", "banana"},
			b:        []string{"apple", "orange"},
			expected: false,
		},
		{
			name:     "empty_slices",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "one_empty_one_not",
			a:        []string{"apple"},
			b:        []string{},
			expected: false,
		},
		{
			name:     "nil_slices",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "one_nil_one_empty",
			a:        nil,
			b:        []string{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := EqualStringSlices(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}
