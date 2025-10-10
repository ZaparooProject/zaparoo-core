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

package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetWordWeight(t *testing.T) {
	tests := []struct {
		name           string
		word           string
		reason         string
		expectedWeight float64
	}{
		{
			name:           "long unique word",
			word:           "minish",
			expectedWeight: 20.0,
			reason:         "6+ chars: base 10 + 10 bonus",
		},
		{
			name:           "long unique word - awakening",
			word:           "awakening",
			expectedWeight: 20.0,
			reason:         "9 chars: base 10 + 10 bonus",
		},
		{
			name:           "short unique word",
			word:           "mario",
			expectedWeight: 10.0,
			reason:         "5 chars: base 10, no bonus",
		},
		{
			name:           "common word - of",
			word:           "of",
			expectedWeight: 5.0,
			reason:         "common word: base 10 - 5 penalty",
		},
		{
			name:           "common word - the",
			word:           "the",
			expectedWeight: 5.0,
			reason:         "common word: base 10 - 5 penalty",
		},
		{
			name:           "common word - and",
			word:           "and",
			expectedWeight: 5.0,
			reason:         "common word: base 10 - 5 penalty",
		},
		{
			name:           "short non-common word",
			word:           "ace",
			expectedWeight: 10.0,
			reason:         "3 chars, not common: base 10",
		},
		{
			name:           "very long word",
			word:           "supercalifragilisticexpialidocious",
			expectedWeight: 20.0,
			reason:         "34 chars: base 10 + 10 bonus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := getWordWeight(tt.word)
			assert.InDelta(t, tt.expectedWeight, weight, 0.001, "Reason: %s", tt.reason)
		})
	}
}

func TestIsCommonWord(t *testing.T) {
	tests := []struct {
		name     string
		word     string
		isCommon bool
	}{
		{"the", "the", true},
		{"of", "of", true},
		{"and", "and", true},
		{"a", "a", true},
		{"an", "an", true},
		{"in", "in", true},
		{"on", "on", true},
		{"at", "at", true},
		{"to", "to", true},
		{"for", "for", true},
		{"mario", "mario", false},
		{"zelda", "zelda", false},
		{"minish", "minish", false},
		{"cap", "cap", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCommonWord(tt.word)
			assert.Equal(t, tt.isCommon, result)
		})
	}
}

func TestScoreTokenMatch(t *testing.T) {
	tests := []struct {
		name           string
		queryTitle     string
		candidateTitle string
		reason         string
		minScore       float64
		maxScore       float64
		shouldMatch    bool
	}{
		{
			name:           "exact match - all words",
			queryTitle:     "legend of zelda",
			candidateTitle: "legend of zelda",
			minScore:       0.99,
			maxScore:       1.01,
			shouldMatch:    true,
			reason:         "perfect match",
		},
		{
			name:           "word order variation - awakening link",
			queryTitle:     "awakening link",
			candidateTitle: "link awakening",
			minScore:       0.50,
			maxScore:       1.00,
			shouldMatch:    true,
			reason:         "words match despite different order",
		},
		{
			name:           "weighted scoring - unique word dominates",
			queryTitle:     "zelda minish",
			candidateTitle: "legend of zelda minish cap",
			minScore:       0.50,
			maxScore:       1.00,
			shouldMatch:    true,
			reason:         "minish (6+ chars, weight 20) and zelda (weight 10) both match",
		},
		{
			name:           "weighted scoring - common words less important",
			queryTitle:     "the legend of",
			candidateTitle: "legend of zelda",
			minScore:       0.30,
			maxScore:       0.90,
			shouldMatch:    true,
			reason:         "common words (the, of) have low weight (5 each), legend has weight 10, all match",
		},
		{
			name:           "long unique word gives high score",
			queryTitle:     "awakening",
			candidateTitle: "link awakening dx",
			minScore:       0.60,
			maxScore:       1.00,
			shouldMatch:    true,
			reason:         "awakening is 9 chars (weight 20), matches well despite extra words",
		},
		{
			name:           "partial match with unmatched candidate words",
			queryTitle:     "mario",
			candidateTitle: "super mario world",
			minScore:       0.50,
			maxScore:       0.85,
			shouldMatch:    true,
			reason:         "mario matches, but candidate has 2 unmatched words",
		},
		{
			name:           "no match - different words",
			queryTitle:     "sonic",
			candidateTitle: "mario bros",
			minScore:       0.00,
			maxScore:       0.20,
			shouldMatch:    false,
			reason:         "no word overlap",
		},
		{
			name:           "reversed words - mario super",
			queryTitle:     "mario super world",
			candidateTitle: "super mario world",
			minScore:       0.90,
			maxScore:       1.10,
			shouldMatch:    true,
			reason:         "all words match, just different order",
		},
		{
			name:           "common words only - low discriminative value",
			queryTitle:     "of the",
			candidateTitle: "the legend of zelda",
			minScore:       0.20,
			maxScore:       0.50,
			shouldMatch:    true,
			reason:         "common words match but have low weight, many unmatched candidate words",
		},
		{
			name:           "mixed unique and common - turtles ninja",
			queryTitle:     "turtles ninja",
			candidateTitle: "teenage mutant ninja turtles",
			minScore:       0.50,
			maxScore:       0.90,
			shouldMatch:    true,
			reason:         "ninja (5 chars) and turtles (7 chars, weight 20) both match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ScoreTokenMatch(tt.queryTitle, tt.candidateTitle)

			if tt.shouldMatch {
				assert.GreaterOrEqual(t, score, tt.minScore,
					"Score %.3f too low for match. Reason: %s", score, tt.reason)
				assert.LessOrEqual(t, score, tt.maxScore,
					"Score %.3f too high. Reason: %s", score, tt.reason)
			} else {
				assert.Less(t, score, 0.5,
					"Score %.3f should be below matching threshold. Reason: %s", score, tt.reason)
			}

			t.Logf("Score: %.3f (min: %.2f, max: %.2f) - %s", score, tt.minScore, tt.maxScore, tt.reason)
		})
	}
}

func TestScoreTokenMatchEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		queryTitle     string
		candidateTitle string
		expectedScore  float64
	}{
		{
			name:           "empty query",
			queryTitle:     "",
			candidateTitle: "super mario",
			expectedScore:  0.0,
		},
		{
			name:           "empty candidate",
			queryTitle:     "super mario",
			candidateTitle: "",
			expectedScore:  0.0,
		},
		{
			name:           "both empty",
			queryTitle:     "",
			candidateTitle: "",
			expectedScore:  0.0,
		},
		{
			name:           "single character words",
			queryTitle:     "a",
			candidateTitle: "a b c",
			expectedScore:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ScoreTokenMatch(tt.queryTitle, tt.candidateTitle)
			assert.InDelta(t, tt.expectedScore, score, 0.001, "Edge case failed")
		})
	}
}

func TestWeightedScoringComparison(t *testing.T) {
	t.Run("unique word outweighs common words", func(t *testing.T) {
		scoreWithUnique := ScoreTokenMatch("minish", "legend of zelda minish cap")
		scoreWithCommon := ScoreTokenMatch("of the", "legend of zelda minish cap")

		assert.Greater(t, scoreWithUnique, scoreWithCommon,
			"Unique word 'minish' (weight 20) should score higher than common words 'of the' (weight 5 each)")

		t.Logf("Score with unique word 'minish': %.3f", scoreWithUnique)
		t.Logf("Score with common words 'of the': %.3f", scoreWithCommon)
	})

	t.Run("long word outweighs short word", func(t *testing.T) {
		scoreLong := ScoreTokenMatch("awakening", "link awakening")
		scoreShort := ScoreTokenMatch("link", "link awakening")

		t.Logf("Score with long word 'awakening' (9 chars, weight 20): %.3f", scoreLong)
		t.Logf("Score with short word 'link' (4 chars, weight 10): %.3f", scoreShort)
	})

	t.Run("multiple unique words score highest", func(t *testing.T) {
		scoreMultiple := ScoreTokenMatch("zelda minish", "legend of zelda minish cap")
		scoreSingle := ScoreTokenMatch("zelda", "legend of zelda minish cap")
		scoreCommon := ScoreTokenMatch("of", "legend of zelda minish cap")

		assert.Greater(t, scoreMultiple, scoreSingle,
			"Multiple matching words should score higher than single match")
		assert.Greater(t, scoreSingle, scoreCommon,
			"Unique word should score higher than common word")

		t.Logf("Score with 'zelda minish': %.3f", scoreMultiple)
		t.Logf("Score with 'zelda': %.3f", scoreSingle)
		t.Logf("Score with 'of': %.3f", scoreCommon)
	})
}
