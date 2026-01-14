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

package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindFuzzyMatches_PreFilter tests that the length difference pre-filter works correctly.
func TestFindFuzzyMatches_PreFilter(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		reason        string
		candidates    []string
		expectedSlugs []string
		maxDistance   int
		minSimilarity float32
	}{
		{
			name:          "filters out candidates exceeding maxDistance",
			query:         "mario",                                // 5 chars
			candidates:    []string{"mari", "marios", "marioxyz"}, // 4, 6, 8 chars
			maxDistance:   2,
			minSimilarity: 0.70,                       // low threshold to ensure pre-filter is what blocks marioxyz
			expectedSlugs: []string{"marios", "mari"}, // "marioxyz" filtered (diff=3), sorted by similarity
			reason:        "marioxyz has length diff of 3, exceeds maxDistance=2",
		},
		{
			name:          "maxDistance=0 only allows same length",
			query:         "zelda",
			candidates:    []string{"zelda", "zelad", "zeldas"}, // 5, 5, 6 chars
			maxDistance:   0,
			minSimilarity: 0.70,
			expectedSlugs: []string{"zelad"}, // only same length (zelda is exact and skipped)
			reason:        "maxDistance=0 requires exact same length",
		},
		{
			name:          "maxDistance=5 allows wider range",
			query:         "sonic",                                 // 5 chars
			candidates:    []string{"son", "sonics", "sonicmania"}, // 3, 6, 10 chars
			maxDistance:   5,
			minSimilarity: 0.70,
			expectedSlugs: []string{"sonics", "son", "sonicmania"}, // sorted by similarity
			reason:        "maxDistance=5 allows lengths 0-10 (diff up to 5)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindFuzzyMatches(tt.query, tt.candidates, tt.maxDistance, tt.minSimilarity)

			gotSlugs := make([]string, 0, len(matches))
			for _, m := range matches {
				gotSlugs = append(gotSlugs, m.Slug)
			}

			assert.Equal(t, tt.expectedSlugs, gotSlugs, tt.reason)
		})
	}
}

// TestFindFuzzyMatches_SimilarityThreshold tests that the minimum similarity threshold is enforced.
func TestFindFuzzyMatches_SimilarityThreshold(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		reason        string
		candidates    []string
		maxDistance   int
		minSimilarity float32
		expectMatches bool
	}{
		{
			name:          "high threshold filters out dissimilar candidates",
			query:         "mario",
			candidates:    []string{"maria"}, // similar but may not reach 0.95
			maxDistance:   2,
			minSimilarity: 0.95,
			expectMatches: false, // "maria" unlikely to reach 0.95 similarity
			reason:        "maria similarity to mario is below 0.95",
		},
		{
			name:          "low threshold accepts more matches",
			query:         "mario",
			candidates:    []string{"maria"},
			maxDistance:   2,
			minSimilarity: 0.70,
			expectMatches: true, // "maria" should exceed 0.70 similarity
			reason:        "maria similarity to mario exceeds 0.70",
		},
		{
			name:          "production threshold 0.85",
			query:         "zelda",
			candidates:    []string{"zelad"}, // common typo
			maxDistance:   2,
			minSimilarity: 0.85,
			expectMatches: true,
			reason:        "common typo should exceed production threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindFuzzyMatches(tt.query, tt.candidates, tt.maxDistance, tt.minSimilarity)

			if tt.expectMatches {
				assert.NotEmpty(t, matches, tt.reason)
				// Verify all matches meet threshold
				for _, match := range matches {
					assert.GreaterOrEqual(t, match.Similarity, tt.minSimilarity,
						"match %q has similarity %.3f below threshold %.3f",
						match.Slug, match.Similarity, tt.minSimilarity)
				}
			} else {
				assert.Empty(t, matches, tt.reason)
			}
		})
	}
}

// TestFindFuzzyMatches_ExactMatchSkipped tests that exact matches are excluded.
func TestFindFuzzyMatches_ExactMatchSkipped(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		candidates    []string
		expectedSlugs []string
	}{
		{
			name:          "exact match excluded from results",
			query:         "mario",
			candidates:    []string{"mario", "marios", "maria"},
			expectedSlugs: []string{"marios", "maria"}, // "mario" exact match excluded
		},
		{
			name:          "no exact match in candidates",
			query:         "sonic",
			candidates:    []string{"sonics", "sonica"},
			expectedSlugs: []string{"sonics", "sonica"},
		},
		{
			name:          "only exact match candidate",
			query:         "zelda",
			candidates:    []string{"zelda"},
			expectedSlugs: nil, // nil or empty, exact match excluded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindFuzzyMatches(tt.query, tt.candidates, 2, 0.70)

			gotSlugs := make([]string, 0, len(matches))
			for _, m := range matches {
				gotSlugs = append(gotSlugs, m.Slug)
			}

			assert.Equal(t, tt.expectedSlugs, gotSlugs)
		})
	}
}

// TestFindFuzzyMatches_Sorting tests that results are sorted by similarity (descending).
func TestFindFuzzyMatches_Sorting(t *testing.T) {
	t.Run("results sorted by similarity descending", func(t *testing.T) {
		query := "mario"
		candidates := []string{
			"maria",  // medium similarity
			"marios", // high similarity
			"mar",    // low similarity
		}

		matches := FindFuzzyMatches(query, candidates, 3, 0.60)

		require.NotEmpty(t, matches, "expected matches")

		// Verify descending order
		for i := range len(matches) - 1 {
			assert.GreaterOrEqual(t, matches[i].Similarity, matches[i+1].Similarity,
				"match %d (%q: %.3f) should have higher similarity than match %d (%q: %.3f)",
				i, matches[i].Slug, matches[i].Similarity,
				i+1, matches[i+1].Slug, matches[i+1].Similarity)
		}

		// Log results for visibility
		for i, match := range matches {
			t.Logf("Rank %d: %q (similarity: %.3f)", i+1, match.Slug, match.Similarity)
		}
	})

	t.Run("exact similarity values maintain stable order", func(t *testing.T) {
		// When similarities are equal, order should be stable (same as input)
		query := "test"
		candidates := []string{"tess", "tent"} // May have similar scores

		matches := FindFuzzyMatches(query, candidates, 2, 0.70)

		// Verify sorting didn't panic and produced valid results
		require.NotEmpty(t, matches)
		for i := range len(matches) - 1 {
			assert.GreaterOrEqual(t, matches[i].Similarity, matches[i+1].Similarity)
		}
	})
}

// TestFindFuzzyMatches_EdgeCases tests edge cases and boundary conditions.
func TestFindFuzzyMatches_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		reason        string
		candidates    []string
		maxDistance   int
		minSimilarity float32
		expectEmpty   bool
	}{
		{
			name:          "empty candidates list",
			query:         "mario",
			candidates:    []string{},
			maxDistance:   2,
			minSimilarity: 0.85,
			expectEmpty:   true,
			reason:        "no candidates to match",
		},
		{
			name:          "nil candidates list",
			query:         "mario",
			candidates:    nil,
			maxDistance:   2,
			minSimilarity: 0.85,
			expectEmpty:   true,
			reason:        "nil candidates handled gracefully",
		},
		{
			name:          "empty query string",
			query:         "",
			candidates:    []string{"mario", "sonic"},
			maxDistance:   2,
			minSimilarity: 0.85,
			expectEmpty:   true,
			reason:        "empty query produces no matches",
		},
		{
			name:          "single character query",
			query:         "a",
			candidates:    []string{"a", "b", "ab"},
			maxDistance:   2,
			minSimilarity: 0.85,
			expectEmpty:   true, // "a" exact match skipped, others unlikely to match
			reason:        "single char query edge case",
		},
		{
			name:          "very long query",
			query:         "supercalifragilisticexpialidocious",
			candidates:    []string{"supercalifragilisticexpialidocious", "supercalifragilistic"},
			maxDistance:   20,
			minSimilarity: 0.85,
			expectEmpty:   false, // "supercalifragilistic" matches (high similarity, within length)
			reason:        "very long strings handled correctly",
		},
		{
			name:          "all candidates filtered by pre-filter",
			query:         "abc",
			candidates:    []string{"abcdefghij"}, // length diff = 7
			maxDistance:   2,
			minSimilarity: 0.70,
			expectEmpty:   true,
			reason:        "all candidates exceed maxDistance",
		},
		{
			name:          "all candidates below similarity threshold",
			query:         "mario",
			candidates:    []string{"zelda", "sonic", "crash"},
			maxDistance:   5,
			minSimilarity: 0.85,
			expectEmpty:   true,
			reason:        "no candidates similar enough",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindFuzzyMatches(tt.query, tt.candidates, tt.maxDistance, tt.minSimilarity)

			if tt.expectEmpty {
				assert.Empty(t, matches, tt.reason)
			} else {
				assert.NotEmpty(t, matches, tt.reason)
			}
		})
	}
}

// TestFindFuzzyMatches_ProductionScenarios tests realistic scenarios with production values.
func TestFindFuzzyMatches_ProductionScenarios(t *testing.T) {
	const (
		maxDistance   = 2    // production value
		minSimilarity = 0.85 // production value
	)

	tests := []struct {
		name          string
		query         string
		expectedFirst string
		reason        string
		candidates    []string
	}{
		{
			name:          "common typo - transposed letters",
			query:         "zelad",
			candidates:    []string{"zelda"}, // without zeland to avoid confusion
			expectedFirst: "zelda",
			reason:        "transposed letters should match original",
		},
		{
			name:          "common typo - mraio",
			query:         "mraio",
			candidates:    []string{"mario", "mariano"},
			expectedFirst: "mario",
			reason:        "transposed letters in mario",
		},
		{
			name:          "extra letter",
			query:         "zeldaa",
			candidates:    []string{"zelda", "zeldas"},
			expectedFirst: "zelda",
			reason:        "extra letter at end",
		},
		{
			name:          "missing letter",
			query:         "supermrio",
			candidates:    []string{"supermario", "supermariobros"},
			expectedFirst: "supermario",
			reason:        "missing letter in middle",
		},
		{
			name:          "prefix matching advantage",
			query:         "sonic",
			candidates:    []string{"sonics", "xsonic"},
			expectedFirst: "sonics",
			reason:        "Jaro-Winkler weights prefix matches higher",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindFuzzyMatches(tt.query, tt.candidates, maxDistance, minSimilarity)

			require.NotEmpty(t, matches, "expected matches for: %s", tt.reason)
			assert.Equal(t, tt.expectedFirst, matches[0].Slug, tt.reason)

			// Log similarity scores
			for i, match := range matches {
				t.Logf("Rank %d: %q (similarity: %.3f)", i+1, match.Slug, match.Similarity)
			}
		})
	}
}

// TestFindFuzzyMatches_CJK tests fuzzy matching with CJK characters.
func TestFindFuzzyMatches_CJK(t *testing.T) {
	const (
		minSimilarity = 0.85
	)

	tests := []struct {
		name        string
		query       string
		reason      string
		candidates  []string
		maxDistance int
		wantMatch   bool
	}{
		{
			name:        "Japanese katakana exact",
			query:       "ドラゴンクエスト",
			candidates:  []string{"ドラゴンクエスト"},
			maxDistance: 2,
			wantMatch:   false, // exact match is skipped
			reason:      "exact CJK match should be skipped",
		},
		{
			name:        "Japanese katakana small variation",
			query:       "ドラゴンクエスト7",           // with number
			candidates:  []string{"ドラゴンクエスト8"}, // different number (1 byte diff)
			maxDistance: 2,
			wantMatch:   true,
			reason:      "should handle small CJK variations within length filter",
		},
		{
			name:        "mixed Latin and CJK small difference",
			query:       "mario1マリオ",
			candidates:  []string{"mario2マリオ"}, // 1 byte different
			maxDistance: 2,
			wantMatch:   true,
			reason:      "should handle mixed scripts with small differences",
		},
		{
			name: "Chinese characters",
			//nolint:gosmopolitan // Testing CJK character handling
			query: "超级马里奥",
			//nolint:gosmopolitan // Testing CJK character handling
			candidates:  []string{"超级马力奥"}, // 1 char different (里→力)
			maxDistance: 2,
			wantMatch:   true,
			reason:      "should handle Chinese character variations within length filter",
		},
		{
			name:        "CJK length difference exceeds filter",
			query:       "ドラゴン",               // 12 bytes
			candidates:  []string{"ドラゴンクエスト"}, // 24 bytes (diff=12)
			maxDistance: 2,
			wantMatch:   false,
			reason:      "CJK strings with large byte-length differences filtered out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindFuzzyMatches(tt.query, tt.candidates, tt.maxDistance, minSimilarity)

			if tt.wantMatch {
				assert.NotEmpty(t, matches, tt.reason)
				if len(matches) > 0 {
					t.Logf("Match: %q (similarity: %.3f)", matches[0].Slug, matches[0].Similarity)
				}
			} else {
				assert.Empty(t, matches, tt.reason)
			}
		})
	}
}

// TestFindFuzzyMatches_MultipleSimilarCandidates tests behavior with many similar candidates.
func TestFindFuzzyMatches_MultipleSimilarCandidates(t *testing.T) {
	t.Run("returns all candidates above threshold", func(t *testing.T) {
		query := "mario"
		candidates := []string{
			"mario1",
			"mario2",
			"mario3",
			"marioa",
			"mariob",
			"marioc",
		}

		matches := FindFuzzyMatches(query, candidates, 2, 0.85)

		// All should match (length diff = 1)
		assert.NotEmpty(t, matches, "expected multiple matches")

		// Verify sorting
		for i := range len(matches) - 1 {
			assert.GreaterOrEqual(t, matches[i].Similarity, matches[i+1].Similarity)
		}

		t.Logf("Found %d matches", len(matches))
	})
}
