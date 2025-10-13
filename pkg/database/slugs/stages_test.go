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

package slugs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeWidth tests Stage 1 of the normalization pipeline
func TestNormalizeWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "fullwidth ASCII letters",
			input:    "ＡＢＣＤＥＦ",
			expected: "ABCDEF",
		},
		{
			name:     "fullwidth numbers",
			input:    "１２３４５",
			expected: "12345",
		},
		{
			name:     "halfwidth katakana to fullwidth",
			input:    "ｳｴｯｼﾞ",
			expected: "ウエッジ",
		},
		{
			name:     "mixed fullwidth ASCII and normal",
			input:    "Super Ｍario １２３",
			expected: "Super Mario 123",
		},
		{
			name:     "fullwidth spaces",
			input:    "Game　Title",
			expected: "Game Title",
		},
		{
			name:     "pure ASCII unchanged",
			input:    "Super Mario Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "fullwidth punctuation",
			input:    "Game！？",
			expected: "Game!?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeWidth(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeUnicode tests Stage 2 of the normalization pipeline
func TestNormalizeUnicode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "trademark symbol removal",
			input:    "Sonic™",
			expected: "Sonic",
		},
		{
			name:     "copyright symbol removal",
			input:    "Game©",
			expected: "Game",
		},
		{
			name:     "currency symbols removal",
			input:    "Price$100€50¥1000",
			expected: "Price100501000",
		},
		{
			name:     "diacritics removal (Latin)",
			input:    "Pokémon",
			expected: "Pokemon",
		},
		{
			name:     "multiple diacritics",
			input:    "Café Münchën",
			expected: "Cafe Munchen",
		},
		{
			name:     "ligatures normalization",
			input:    "ﬁnal ﬂight",
			expected: "final flight",
		},
		{
			name:     "CJK preserved",
			input:    "ドラゴンクエスト",
			expected: "ドラゴンクエスト",
		},
		{
			name:     "mixed Latin diacritics and CJK",
			input:    "Pokémon ポケモン",
			expected: "Pokémon ポケモン", // CJK present uses NFC, preserves diacritics
		},
		{
			name:     "pure ASCII unchanged (fast path)",
			input:    "Super Mario Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Cyrillic preserved",
			input:    "Тетрис",
			expected: "Тетрис",
		},
		{
			name:     "Arabic preserved",
			input:    "العاب",
			expected: "العاب",
		},
		{
			name:     "Hebrew preserved",
			input:    "משחק",
			expected: "משחק",
		},
		{
			name:     "multiple symbols",
			input:    "Game™©®",
			expected: "Game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeUnicode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStripTrailingArticle tests Stage 4 of the normalization pipeline
func TestStripTrailingArticle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple trailing article",
			input:    "Legend, The",
			expected: "Legend",
		},
		{
			name:     "trailing article with space",
			input:    "Mega Man, The",
			expected: "Mega Man",
		},
		{
			name:     "case insensitive",
			input:    "Story, the",
			expected: "Story",
		},
		{
			name:     "trailing article before colon",
			input:    "Game, The:",
			expected: "Game:",
		},
		{
			name:     "trailing article before dash",
			input:    "Title, The-",
			expected: "Title-",
		},
		{
			name:     "trailing article before parenthesis",
			input:    "Movie, The(",
			expected: "Movie(",
		},
		{
			name:     "trailing article before bracket",
			input:    "Series, The[",
			expected: "Series[",
		},
		{
			name:     "no trailing article",
			input:    "Super Mario Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "comma without article",
			input:    "Game, Part 2",
			expected: "Game, Part 2",
		},
		{
			name:     "article before separator",
			input:    "The Legend, The Best",
			expected: "The Legend Best",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := StripTrailingArticle(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStageIdempotence tests that each stage is idempotent
func TestStageIdempotence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stageFn  func(string) string
		input    string
		expected string
	}{
		{
			name:     "NormalizeWidth idempotent",
			stageFn:  NormalizeWidth,
			input:    "ＡＢＣＤＥＦ",
			expected: "ABCDEF",
		},
		{
			name:     "NormalizeUnicode idempotent",
			stageFn:  NormalizeUnicode,
			input:    "Pokémon™",
			expected: "Pokemon",
		},
		{
			name:     "StripTrailingArticle idempotent",
			stageFn:  StripTrailingArticle,
			input:    "Legend, The",
			expected: "Legend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Apply once
			result1 := tt.stageFn(tt.input)
			assert.Equal(t, tt.expected, result1)

			// Apply again to verify idempotence
			result2 := tt.stageFn(result1)
			assert.Equal(t, result1, result2, "Stage should be idempotent: f(f(x)) == f(x)")
		})
	}
}

// TestStageComposition tests that stages can be composed in sequence
func TestStageComposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "width + unicode normalization",
			input:    "Ｐokémon",
			expected: "Pokemon",
		},
		{
			name:     "unicode + trailing article",
			input:    "Légend, The™",
			expected: "Legend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Apply stages in sequence
			result := tt.input
			result = NormalizeWidth(result)
			result = NormalizeUnicode(result)
			result = StripTrailingArticle(result)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStageEdgeCases tests edge cases for all stages
func TestStageEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty strings", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, NormalizeWidth(""))
		assert.Empty(t, NormalizeUnicode(""))
		assert.Empty(t, StripTrailingArticle(""))
	})

	t.Run("whitespace only", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, " ", NormalizeWidth(" "))
		assert.Equal(t, " ", NormalizeUnicode(" "))
		assert.Equal(t, "   ", StripTrailingArticle("   ")) // No match, returns as-is
	})

	t.Run("very long strings", func(t *testing.T) {
		t.Parallel()
		longInput := "ＡＢＣＤＥＦ" + "X" + string(make([]byte, 1000))
		result := NormalizeWidth(longInput)
		assert.NotEmpty(t, result)
	})

	t.Run("special unicode ranges", func(t *testing.T) {
		t.Parallel()
		// Emoji (should be handled gracefully)
		assert.NotEmpty(t, NormalizeUnicode("Game 🎮"))

		// Zero-width characters
		assert.NotEmpty(t, NormalizeUnicode("Game\u200bTitle")) // Zero-width space
	})
}
