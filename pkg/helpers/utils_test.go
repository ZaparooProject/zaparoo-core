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
	"sort"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
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

func TestSlugifyString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal_string",
			input:    "Mario Bros",
			expected: "mariobros",
		},
		{
			name:     "with_parentheses",
			input:    "Super Mario Bros (USA)",
			expected: "supermariobros",
		},
		{
			name:     "with_brackets",
			input:    "Zelda [Link's Awakening]",
			expected: "zelda",
		},
		{
			name:     "with_special_chars",
			input:    "Street Fighter II: Championship Edition!",
			expected: "streetfighteriichampionshipedition",
		},
		{
			name:     "numbers_preserved",
			input:    "Final Fantasy VII (Disc 1)",
			expected: "finalfantasyvii",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "only_special_chars",
			input:    "!@#$%^&*()",
			expected: "",
		},
		{
			name:     "mixed_case_letters",
			input:    "SuPeR mArIo BrOs",
			expected: "supermariobros",
		},
		{
			name:     "multiple_spaces",
			input:    "Mario    Bros   3",
			expected: "mariobros3",
		},
		{
			name:     "nested_parentheses",
			input:    "Game (Version (Final))",
			expected: "game",
		},
		{
			name:     "mixed_brackets_and_parens",
			input:    "Game [USA] (Rev 1)",
			expected: "game",
		},
		{
			name:     "underscore_and_dash",
			input:    "super_mario-bros",
			expected: "supermariobros",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SlugifyString(tt.input)
			assert.Equal(t, tt.expected, result, "SlugifyString result mismatch")
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

func TestHasSpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "no_spaces",
			input:    "hello",
			expected: false,
		},
		{
			name:     "single_space_middle",
			input:    "hello world",
			expected: true,
		},
		{
			name:     "space_at_start",
			input:    " hello",
			expected: true,
		},
		{
			name:     "space_at_end",
			input:    "hello ",
			expected: true,
		},
		{
			name:     "multiple_spaces",
			input:    "hello   world",
			expected: true,
		},
		{
			name:     "only_spaces",
			input:    "   ",
			expected: true,
		},
		{
			name:     "empty_string",
			input:    "",
			expected: false,
		},
		{
			name:     "single_character",
			input:    "a",
			expected: false,
		},
		{
			name:     "special_chars_no_space",
			input:    "hello-world_test",
			expected: false,
		},
		{
			name:     "special_chars_with_space",
			input:    "hello-world test",
			expected: true,
		},
		{
			name:     "tab_character",
			input:    "hello\tworld",
			expected: false, // Only checks for space character, not tab
		},
		{
			name:     "newline_character",
			input:    "hello\nworld",
			expected: false, // Only checks for space character, not newline
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := HasSpace(tt.input)
			assert.Equal(t, tt.expected, result, "HasSpace result mismatch")
		})
	}
}
