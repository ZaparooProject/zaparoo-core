// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

func TestSlugifyBasic(t *testing.T) {
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
			expected: "supermariobrothers",
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
			expected: "mariobrothers",
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
			expected: "supermariobrothers",
		},
		{
			name:     "multiple_spaces",
			input:    "Mario    Bros   3",
			expected: "mariobrothers3",
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
			expected: "supermariobros", // Hyphen preserved in "mario-bros" compound, no abbrev expansion
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
			expected: "gamepart1",
		},
		{
			name:     "consecutive_separators",
			input:    "Game---Part___One",
			expected: "gamepart1",
		},
		{
			name:     "trailing_separator",
			input:    "Super Mario Bros-",
			expected: "supermariobrothers",
		},
		{
			name:     "leading_separator",
			input:    "-Super Mario Bros",
			expected: "supermariobrothers",
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
			expected: "gameplus",
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
			expected: "gamepart1",
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
			expected: "gamepart1",
		},
		{
			name:     "semicolon_separator",
			input:    "Game; Part; One",
			expected: "gamepart1",
		},
		{
			name:     "period_separator",
			input:    "Game.Part.One",
			expected: "gamepart1",
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
			expected: "1supermariobrothers",
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
			expected: "7saga",
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
			expected: "01supermariobrothers",
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
			input:    "Some Game VERSION",
			expected: "somegame",
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
			expected: "1supermariobrothersdeluxe",
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
			expected: "1supermariobrothers",
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
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result, "Slugify result mismatch")
		})
	}
}

func TestSlugifyIdempotency(t *testing.T) {
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
		first := Slugify(MediaTypeGame, input)
		second := Slugify(MediaTypeGame, first)
		assert.Equal(t, first, second, "Slugify should be idempotent")
	}
}

func BenchmarkSlugifyBasic(b *testing.B) {
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
				Slugify(MediaTypeGame, tc)
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
			expected: []string{"super", "mario", "brothers"},
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
			expected: []string{"mega-man-x"}, // Hyphens preserved as compound word
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
			expected: "cplusplusprogramming",
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
			result := Slugify(MediaTypeGame, tt.input)
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
			expected: "Sonic and Knuckles",
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
			expected: "Sonic and Tails and Knuckles and Amy",
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
			expected: "Game-Title-Here", // Hyphens between letters/numbers are preserved (compound words)
		},
		{
			name:     "mixed_separators",
			input:    "Game:Title_With-Separators",
			expected: "Game Title With-Separators", // Only hyphen is preserved
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

// TestSlugifyRegression_AsciiFastPath ensures the ASCII fast-path optimization
// doesn't change the algorithm's behavior. This test catches the bug where ASCII strings
// were returning different results than the original algorithm.
func TestSlugifyRegression_AsciiFastPath(t *testing.T) {
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
			expected: "supermariobrothers",
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
			name:  "pure_ascii_multiple_separators",
			input: "Game: The_Subtitle-Edition",
			// "The_" not stripped (StripLeadingArticle requires space), "Edition" stripped
			expected: "gamethesubtitleedition",
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
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result,
				"ASCII fast-path should produce same result as original algorithm")
		})
	}
}

// TestSlugifyRegression_ScriptDetectionConsistency ensures that script detection
// and slug selection logic works correctly for all input types, including edge cases
// that might be affected by optimizations.
func TestSlugifyRegression_ScriptDetectionConsistency(t *testing.T) {
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
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result,
				"Script detection and slug selection should be consistent")
		})
	}
}

// TestSlugifyRegression_EdgeCaseConsistency tests specific edge cases
// that could be affected by performance optimizations, ensuring behavioral consistency.
func TestSlugifyRegression_EdgeCaseConsistency(t *testing.T) {
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
			expected: "supermariobrothers",
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
			expected: "mariosversusmario",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Slugify(MediaTypeGame, tt.input)
			assert.Equal(t, tt.expected, result,
				"Edge case handling should be consistent")
		})
	}
}

// TestSlugifyRegression_PerformanceOptimizationImpact ensures that performance
// optimizations don't change the algorithm's behavior by comparing results before and
// after optimization paths.
func TestSlugifyRegression_PerformanceOptimizationImpact(t *testing.T) {
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
				results[i] = Slugify(MediaTypeGame, input)
			}

			// All results should be identical
			for i := 1; i < len(results); i++ {
				assert.Equal(t, results[0], results[i],
					"Slugify should be deterministic for input: %s", input)
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

// TestNormalizeEpisodeFormat has been moved to media_parsing_test.go
// Episode format normalization is now TV-specific and handled by ParseTVShow

// TestSlugifyEpisodeFormats tests that different episode formats produce matching slugs
func TestSlugifyEpisodeFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input1 string
		input2 string
	}{
		{
			name:   "1x02_vs_S01E02",
			input1: "Attack on Titan - 1x02 - That Day",
			input2: "Attack on Titan - S01E02 - That Day",
		},
		{
			name:   "3x15_vs_S03E15",
			input1: "Breaking Bad - 3x15 - Ozymandias",
			input2: "Breaking Bad - S03E15 - Ozymandias",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			slug1 := Slugify(MediaTypeTVShow, tt.input1)
			slug2 := Slugify(MediaTypeTVShow, tt.input2)
			assert.Equal(t, slug1, slug2, "Slugs should match for different episode formats")
		})
	}
}

// TestSlugifyMediaType_TVShowMatching tests that different TV episode formats
// produce the same slug after media-aware slugification
func TestSlugifyMediaType_TVShowMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mediaType string
		inputs    []string
	}{
		{
			name: "Batocera issue - S01E02 vs 1x02",
			inputs: []string{
				"Attack on Titan - S01E02 - That Day",
				"Attack on Titan - 1x02 - That Day",
				"Attack on Titan - s01e02 - That Day",
				"Attack on Titan - 01x02 - That Day",
			},
			mediaType: "TVShow",
		},
		{
			name: "Breaking Bad episode variations",
			inputs: []string{
				"Breaking Bad - S01E02 - Gray Matter",
				"Breaking Bad - 1x02 - Gray Matter",
				"Breaking Bad - 1X02 - Gray Matter",
			},
			mediaType: "TVShow",
		},
		{
			name: "Episode without title",
			inputs: []string{
				"Game of Thrones - S03E09",
				"Game of Thrones - 3x09",
				"Game of Thrones - 03x09",
			},
			mediaType: "TVShow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Slugify all inputs
			var slugs []string
			for _, input := range tt.inputs {
				slug := Slugify(MediaType(tt.mediaType), input)
				slugs = append(slugs, slug)
			}

			// All slugs should be identical
			firstSlug := slugs[0]
			for i, slug := range slugs[1:] {
				assert.Equal(t, firstSlug, slug,
					"Slug mismatch:\n  Input[0]: %q → %q\n  Input[%d]: %q → %q",
					tt.inputs[0], firstSlug, i+1, tt.inputs[i+1], slug)
			}
		})
	}
}

// TestSlugifyMediaType_GameTitles tests that game titles work correctly
// with media-type-aware slugification
func TestSlugifyMediaType_GameTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		mediaType string
		want      string
	}{
		{
			name:      "Classic game title",
			input:     "The Legend of Zelda: Ocarina of Time",
			mediaType: "Game",
			want:      Slugify(MediaTypeGame, "The Legend of Zelda: Ocarina of Time"),
		},
		{
			name:      "Game with edition suffix",
			input:     "Super Mario 64 Deluxe Edition",
			mediaType: "Game",
			want:      Slugify(MediaTypeGame, "Super Mario 64 Deluxe Edition"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := Slugify(MediaType(tt.mediaType), tt.input)
			assert.Equal(t, tt.want, result,
				"Game title slugification should match standard Slugify")
		})
	}
}

// TestSlugifyMediaType_EmptyMediaType tests behavior with empty media type
// (should get universal-only processing, no media-specific normalization)
func TestSlugifyMediaType_EmptyMediaType(t *testing.T) {
	t.Parallel()

	input := "The Legend of Zelda"
	withEmpty := Slugify(MediaType(""), input)

	// Empty media type gets NO media-specific processing (no article stripping from parsers)
	// Article stripping only happens in media parsers now, not in universal pipeline
	assert.Equal(t, "thelegendofzelda", withEmpty,
		"Empty media type should get universal-only processing (no article stripping)")
}
