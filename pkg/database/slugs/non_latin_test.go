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

func TestSlugify_NonLatinCharacters(t *testing.T) {
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
			input:    "街头霸王", //nolint:gosmopolitan // Chinese test data
			expected: "街头霸王", //nolint:gosmopolitan // Chinese test data
		},
		{
			name:     "Korean characters only",
			input:    "스트리트파이터",
			expected: "스트리트파이터", // CJK preserved
		},
		{
			name:     "Arabic characters only",
			input:    "سوبر ماريو",
			expected: "سوبرماريو", // Arabic preserved with multi-script support
		},
		{
			name:     "Cyrillic characters only",
			input:    "Супер Марио",
			expected: "супермарио", // Cyrillic preserved and lowercased
		},
		{
			name:     "Greek characters only",
			input:    "Σούπερ Μάριο",
			expected: "σουπερμαριο", // Greek preserved, lowercased, diacritics removed
		},
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
			expected: "fussballmanager", // ß → ss
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
			expected: "abenkokken", // Å → A, Ø → O
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
			expected: "lodzslask", // Ł → L
		},
		{
			name:     "Czech characters",
			input:    "Václav Havel",
			expected: "vaclavhavel",
		},
		{
			name:     "Mixed non-Latin with metadata",
			input:    "Super Mario 超级 (USA) [!]", //nolint:gosmopolitan // Chinese test data
			expected: "supermario超级",             //nolint:gosmopolitan // Chinese test data
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
			expected: "supermariobrothers",
		},
		{
			name:     "Mathematical symbols",
			input:    "Game ∞ + ∑ Edition",
			expected: "gameand",
		},
		{
			name:     "Mixed scripts complex",
			input:    "The Zelda 传说 ストリート: Link's Awakening", //nolint:gosmopolitan // Chinese test data
			expected: "zelda传说ストリートlinksawakening",           //nolint:gosmopolitan // Chinese test data
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
