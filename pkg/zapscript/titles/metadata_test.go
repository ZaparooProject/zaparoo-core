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

package titles

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateMatchInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                      string
		input                     string
		expectedCanonicalSlug     string
		expectedMainTitleSlug     string
		expectedSecondarySlug     string
		expectedHasSecondary      bool
		expectedHasLeadingArticle bool
	}{
		{
			name:                      "simple title without subtitle",
			input:                     "Super Mario World",
			expectedCanonicalSlug:     "supermarioworld",
			expectedMainTitleSlug:     "supermarioworld",
			expectedSecondarySlug:     "",
			expectedHasSecondary:      false,
			expectedHasLeadingArticle: false,
		},
		{
			name:                      "title with colon separator",
			input:                     "The Legend of Zelda: Ocarina of Time",
			expectedCanonicalSlug:     "legendofzeldaocarinaoftime",
			expectedMainTitleSlug:     "legendofzelda",
			expectedSecondarySlug:     "ocarinaoftime",
			expectedHasSecondary:      true,
			expectedHasLeadingArticle: true,
		},
		{
			name:                      "title with dash separator",
			input:                     "Final Fantasy - VII",
			expectedCanonicalSlug:     "finalfantasy7",
			expectedMainTitleSlug:     "finalfantasy",
			expectedSecondarySlug:     "7",
			expectedHasSecondary:      true,
			expectedHasLeadingArticle: false,
		},
		{
			name:                      "title with 's separator",
			input:                     "Link's Awakening",
			expectedCanonicalSlug:     "linksawakening",
			expectedMainTitleSlug:     "links",
			expectedSecondarySlug:     "awakening",
			expectedHasSecondary:      true,
			expectedHasLeadingArticle: false,
		},
		{
			name:                      "title with leading article 'The'",
			input:                     "The Simpsons",
			expectedCanonicalSlug:     "simpsons",
			expectedMainTitleSlug:     "simpsons",
			expectedSecondarySlug:     "",
			expectedHasSecondary:      false,
			expectedHasLeadingArticle: true,
		},
		{
			name:                      "title with leading article 'A'",
			input:                     "A Link to the Past",
			expectedCanonicalSlug:     "linktothepast",
			expectedMainTitleSlug:     "linktothepast",
			expectedSecondarySlug:     "",
			expectedHasSecondary:      false,
			expectedHasLeadingArticle: true,
		},
		{
			name:                      "subtitle with leading article",
			input:                     "Zelda: The Wind Waker",
			expectedCanonicalSlug:     "zeldawindwaker",
			expectedMainTitleSlug:     "zelda",
			expectedSecondarySlug:     "windwaker",
			expectedHasSecondary:      true,
			expectedHasLeadingArticle: false,
		},
		{
			name:                      "complex title with both article and subtitle",
			input:                     "The Elder Scrolls: Skyrim",
			expectedCanonicalSlug:     "elderscrollsskyrim",
			expectedMainTitleSlug:     "elderscrolls",
			expectedSecondarySlug:     "skyrim",
			expectedHasSecondary:      true,
			expectedHasLeadingArticle: true,
		},
		{
			name:                      "empty string",
			input:                     "",
			expectedCanonicalSlug:     "",
			expectedMainTitleSlug:     "",
			expectedSecondarySlug:     "",
			expectedHasSecondary:      false,
			expectedHasLeadingArticle: false,
		},
		{
			name:                      "whitespace only",
			input:                     "   ",
			expectedCanonicalSlug:     "",
			expectedMainTitleSlug:     "",
			expectedSecondarySlug:     "",
			expectedHasSecondary:      false,
			expectedHasLeadingArticle: false,
		},
		{
			name:                      "special characters and numbers",
			input:                     "Street Fighter II: The World Warrior",
			expectedCanonicalSlug:     "streetfighter2worldwarrior",
			expectedMainTitleSlug:     "streetfighter2",
			expectedSecondarySlug:     "worldwarrior",
			expectedHasSecondary:      true,
			expectedHasLeadingArticle: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GenerateMatchInfo(tt.input)

			assert.Equal(t, tt.expectedCanonicalSlug, result.CanonicalSlug, "canonical slug mismatch")
			assert.Equal(t, tt.expectedMainTitleSlug, result.MainTitleSlug, "main title slug mismatch")
			assert.Equal(t, tt.expectedSecondarySlug, result.SecondaryTitleSlug, "secondary slug mismatch")
			assert.Equal(t, tt.expectedHasSecondary, result.HasSecondaryTitle, "has secondary title mismatch")
			assert.Equal(t, tt.expectedHasLeadingArticle, result.HasLeadingArticle, "has leading article mismatch")
			assert.Equal(t, tt.input, result.OriginalInput, "original input should be preserved")
		})
	}
}

func TestGenerateProgressiveTrimCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		input              string
		expectedFirstSlug  string
		expectedLastSlug   string
		maxDepth           int
		expectedCount      int
		minExpectedSlugLen int
	}{
		{
			name:              "simple title with 3 words",
			input:             "Super Mario World",
			maxDepth:          3,
			expectedCount:     4, // 2 slug levels x 2 (exact+prefix), "super" is too short (< 6 chars)
			expectedFirstSlug: "supermarioworld",
			expectedLastSlug:  "supermario", // "super" filtered out (< 6 chars)
		},
		{
			name:               "long title exceeding maxDepth",
			input:              "The Legend of Zelda Ocarina of Time",
			maxDepth:           3,
			expectedCount:      8, // limited by maxDepth: 4 levels x 2 (exact+prefix)
			expectedFirstSlug:  "legendofzeldaocarinaoftime",
			minExpectedSlugLen: 6,
		},
		{
			name:          "title too short (< 3 words)",
			input:         "Mario Bros",
			maxDepth:      3,
			expectedCount: 0, // less than minProgressiveTrimWordCount
		},
		{
			name:          "title with 2 words",
			input:         "Street Fighter",
			maxDepth:      3,
			expectedCount: 0, // less than minProgressiveTrimWordCount
		},
		{
			name:              "title with metadata brackets stripped",
			input:             "Game Title (USA) (Rev 1)",
			maxDepth:          3,
			expectedCount:     0, // "Game Title" has 2 words (< 3), below minProgressiveTrimWordCount
			expectedFirstSlug: "",
			expectedLastSlug:  "",
		},
		{
			name:               "title with edition suffixes stripped",
			input:              "Super Mario World Deluxe Edition",
			maxDepth:           3,
			expectedCount:      6,                       // Edition suffixes stripped, leaving 3 words
			expectedFirstSlug:  "supermarioworlddeluxe", // "Edition" stripped, "Deluxe" remains
			minExpectedSlugLen: 6,
		},
		{
			name:          "empty string",
			input:         "",
			maxDepth:      3,
			expectedCount: 0,
		},
		{
			name:          "maxDepth 0 means no limit",
			input:         "A B C D E",
			maxDepth:      0,
			expectedCount: 0, // Single letter words are filtered out during slugification
		},
		{
			name:               "slug too short gets skipped",
			input:              "Game Title ABCDEF",
			maxDepth:           10,
			expectedCount:      4, // stops when slug < 6 chars
			expectedFirstSlug:  "gametitleabcdef",
			minExpectedSlugLen: 6,
		},
		{
			name:               "duplicate slugs deduplicated",
			input:              "Game Game Game Foo",
			maxDepth:           3,
			expectedCount:      6, // 3 slug levels x 2 (exact+prefix)
			expectedFirstSlug:  "gamegamegamefoo",
			minExpectedSlugLen: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GenerateProgressiveTrimCandidates(tt.input, tt.maxDepth)

			assert.Len(t, result, tt.expectedCount, "candidate count mismatch")

			if tt.expectedCount > 0 {
				if tt.expectedFirstSlug != "" {
					assert.Equal(t, tt.expectedFirstSlug, result[0].Slug, "first slug mismatch")
					assert.True(t, result[0].IsExactMatch, "first candidate should be exact match")
					assert.False(t, result[0].IsPrefixMatch, "first candidate should not be prefix match")
				}

				if tt.expectedLastSlug != "" {
					lastSlug := result[len(result)-1].Slug
					assert.Equal(t, tt.expectedLastSlug, lastSlug, "last slug mismatch")
				}

				// Verify all slugs meet minimum length
				if tt.minExpectedSlugLen > 0 {
					for _, candidate := range result {
						assert.GreaterOrEqual(t, len(candidate.Slug), tt.minExpectedSlugLen,
							"slug %s should meet minimum length", candidate.Slug)
					}
				}

				// Verify alternating exact/prefix pattern
				for i, candidate := range result {
					if i%2 == 0 {
						assert.True(t, candidate.IsExactMatch, "even index should be exact match")
						assert.False(t, candidate.IsPrefixMatch, "even index should not be prefix match")
					} else {
						assert.False(t, candidate.IsExactMatch, "odd index should not be exact match")
						assert.True(t, candidate.IsPrefixMatch, "odd index should be prefix match")
					}
				}
			}
		})
	}
}

func TestGenerateProgressiveTrimCandidatesWordCount(t *testing.T) {
	t.Parallel()

	input := "Super Mario World Special Edition"
	maxDepth := 3

	result := GenerateProgressiveTrimCandidates(input, maxDepth)

	// Verify word counts decrease
	for i := 0; i < len(result); i += 2 { // Check every exact match
		if i+2 < len(result) {
			assert.Greater(t, result[i].WordCount, result[i+2].WordCount,
				"word count should decrease with each trim")
		}
	}
}

func TestGenerateProgressiveTrimCandidatesNoDuplicates(t *testing.T) {
	t.Parallel()

	input := "Super Mario World Special Fun Game"
	maxDepth := 5

	result := GenerateProgressiveTrimCandidates(input, maxDepth)

	// Each slug appears exactly twice: once as exact match, once as prefix match
	// Verify no duplicate exact+prefix pairs
	type key struct {
		slug          string
		isExactMatch  bool
		isPrefixMatch bool
	}
	seen := make(map[key]bool)
	for _, candidate := range result {
		k := key{candidate.Slug, candidate.IsExactMatch, candidate.IsPrefixMatch}
		assert.False(t, seen[k], "duplicate candidate found: %+v", candidate)
		seen[k] = true
	}
}
