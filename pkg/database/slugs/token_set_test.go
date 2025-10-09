// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package slugs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScoreTokenSetRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		candidate   string
		minScore    float64
		shouldMatch bool
		description string
	}{
		{
			name:        "exact_match",
			query:       "Super Mario Bros",
			candidate:   "Super Mario Bros",
			minScore:    0.95,
			shouldMatch: true,
			description: "Identical titles should score very high",
		},
		{
			name:        "word_order_different",
			query:       "awakening link zelda",
			candidate:   "Legend of Zelda Link's Awakening",
			minScore:    0.50,
			shouldMatch: true,
			description: "All query words present despite different order (candidate has extras)",
		},
		{
			name:        "extra_words_in_candidate",
			query:       "Super Mario Bros",
			candidate:   "Super Mario Bros 3",
			minScore:    0.70,
			shouldMatch: true,
			description: "Candidate has extra words, but all query words match",
		},
		{
			name:        "extra_words_in_query",
			query:       "Super Mario Bros 3 NES USA",
			candidate:   "Super Mario Bros 3",
			minScore:    0.60,
			shouldMatch: true,
			description: "Query has metadata words, candidate is core title",
		},
		{
			name:        "duplicate_words_query",
			query:       "mario mario kart",
			candidate:   "Mario Kart",
			minScore:    0.90,
			shouldMatch: true,
			description: "Duplicates in query should be deduplicated",
		},
		{
			name:        "partial_match_high_overlap",
			query:       "zelda awakening",
			candidate:   "Legend of Zelda Link's Awakening",
			minScore:    0.65,
			shouldMatch: true,
			description: "Partial query matches subset of candidate",
		},
		{
			name:        "rom_variant_dx",
			query:       "zelda link awakening dx",
			candidate:   "Legend of Zelda Link's Awakening DX",
			minScore:    0.60,
			shouldMatch: true,
			description: "ROM variant with DX suffix (candidate has legend, of)",
		},
		{
			name:        "rom_variant_no_dx",
			query:       "zelda link awakening dx",
			candidate:   "Legend of Zelda Link's Awakening",
			minScore:    0.40,
			shouldMatch: true,
			description: "Query has DX but candidate doesn't (still partial match)",
		},
		{
			name:        "subtitle_only",
			query:       "ocarina of time",
			candidate:   "Legend of Zelda Ocarina of Time",
			minScore:    0.70,
			shouldMatch: true,
			description: "Subtitle-only query matches full title",
		},
		{
			name:        "japanese_romanization",
			query:       "shin megami tensei",
			candidate:   "Shin Megami Tensei Nocturne",
			minScore:    0.70,
			shouldMatch: true,
			description: "Japanese title base matches variant",
		},
		{
			name:        "no_common_words",
			query:       "sonic hedgehog",
			candidate:   "Mario Kart",
			minScore:    0.0,
			shouldMatch: false,
			description: "No common words should score 0",
		},
		{
			name:        "single_common_word",
			query:       "super mario",
			candidate:   "Super Metroid",
			minScore:    0.30,
			shouldMatch: false,
			description: "Only one common word, low score",
		},
		{
			name:        "verbose_query_core_match",
			query:       "final fantasy 7 international version",
			candidate:   "Final Fantasy VII",
			minScore:    0.50,
			shouldMatch: true,
			description: "Verbose query should match core title",
		},
		{
			name:        "edition_suffix",
			query:       "street fighter",
			candidate:   "Street Fighter Special Edition",
			minScore:    0.70,
			shouldMatch: true,
			description: "Base title matches edition variant",
		},
		{
			name:        "the_article_differences",
			query:       "legend of zelda",
			candidate:   "The Legend of Zelda",
			minScore:    0.90,
			shouldMatch: true,
			description: "Article differences shouldn't matter much",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := ScoreTokenSetRatio(tt.query, tt.candidate)

			t.Logf("Query: '%s'", tt.query)
			t.Logf("Candidate: '%s'", tt.candidate)
			t.Logf("Score: %.3f (min: %.3f)", score, tt.minScore)

			if tt.shouldMatch {
				assert.GreaterOrEqual(t, score, tt.minScore,
					"%s: expected score >= %.2f, got %.2f", tt.description, tt.minScore, score)
			} else {
				assert.Less(t, score, 0.5,
					"%s: expected low score, got %.2f", tt.description, score)
			}
		})
	}
}

func TestScoreTokenSetRatio_CompareWithTokenMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		query           string
		candidate       string
		expectSetBetter bool
		description     string
	}{
		{
			name:            "extra_candidate_words",
			query:           "mario kart",
			candidate:       "Mario Kart 64 Deluxe Edition",
			expectSetBetter: true,
			description:     "Set ratio should handle extra candidate words better",
		},
		{
			name:            "word_reordering",
			query:           "link awakening zelda",
			candidate:       "Zelda Link's Awakening",
			expectSetBetter: false,
			description:     "Both handle reordering well (TokenMatch already order-independent)",
		},
		{
			name:            "duplicate_words",
			query:           "super super mario",
			candidate:       "Super Mario Bros",
			expectSetBetter: true,
			description:     "Set ratio handles duplicates naturally",
		},
		{
			name:            "exact_match_both_good",
			query:           "Super Mario Bros",
			candidate:       "Super Mario Bros",
			expectSetBetter: false,
			description:     "Both should score high on exact match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tokenScore := ScoreTokenMatch(tt.query, tt.candidate)
			setScore := ScoreTokenSetRatio(tt.query, tt.candidate)

			t.Logf("Query: '%s'", tt.query)
			t.Logf("Candidate: '%s'", tt.candidate)
			t.Logf("TokenMatch score: %.3f", tokenScore)
			t.Logf("TokenSetRatio score: %.3f", setScore)

			if tt.expectSetBetter {
				assert.Greater(t, setScore, tokenScore,
					"%s: expected SetRatio (%.3f) > TokenMatch (%.3f)",
					tt.description, setScore, tokenScore)
			} else {
				// For good matches, at least ONE should score high
				maxScore := tokenScore
				if setScore > maxScore {
					maxScore = setScore
				}
				assert.Greater(t, maxScore, 0.7,
					"At least one method should score high (TokenMatch: %.3f, SetRatio: %.3f)",
					tokenScore, setScore)
			}
		})
	}
}

func TestUniqueWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no_duplicates",
			input:    []string{"mario", "kart", "64"},
			expected: []string{"mario", "kart", "64"},
		},
		{
			name:     "with_duplicates",
			input:    []string{"super", "mario", "super", "bros"},
			expected: []string{"super", "mario", "bros"},
		},
		{
			name:     "all_duplicates",
			input:    []string{"zelda", "zelda", "zelda"},
			expected: []string{"zelda"},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "preserves_order",
			input:    []string{"c", "a", "b", "a"},
			expected: []string{"c", "a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := uniqueWords(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetIntersection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		set1     []string
		set2     []string
		expected int
	}{
		{
			name:     "full_overlap",
			set1:     []string{"mario", "kart"},
			set2:     []string{"mario", "kart"},
			expected: 2,
		},
		{
			name:     "partial_overlap",
			set1:     []string{"mario", "kart", "64"},
			set2:     []string{"mario", "party"},
			expected: 1,
		},
		{
			name:     "no_overlap",
			set1:     []string{"sonic", "hedgehog"},
			set2:     []string{"mario", "kart"},
			expected: 0,
		},
		{
			name:     "empty_set",
			set1:     []string{},
			set2:     []string{"mario"},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := setIntersection(tt.set1, tt.set2)
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestSetDifference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		set1     []string
		set2     []string
		expected []string
	}{
		{
			name:     "elements_in_set1_only",
			set1:     []string{"mario", "kart", "64"},
			set2:     []string{"mario"},
			expected: []string{"kart", "64"},
		},
		{
			name:     "no_difference",
			set1:     []string{"mario", "kart"},
			set2:     []string{"mario", "kart"},
			expected: []string{},
		},
		{
			name:     "all_different",
			set1:     []string{"sonic", "hedgehog"},
			set2:     []string{"mario", "kart"},
			expected: []string{"sonic", "hedgehog"},
		},
		{
			name:     "empty_set2",
			set1:     []string{"mario", "kart"},
			set2:     []string{},
			expected: []string{"mario", "kart"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := setDifference(tt.set1, tt.set2)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}
