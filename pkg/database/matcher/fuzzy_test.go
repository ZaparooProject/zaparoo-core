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
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/hbollon/go-edlib"
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

			var gotSlugs []string
			if len(matches) > 0 {
				gotSlugs = make([]string, 0, len(matches))
				for _, m := range matches {
					gotSlugs = append(gotSlugs, m.Slug)
				}
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
			query:       "ドラゴンクエスト",
			candidates:  []string{"ドラゴンクエスド"}, // one kana swapped, same length, no digits
			maxDistance: 2,
			wantMatch:   true,
			reason:      "should handle small CJK variations within length filter",
		},
		{
			name:        "mixed Latin and CJK small difference",
			query:       "marioaマリオ",
			candidates:  []string{"mariobマリオ"}, // 1 byte different, no digits
			maxDistance: 2,
			wantMatch:   true,
			reason:      "should handle mixed scripts with small differences",
		},
		{
			name:        "differing embedded numbers are not a CJK typo",
			query:       "ドラゴンクエスト7",
			candidates:  []string{"ドラゴンクエスト8"}, // same length, but "7" and "8" are different sequel numbers
			maxDistance: 2,
			wantMatch:   false,
			reason:      "a sequel number is not a typo, even when it's a single differing byte",
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

// TestSameTitleNumbers covers issue #1561: "Street Fighter II" slugifies to
// within one character of "Street Fighter", and Jaro-Winkler alone scores that
// a near-perfect typo match. SameTitleNumbers is what tells them apart.
func TestSameTitleNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		a      string
		b      string
		reason string
		want   bool
	}{
		{
			name:   "identical, no numbers",
			a:      "streetfighter",
			b:      "streetfighter",
			want:   true,
			reason: "trivially equal",
		},
		{
			name:   "sequel number vs none - the reported bug",
			a:      "streetfighter2",
			b:      "streetfighter",
			want:   false,
			reason: "a query one sequel number longer is not a typo of the original",
		},
		{
			name:   "none vs sequel number, reversed",
			a:      "megaman",
			b:      "megaman2",
			want:   false,
			reason: "disagreement is symmetric regardless of which side has the number",
		},
		{
			name:   "same number, roman vs arabic already normalized",
			a:      "streetfighter2",
			b:      "streetfighter2theworldwarrior",
			want:   true,
			reason: "both carry the same sequel number; the extra subtitle words carry none",
		},
		{
			name:   "different numbers",
			a:      "megaman2",
			b:      "megaman3",
			want:   false,
			reason: "different sequels, not a typo of each other",
		},
		{
			name:   "leading zeros ignored",
			a:      "touhou06",
			b:      "touhou6",
			want:   true,
			reason: "06 and 6 name the same numbered entry",
		},
		{
			name:   "lone 1 treated as no number",
			a:      "finalfantasy1",
			b:      "finalfantasy",
			want:   true,
			reason: "a series' first game is often unnumbered",
		},
		{
			name:   "lone 1 does not excuse a real second number",
			a:      "finalfantasy1",
			b:      "finalfantasy2",
			want:   false,
			reason: "1 is excused as unnumbered, but 2 is still a different, real sequel number",
		},
		{
			name:   "multiple digit runs, second run differs",
			a:      "ninjagaiden3chapter2",
			b:      "ninjagaiden3chapter5",
			want:   false,
			reason: "the shared 3 doesn't excuse the differing chapter number",
		},
		{
			name:   "multiple digit runs, both agree",
			a:      "ninjagaiden3chapter2",
			b:      "ninjagaiden3chapter2remastered",
			want:   true,
			reason: "every digit run agrees; extra trailing words carry none",
		},
		{
			name:   "empty strings",
			a:      "",
			b:      "",
			want:   true,
			reason: "no digits on either side",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SameTitleNumbers(tt.a, tt.b)
			assert.Equal(t, tt.want, got, tt.reason)
			// Symmetric by construction (string equality of canonical forms).
			assert.Equal(t, got, SameTitleNumbers(tt.b, tt.a), "must be symmetric")
		})
	}
}

// TestTokenPrefixMatch covers the companion half of #1561: reaching an actual
// sequel game rather than merely refusing to launch the original. MiSTer
// arcade titles like "Street Fighter II The World Warrior" carry no colon or
// dash to mark "The World Warrior" as a subtitle, so a bare "Street Fighter II"
// query needs a delimiter-agnostic, word-boundary prefix check to reach it.
func TestTokenPrefixMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		candidate string
		reason    string
		want      bool
	}{
		{
			name:      "real prefix, no delimiter",
			query:     "Street Fighter II",
			candidate: "Street Fighter II The World Warrior",
			want:      true,
			reason:    "query is a strict word-for-word prefix of the candidate",
		},
		{
			name:      "candidate adds a word before, not after",
			query:     "Street Fighter II",
			candidate: "Super Street Fighter II",
			want:      false,
			reason:    "the extra word comes first, so this is not a prefix relationship",
		},
		{
			name:      "compound word is not a prefix of itself split",
			query:     "Fire",
			candidate: "Firefly",
			want:      false,
			reason:    "token-level comparison: \"firefly\" is one token, not \"fire\"+\"fly\"",
		},
		{
			name:      "genuine multi-word prefix with a compound-looking name",
			query:     "Fire",
			candidate: "Fire Emblem",
			want:      true,
			reason:    "two real words, so the prefix relationship holds",
		},
		{
			name:      "equal titles are not a strict prefix",
			query:     "Street Fighter",
			candidate: "Street Fighter",
			want:      false,
			reason:    "an exact match is handled elsewhere, not as a prefix",
		},
		{
			name:      "candidate shorter than query",
			query:     "Street Fighter II The World Warrior",
			candidate: "Street Fighter II",
			want:      false,
			reason:    "the candidate must have more tokens, not fewer",
		},
		{
			name:      "empty query",
			query:     "",
			candidate: "Street Fighter II",
			want:      false,
			reason:    "an empty query has no tokens to prefix-match with",
		},
		{
			name:      "colon already normalized away before tokenizing",
			query:     "Street Fighter II",
			candidate: "Street Fighter II: The World Warrior",
			want:      true,
			reason:    "ParseGame drops the colon before tokenizing, so this behaves like the bare case",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TokenPrefixMatch(slugs.MediaTypeGame, tt.query, tt.candidate)
			assert.Equal(t, tt.want, got, tt.reason)
		})
	}
}

// TestTokenCoverageRatio covers issue #1561's fuzzy residual: a whole-string
// Jaro-Winkler score can be high for reasons that have nothing to do with
// being the same title. "Street Fighter II Turbo" scores 0.927 against
// "Street Fighter Zero 2" by character overlap alone - they share a
// "streetfighter" prefix and both carry a "2", so SameTitleNumbers doesn't
// separate them either - even though "turbo" and "zero" are simply unrelated
// words, which per-token comparison sees directly. "Metriod" (a typo of
// "Metroid," not itself indexed on the system where this was found) scores
// "Mr. Do!" ("misterdo" once "Mr." expands) at 0.855 with barely a shared
// prefix at all - the same failure mode, a different shape.
func TestTokenCoverageRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		candidate string
		reason    string
		want      float64
	}{
		{
			name:      "long shared prefix, one unrelated word on each side",
			query:     "Street Fighter II Turbo",
			candidate: "Street Fighter Zero 2",
			want:      0.75,
			reason:    "\"turbo\" has no match; \"street\", \"fighter\" and \"2\" all do (3 of 4)",
		},
		{
			name:      "no shared prefix, coincidentally high Jaro-Winkler score",
			query:     "Metriod",
			candidate: "Mr. Do!",
			want:      0,
			reason:    "\"metriod\" matches neither \"mister\" nor \"do\" closely enough",
		},
		{
			name:      "single letter must not credit an unrelated abbreviation",
			query:     "Rocky V",
			candidate: "Rocky Versus Apollo",
			want:      0.5,
			reason: "a single-letter word is trivially \"one deletion away\" from any two-letter " +
				"abbreviation containing it (here \"v\" from \"vs\"); \"v\" must not silently expand to " +
				"\"versus\" and credit an unrelated candidate word - only \"rocky\" (1 of 2) is covered",
		},
		{
			name:      "ordinary typo: missing letter",
			query:     "Donky Kong Country",
			candidate: "Donkey Kong Country",
			want:      1,
		},
		{
			name:      "ordinary typo: transposition",
			query:     "Mraio",
			candidate: "Mario",
			want:      1,
		},
		{
			name:      "bare prefix: candidate has extra words, not penalized",
			query:     "Street Fighter II",
			candidate: "Street Fighter II The World Warrior",
			want:      1,
			reason:    "every query word is covered; the candidate's extra words don't count against it",
		},
		{
			name:      "lone 1 is never required, same as SameTitleNumbers",
			query:     "Final Fantasy I",
			candidate: "Final Fantasy",
			want:      1,
			reason:    "a series' first game is often unnumbered",
		},
		{
			name:      "typo of an abbreviation still credits its expansion",
			query:     "Super Mario Bross",
			candidate: "Super Mario Brothers",
			want:      1,
			reason:    "\"bross\" is one edit from \"bros\", which normal slugification expands to \"brothers\"",
		},
		{
			name:      "'n contraction, attached vs spelled out",
			query:     "Ghosts n Goblins",
			candidate: "Ghosts'n Goblins",
			want:      1,
			reason:    "both normalize \"n\"/\"'n\" to \"and\" before tokenizing",
		},
		{
			name:      "'n contraction, glued on both sides vs spelled out",
			query:     "Bump n Jump",
			candidate: "Bump'n'Jump",
			want:      1,
			reason: "a real MiSTer catalog title (\"Bump'n'Jump\") glues the contraction on both sides " +
				"with no space at all, unlike \"Ghosts'n Goblins\" which only glues the left side",
		},
		{
			name:      "identical strings",
			query:     "Street Fighter",
			candidate: "Street Fighter",
			want:      1,
		},
		{
			name:      "empty query",
			query:     "",
			candidate: "Street Fighter",
			want:      0,
		},
		{
			name:      "no valid perfect matching exists",
			query:     "Mario Kart",
			candidate: "Kario Mart",
			want:      0.5,
			reason:    "\"kart\" is not close enough to the remaining \"mart\" once \"mario\" claims \"kario\"",
		},
		{
			name:      "requires a maximum matching, not a greedy one",
			query:     "Cattle Castel",
			candidate: "Castle Battle",
			want:      1,
			reason: "greedily assigning \"cattle\"'s single best match (\"castle\") first leaves \"castel\" " +
				"with nothing, even though the query's incidental word order must not change whether " +
				"coverage is complete: cattle-battle and castel-castle both clear the similarity threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TokenCoverageRatio(slugs.MediaTypeGame, tt.query, tt.candidate)
			assert.InDelta(t, tt.want, got, 0.001, tt.reason)
		})
	}
}

// TestTokenCoverageRatioOrderIndependent guards the maximum-matching fix
// directly: reordering the query's own words must never change the ratio,
// since coverage asks whether every word is accounted for, not in what order
// they happened to be typed. A greedy, query-order-dependent assignment
// (assign each query token its single best remaining candidate, in query
// order) can find a matching for one order and miss an equally valid one for
// another - see "requires a maximum matching, not a greedy one" above for a
// concrete case this regression-tests.
func TestTokenCoverageRatioOrderIndependent(t *testing.T) {
	t.Parallel()
	queries := []string{"Cattle Castel", "Castel Cattle"}
	const candidate = "Castle Battle"
	var want float64
	for i, query := range queries {
		got := TokenCoverageRatio(slugs.MediaTypeGame, query, candidate)
		if i == 0 {
			want = got
			continue
		}
		assert.InDelta(t, want, got, 0.001,
			"reordering query tokens changed the ratio: %q vs %q", queries[0], query)
	}
}

func TestFilterByTokenCoverage(t *testing.T) {
	t.Parallel()

	matches := []FuzzyMatch{
		{Slug: "streetfighterzero2", Similarity: 0.927},
		{Slug: "streetfighter2turboo", Similarity: 0.9}, // genuine typo of the query, extra "o"
	}
	namesBySlug := map[string]string{
		"streetfighterzero2":   "Street Fighter Zero 2",
		"streetfighter2turboo": "Street Fighter II Turbo",
	}
	filtered := FilterByTokenCoverage(slugs.MediaTypeGame, "Street Fighter II Turbo", matches, namesBySlug)

	kept := make([]string, 0, len(filtered))
	for _, m := range filtered {
		kept = append(kept, m.Slug)
	}
	assert.NotContains(t, kept, "streetfighterzero2",
		"\"turbo\" has no match in \"Street Fighter Zero 2\" - not a typo of the query")
	assert.Contains(t, kept, "streetfighter2turboo",
		"a genuine typo of the query itself must still pass")
}

// buildSyntheticCandidates generates n deterministic game-title-like slugs
// using a fixed seed for reproducible benchmarks.
func buildSyntheticCandidates(n int) []string {
	words := []string{
		"super", "mario", "zelda", "sonic", "metroid", "castlevania",
		"mega", "man", "final", "fantasy", "dragon", "quest", "street",
		"fighter", "mortal", "kombat", "donkey", "kong", "kirby", "star",
		"fox", "fire", "emblem", "pokemon", "contra", "ninja", "gaiden",
	}

	//nolint:gosec // Deterministic seed for reproducible benchmarks
	rng := rand.New(rand.NewSource(42))
	candidates := make([]string, n)

	for i := range n {
		// Build slug from 2-4 random words
		wordCount := 2 + rng.Intn(3)
		parts := make([]string, wordCount)
		for w := range wordCount {
			parts[w] = words[rng.Intn(len(words))]
		}
		// Append unique suffix to avoid duplicates
		candidates[i] = fmt.Sprintf("%s-%d", strings.Join(parts, "-"), i)
	}

	return candidates
}

func BenchmarkFindFuzzyMatches_500k(b *testing.B) {
	b.ReportAllocs()
	candidates := buildSyntheticCandidates(500_000)
	query := "supermariobros"
	b.ResetTimer()
	for b.Loop() {
		FindFuzzyMatches(query, candidates, 2, 0.85)
	}
}

func BenchmarkFindFuzzyMatches_1M(b *testing.B) {
	b.ReportAllocs()
	candidates := buildSyntheticCandidates(1_000_000)
	query := "supermariobros"
	b.ResetTimer()
	for b.Loop() {
		FindFuzzyMatches(query, candidates, 2, 0.85)
	}
}

func BenchmarkFindFuzzyMatches_LengthPreFilter(b *testing.B) {
	candidates := buildSyntheticCandidates(500_000)
	query := "supermariobros"

	b.Run("maxDistance=3", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			FindFuzzyMatches(query, candidates, 3, 0.85)
		}
	})

	b.Run("maxDistance=10", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			FindFuzzyMatches(query, candidates, 10, 0.85)
		}
	})
}

func BenchmarkFindFuzzyMatches_NoMatch_500k(b *testing.B) {
	b.ReportAllocs()
	candidates := buildSyntheticCandidates(500_000)
	query := "zzzzzzzzzznotarealname"
	b.ResetTimer()
	for b.Loop() {
		FindFuzzyMatches(query, candidates, 2, 0.85)
	}
}

func BenchmarkJaroWinkler_Similarity(b *testing.B) {
	b.ReportAllocs()
	s1 := "supermariobros"
	s2 := "supermraiobrso"
	b.ResetTimer()
	for b.Loop() {
		edlib.JaroWinklerSimilarity(s1, s2)
	}
}

func BenchmarkGenerateTokenSignature_500k(b *testing.B) {
	b.ReportAllocs()

	words := []string{
		"Super", "Mario", "Zelda", "Sonic", "Metroid", "Castlevania",
		"Mega", "Man", "Final", "Fantasy", "Dragon", "Quest", "Street",
		"Fighter", "Mortal", "Kombat", "Donkey", "Kong", "Kirby", "Star",
		"Fox", "Fire", "Emblem", "Pokemon", "Contra", "Ninja", "Gaiden",
	}

	//nolint:gosec // Deterministic seed for reproducible benchmarks
	rng := rand.New(rand.NewSource(42))
	titles := make([]string, 500_000)
	for i := range 500_000 {
		wordCount := 2 + rng.Intn(3)
		parts := make([]string, wordCount)
		for w := range wordCount {
			parts[w] = words[rng.Intn(len(words))]
		}
		titles[i] = strings.Join(parts, " ")
	}

	b.ResetTimer()
	for b.Loop() {
		for _, title := range titles {
			GenerateTokenSignature(slugs.MediaTypeGame, title)
		}
	}
}
