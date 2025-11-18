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

// TestSlugify_CJKPreservation tests the intelligent hybrid slug generation
// that preserves CJK characters when appropriate while preferring ASCII for mixed titles.
func TestSlugify_CJKPreservation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Pure CJK titles - should be preserved
		{
			name:     "Japanese katakana only",
			input:    "ドラゴンクエスト",
			expected: "ドラゴンクエスト",
		},
		{
			name:     "Japanese hiragana only",
			input:    "どうぶつの森", //nolint:gosmopolitan // Japanese test data
			expected: "どうぶつの森", //nolint:gosmopolitan // Japanese test data
		},
		{
			name:     "Chinese characters only",
			input:    "街头霸王", //nolint:gosmopolitan // Chinese test data
			expected: "街头霸王", //nolint:gosmopolitan // Chinese test data
		},
		{
			name:     "Korean Hangul only",
			input:    "스트리트파이터",
			expected: "스트리트파이터",
		},

		// CJK with Roman numerals - numerals converted, CJK preserved
		{
			name:     "Japanese with Roman numeral",
			input:    "ファイナルファンタジーVII",
			expected: "ファイナルファンタジー7",
		},
		{
			name:     "Japanese with Roman numeral II",
			input:    "ドラゴンクエストII",
			expected: "ドラゴンクエスト2",
		},
		{
			name:     "Korean with Roman numeral",
			input:    "파이널판타지 VII",
			expected: "파이널판타지7",
		},

		// Mixed Latin + CJK - ASCII slug preferred (CJK stripped)
		{
			name:     "Mixed Latin and Japanese",
			input:    "Street Fighter ストリート",
			expected: "streetfighterストリート",
		},
		{
			name:     "Mixed Latin and Chinese",
			input:    "Super Mario 超级马里奥", //nolint:gosmopolitan // Chinese test data
			expected: "supermario超级马里奥",   //nolint:gosmopolitan // Chinese test data
		},
		{
			name:     "Mixed Latin and Korean",
			input:    "Zelda 젤다의전설",
			expected: "zelda젤다의전설",
		},
		{
			name:     "Latin dominant with CJK subtitle",
			input:    "Final Fantasy VII ファイナルファンタジー7",
			expected: "finalfantasy7ファイナルファンタジー7", // VII->7, both parts concatenated
		},

		// CJK with metadata - metadata stripped, CJK preserved
		{
			name:     "Japanese with region code",
			input:    "ドラゴンクエスト (Japan)",
			expected: "ドラゴンクエスト",
		},
		{
			name:     "Japanese with multiple metadata brackets",
			input:    "ファイナルファンタジー (Japan) [!]",
			expected: "ファイナルファンタジー",
		},
		{
			name:     "Korean with metadata",
			input:    "스트리트파이터 (Korea) [T+Eng]",
			expected: "스트리트파이터",
		},

		// Edge cases - short ASCII from Roman numerals
		{
			name:     "CJK title that slugifies to short ASCII (Roman numeral only)",
			input:    "III",
			expected: "3",
		},
		{
			name:     "Japanese title with only Roman numeral",
			input:    "VII",
			expected: "7",
		},

		// Width normalization - consistent fullwidth katakana
		{
			name:     "Fullwidth katakana preserved",
			input:    "ウェッジパンプス",
			expected: "ウェッジパンプス", // Already fullwidth, stays the same
		},
		{
			name:     "Fullwidth ASCII numbers",
			input:    "Game １２３",
			expected: "game123",
		},
		{
			name:     "Mixed fullwidth and halfwidth",
			input:    "Ａｂｃ123ＤＥＦ",
			expected: "abc123def",
		},

		// Pure Latin - should work as before
		{
			name:     "Pure Latin title",
			input:    "The Legend of Zelda",
			expected: "legendofzelda",
		},
		{
			name:     "Latin with metadata",
			input:    "Super Mario Bros (USA) [!]",
			expected: "supermariobrothers",
		},
		{
			name:     "Latin with Roman numerals",
			input:    "Final Fantasy VII",
			expected: "finalfantasy7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, "Slugify result mismatch")
		})
	}
}

// TestSlugify_WidthNormalization specifically tests fullwidth/halfwidth conversion
func TestSlugify_WidthNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Fullwidth Latin letters",
			input:    "ＡＢＣＤＥＦ",
			expected: "abcdef",
		},
		{
			name:     "Fullwidth numbers",
			input:    "１２３４５",
			expected: "12345",
		},
		{
			name:     "Fullwidth punctuation in title",
			input:    "Game：Subtitle",
			expected: "gamesubtitle",
		},
		{
			name:     "Mixed fullwidth and regular ASCII",
			input:    "Super Ｍario １２３",
			expected: "supermario123",
		},
		{
			name:     "Halfwidth katakana to fullwidth",
			input:    "ｳｴｯｼﾞ", // Halfwidth katakana
			expected: "ウエッジ",  // Becomes fullwidth katakana after width.Fold
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, "Width normalization failed")
		})
	}
}

// TestSlugify_MixedLanguageMatchingCompatibility tests that mixed-language
// titles produce predictable ASCII slugs for matching
func TestSlugify_MixedLanguageMatchingCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expected    string
		description string
	}{
		{
			name:        "CJK suffix concatenated for dual searchability",
			input:       "Super Mario Bros スーパーマリオ",
			expected:    "supermariobrothersスーパーマリオ",
			description: "Both Latin and CJK portions preserved - searchable by either",
		},
		{
			name:        "CJK prefix concatenated (order preserved)",
			input:       "スーパーマリオ Super Mario Bros",
			expected:    "スーパーマリオsupermariobrothers",
			description: "Order preserved - both parts searchable",
		},
		{
			name:        "Multiple CJK segments concatenated",
			input:       "Final 最终 Fantasy 幻想 VII", //nolint:gosmopolitan // Chinese test data
			expected:    "final最终fantasy幻想7",       //nolint:gosmopolitan // Chinese test data
			description: "All segments (Latin + CJK) concatenated with numerals converted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestNormalizeToWords_CJKSupport tests that word normalization works with CJK
func TestNormalizeToWords_CJKSupport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Pure Japanese with space",
			input:    "ドラゴン クエスト",
			expected: []string{"ドラゴン", "クエスト"},
		},
		{
			name:     "Pure Japanese no space",
			input:    "ドラゴンクエスト",
			expected: []string{"ドラゴンクエスト"}, // No spaces - treated as one word
		},
		{
			name:     "Mixed Latin and Japanese",
			input:    "Final Fantasy ファイナルファンタジー",
			expected: []string{"final", "fantasy", "ファイナルファンタジー"},
		},
		{
			name:     "Chinese with Latin",
			input:    "Super Mario 超级马里奥",                 //nolint:gosmopolitan // Chinese test data
			expected: []string{"super", "mario", "超级马里奥"}, //nolint:gosmopolitan // Chinese test data
		},
		{
			name:     "Korean with spaces",
			input:    "스트리트 파이터",
			expected: []string{"스트리트", "파이터"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeToWords(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizeToWords mismatch for CJK")
		})
	}
}

// TestSlugify_Idempotency ensures the function remains idempotent with CJK support
func TestSlugify_Idempotency_CJK(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"ドラゴンクエスト",
		"Super Mario Bros スーパーマリオ",
		"Final Fantasy VII",
		"ファイナルファンタジー7",
		"Game １２３",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			first := Slugify(MediaTypeGame, input)
			second := Slugify(MediaTypeGame, first)
			assert.Equal(t, first, second, "Slugify should be idempotent")
		})
	}
}
