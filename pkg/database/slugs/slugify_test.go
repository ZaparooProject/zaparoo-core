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
			expected: "game1subtitle",
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
			expected: "game",
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
			expected: "1supermariobros",
		},
		{
			name:     "leading_number_prefix_dash",
			input:    "2 - Sonic the Hedgehog",
			expected: "2sonicthehedgehog",
		},
		{
			name:     "leading_number_prefix_space",
			input:    "03 Zelda",
			expected: "03zelda",
		},
		{
			name:     "leading_number_prefix_multiple_digits",
			input:    "123. Game",
			expected: "123game",
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
			expected: "1thelegendofzelda",
		},
		{
			name:     "leading_prefix_with_metadata",
			input:    "01 - Super Mario Bros (USA)",
			expected: "01supermariobros",
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
			expected: "skyrimspecial",
		},
		{
			name:     "edition_suffix_deluxe",
			input:    "Grand Theft Auto V Deluxe Edition",
			expected: "grandtheftauto5deluxe",
		},
		{
			name:     "edition_suffix_goty",
			input:    "The Witcher 3 GOTY Edition",
			expected: "witcher3goty",
		},
		{
			name:     "edition_suffix_game_of_the_year",
			input:    "Fallout 4 Game of the Year Edition",
			expected: "fallout4gameoftheyear",
		},
		{
			name:     "edition_suffix_definitive",
			input:    "Halo The Master Chief Collection Definitive Edition",
			expected: "halothemasterchiefcollectiondefinitive",
		},
		{
			name:     "edition_suffix_ultimate",
			input:    "Forza Horizon 5 Ultimate Edition",
			expected: "forzahorizon5ultimate",
		},
		{
			name:     "edition_suffix_case_insensitive",
			input:    "Game VERSION",
			expected: "game",
		},
		{
			name:     "edition_suffix_mixed_case",
			input:    "Test DeLuXe EdItIoN",
			expected: "testdeluxe",
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
			expected: "legendofzeldaspecial",
		},
		{
			name:     "edition_with_number_prefix",
			input:    "1. Super Mario Bros Deluxe Edition",
			expected: "1supermariobrosdeluxe",
		},
		{
			name:     "multiple_edition_words",
			input:    "Game Special Edition Version",
			expected: "gamespecialedition",
		},
		{
			name:     "secondary_title_colon_with_leading_article",
			input:    "Legend of Zelda: The Minish Cap",
			expected: "legendofzeldaminishcap",
		},
		{
			name:     "secondary_title_dash_with_leading_article",
			input:    "Movie - The Game",
			expected: "moviegame",
		},
		{
			name:     "secondary_title_possessive_with_leading_article",
			input:    "Disney's The Lion King",
			expected: "disneyslionking",
		},
		{
			name:     "secondary_title_with_a_article",
			input:    "Batman: A Telltale Series",
			expected: "batmantelltaleseries",
		},
		{
			name:     "secondary_title_with_an_article",
			input:    "Game: An Adventure",
			expected: "gameadventure",
		},
		{
			name:     "secondary_title_no_article",
			input:    "Zelda: Link's Awakening",
			expected: "zeldalinksawakening",
		},
		{
			name:     "main_title_leading_article_with_secondary",
			input:    "The Legend of Zelda: Ocarina of Time",
			expected: "legendofzeldaocarinaoftime",
		},
		{
			name:     "both_titles_with_articles",
			input:    "The Game: The Beginning",
			expected: "gamebeginning",
		},
		{
			name:     "trailing_article_without_secondary_title",
			input:    "Legend of Zelda, The",
			expected: "legendofzelda",
		},
		{
			name:     "trailing_article_with_secondary_title_colon",
			input:    "Legend of Zelda, The: Link's Awakening",
			expected: "legendofzeldalinksawakening",
		},
		{
			name:     "trailing_article_with_secondary_title_dash",
			input:    "Final Fantasy, The - 7",
			expected: "finalfantasy7",
		},
		{
			name:     "trailing_article_with_secondary_title_possessive",
			input:    "Game, The's Adventure",
			expected: "gamethesadventure",
		},
		{
			name:     "multiple_delimiters_colon_first",
			input:    "Legend of Zelda: Link's Awakening",
			expected: "legendofzeldalinksawakening",
		},
		{
			name:     "multiple_delimiters_colon_beats_dash",
			input:    "Sonic - The Hedgehog: Part 2",
			expected: "sonicthehedgehogpart2",
		},
		{
			name:     "multiple_delimiters_dash_beats_possessive",
			input:    "Mario's Adventure - The Quest",
			expected: "mariosadventurequest",
		},
		{
			name:     "all_three_delimiters_colon_wins",
			input:    "Game's Title: The Subtitle - Extra",
			expected: "gamestitlesubtitleextra",
		},
		{
			name:     "possessive_before_colon_colon_wins",
			input:    "Someone's Something: Time to Die",
			expected: "someonessomethingtimetodie",
		},
		{
			name:     "possessive_before_dash_dash_wins",
			input:    "Player's Choice - Final Battle",
			expected: "playerschoicefinalbattle",
		},
		{
			name:     "fullwidth_number_prefix",
			input:    "１. Super Mario Bros",
			expected: "1supermariobros",
		},
		{
			name:     "fullwidth_delimiter_colon",
			input:    "Zelda：Link's Awakening",
			expected: "zeldalinksawakening",
		},
		{
			name:     "fullwidth_delimiter_dash",
			input:    "Game － The Sequel",
			expected: "gamesequel",
		},
		{
			name:     "ligature_fi",
			input:    "ﬁnal Fantasy VII",
			expected: "finalfantasy7",
		},
		{
			name:     "superscript_number",
			input:    "Game²",
			expected: "game2",
		},
		{
			name:     "circled_number",
			input:    "① First Game",
			expected: "1firstgame",
		},
		{
			name:     "trademark_symbol",
			input:    "Sonic™ Adventure",
			expected: "sonicadventure",
		},
		{
			name:     "combined_fullwidth_and_unicode",
			input:    "１. Pokémon：The Game",
			expected: "1pokemongame",
		},
		{
			name:     "registered_symbol",
			input:    "Game®",
			expected: "game",
		},
		{
			name:     "copyright_symbol",
			input:    "©Disney's Game",
			expected: "disneysgame",
		},
		{
			name:     "service_mark_symbol",
			input:    "Brand℠ Adventure",
			expected: "brandadventure",
		},
		{
			name:     "version_suffix_v1_2",
			input:    "Game v1.2",
			expected: "game",
		},
		{
			name:     "version_suffix_v10",
			input:    "Title v10",
			expected: "title",
		},
		{
			name:     "version_suffix_vIII",
			input:    "Game vIII",
			expected: "game",
		},
		{
			name:     "version_suffix_v1",
			input:    "Pokemon Red v1",
			expected: "pokemonred",
		},
		{
			name:     "version_suffix_with_dot",
			input:    "Game v.1.0",
			expected: "game",
		},
		{
			name:     "version_suffix_complex",
			input:    "Title v1.2.3.4",
			expected: "title",
		},
		{
			name:     "roman_numeral_xi",
			input:    "Final Fantasy XI",
			expected: "finalfantasy11",
		},
		{
			name:     "roman_numeral_xii",
			input:    "Final Fantasy XII",
			expected: "finalfantasy12",
		},
		{
			name:     "roman_numeral_xiii",
			input:    "Final Fantasy XIII",
			expected: "finalfantasy13",
		},
		{
			name:     "roman_numeral_xiv",
			input:    "Final Fantasy XIV",
			expected: "finalfantasy14",
		},
		{
			name:     "roman_numeral_xv",
			input:    "Final Fantasy XV",
			expected: "finalfantasy15",
		},
		{
			name:     "roman_numeral_xvi",
			input:    "Game XVI",
			expected: "game16",
		},
		{
			name:     "roman_numeral_xvii",
			input:    "Game XVII",
			expected: "game17",
		},
		{
			name:     "roman_numeral_xviii",
			input:    "Game XVIII",
			expected: "game18",
		},
		{
			name:     "roman_numeral_xix",
			input:    "Game XIX",
			expected: "game19",
		},
		{
			name:     "roman_numeral_x_preserved",
			input:    "Mega Man X",
			expected: "megamanx",
		},
		{
			name:     "braces_stripped",
			input:    "Game {Special}",
			expected: "game",
		},
		{
			name:     "angle_brackets_stripped",
			input:    "Game <Ultimate>",
			expected: "game",
		},
		{
			name:     "mixed_brackets_braces_angles",
			input:    "Game (USA) [!] {Special} <Edition>",
			expected: "game",
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
		"Legend of Zelda: The Minish Cap",
		"Disney's The Lion King",
		"Movie - The Game",
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

func TestNormalizeToWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "basic_title",
			input:    "Super Mario Bros",
			expected: []string{"super", "mario", "bros"},
		},
		{
			name:     "with_metadata",
			input:    "Legend of Zelda (USA) [!]",
			expected: []string{"legend", "of", "zelda"},
		},
		{
			name:     "with_roman_numerals",
			input:    "Final Fantasy VII",
			expected: []string{"final", "fantasy", "7"},
		},
		{
			name:     "with_article_and_secondary",
			input:    "The Legend of Zelda: The Minish Cap",
			expected: []string{"legend", "of", "zelda", "minish", "cap"},
		},
		{
			name:     "with_ampersand",
			input:    "Sonic & Knuckles",
			expected: []string{"sonic", "and", "knuckles"},
		},
		{
			name:     "with_separators",
			input:    "Mega-Man-X",
			expected: []string{"mega", "man", "x"},
		},
		{
			name:     "empty_input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "only_metadata",
			input:    "(USA) [!]",
			expected: []string{},
		},
		{
			name:     "complex_title",
			input:    "The Pokémon Stadium 2 (USA) (Rev A) [!]",
			expected: []string{"pokemon", "stadium", "2"},
		},
		{
			name:     "with_edition_suffix",
			input:    "Skyrim Special Edition",
			expected: []string{"skyrim", "special"},
		},
		{
			name:     "sequel_numbers",
			input:    "Street Fighter II Turbo",
			expected: []string{"street", "fighter", "2", "turbo"},
		},
		{
			name:     "multiple_roman_numerals",
			input:    "Final Fantasy VII Part II",
			expected: []string{"final", "fantasy", "7", "part", "2"},
		},
		{
			name:     "preserves_x",
			input:    "Mega Man X",
			expected: []string{"mega", "man", "x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeToWords(tt.input)
			assert.Equal(t, tt.expected, result, "NormalizeToWords result mismatch")
		})
	}
}

func TestNormalizeToWordsVsSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"basic", "Super Mario Bros"},
		{"metadata", "Legend of Zelda (USA)"},
		{"roman_numerals", "Final Fantasy VII"},
		{"secondary_title", "Zelda: Link's Awakening"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			slug := SlugifyString(tt.input)
			words := NormalizeToWords(tt.input)

			wordsJoined := ""
			for _, word := range words {
				wordsJoined += word
			}

			assert.Equal(t, slug, wordsJoined,
				"NormalizeToWords joined should equal SlugifyString")
		})
	}
}

func TestConjunctionNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ampersand",
			input:    "Sonic & Knuckles",
			expected: "sonicandknuckles",
		},
		{
			name:     "plus_sign",
			input:    "Rock + Roll Racing",
			expected: "rockandrollracing",
		},
		{
			name:     "n_with_both_apostrophes",
			input:    "Rock 'n' Roll Racing",
			expected: "rockandrollracing",
		},
		{
			name:     "n_with_left_apostrophe",
			input:    "Rock 'n Roll Racing",
			expected: "rockandrollracing",
		},
		{
			name:     "n_with_right_apostrophe",
			input:    "Rock n' Roll Racing",
			expected: "rockandrollracing",
		},
		{
			name:     "standalone_n",
			input:    "Rock n Roll Racing",
			expected: "rockandrollracing",
		},
		{
			name:     "multiple_conjunctions",
			input:    "Sonic & Knuckles + Tails 'n' Shadow",
			expected: "sonicandknucklesandtailsandshadow",
		},
		{
			name:     "already_and",
			input:    "Sonic and Knuckles",
			expected: "sonicandknuckles",
		},
		{
			name:     "mixed_conjunctions",
			input:    "Rock 'n Roll & Jazz + Blues",
			expected: "rockandrollandjazzandblues",
		},
		{
			name:     "conjunction_with_metadata",
			input:    "Rock + Roll Racing (USA) [!]",
			expected: "rockandrollracing",
		},
		{
			name:     "does_not_match_cplusplus",
			input:    "C++Programming",
			expected: "cprogramming",
		},
		{
			name:     "does_not_match_n_without_spaces",
			input:    "nintendo",
			expected: "nintendo",
		},
		{
			name:     "does_not_match_n_at_start",
			input:    "n Roll Racing",
			expected: "nrollracing",
		},
		{
			name:     "does_not_match_n_at_end",
			input:    "Rock n",
			expected: "rockn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SlugifyString(tt.input)
			assert.Equal(t, tt.expected, result, "Conjunction normalization failed")
		})
	}
}

func TestConjunctionNormalizationInWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "plus_sign_words",
			input:    "Rock + Roll Racing",
			expected: []string{"rock", "and", "roll", "racing"},
		},
		{
			name:     "n_with_apostrophes_words",
			input:    "Rock 'n' Roll",
			expected: []string{"rock", "and", "roll"},
		},
		{
			name:     "standalone_n_words",
			input:    "Rock n Roll",
			expected: []string{"rock", "and", "roll"},
		},
		{
			name:     "multiple_conjunctions_words",
			input:    "Sonic & Tails + Knuckles 'n' Amy",
			expected: []string{"sonic", "and", "tails", "and", "knuckles", "and", "amy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeToWords(tt.input)
			assert.Equal(t, tt.expected, result, "Conjunction normalization in words failed")
		})
	}
}

func TestStripLeadingArticle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "the_prefix",
			input:    "The Legend of Zelda",
			expected: "Legend of Zelda",
		},
		{
			name:     "a_prefix",
			input:    "A New Hope",
			expected: "New Hope",
		},
		{
			name:     "an_prefix",
			input:    "An American Tail",
			expected: "American Tail",
		},
		{
			name:     "no_article",
			input:    "Super Mario Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "article_not_at_start",
			input:    "Someone The Great",
			expected: "Someone The Great",
		},
		{
			name:     "the_lowercase",
			input:    "the quick brown fox",
			expected: "quick brown fox",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := StripLeadingArticle(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStripMetadataBrackets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "parentheses",
			input:    "Game (USA)",
			expected: "Game",
		},
		{
			name:     "square_brackets",
			input:    "Game [!]",
			expected: "Game",
		},
		{
			name:     "braces",
			input:    "Game {Europe}",
			expected: "Game",
		},
		{
			name:     "angle_brackets",
			input:    "Game <Beta>",
			expected: "Game",
		},
		{
			name:     "all_bracket_types",
			input:    "Game (USA)[!]{En}<Proto>",
			expected: "Game",
		},
		{
			name:     "multiple_same_type",
			input:    "Game (USA) (v1.2)",
			expected: "Game",
		},
		{
			name:     "no_brackets",
			input:    "Plain Game",
			expected: "Plain Game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := StripMetadataBrackets(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStripEditionAndVersionSuffixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "special_edition",
			input:    "Game Special Edition",
			expected: "Game Special",
		},
		{
			name:     "deluxe_edition",
			input:    "Title Deluxe Edition",
			expected: "Title Deluxe",
		},
		{
			name:     "goty_edition",
			input:    "Game GOTY Edition",
			expected: "Game GOTY",
		},
		{
			name:     "version_number",
			input:    "Title v1.2",
			expected: "Title",
		},
		{
			name:     "version_roman",
			input:    "Game vIII",
			expected: "Game",
		},
		{
			name:     "both_edition_and_version",
			input:    "Game Special Edition v2.0",
			expected: "Game Special Edition",
		},
		{
			name:     "no_suffix",
			input:    "Plain Game",
			expected: "Plain Game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := StripEditionAndVersionSuffixes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeConjunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ampersand",
			input:    "Sonic & Knuckles",
			expected: "Sonic  and  Knuckles",
		},
		{
			name:     "plus_sign",
			input:    "Rock + Roll Racing",
			expected: "Rock and Roll Racing",
		},
		{
			name:     "n_with_apostrophes",
			input:    "Rock 'n' Roll",
			expected: "Rock and Roll",
		},
		{
			name:     "n_with_left_apostrophe",
			input:    "Rock 'n Roll",
			expected: "Rock and Roll",
		},
		{
			name:     "n_with_right_apostrophe",
			input:    "Rock n' Roll",
			expected: "Rock and Roll",
		},
		{
			name:     "standalone_n",
			input:    "Rock n Roll",
			expected: "Rock and Roll",
		},
		{
			name:     "multiple_conjunctions",
			input:    "Sonic & Tails + Knuckles 'n' Amy",
			expected: "Sonic  and  Tails and Knuckles and Amy",
		},
		{
			name:     "no_conjunctions",
			input:    "Plain Game Title",
			expected: "Plain Game Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeSymbolsAndSeparators(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertRomanNumerals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "numeral_vii",
			input:    "Final Fantasy VII",
			expected: "final fantasy 7",
		},
		{
			name:     "numeral_ii",
			input:    "Street Fighter II",
			expected: "street fighter 2",
		},
		{
			name:     "numeral_iii",
			input:    "Game III",
			expected: "game 3",
		},
		{
			name:     "numeral_iv",
			input:    "Title IV",
			expected: "title 4",
		},
		{
			name:     "numeral_v",
			input:    "Game V",
			expected: "game 5",
		},
		{
			name:     "numeral_vi",
			input:    "Game VI",
			expected: "game 6",
		},
		{
			name:     "numeral_viii",
			input:    "Game VIII",
			expected: "game 8",
		},
		{
			name:     "numeral_ix",
			input:    "Game IX",
			expected: "game 9",
		},
		{
			name:     "numeral_x_preserved",
			input:    "Mega Man X",
			expected: "mega man x",
		},
		{
			name:     "numeral_xi",
			input:    "Final Fantasy XI",
			expected: "final fantasy 11",
		},
		{
			name:     "numeral_xix",
			input:    "Game XIX",
			expected: "game 19",
		},
		{
			name:     "suffix_i",
			input:    "Game I",
			expected: "game 1",
		},
		{
			name:     "no_numerals",
			input:    "Plain Game",
			expected: "plain game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertRomanNumerals(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeSeparators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "colon",
			input:    "Zelda:Link",
			expected: "Zelda Link",
		},
		{
			name:     "underscore",
			input:    "Super_Mario_Bros",
			expected: "Super Mario Bros",
		},
		{
			name:     "hyphen",
			input:    "Game-Title-Here",
			expected: "Game Title Here",
		},
		{
			name:     "mixed_separators",
			input:    "Game:Title_With-Separators",
			expected: "Game Title With Separators",
		},
		{
			name:     "no_separators",
			input:    "Plain Game",
			expected: "Plain Game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeSymbolsAndSeparators(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSlugifyStringRegression_AsciiFastPath ensures the ASCII fast-path optimization
// doesn't change the algorithm's behavior. This test catches the bug where ASCII strings
// were returning different results than the original algorithm.
func TestSlugifyStringRegression_AsciiFastPath(t *testing.T) {
	t.Parallel()

	// These test cases specifically target edge cases where the ASCII fast-path
	// optimization could produce different results than the original algorithm.
	// The key insight is that even ASCII strings need both asciiSlug and unicodeSlug
	// computation for proper script detection logic.
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Pure ASCII strings that should behave identically
		{
			name:     "pure_ascii_simple",
			input:    "Super Mario Bros",
			expected: "supermariobros",
		},
		{
			name:     "pure_ascii_with_metadata",
			input:    "The Legend of Zelda (USA) [!]",
			expected: "legendofzelda",
		},
		{
			name:     "pure_ascii_with_roman_numerals",
			input:    "Final Fantasy VII",
			expected: "finalfantasy7",
		},
		{
			name:     "pure_ascii_with_ampersand",
			input:    "Sonic & Knuckles",
			expected: "sonicandknuckles",
		},
		{
			name:     "pure_ascii_with_apostrophe",
			input:    "Link's Awakening",
			expected: "linksawakening",
		},
		{
			name:     "pure_ascii_with_colon_subtitle",
			input:    "Mega Man X: The First Battle",
			expected: "megamanxfirstbattle",
		},
		{
			name:     "pure_ascii_with_dash_subtitle",
			input:    "Street Fighter - The World Warrior",
			expected: "streetfighterworldwarrior",
		},
		{
			name:     "pure_ascii_trailing_article",
			input:    "Legend of Zelda, The",
			expected: "legendofzelda",
		},
		{
			name:     "pure_ascii_leading_number",
			input:    "007 - The World is Not Enough",
			expected: "007worldisnotenough",
		},
		{
			name:     "pure_ascii_edition_suffix",
			input:    "Game of the Year Edition",
			expected: "gameoftheyear",
		},
		{
			name:     "pure_ascii_version_suffix",
			input:    "Game v2.5",
			expected: "game",
		},
		{
			name:     "pure_ascii_complex_metadata",
			input:    "Super Mario World (USA) (Rev 1) [!]",
			expected: "supermarioworld",
		},
		{
			name:     "pure_ascii_multiple_separators",
			input:    "Game: The_Subtitle-Edition",
			expected: "gamethesubtitle",
		},
		{
			name:     "pure_ascii_empty_after_stripping",
			input:    "(USA) [!] v1.0",
			expected: "v10",
		},
		{
			name:     "pure_ascii_only_spaces",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SlugifyString(tt.input)
			assert.Equal(t, tt.expected, result,
				"ASCII fast-path should produce same result as original algorithm")
		})
	}
}

// TestSlugifyStringRegression_ScriptDetectionConsistency ensures that script detection
// and slug selection logic works correctly for all input types, including edge cases
// that might be affected by optimizations.
func TestSlugifyStringRegression_ScriptDetectionConsistency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Unicode strings that should preserve Unicode characters
		{
			name:     "cjk_mixed_with_ascii",
			input:    "Street Fighter ストリートファイター",
			expected: "streetfighterストリートファイター",
		},
		{
			name:     "pure_cjk",
			input:    "ドラゴンクエストIII",
			expected: "ドラゴンクエスト3",
		},
		{
			name:     "cyrillic",
			input:    "Тетрис",
			expected: "тетрис",
		},
		{
			name:     "greek",
			input:    "Πόλεμος των Άστρων",
			expected: "πολεμοστωναστρων",
		},
		{
			name:     "arabic",
			input:    "لعبة الحرب",
			expected: "لعبةالحرب",
		},
		{
			name:     "mixed_unicode_with_metadata",
			input:    "ドラゴンクエストIII (Japan)",
			expected: "ドラゴンクエスト3",
		},
		{
			name:     "turkish_special_case",
			input:    "İstanbul Şehir",
			expected: "istanbulsehir",
		},
		{
			name:     "pokemon_with_accent",
			input:    "Pokémon Stadium",
			expected: "pokemonstadium",
		},
		{
			name:     "cafe_with_accent",
			input:    "Café del Mar",
			expected: "cafedelmar",
		},
		{
			name:     "fullwidth_ascii",
			input:    "Ｓｕｐｅｒ　Ｍａｒｉｏ",
			expected: "supermario",
		},
		{
			name:     "mixed_fullwidth_and_normal",
			input:    "Ｓｕｐｅｒ Mario",
			expected: "supermario",
		},
		{
			name:     "trademark_symbol",
			input:    "Sonic™",
			expected: "sonic",
		},
		{
			name:     "copyright_symbol",
			input:    "Game©2023",
			expected: "game2023",
		},
		{
			name:     "currency_symbol",
			input:    "Game $100",
			expected: "game100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SlugifyString(tt.input)
			assert.Equal(t, tt.expected, result,
				"Script detection and slug selection should be consistent")
		})
	}
}

// TestSlugifyStringRegression_EdgeCaseConsistency tests specific edge cases
// that could be affected by performance optimizations, ensuring behavioral consistency.
func TestSlugifyStringRegression_EdgeCaseConsistency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Edge cases that could break with optimizations
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "only_whitespace",
			input:    "   \t\n   ",
			expected: "",
		},
		{
			name:     "only_special_chars",
			input:    "!@#$%^&*()[]{}",
			expected: "and",
		},
		{
			name:     "only_metadata",
			input:    "(USA) [!] [T+Eng1.0]",
			expected: "",
		},
		{
			name:     "single_character",
			input:    "A",
			expected: "a",
		},
		{
			name:     "single_unicode_char",
			input:    "あ",
			expected: "あ",
		},
		{
			name:     "numbers_only",
			input:    "123",
			expected: "123",
		},
		{
			name:     "leading_and_trailing_spaces",
			input:    "  Super Mario  ",
			expected: "supermario",
		},
		{
			name:     "multiple_consecutive_spaces",
			input:    "Super    Mario    Bros",
			expected: "supermariobros",
		},
		{
			name:     "mixed_separators_consecutive",
			input:    "Game:_-_-Title",
			expected: "gametitle",
		},
		{
			name:     "nested_brackets_parens",
			input:    "Game (Version [Final])",
			expected: "game",
		},
		{
			name:     "malformed_brackets",
			input:    "Game (USA [!]",
			expected: "game",
		},
		{
			name:     "version_with_decimal",
			input:    "Game v2.5.1",
			expected: "game",
		},
		{
			name:     "roman_numeral_mixed_with_numbers",
			input:    "Game 2 VII",
			expected: "game27",
		},
		{
			name:     "apostrophe_possessive_vs_contractions",
			input:    "Mario's vs Mario",
			expected: "mariosvsmario",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SlugifyString(tt.input)
			assert.Equal(t, tt.expected, result,
				"Edge case handling should be consistent")
		})
	}
}

// TestSlugifyStringRegression_PerformanceOptimizationImpact ensures that performance
// optimizations don't change the algorithm's behavior by comparing results before and
// after optimization paths.
func TestSlugifyStringRegression_PerformanceOptimizationImpact(t *testing.T) {
	t.Parallel()

	// Test cases that specifically exercise different optimization paths
	testCases := []string{
		// ASCII fast-path cases
		"Super Mario Bros",
		"The Legend of Zelda",
		"Final Fantasy VII",
		"Sonic & Knuckles",
		"Street Fighter II",

		// Unicode processing cases
		"Pokémon Stadium",
		"Café International",
		"ドラゴンクエストIII",
		"Street Fighter ストリートファイター",
		"Тетрис",

		// Mixed cases
		"Game™ (USA)",
		"Ｓｕｐｅｒ Mario",
		"Game ©2023",
	}

	for _, input := range testCases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			// Run the function multiple times to test consistency
			results := make([]string, 5)
			for i := range results {
				results[i] = SlugifyString(input)
			}

			// All results should be identical
			for i := 1; i < len(results); i++ {
				assert.Equal(t, results[0], results[i],
					"SlugifyString should be deterministic for input: %s", input)
			}

			// Result should not be empty unless input is empty or only special chars
			if input != "" && results[0] == "" {
				// Check if input contains only special characters/metadata
				stripped := StripMetadataBrackets(input)
				stripped = nonAlphanumRegex.ReplaceAllString(stripped, "")
				assert.Empty(t, stripped,
					"Non-empty input should not produce empty slug unless only special chars: %s", input)
			}
		})
	}
}

func TestConvertRomanNumerals_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Word boundaries - should NOT convert
		{
			name:     "word_containing_i_should_not_convert",
			input:    "this is invisible",
			expected: "this is invisible",
		},
		{
			name:     "word_containing_v_should_not_convert",
			input:    "vivid river",
			expected: "vivid river",
		},
		{
			name:     "word_containing_iv_should_not_convert",
			input:    "alive and giving",
			expected: "alive and giving",
		},
		{
			name:     "word_containing_ix_should_not_convert",
			input:    "mixing pixel",
			expected: "mixing pixel",
		},
		{
			name:     "word_containing_vii_should_not_convert",
			input:    "soviet viio",
			expected: "soviet viio",
		},

		// Adjacent to digits - should NOT convert
		{
			name:     "v_adjacent_to_digit",
			input:    "v1.0",
			expected: "v1.0",
		},
		{
			name:     "vii_adjacent_to_digit",
			input:    "version VII2",
			expected: "version vii2",
		},
		{
			name:     "digit_then_roman",
			input:    "1IX test",
			expected: "1ix test",
		},

		// Adjacent to punctuation - SHOULD convert
		{
			name:     "roman_with_colon",
			input:    "Game VII: Subtitle",
			expected: "game 7: subtitle",
		},
		{
			name:     "roman_with_dash",
			input:    "Game III-Part",
			expected: "game 3-part",
		},
		{
			name:     "roman_with_underscore_no_convert",
			input:    "Game_II_Final",
			expected: "game_ii_final", // Underscore is a word char, so no boundary
		},
		{
			name:     "roman_after_underscore_with_space",
			input:    "Game_ II Final",
			expected: "game_ 2 final", // Space creates boundary
		},
		{
			name:     "roman_with_parenthesis",
			input:    "Game (II)",
			expected: "game (2)",
		},

		// At string boundaries
		{
			name:     "roman_at_start",
			input:    "III Kings",
			expected: "3 kings",
		},
		{
			name:     "roman_at_end",
			input:    "Final VII",
			expected: "final 7",
		},
		{
			name:     "only_roman",
			input:    "VII",
			expected: "7",
		},

		// Multiple romans
		{
			name:     "multiple_romans_in_string",
			input:    "Game II Part III",
			expected: "game 2 part 3",
		},
		{
			name:     "three_romans",
			input:    "I II III",
			expected: "1 2 3",
		},

		// Adjacent to Unicode/CJK - SHOULD convert
		{
			name:     "roman_adjacent_to_cjk",
			input:    "ドラゴンクエストIII",
			expected: "ドラゴンクエスト3",
		},
		{
			name:     "roman_between_cjk",
			input:    "ファイナルファンタジーVII",
			expected: "ファイナルファンタジー7",
		},
		{
			name:     "cjk_space_roman",
			input:    "ドラゴンクエスト VII",
			expected: "ドラゴンクエスト 7",
		},

		// Pattern matching order (longest first)
		{
			name:     "xviii_not_viii_then_i",
			input:    "Game XVIII",
			expected: "game 18",
		},
		{
			name:     "xiii_not_iii",
			input:    "Game XIII",
			expected: "game 13",
		},

		// Case variations
		{
			name:     "lowercase_roman",
			input:    "game vii",
			expected: "game 7",
		},
		{
			name:     "mixed_case_roman",
			input:    "Game ViI",
			expected: "game 7",
		},

		// Invalid/non-standard patterns - should NOT convert
		{
			name:     "invalid_iiii",
			input:    "Game IIII",
			expected: "game iiii",
		},
		{
			name:     "invalid_vv",
			input:    "Game VV",
			expected: "game vv",
		},
		{
			name:     "invalid_iix",
			input:    "Game IIX",
			expected: "game iix",
		},

		// Edge cases
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "only_spaces",
			input:    "   ",
			expected: "   ",
		},
		{
			name:     "single_i_at_start",
			input:    "I am",
			expected: "1 am",
		},
		{
			name:     "single_i_at_end",
			input:    "Part I",
			expected: "part 1",
		},

		// Real-world game titles
		{
			name:     "007_world_is_not_enough",
			input:    "007 World Is Not Enough",
			expected: "007 world is not enough",
		},
		{
			name:     "marios_vs_mario",
			input:    "Mario's vs Mario",
			expected: "mario's vs mario",
		},
		{
			name:     "cafe_international",
			input:    "Cafe International",
			expected: "cafe international",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertRomanNumerals(tt.input)
			assert.Equal(t, tt.expected, result, "ConvertRomanNumerals failed for input: %q", tt.input)
		})
	}
}
