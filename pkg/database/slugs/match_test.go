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

func TestGenerateMatchInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		input              string
		expectedCanonical  string
		expectedMainTitle  string
		expectedSubtitle   string
		expectedHasSubt    bool
		expectedHasArticle bool
	}{
		{
			name:               "simple_title",
			input:              "Super Mario World",
			expectedCanonical:  "supermarioworld",
			expectedMainTitle:  "supermarioworld",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "with_colon_subtitle",
			input:              "The Legend of Zelda: Link's Awakening",
			expectedCanonical:  "legendofzeldalinksawakening",
			expectedMainTitle:  "legendofzelda",
			expectedSubtitle:   "linksawakening",
			expectedHasSubt:    true,
			expectedHasArticle: true,
		},
		{
			name:               "with_hyphen_subtitle",
			input:              "Mega Man - The Wily Wars",
			expectedCanonical:  "megamanwilywars",
			expectedMainTitle:  "megaman",
			expectedSubtitle:   "wilywars",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "no_subtitle_with_article",
			input:              "The Adventures of Link",
			expectedCanonical:  "adventuresoflink",
			expectedMainTitle:  "adventuresoflink",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: true,
		},
		{
			name:               "complex_with_metadata",
			input:              "Final Fantasy VII: Crisis Core (USA)",
			expectedCanonical:  "finalfantasy7crisiscore",
			expectedMainTitle:  "finalfantasy7",
			expectedSubtitle:   "crisiscore",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "empty_string",
			input:              "",
			expectedCanonical:  "",
			expectedMainTitle:  "",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "whitespace_only",
			input:              "   ",
			expectedCanonical:  "",
			expectedMainTitle:  "",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "only_article",
			input:              "The",
			expectedCanonical:  "the",
			expectedMainTitle:  "the",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "article_with_colon_no_subtitle",
			input:              "The Game:",
			expectedCanonical:  "game",
			expectedMainTitle:  "game",
			expectedSubtitle:   "",
			expectedHasSubt:    true,
			expectedHasArticle: true,
		},
		{
			name:               "hyphen_subtitle_with_article",
			input:              "The Matrix - Reloaded",
			expectedCanonical:  "matrixreloaded",
			expectedMainTitle:  "matrix",
			expectedSubtitle:   "reloaded",
			expectedHasSubt:    true,
			expectedHasArticle: true,
		},
		{
			name:               "multiple_colons",
			input:              "Game: Part: One",
			expectedCanonical:  "gamepartone",
			expectedMainTitle:  "game",
			expectedSubtitle:   "partone",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "multiple_hyphens",
			input:              "Game - Part - One",
			expectedCanonical:  "gamepartone",
			expectedMainTitle:  "game",
			expectedSubtitle:   "partone",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "mixed_colon_and_hyphen",
			input:              "Game: Part - One",
			expectedCanonical:  "gamepartone",
			expectedMainTitle:  "game",
			expectedSubtitle:   "partone",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "colon_at_end",
			input:              "Final Fantasy:",
			expectedCanonical:  "finalfantasy",
			expectedMainTitle:  "finalfantasy",
			expectedSubtitle:   "",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "hyphen_at_end",
			input:              "Final Fantasy - ",
			expectedCanonical:  "finalfantasy",
			expectedMainTitle:  "finalfantasy",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "colon_at_start",
			input:              ": The Beginning",
			expectedCanonical:  "beginning",
			expectedMainTitle:  "",
			expectedSubtitle:   "beginning",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "hyphen_without_spaces",
			input:              "F-Zero",
			expectedCanonical:  "fzero",
			expectedMainTitle:  "fzero",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "single_hyphen_with_spaces",
			input:              "Game -",
			expectedCanonical:  "game",
			expectedMainTitle:  "game",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "article_case_sensitivity",
			input:              "THE LAST OF US",
			expectedCanonical:  "lastofus",
			expectedMainTitle:  "lastofus",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: true,
		},
		{
			name:               "article_mixed_case",
			input:              "ThE Legend",
			expectedCanonical:  "legend",
			expectedMainTitle:  "legend",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: true,
		},
		{
			name:               "subtitle_with_metadata",
			input:              "Zelda: Link (USA)",
			expectedCanonical:  "zeldalink",
			expectedMainTitle:  "zelda",
			expectedSubtitle:   "link",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "article_and_subtitle_with_metadata",
			input:              "The Zelda: Link (USA)",
			expectedCanonical:  "zeldalink",
			expectedMainTitle:  "zelda",
			expectedSubtitle:   "link",
			expectedHasSubt:    true,
			expectedHasArticle: true,
		},
		{
			name:               "special_chars_only",
			input:              "!@#$%",
			expectedCanonical:  "",
			expectedMainTitle:  "",
			expectedSubtitle:   "",
			expectedHasSubt:    false,
			expectedHasArticle: false,
		},
		{
			name:               "unicode_subtitle",
			input:              "Pokémon: Red Version",
			expectedCanonical:  "pokemonred",
			expectedMainTitle:  "pokemon",
			expectedSubtitle:   "red",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "non_latin_main_title",
			input:              "ストリート: Fighter",
			expectedCanonical:  "fighter",
			expectedMainTitle:  "",
			expectedSubtitle:   "fighter",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "non_latin_subtitle",
			input:              "Street: ファイター",
			expectedCanonical:  "street",
			expectedMainTitle:  "street",
			expectedSubtitle:   "",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
		{
			name:               "both_non_latin",
			input:              "ストリート: ファイター",
			expectedCanonical:  "",
			expectedMainTitle:  "",
			expectedSubtitle:   "",
			expectedHasSubt:    true,
			expectedHasArticle: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GenerateMatchInfo(tt.input)
			assert.Equal(t, tt.input, result.OriginalInput)
			assert.Equal(t, tt.expectedCanonical, result.CanonicalSlug)
			assert.Equal(t, tt.expectedMainTitle, result.MainTitleSlug)
			assert.Equal(t, tt.expectedSubtitle, result.SubtitleSlug)
			assert.Equal(t, tt.expectedHasSubt, result.HasSubtitle)
			assert.Equal(t, tt.expectedHasArticle, result.HasLeadingArticle)
		})
	}
}
