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

func TestSlugifyString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "unicode_pokemon",
			input:    "Pokémon Red",
			expected: "pokemonred",
		},
		{
			name:     "unicode_cafe",
			input:    "Café International",
			expected: "cafeinternational",
		},
		{
			name:     "leading_article_the",
			input:    "The Legend of Zelda",
			expected: "legendofzelda",
		},
		{
			name:     "trailing_article_the",
			input:    "Legend of Zelda, The",
			expected: "legendofzelda",
		},
		{
			name:     "leading_article_the_adventures",
			input:    "The Adventures of Link",
			expected: "adventuresoflink",
		},
		{
			name:     "trailing_article_mega_man",
			input:    "Mega Man, The",
			expected: "megaman",
		},
		{
			name:     "trailing_article_before_subtitle_dash",
			input:    "Legend of Zelda, The - Ocarina of Time",
			expected: "legendofzeldaocarinaoftime",
		},
		{
			name:     "trailing_article_before_subtitle_colon",
			input:    "Legend of Zelda, The: Ocarina of Time",
			expected: "legendofzeldaocarinaoftime",
		},
		{
			name:     "trailing_article_before_metadata",
			input:    "Legend of Zelda, The (USA) (Rev 1)",
			expected: "legendofzelda",
		},
		{
			name:     "trailing_article_before_brackets",
			input:    "Mega Man, The [!]",
			expected: "megaman",
		},
		{
			name:     "trailing_article_with_extra_spaces",
			input:    "Game, The   -   Subtitle",
			expected: "gamesubtitle",
		},
		{
			name:     "ampersand_sonic",
			input:    "Sonic & Knuckles",
			expected: "sonicandknuckles",
		},
		{
			name:     "ampersand_already_and",
			input:    "Sonic and Knuckles",
			expected: "sonicandknuckles",
		},
		{
			name:     "ampersand_tom_jerry",
			input:    "Tom & Jerry",
			expected: "tomandjerry",
		},
		{
			name:     "metadata_usa",
			input:    "Super Mario Bros (USA)",
			expected: "supermariobros",
		},
		{
			name:     "metadata_europe_rev",
			input:    "Sonic (Europe) (Rev 1)",
			expected: "sonic",
		},
		{
			name:     "metadata_brackets",
			input:    "Zelda [!]",
			expected: "zelda",
		},
		{
			name:     "metadata_complex",
			input:    "Game (USA) [b1] [T+Eng]",
			expected: "game",
		},
		{
			name:     "roman_numeral_vii",
			input:    "Final Fantasy VII",
			expected: "finalfantasy7",
		},
		{
			name:     "roman_numeral_ii",
			input:    "Street Fighter II Turbo",
			expected: "streetfighter2turbo",
		},
		{
			name:     "roman_numeral_viii",
			input:    "Dragon Quest VIII",
			expected: "dragonquest8",
		},
		{
			name:     "roman_numeral_i_end",
			input:    "Final Fantasy I",
			expected: "finalfantasy1",
		},
		{
			name:     "roman_numeral_i_before_colon",
			input:    "Game I: The Subtitle",
			expected: "game1thesubtitle",
		},
		{
			name:     "roman_numeral_i_midword_ski",
			input:    "Ski Championship",
			expected: "skichampionship",
		},
		{
			name:     "roman_numeral_ii_after_number",
			input:    "Resident Evil 4 II",
			expected: "residentevil42",
		},
		{
			name:     "roman_numeral_wwii",
			input:    "World War II",
			expected: "worldwar2",
		},
		{
			name:     "separator_colon",
			input:    "Zelda: Link's Awakening",
			expected: "zeldalinksawakening",
		},
		{
			name:     "separator_mega_man",
			input:    "Mega Man X: Command Mission",
			expected: "megamanxcommandmission",
		},
		{
			name:     "separator_hyphen_fzero",
			input:    "F-Zero",
			expected: "fzero",
		},
		{
			name:     "separator_hyphen_rtype",
			input:    "R-Type",
			expected: "rtype",
		},
		{
			name:     "combined_pokemon_stadium",
			input:    "The Pokémon Stadium 2 (USA) [!]",
			expected: "pokemonstadium2",
		},
		{
			name:     "combined_zelda_ocarina",
			input:    "The Legend of Zelda: Ocarina of Time (Europe) (Rev A)",
			expected: "legendofzeldaocarinaoftime",
		},
		{
			name:     "combined_batman_robin",
			input:    "The Adventures of Batman & Robin",
			expected: "adventuresofbatmanandrobin",
		},
		{
			name:     "edge_empty",
			input:    "",
			expected: "",
		},
		{
			name:     "edge_whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "edge_metadata_only_usa",
			input:    "(USA)",
			expected: "",
		},
		{
			name:     "edge_metadata_only_brackets",
			input:    "[!]",
			expected: "",
		},
		{
			name:     "edge_special_chars",
			input:    "!@#$%",
			expected: "",
		},
		{
			name:     "edge_just_the",
			input:    "The",
			expected: "the",
		},
		{
			name:     "normal_mario_bros",
			input:    "Mario Bros",
			expected: "mariobros",
		},
		{
			name:     "brackets_subtitle_stripped",
			input:    "Zelda [Link's Awakening]",
			expected: "zelda",
		},
		{
			name:     "special_chars_street_fighter",
			input:    "Street Fighter II: Championship Edition!",
			expected: "streetfighter2championshipedition",
		},
		{
			name:     "numbers_preserved_disc",
			input:    "Final Fantasy VII (Disc 1)",
			expected: "finalfantasy7",
		},
		{
			name:     "only_special_chars",
			input:    "!@#$%",
			expected: "",
		},
		{
			name:     "mixed_case",
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
			name:     "mixed_brackets_parens",
			input:    "Game [USA] (Rev 1)",
			expected: "game",
		},
		{
			name:     "underscore_dash",
			input:    "super_mario-bros",
			expected: "supermariobros",
		},
		{
			name:     "apostrophe_links",
			input:    "Link's Awakening",
			expected: "linksawakening",
		},
		{
			name:     "apostrophe_links_no_apostrophe",
			input:    "Links Awakening",
			expected: "linksawakening",
		},
		{
			name:     "roman_numeral_iii_lowercase",
			input:    "game iii",
			expected: "game3",
		},
		{
			name:     "roman_numeral_iv_midword_alive",
			input:    "Alive & Kicking",
			expected: "aliveandkicking",
		},
		{
			name:     "roman_numeral_vi_midword_video",
			input:    "Video Game",
			expected: "videogame",
		},
		{
			name:     "roman_numeral_v_end",
			input:    "Grand Theft Auto V",
			expected: "grandtheftauto5",
		},
		{
			name:     "roman_numeral_ix_midword_phoenix",
			input:    "Phoenix Rising",
			expected: "phoenixrising",
		},
		{
			name:     "multiple_roman_numerals",
			input:    "Final Fantasy VII - Part II",
			expected: "finalfantasy7part2",
		},
		{
			name:     "trailing_article_only_comma",
			input:    "Game, The",
			expected: "game",
		},
		{
			name:     "leading_article_uppercase",
			input:    "THE LAST OF US",
			expected: "lastofus",
		},
		{
			name:     "leading_article_mixed_case",
			input:    "ThE lEgEnD",
			expected: "legend",
		},
		{
			name:     "multiple_ampersands",
			input:    "Tom & Jerry & Friends",
			expected: "tomandjerryandfriends",
		},
		{
			name:     "nested_parentheses_complex",
			input:    "Game (Version (Final) [Beta])",
			expected: "game",
		},
		{
			name:     "brackets_only_no_content",
			input:    "Game []",
			expected: "game",
		},
		{
			name:     "parentheses_only_no_content",
			input:    "Game ()",
			expected: "game",
		},
		{
			name:     "multiple_colons",
			input:    "Game: Part: One",
			expected: "gamepartone",
		},
		{
			name:     "consecutive_separators",
			input:    "Game---Part___One",
			expected: "gamepartone",
		},
		{
			name:     "trailing_separator",
			input:    "Super Mario Bros-",
			expected: "supermariobros",
		},
		{
			name:     "leading_separator",
			input:    "-Super Mario Bros",
			expected: "supermariobros",
		},
		{
			name:     "numbers_only",
			input:    "1234567890",
			expected: "1234567890",
		},
		{
			name:     "single_letter",
			input:    "A",
			expected: "a",
		},
		{
			name:     "single_number",
			input:    "5",
			expected: "5",
		},
		{
			name:     "very_long_title",
			input:    "The Super Duper Extra Long Game Title With Many Words And Stuff (USA) (Rev A) [!]",
			expected: "superduperextralonggametitlewithmanywordsandstuff",
		},
		{
			name:     "quotation_marks",
			input:    `Game "The Best" Edition`,
			expected: "gamethebest",
		},
		{
			name:     "single_quotes",
			input:    "Game 'Special' Edition",
			expected: "gamespecial",
		},
		{
			name:     "backticks",
			input:    "Game `Limited` Edition",
			expected: "gamelimited",
		},
		{
			name:     "currency_symbols",
			input:    "Game $99 Edition",
			expected: "game99",
		},
		{
			name:     "percentage_symbol",
			input:    "Game 100% Edition",
			expected: "game100",
		},
		{
			name:     "plus_minus_symbols",
			input:    "Game +/-",
			expected: "game",
		},
		{
			name:     "equals_symbol",
			input:    "Game = Fun",
			expected: "gamefun",
		},
		{
			name:     "question_exclamation",
			input:    "Game?! Ultimate Edition!",
			expected: "gameultimateedition",
		},
		{
			name:     "slash_forward_backward",
			input:    "Game/Part\\One",
			expected: "gamepartone",
		},
		{
			name:     "pipe_symbol",
			input:    "Game | Special",
			expected: "gamespecial",
		},
		{
			name:     "at_symbol",
			input:    "Game @ Home",
			expected: "gamehome",
		},
		{
			name:     "hash_symbol",
			input:    "Game #1",
			expected: "game1",
		},
		{
			name:     "caret_symbol",
			input:    "Game^2",
			expected: "game2",
		},
		{
			name:     "tilde_symbol",
			input:    "Game~Special",
			expected: "gamespecial",
		},
		{
			name:     "asterisk_symbol",
			input:    "Game*Star",
			expected: "gamestar",
		},
		{
			name:     "less_greater_than",
			input:    "Game <Ultimate> Edition",
			expected: "gameultimate",
		},
		{
			name:     "comma_separator",
			input:    "Game, Part, One",
			expected: "gamepartone",
		},
		{
			name:     "semicolon_separator",
			input:    "Game; Part; One",
			expected: "gamepartone",
		},
		{
			name:     "period_separator",
			input:    "Game.Part.One",
			expected: "gamepartone",
		},
		{
			name:     "multiple_metadata_types",
			input:    "Game (USA) [!] (Rev 1) [T+Eng] (Beta)",
			expected: "game",
		},
		{
			name:     "metadata_with_nested_brackets",
			input:    "Game [Version [1.0]]",
			expected: "game",
		},
		{
			name:     "zero_width_space",
			input:    "Game\u200BTitle",
			expected: "gametitle",
		},
		{
			name:     "tab_characters",
			input:    "Game\tTitle",
			expected: "gametitle",
		},
		{
			name:     "newline_characters",
			input:    "Game\nTitle",
			expected: "gametitle",
		},
		{
			name:     "carriage_return",
			input:    "Game\rTitle",
			expected: "gametitle",
		},
		{
			name:     "non_breaking_space",
			input:    "Game\u00A0Title",
			expected: "gametitle",
		},
		{
			name:     "leading_number_prefix_dot",
			input:    "1. Super Mario Bros",
			expected: "supermariobros",
		},
		{
			name:     "leading_number_prefix_dash",
			input:    "2 - Sonic the Hedgehog",
			expected: "sonicthehedgehog",
		},
		{
			name:     "leading_number_prefix_space",
			input:    "03 Zelda",
			expected: "zelda",
		},
		{
			name:     "leading_number_prefix_multiple_digits",
			input:    "123. Game",
			expected: "game",
		},
		{
			name:     "game_name_starting_with_number",
			input:    "1942",
			expected: "1942",
		},
		{
			name:     "game_name_starting_with_number_words",
			input:    "7th Saga",
			expected: "7thsaga",
		},
		{
			name:     "game_name_3d",
			input:    "3D Worldrunner",
			expected: "3dworldrunner",
		},
		{
			name:     "leading_prefix_with_article",
			input:    "1. The Legend of Zelda",
			expected: "legendofzelda",
		},
		{
			name:     "leading_prefix_with_metadata",
			input:    "01 - Super Mario Bros (USA)",
			expected: "supermariobros",
		},
		{
			name:     "edition_suffix_version",
			input:    "Pokemon Ruby Version",
			expected: "pokemonruby",
		},
		{
			name:     "edition_suffix_firered_version",
			input:    "Pokemon FireRed Version",
			expected: "pokemonfirered",
		},
		{
			name:     "edition_suffix_edition",
			input:    "Skyrim Special Edition",
			expected: "skyrim",
		},
		{
			name:     "edition_suffix_deluxe",
			input:    "Grand Theft Auto V Deluxe Edition",
			expected: "grandtheftauto5",
		},
		{
			name:     "edition_suffix_goty",
			input:    "The Witcher 3 GOTY Edition",
			expected: "witcher3",
		},
		{
			name:     "edition_suffix_game_of_the_year",
			input:    "Fallout 4 Game of the Year Edition",
			expected: "fallout4",
		},
		{
			name:     "edition_suffix_definitive",
			input:    "Halo The Master Chief Collection Definitive Edition",
			expected: "halothemasterchiefcollection",
		},
		{
			name:     "edition_suffix_ultimate",
			input:    "Forza Horizon 5 Ultimate Edition",
			expected: "forzahorizon5",
		},
		{
			name:     "edition_suffix_case_insensitive",
			input:    "Game VERSION",
			expected: "game",
		},
		{
			name:     "edition_suffix_mixed_case",
			input:    "Test DeLuXe EdItIoN",
			expected: "test",
		},
		{
			name:     "edition_not_at_end",
			input:    "Version Control System",
			expected: "versioncontrolsystem",
		},
		{
			name:     "edition_with_metadata",
			input:    "Pokemon Emerald Version (USA)",
			expected: "pokemonemerald",
		},
		{
			name:     "edition_with_article",
			input:    "The Legend of Zelda Special Edition",
			expected: "legendofzelda",
		},
		{
			name:     "edition_with_number_prefix",
			input:    "1. Super Mario Bros Deluxe Edition",
			expected: "supermariobros",
		},
		{
			name:     "multiple_edition_words",
			input:    "Game Special Edition Version",
			expected: "gamespecialedition",
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

func TestSlugifyStringIdempotency(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"The Legend of Zelda",
		"Pokémon Red",
		"Final Fantasy VII",
		"Super Mario Bros (USA)",
		"Sonic & Knuckles",
	}

	for _, input := range inputs {
		first := SlugifyString(input)
		second := SlugifyString(first)
		assert.Equal(t, first, second, "SlugifyString should be idempotent")
	}
}

func BenchmarkSlugifyString(b *testing.B) {
	testCases := []string{
		"The Legend of Zelda: Ocarina of Time (USA) [!]",
		"Pokémon Stadium 2",
		"Final Fantasy VII",
		"Super Mario Bros",
		"Sonic & Knuckles",
	}

	for _, tc := range testCases {
		b.Run(tc, func(b *testing.B) {
			for range b.N {
				SlugifyString(tc)
			}
		})
	}
}
