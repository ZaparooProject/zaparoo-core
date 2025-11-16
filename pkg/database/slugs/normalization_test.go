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

// TestNormalizeOrdinals tests the ordinal number normalization functionality.
// Ordinals (1st, 2nd, 3rd, 4th, etc.) should be converted to their base numbers (1, 2, 3, 4).
// This allows "Street Fighter 2nd Impact" and "Street Fighter II Impact" to both
// normalize to "street fighter 2 impact".
func TestNormalizeOrdinals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic ordinals
		{
			name:     "first",
			input:    "1st Place",
			expected: "1 Place",
		},
		{
			name:     "second",
			input:    "2nd Edition",
			expected: "2 Edition",
		},
		{
			name:     "third",
			input:    "3rd Strike",
			expected: "3 Strike",
		},
		{
			name:     "fourth",
			input:    "4th Dimension",
			expected: "4 Dimension",
		},
		{
			name:     "fifth",
			input:    "5th Element",
			expected: "5 Element",
		},
		{
			name:     "tenth",
			input:    "10th Anniversary",
			expected: "10 Anniversary",
		},
		{
			name:     "eleventh",
			input:    "11th Hour",
			expected: "11 Hour",
		},
		{
			name:     "twelfth",
			input:    "12th Man",
			expected: "12 Man",
		},
		{
			name:     "thirteenth",
			input:    "13th Floor",
			expected: "13 Floor",
		},
		{
			name:     "twenty_first",
			input:    "21st Century",
			expected: "21 Century",
		},
		{
			name:     "twenty_second",
			input:    "22nd Street",
			expected: "22 Street",
		},
		{
			name:     "twenty_third",
			input:    "23rd Precinct",
			expected: "23 Precinct",
		},
		{
			name:     "hundredth",
			input:    "100th Game",
			expected: "100 Game",
		},

		// Game-specific examples
		{
			name:     "street_fighter_2nd_impact",
			input:    "Street Fighter 2nd Impact",
			expected: "Street Fighter 2 Impact",
		},
		{
			name:     "super_mario_bros_2nd_edition",
			input:    "Super Mario Bros 2nd Edition",
			expected: "Super Mario Bros 2 Edition",
		},
		{
			name:     "resident_evil_4th_survivor",
			input:    "Resident Evil 4th Survivor",
			expected: "Resident Evil 4 Survivor",
		},
		{
			name:     "the_7th_saga",
			input:    "The 7th Saga",
			expected: "The 7 Saga",
		},

		// Edge cases
		{
			name:     "multiple_ordinals",
			input:    "1st to 3rd Place",
			expected: "1 to 3 Place",
		},
		{
			name:     "ordinal_at_end",
			input:    "Game 21st",
			expected: "Game 21",
		},
		{
			name:     "ordinal_at_start",
			input:    "1st Game",
			expected: "1 Game",
		},
		{
			name:     "no_ordinals",
			input:    "Plain Title",
			expected: "Plain Title",
		},
		{
			name:     "ordinal_followed_by_punctuation",
			input:    "21st: The Beginning",
			expected: "21: The Beginning",
		},

		// Should NOT match (no word boundary)
		{
			name:     "not_ordinal_within_word",
			input:    "test1st",
			expected: "test1st",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeOrdinals(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizeOrdinals failed")
		})
	}
}

// TestExpandAbbreviations tests abbreviation expansion functionality.
// This ensures "vs" → "versus", "bros" → "brothers", "dr" → "doctor", "vol" → "volume",
// "pt" → "part", "ft" → "featuring", and "feat." → "featuring" (period required).
func TestExpandAbbreviations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// vs/versus
		{
			name:     "vs_no_period",
			input:    "Mario vs Donkey Kong",
			expected: "Mario versus Donkey Kong",
		},
		{
			name:     "vs_with_period",
			input:    "Mario vs. Donkey Kong",
			expected: "Mario versus Donkey Kong",
		},
		{
			name:     "vs_uppercase",
			input:    "Mario VS Donkey Kong",
			expected: "Mario versus Donkey Kong",
		},
		{
			name:     "vs_mixed_case",
			input:    "Mario Vs Donkey Kong",
			expected: "Mario versus Donkey Kong",
		},

		// bros/brothers
		{
			name:     "bros_no_period",
			input:    "Super Mario Bros",
			expected: "Super Mario brothers",
		},
		{
			name:     "bros_with_period",
			input:    "Super Mario Bros.",
			expected: "Super Mario brothers",
		},
		{
			name:     "bros_uppercase",
			input:    "Super Mario BROS",
			expected: "Super Mario brothers",
		},

		// dr/doctor
		{
			name:     "dr_no_period",
			input:    "Dr Mario",
			expected: "doctor Mario",
		},
		{
			name:     "dr_with_period",
			input:    "Dr. Mario",
			expected: "doctor Mario",
		},
		{
			name:     "dr_uppercase",
			input:    "DR MARIO",
			expected: "doctor MARIO",
		},

		// mr/mister
		{
			name:     "mr_no_period",
			input:    "Mr Do",
			expected: "mister Do",
		},
		{
			name:     "mr_with_period",
			input:    "Mr. Do",
			expected: "mister Do",
		},

		// vol/volume
		{
			name:     "vol_no_period",
			input:    "Game Vol 2",
			expected: "Game volume 2",
		},
		{
			name:     "vol_with_period",
			input:    "Series Vol. 3",
			expected: "Series volume 3",
		},
		{
			name:     "vol_uppercase",
			input:    "Collection VOL 1",
			expected: "Collection volume 1",
		},

		// pt/part
		{
			name:     "pt_no_period",
			input:    "Game Pt 2",
			expected: "Game part 2",
		},
		{
			name:     "pt_with_period",
			input:    "Episode Pt. 3",
			expected: "Episode part 3",
		},
		{
			name:     "pt_uppercase",
			input:    "Story PT 1",
			expected: "Story part 1",
		},

		// ft/featuring
		{
			name:     "ft_no_period",
			input:    "Song ft Artist",
			expected: "Song featuring Artist",
		},
		{
			name:     "ft_with_period",
			input:    "Track ft. Singer",
			expected: "Track featuring Singer",
		},
		{
			name:     "ft_uppercase",
			input:    "Music FT Band",
			expected: "Music featuring Band",
		},

		// feat./featuring (period required)
		{
			name:     "feat_with_period",
			input:    "Song feat. Artist",
			expected: "Song featuring Artist",
		},
		{
			name:     "feat_uppercase_with_period",
			input:    "Track FEAT. Singer",
			expected: "Track featuring Singer",
		},
		{
			name:     "feat_without_period_not_expanded",
			input:    "A great feat",
			expected: "A great feat", // Should NOT expand - "feat" is a real word
		},

		// Multiple abbreviations
		{
			name:     "multiple_abbreviations",
			input:    "Dr Mario vs Mr Do Bros",
			expected: "doctor Mario versus mister Do brothers",
		},

		// Should NOT expand (not standalone words)
		{
			name:     "vs_within_word",
			input:    "versus",
			expected: "versus", // Correctly doesn't expand - "versus" is a complete word, not abbreviated "vs"
		},
		{
			name:     "bros_within_word",
			input:    "ambros",
			expected: "ambros",
		},

		// Edge cases
		{
			name:     "abbreviation_at_start",
			input:    "vs The World",
			expected: "versus The World",
		},
		{
			name:     "abbreviation_at_end",
			input:    "Super Mario Bros",
			expected: "Super Mario brothers",
		},
		{
			name:     "no_abbreviations",
			input:    "Plain Game Title",
			expected: "Plain Game Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ExpandAbbreviations(tt.input)
			assert.Equal(t, tt.expected, result, "ExpandAbbreviations failed")
		})
	}
}

// TestPlusSymbolNormalization tests the plus symbol normalization functionality.
// This ensures "Game+" → "Game plus" and "Mario Kart 8+" → "Mario Kart 8 plus".
func TestPlusSymbolNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plus_at_end",
			input:    "Game+",
			expected: "gameplus",
		},
		{
			name:     "plus_with_space_before",
			input:    "Game +",
			expected: "gameplus",
		},
		{
			name:     "plus_between_words",
			input:    "Game + Expansion",
			expected: "gameandexpansion", // "+" gets converted to "and" when surrounded by spaces
		},
		{
			name:     "mario_kart_8_plus",
			input:    "Mario Kart 8+",
			expected: "mariokart8plus",
		},
		{
			name:     "mario_kart_8_deluxe_plus",
			input:    "Mario Kart 8 Deluxe+",
			expected: "mariokart8deluxeplus",
		},
		{
			name:     "game_plus_deluxe",
			input:    "Game+ Deluxe",
			expected: "gameplusdeluxe",
		},
		{
			name:     "multiple_plus",
			input:    "Game+ Plus+ Edition",
			expected: "gameplusplusplus",
		},
		{
			name:     "plus_with_metadata",
			input:    "Game+ (USA)",
			expected: "gameplus",
		},
		{
			name:     "no_plus",
			input:    "Plain Game",
			expected: "plaingame",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, "Plus symbol normalization failed")
		})
	}
}

// TestIntegratedNewNormalizations tests the complete integration of all new normalization features.
// This ensures ordinals, abbreviations, and plus symbols work correctly together in the full pipeline.
func TestIntegratedNewNormalizations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Ordinal + Roman Numeral matching
		{
			name:     "street_fighter_2nd_vs_ii_match",
			input:    "Street Fighter 2nd Impact",
			expected: "streetfighter2impact",
		},
		{
			name:     "street_fighter_ii_vs_2nd_match",
			input:    "Street Fighter II Impact",
			expected: "streetfighter2impact",
		},

		// Abbreviation expansion
		{
			name:     "mario_vs_donkey_kong",
			input:    "Mario vs Donkey Kong",
			expected: "marioversusdonkeykong",
		},
		{
			name:     "mario_versus_donkey_kong_match",
			input:    "Mario versus Donkey Kong",
			expected: "marioversusdonkeykong",
		},
		{
			name:     "super_mario_bros",
			input:    "Super Mario Bros.",
			expected: "supermariobrothers",
		},
		{
			name:     "super_mario_brothers_match",
			input:    "Super Mario Brothers",
			expected: "supermariobrothers",
		},
		{
			name:     "dr_mario",
			input:    "Dr. Mario",
			expected: "doctormario",
		},
		{
			name:     "doctor_mario_match",
			input:    "Doctor Mario",
			expected: "doctormario",
		},

		// Plus symbol
		{
			name:     "game_plus",
			input:    "Game+",
			expected: "gameplus",
		},
		{
			name:     "game_plus_written_out_match",
			input:    "Game Plus",
			expected: "gameplus",
		},

		// Combined: All three normalizations
		{
			name:     "complex_title_with_all_normalizations",
			input:    "Super Mario Bros. 2nd Edition vs Dr. Mario+",
			expected: "supermariobrothers2editionversusdoctormarioplus",
		},
		{
			name:     "street_fighter_3rd_strike_vs",
			input:    "Street Fighter 3rd Strike vs Tekken",
			expected: "streetfighter3strikeversustekken",
		},
		{
			name:     "the_21st_century_bros_plus",
			input:    "The 21st Century Bros+",
			expected: "21centurybrothersplus",
		},

		// Real-world game title examples
		{
			name:     "street_fighter_vs_series",
			input:    "Street Fighter vs Marvel",
			expected: "streetfighterversusmarvel",
		},
		{
			name:     "smash_bros_ultimate",
			input:    "Super Smash Bros. Ultimate",
			expected: "supersmashbrothersultimate",
		},
		{
			name:     "mario_kart_8_deluxe_plus",
			input:    "Mario Kart 8+",
			expected: "mariokart8plus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, "Integrated normalization failed")
		})
	}
}

// TestNewNormalizationsWithMetadata ensures new normalizations work correctly with metadata stripping.
func TestNewNormalizationsWithMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ordinal_with_metadata",
			input:    "Street Fighter 2nd Impact (USA) [!]",
			expected: "streetfighter2impact",
		},
		{
			name:     "abbreviation_with_metadata",
			input:    "Super Mario Bros. (USA) (Rev 1)",
			expected: "supermariobrothers",
		},
		{
			name:     "plus_with_metadata",
			input:    "Game+ (Europe) [En]",
			expected: "gameplus",
		},
		{
			name:     "all_with_metadata",
			input:    "Dr. Mario vs Luigi 3rd Edition+ (USA) [!]",
			expected: "doctormarioversusluigi3editionplus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, "Normalization with metadata failed")
		})
	}
}

// TestNewNormalizationsIdempotency ensures the new normalizations maintain idempotency.
func TestNewNormalizationsIdempotency(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"Street Fighter 2nd Impact",
		"Mario vs Donkey Kong",
		"Super Mario Bros.",
		"Dr. Mario",
		"Game+",
		"The 21st Century vs The 22nd",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			first := Slugify(MediaTypeGame, input)
			second := Slugify(MediaTypeGame, first)
			assert.Equal(t, first, second, "New normalizations should maintain idempotency")
		})
	}
}

// TestNormalizeOrdinalsInWords ensures ordinal normalization works in word-level normalization.
func TestNormalizeOrdinalsInWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "ordinal_in_words",
			input:    "Street Fighter 2nd Impact",
			expected: []string{"street", "fighter", "2", "impact"},
		},
		{
			name:     "multiple_ordinals_in_words",
			input:    "From 1st to 3rd Place",
			expected: []string{"from", "1", "to", "3", "place"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeToWords(tt.input)
			assert.Equal(t, tt.expected, result, "Ordinal normalization in words failed")
		})
	}
}

// TestNormalizePunctuation tests Unicode punctuation normalization.
// This ensures curly quotes, fancy dashes, and other punctuation variants
// are normalized to their ASCII equivalents BEFORE other normalization stages.
func TestNormalizePunctuation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Quote variants
		{
			name:     "curly_single_quote_left",
			input:    "Link\u2018s Awakening",
			expected: "Link's Awakening",
		},
		{
			name:     "curly_single_quote_right",
			input:    "Link\u2019s Awakening",
			expected: "Link's Awakening",
		},
		{
			name:     "curly_double_quote_left",
			input:    "\u201cQuote\u201d",
			expected: "\"Quote\"",
		},
		{
			name:     "curly_double_quote_right",
			input:    "\u201cQuote\u201d",
			expected: "\"Quote\"",
		},
		{
			name:     "prime_mark",
			input:    "5\u2032 6\u2033",
			expected: "5' 6\"",
		},
		{
			name:     "grave_accent_as_quote",
			input:    "`quoted`",
			expected: "'quoted'",
		},
		{
			name:     "acute_accent_as_apostrophe",
			input:    "it\u00B4s",
			expected: "it's",
		},

		// Dash variants
		{
			name:     "en_dash",
			input:    "Super\u2013Mario",
			expected: "Super-Mario",
		},
		{
			name:     "em_dash",
			input:    "Game\u2014Title",
			expected: "Game-Title",
		},
		{
			name:     "horizontal_bar",
			input:    "Test\u2015Bar",
			expected: "Test-Bar",
		},
		{
			name:     "minus_sign",
			input:    "10\u221210",
			expected: "10-10",
		},
		{
			name:     "figure_dash",
			input:    "Game\u2012Name",
			expected: "Game-Name",
		},

		// Ellipsis
		{
			name:     "unicode_ellipsis",
			input:    "To be continued\u2026",
			expected: "To be continued...",
		},

		// Multiple variants
		{
			name:     "mixed_punctuation",
			input:    "\u201cLink\u2019s Awakening\u201d \u2013 Zelda",
			expected: "\"Link's Awakening\" - Zelda",
		},

		// No changes needed
		{
			name:     "already_normalized",
			input:    "Plain Text",
			expected: "Plain Text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizePunctuation(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizePunctuation failed")
		})
	}
}

// TestPunctuationNormalizationIntegration tests that punctuation normalization
// works correctly with the full slug pipeline, particularly ensuring:
// 1. Curly quotes are normalized before 'n' conjunction detection
// 2. Fancy dashes are normalized before separator handling and abbreviation expansion
func TestPunctuationNormalizationIntegration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Conjunction detection with curly quotes
		{
			name:     "rock_n_roll_with_curly_quotes",
			input:    "Rock \u2018n\u2019 Roll",
			expected: "rockandroll",
		},
		{
			name:     "rock_n_roll_with_straight_quotes",
			input:    "Rock 'n' Roll",
			expected: "rockandroll",
		},
		{
			name:     "rock_n_roll_variants_match",
			input:    "Rock \u2018n\u2019 Roll Racing",
			expected: "rockandrollracing",
		},

		// Abbreviation expansion with fancy dashes
		// NOTE: With hyphen preservation, these compound words don't get abbreviation expansion
		// because "Super-Bros." is treated as a single word, not "Super" + "Bros."
		{
			name:     "super_bros_with_en_dash",
			input:    "Super\u2013Bros.",
			expected: "superbros", // En-dash→hyphen, hyphen preserved as compound, no abbrev expansion
		},
		{
			name:     "super_bros_with_em_dash",
			input:    "Super\u2014Bros.",
			expected: "superbros", // Em-dash→hyphen, hyphen preserved as compound, no abbrev expansion
		},
		{
			name:     "super_bros_with_hyphen",
			input:    "Super-Bros.",
			expected: "superbros", // Hyphen preserved as compound word, no abbreviation expansion
		},

		// Link's Awakening with curly apostrophe
		{
			name:     "links_awakening_curly_apostrophe",
			input:    "Link\u2019s Awakening",
			expected: "linksawakening",
		},
		{
			name:     "links_awakening_straight_apostrophe",
			input:    "Link's Awakening",
			expected: "linksawakening",
		},

		// Real-world examples with mixed punctuation
		{
			name:     "game_title_with_curly_quotes_and_en_dash",
			input:    "\u201cMario\u2019s Adventure\u201d \u2013 Special Edition",
			expected: "mariosadventurespecial", // "Edition" is stripped by Stage 7
		},
		{
			name:     "game_with_ellipsis",
			input:    "To be continued\u2026",
			expected: "tobecontinued",
		},

		// Edge case: Mixed dash types in same title
		{
			name:     "mixed_dashes",
			input:    "Game-Name\u2013SubName\u2014Extra",
			expected: "gamenamesubnameextra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, "Punctuation normalization integration failed")
		})
	}
}

// TestPunctuationNormalizationIdempotency ensures punctuation normalization maintains idempotency.
func TestPunctuationNormalizationIdempotency(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"Link\u2019s Awakening",
		"Rock \u2018n\u2019 Roll",
		"Super\u2013Bros.",
		"\u201cQuote\u201d",
		"Game\u2026",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			first := Slugify(MediaTypeGame, input)
			second := Slugify(MediaTypeGame, first)
			assert.Equal(t, first, second, "Punctuation normalization should maintain idempotency")
		})
	}
}
