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

func TestScorePrefixCandidate(t *testing.T) {
	testCases := []struct {
		name           string
		querySlug      string
		candidateSlug  string
		compareTo      string
		expectedHigher bool
	}{
		{
			name:           "SE edition scores higher than sequel 2",
			querySlug:      "alienbreed",
			candidateSlug:  "alienbreedse",
			expectedHigher: true,
			compareTo:      "alienbreed2",
		},
		{
			name:           "Remastered scores higher than sequel",
			querySlug:      "finalfantasy",
			candidateSlug:  "finalfantasyremastered",
			expectedHigher: true,
			compareTo:      "finalfantasy2",
		},
		{
			name:           "Closer length preferred when no editions",
			querySlug:      "speedball",
			candidateSlug:  "speedball",
			expectedHigher: true,
			compareTo:      "speedballextendedmegasuperedition",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			score1 := ScorePrefixCandidate(tc.querySlug, tc.candidateSlug)
			score2 := ScorePrefixCandidate(tc.querySlug, tc.compareTo)

			if tc.expectedHigher {
				assert.Greater(t, score1, score2,
					"Expected %s (score=%d) to score higher than %s (score=%d)",
					tc.candidateSlug, score1, tc.compareTo, score2)
			} else {
				assert.Less(t, score1, score2,
					"Expected %s (score=%d) to score lower than %s (score=%d)",
					tc.candidateSlug, score1, tc.compareTo, score2)
			}
		})
	}
}

func TestTokenizeSlugWords(t *testing.T) {
	testCases := []struct {
		name     string
		slug     string
		expected []string
	}{
		{
			name:     "Simple words",
			slug:     "alien breed se",
			expected: []string{"alien", "breed", "se"},
		},
		{
			name:     "With numbers",
			slug:     "alien breed 2",
			expected: []string{"alien", "breed", "2"},
		},
		{
			name:     "Mixed case normalized",
			slug:     "Alien Breed SE",
			expected: []string{"alien", "breed", "se"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := TokenizeSlugWords(tc.slug)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestStartsWithWordSequence(t *testing.T) {
	testCases := []struct {
		name      string
		candidate []string
		query     []string
		expected  bool
	}{
		{
			name:      "Exact prefix match",
			candidate: []string{"alien", "breed", "se"},
			query:     []string{"alien", "breed"},
			expected:  true,
		},
		{
			name:      "No match different first word",
			candidate: []string{"alienator", "breedx"},
			query:     []string{"alien", "breed"},
			expected:  false,
		},
		{
			name:      "Query longer than candidate",
			candidate: []string{"alien"},
			query:     []string{"alien", "breed"},
			expected:  false,
		},
		{
			name:      "Single word match",
			candidate: []string{"mario", "bros"},
			query:     []string{"mario"},
			expected:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := StartsWithWordSequence(tc.candidate, tc.query)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestHasEditionLikeSuffix(t *testing.T) {
	testCases := []struct {
		slug     string
		expected bool
	}{
		{"alienbreedse", true},
		{"gameremastered", true},
		{"specialedition", true},
		{"directorscut", true},
		{"cd32version", true},
		{"alienbreed2", false},
		{"simpleame", false},
	}

	for _, tc := range testCases {
		t.Run(tc.slug, func(t *testing.T) {
			result := hasEditionLikeSuffix(tc.slug)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestHasSequelLikeSuffix(t *testing.T) {
	testCases := []struct {
		slug     string
		expected bool
	}{
		{"alien breed 2", true},
		{"final fantasy iii", true},
		{"game 4", true},
		{"game vii", true},
		{"alien breed se", false},
		{"simple game", false},
	}

	for _, tc := range testCases {
		t.Run(tc.slug, func(t *testing.T) {
			result := hasSequelLikeSuffix(tc.slug)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestStripLeadingArticle(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"The Minish Cap", "Minish Cap"},
		{"the minish cap", "minish cap"},
		{"A New Hope", "New Hope"},
		{"An Adventure", "Adventure"},
		{"Game", "Game"},
		{"  The  Spaced  ", "Spaced"},
		{"Them", "Them"},
		{"Another Day", "Another Day"},
		{"", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := stripLeadingArticle(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGenerateMatchInfoSecondaryTitleArticleStripping(t *testing.T) {
	testCases := []struct {
		name                      string
		input                     string
		expectedMainSlug          string
		expectedSecondarySlug     string
		expectedHasSecondaryTitle bool
	}{
		{
			name:                      "Colon with leading article in secondary title",
			input:                     "Legend of Zelda: The Minish Cap",
			expectedMainSlug:          "legendofzelda",
			expectedSecondarySlug:     "minishcap",
			expectedHasSecondaryTitle: true,
		},
		{
			name:                      "Dash with leading article in secondary title",
			input:                     "Movie - The Game",
			expectedMainSlug:          "movie",
			expectedSecondarySlug:     "game",
			expectedHasSecondaryTitle: true,
		},
		{
			name:                      "Possessive with leading article in secondary title",
			input:                     "Disney's The Lion King",
			expectedMainSlug:          "disneys",
			expectedSecondarySlug:     "lionking",
			expectedHasSecondaryTitle: true,
		},
		{
			name:                      "Secondary title with 'A' article",
			input:                     "Batman: A Telltale Series",
			expectedMainSlug:          "batman",
			expectedSecondarySlug:     "telltaleseries",
			expectedHasSecondaryTitle: true,
		},
		{
			name:                      "Secondary title with 'An' article",
			input:                     "Game: An Adventure",
			expectedMainSlug:          "game",
			expectedSecondarySlug:     "adventure",
			expectedHasSecondaryTitle: true,
		},
		{
			name:                      "Secondary title without article",
			input:                     "Zelda: Link's Awakening",
			expectedMainSlug:          "zelda",
			expectedSecondarySlug:     "linksawakening",
			expectedHasSecondaryTitle: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GenerateMatchInfo(tc.input)
			assert.Equal(t, tc.expectedMainSlug, result.MainTitleSlug)
			assert.Equal(t, tc.expectedSecondarySlug, result.SecondaryTitleSlug)
			assert.Equal(t, tc.expectedHasSecondaryTitle, result.HasSecondaryTitle)
		})
	}
}
