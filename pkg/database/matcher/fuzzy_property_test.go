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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"pgregory.net/rapid"
)

// ============================================================================
// Generators
// ============================================================================

// slugStringGen generates slug-like strings (lowercase, alphanumeric).
func slugStringGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z0-9]{1,30}`)
}

// gameNameGen generates realistic game name strings with word boundaries.
func gameNameGen() *rapid.Generator[string] {
	words := []string{
		"Super", "Mario", "Bros", "World", "Zelda", "Link", "Sonic",
		"Adventure", "Quest", "Final", "Fantasy", "Dragon", "Crystal",
		"Legend", "Hero", "Star", "Galaxy", "Racing", "Sports", "Battle",
	}
	return rapid.Custom(func(t *rapid.T) string {
		count := rapid.IntRange(1, 4).Draw(t, "wordCount")
		parts := make([]string, count)
		for i := range count {
			parts[i] = rapid.SampledFrom(words).Draw(t, "word")
		}
		return strings.Join(parts, " ")
	})
}

// mediaTypeGen generates valid MediaType values.
func mediaTypeGen() *rapid.Generator[slugs.MediaType] {
	return rapid.SampledFrom([]slugs.MediaType{
		slugs.MediaTypeGame,
		slugs.MediaTypeMovie,
		slugs.MediaTypeApplication,
	})
}

// fuzzyMatchGen generates FuzzyMatch values.
func fuzzyMatchGen() *rapid.Generator[FuzzyMatch] {
	return rapid.Custom(func(t *rapid.T) FuzzyMatch {
		return FuzzyMatch{
			Slug:       slugStringGen().Draw(t, "slug"),
			Similarity: rapid.Float32Range(0.0, 1.0).Draw(t, "similarity"),
		}
	})
}

// fuzzyMatchSliceGen generates a slice of FuzzyMatch values.
func fuzzyMatchSliceGen() *rapid.Generator[[]FuzzyMatch] {
	return rapid.SliceOfN(fuzzyMatchGen(), 0, 20)
}

// mediaTitleGen generates MediaTitle values.
func mediaTitleGen() *rapid.Generator[database.MediaTitle] {
	return rapid.Custom(func(t *rapid.T) database.MediaTitle {
		return database.MediaTitle{
			Name: gameNameGen().Draw(t, "name"),
			Slug: slugStringGen().Draw(t, "slug"),
		}
	})
}

// mediaTitleSliceGen generates a slice of MediaTitle values.
func mediaTitleSliceGen() *rapid.Generator[[]database.MediaTitle] {
	return rapid.SliceOfN(mediaTitleGen(), 0, 20)
}

// ============================================================================
// FindFuzzyMatches Property Tests
// ============================================================================

// TestPropertyFindFuzzyMatchesExactMatchSkipped verifies exact matches are excluded.
func TestPropertyFindFuzzyMatchesExactMatchSkipped(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		otherCandidates := rapid.SliceOfN(slugStringGen(), 0, 10).Draw(t, "others")

		// Include the exact query in candidates
		candidates := append([]string{query}, otherCandidates...)

		matches := FindFuzzyMatches(query, candidates, 5, 0.0)

		// Verify exact match is not in results
		for _, match := range matches {
			if match.Slug == query {
				t.Fatalf("Exact match %q should be excluded from results", query)
			}
		}
	})
}

// TestPropertyFindFuzzyMatchesSimilarityThreshold verifies minSimilarity enforcement.
func TestPropertyFindFuzzyMatchesSimilarityThreshold(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		candidates := rapid.SliceOfN(slugStringGen(), 1, 20).Draw(t, "candidates")
		minSimilarity := rapid.Float32Range(0.0, 1.0).Draw(t, "minSimilarity")

		matches := FindFuzzyMatches(query, candidates, 100, minSimilarity)

		// All matches must meet threshold
		for _, match := range matches {
			if match.Similarity < minSimilarity {
				t.Fatalf("Match %q has similarity %.3f below threshold %.3f",
					match.Slug, match.Similarity, minSimilarity)
			}
		}
	})
}

// TestPropertyFindFuzzyMatchesSortedDescending verifies results are sorted by similarity.
func TestPropertyFindFuzzyMatchesSortedDescending(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		candidates := rapid.SliceOfN(slugStringGen(), 1, 20).Draw(t, "candidates")

		matches := FindFuzzyMatches(query, candidates, 10, 0.0)

		// Verify descending order
		for i := 1; i < len(matches); i++ {
			if matches[i].Similarity > matches[i-1].Similarity {
				t.Fatalf("Results not sorted descending: %.3f > %.3f at index %d",
					matches[i].Similarity, matches[i-1].Similarity, i)
			}
		}
	})
}

// TestPropertyFindFuzzyMatchesLengthPreFilter verifies maxDistance enforcement.
func TestPropertyFindFuzzyMatchesLengthPreFilter(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		candidates := rapid.SliceOfN(slugStringGen(), 1, 20).Draw(t, "candidates")
		maxDistance := rapid.IntRange(0, 10).Draw(t, "maxDistance")

		matches := FindFuzzyMatches(query, candidates, maxDistance, 0.0)

		// All matches must be within length distance
		for _, match := range matches {
			lenDiff := len(match.Slug) - len(query)
			if lenDiff < 0 {
				lenDiff = -lenDiff
			}
			if lenDiff > maxDistance {
				t.Fatalf("Match %q has length diff %d exceeding maxDistance %d",
					match.Slug, lenDiff, maxDistance)
			}
		}
	})
}

// TestPropertyFindFuzzyMatchesDeterministic verifies same inputs produce same outputs.
func TestPropertyFindFuzzyMatchesDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		candidates := rapid.SliceOfN(slugStringGen(), 1, 20).Draw(t, "candidates")
		maxDistance := rapid.IntRange(0, 10).Draw(t, "maxDistance")
		minSimilarity := rapid.Float32Range(0.0, 1.0).Draw(t, "minSimilarity")

		matches1 := FindFuzzyMatches(query, candidates, maxDistance, minSimilarity)
		matches2 := FindFuzzyMatches(query, candidates, maxDistance, minSimilarity)

		if len(matches1) != len(matches2) {
			t.Fatalf("Non-deterministic: got %d and %d matches", len(matches1), len(matches2))
		}

		for i := range matches1 {
			if matches1[i].Slug != matches2[i].Slug ||
				matches1[i].Similarity != matches2[i].Similarity {
				t.Fatalf("Non-deterministic at index %d: %v vs %v", i, matches1[i], matches2[i])
			}
		}
	})
}

// TestPropertyFindFuzzyMatchesSimilarityBounds verifies similarity is in [0, 1].
func TestPropertyFindFuzzyMatchesSimilarityBounds(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		candidates := rapid.SliceOfN(slugStringGen(), 1, 20).Draw(t, "candidates")

		matches := FindFuzzyMatches(query, candidates, 100, 0.0)

		for _, match := range matches {
			if match.Similarity < 0.0 || match.Similarity > 1.0 {
				t.Fatalf("Similarity %.3f out of bounds [0, 1]", match.Similarity)
			}
		}
	})
}

// TestPropertyFindFuzzyMatchesNeverPanics verifies function never panics.
func TestPropertyFindFuzzyMatchesNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := rapid.String().Draw(t, "query")
		candidates := rapid.SliceOfN(rapid.String(), 0, 20).Draw(t, "candidates")
		maxDistance := rapid.Int().Draw(t, "maxDistance")
		minSimilarity := rapid.Float32().Draw(t, "minSimilarity")

		// Should not panic
		_ = FindFuzzyMatches(query, candidates, maxDistance, minSimilarity)
	})
}

// ============================================================================
// GenerateTokenSignature Property Tests
// ============================================================================

// TestPropertyGenerateTokenSignatureOrderIndependent verifies word order doesn't matter.
func TestPropertyGenerateTokenSignatureOrderIndependent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")

		// Generate a name with multiple words
		words := []string{"Mario", "Super", "World"}
		permutation := rapid.Permutation(words).Draw(t, "permutation")

		name1 := strings.Join(words, " ")
		name2 := strings.Join(permutation, " ")

		sig1 := GenerateTokenSignature(mediaType, name1)
		sig2 := GenerateTokenSignature(mediaType, name2)

		if sig1 != sig2 {
			t.Fatalf("Token signature should be order-independent:\n"+
				"  %q -> %q\n  %q -> %q", name1, sig1, name2, sig2)
		}
	})
}

// TestPropertyGenerateTokenSignatureDeterministic verifies same inputs produce same output.
func TestPropertyGenerateTokenSignatureDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		name := gameNameGen().Draw(t, "name")

		sig1 := GenerateTokenSignature(mediaType, name)
		sig2 := GenerateTokenSignature(mediaType, name)

		if sig1 != sig2 {
			t.Fatalf("Same input should produce same signature: %q vs %q", sig1, sig2)
		}
	})
}

// TestPropertyGenerateTokenSignatureNeverEmpty verifies non-empty input produces non-empty output.
func TestPropertyGenerateTokenSignatureNeverEmpty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		name := gameNameGen().Draw(t, "name")

		sig := GenerateTokenSignature(mediaType, name)

		if name != "" && sig == "" {
			t.Fatalf("Non-empty name %q should produce non-empty signature", name)
		}
	})
}

// TestPropertyGenerateTokenSignatureNeverPanics verifies function never panics.
func TestPropertyGenerateTokenSignatureNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		name := rapid.String().Draw(t, "name")

		// Should not panic
		_ = GenerateTokenSignature(mediaType, name)
	})
}

// ============================================================================
// FindTokenSignatureMatches Property Tests
// ============================================================================

// TestPropertyFindTokenSignatureMatchesDeterministic verifies determinism.
func TestPropertyFindTokenSignatureMatchesDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		queryName := gameNameGen().Draw(t, "queryName")
		candidates := mediaTitleSliceGen().Draw(t, "candidates")

		matches1 := FindTokenSignatureMatches(mediaType, queryName, candidates)
		matches2 := FindTokenSignatureMatches(mediaType, queryName, candidates)

		if len(matches1) != len(matches2) {
			t.Fatalf("Non-deterministic: got %d and %d matches", len(matches1), len(matches2))
		}

		for i := range matches1 {
			if matches1[i] != matches2[i] {
				t.Fatalf("Non-deterministic at index %d: %q vs %q", i, matches1[i], matches2[i])
			}
		}
	})
}

// TestPropertyFindTokenSignatureMatchesNeverPanics verifies function never panics.
func TestPropertyFindTokenSignatureMatchesNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		mediaType := mediaTypeGen().Draw(t, "mediaType")
		queryName := rapid.String().Draw(t, "queryName")
		candidates := mediaTitleSliceGen().Draw(t, "candidates")

		// Should not panic
		_ = FindTokenSignatureMatches(mediaType, queryName, candidates)
	})
}

// ============================================================================
// ApplyDamerauLevenshteinTieBreaker Property Tests
// ============================================================================

// TestPropertyDLTieBreakerEmptyReturnsEmpty verifies empty input returns empty.
func TestPropertyDLTieBreakerEmptyReturnsEmpty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		topN := rapid.IntRange(1, 10).Draw(t, "topN")

		result := ApplyDamerauLevenshteinTieBreaker(query, []FuzzyMatch{}, topN)

		if len(result) != 0 {
			t.Fatalf("Empty matches should return empty, got %d", len(result))
		}
	})
}

// TestPropertyDLTieBreakerSingleReturnsUnchanged verifies single match returns unchanged.
func TestPropertyDLTieBreakerSingleReturnsUnchanged(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		match := fuzzyMatchGen().Draw(t, "match")
		topN := rapid.IntRange(1, 10).Draw(t, "topN")

		result := ApplyDamerauLevenshteinTieBreaker(query, []FuzzyMatch{match}, topN)

		if len(result) != 1 {
			t.Fatalf("Single match should return single result, got %d", len(result))
		}
		if result[0].Slug != match.Slug {
			t.Fatalf("Single match should be unchanged: %q vs %q", result[0].Slug, match.Slug)
		}
	})
}

// TestPropertyDLTieBreakerLimitedByTopN verifies result is limited by topN.
func TestPropertyDLTieBreakerLimitedByTopN(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		matches := rapid.SliceOfN(fuzzyMatchGen(), 2, 20).Draw(t, "matches")
		topN := rapid.IntRange(1, 10).Draw(t, "topN")

		result := ApplyDamerauLevenshteinTieBreaker(query, matches, topN)

		// Result should be at most topN or len(matches), whichever is smaller
		expectedMax := topN
		if len(matches) < expectedMax {
			expectedMax = len(matches)
		}

		if len(result) > expectedMax {
			t.Fatalf("Result length %d exceeds expected max %d", len(result), expectedMax)
		}
	})
}

// TestPropertyDLTieBreakerDeterministic verifies same inputs produce same output.
func TestPropertyDLTieBreakerDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		matches := fuzzyMatchSliceGen().Draw(t, "matches")
		topN := rapid.IntRange(1, 10).Draw(t, "topN")

		result1 := ApplyDamerauLevenshteinTieBreaker(query, matches, topN)
		result2 := ApplyDamerauLevenshteinTieBreaker(query, matches, topN)

		if len(result1) != len(result2) {
			t.Fatalf("Non-deterministic: got %d and %d results", len(result1), len(result2))
		}

		for i := range result1 {
			if result1[i].Slug != result2[i].Slug {
				t.Fatalf("Non-deterministic at index %d: %q vs %q",
					i, result1[i].Slug, result2[i].Slug)
			}
		}
	})
}

// TestPropertyDLTieBreakerPreservesSlugs verifies result slugs come from input.
func TestPropertyDLTieBreakerPreservesSlugs(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := slugStringGen().Draw(t, "query")
		matches := fuzzyMatchSliceGen().Draw(t, "matches")
		topN := rapid.IntRange(1, 10).Draw(t, "topN")

		result := ApplyDamerauLevenshteinTieBreaker(query, matches, topN)

		// Build set of input slugs
		inputSlugs := make(map[string]bool)
		for _, m := range matches {
			inputSlugs[m.Slug] = true
		}

		// All result slugs must be from input
		for _, r := range result {
			if !inputSlugs[r.Slug] {
				t.Fatalf("Result slug %q not found in input", r.Slug)
			}
		}
	})
}

// TestPropertyDLTieBreakerNeverPanics verifies function never panics.
func TestPropertyDLTieBreakerNeverPanics(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		query := rapid.String().Draw(t, "query")
		matches := fuzzyMatchSliceGen().Draw(t, "matches")
		topN := rapid.Int().Draw(t, "topN")

		// Should not panic
		_ = ApplyDamerauLevenshteinTieBreaker(query, matches, topN)
	})
}
