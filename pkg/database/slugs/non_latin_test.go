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

func TestSlugifyString_NonLatinCharacters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Japanese katakana only",
			input:    "ストリートファイター",
			expected: "ストリートファイター", // CJK preserved
		},
		{
			name:     "Chinese characters only",
			input:    "街头霸王", //nolint:gosmopolitan // Intentionally testing Chinese character handling
			expected: "街头霸王", // CJK preserved
		},
		{
			name:     "Korean characters only",
			input:    "스트리트파이터",
			expected: "스트리트파이터", // CJK preserved
		},
		{
			name:     "Arabic characters only",
			input:    "سوبر ماريو",
			expected: "", // Arabic stripped (not in CJK category)
		},
		{
			name:     "Cyrillic characters only",
			input:    "Супер Марио",
			expected: "", // Cyrillic stripped (not in CJK category)
		},
		{
			name:     "Greek characters only",
			input:    "Σούπερ Μάριο",
			expected: "", // Greek stripped (not in CJK category)
		},
		{
			name:     "Mixed Latin and Japanese",
			input:    "Street Fighter ストリート",
			expected: "streetfighter",
		},
		{
			name:     "Mixed Latin and Chinese",
			input:    "Super Mario 超级马里奥", //nolint:gosmopolitan // Intentionally testing mixed Chinese
			expected: "supermario",
		},
		{
			name:     "Mixed Latin and Korean",
			input:    "Zelda 젤다의 전설",
			expected: "zelda",
		},
		{
			name:     "Accented characters normalized",
			input:    "Pokémon",
			expected: "pokemon",
		},
		{
			name:     "Mixed accents and regular",
			input:    "Café Racer",
			expected: "caferacer",
		},
		{
			name:     "German umlauts",
			input:    "Fußball Manager",
			expected: "fuballmanager",
		},
		{
			name:     "Spanish accents",
			input:    "José García",
			expected: "josegarcia",
		},
		{
			name:     "French accents mixed",
			input:    "Château d'Ivoire",
			expected: "chateaudivoire",
		},
		{
			name:     "Nordic characters",
			input:    "Åben Køkken",
			expected: "abenkkken",
		},
		{
			name:     "Turkish characters",
			input:    "Şehir Turu",
			expected: "sehirturu",
		},
		{
			name:     "Vietnamese tones",
			input:    "Nguyễn Phương",
			expected: "nguyenphuong",
		},
		{
			name:     "Polish characters",
			input:    "Łódź Śląsk",
			expected: "odzslask",
		},
		{
			name:     "Czech characters",
			input:    "Václav Havel",
			expected: "vaclavhavel",
		},
		{
			name:     "Mixed non-Latin with metadata",
			input:    "Super Mario 超级 (USA) [!]", //nolint:gosmopolitan // Intentionally testing Chinese with metadata
			expected: "supermario",
		},
		{
			name:     "Japanese with Roman numerals",
			input:    "ファイナルファンタジー VII",
			expected: "ファイナルファンタジー7", // CJK preserved, Roman numeral converted
		},
		{
			name:     "Emoji characters only",
			input:    "🎮🎯🏆",
			expected: "",
		},
		{
			name:     "Mixed emoji and Latin",
			input:    "Super 🎮 Mario Bros",
			expected: "supermariobros",
		},
		{
			name:     "Mathematical symbols",
			input:    "Game ∞ + ∑ Edition",
			expected: "gameand",
		},
		{
			name:     "Mixed scripts complex",
			input:    "The Zelda 传说 ストリート: Link's Awakening", //nolint:gosmopolitan // Testing mixed scripts
			expected: "zeldalinksawakening",
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
