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
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/hbollon/go-edlib"
	"github.com/rs/zerolog/log"
)

// FuzzyMatch represents a slug that matches the query with a similarity score.
type FuzzyMatch struct {
	Slug       string
	Similarity float32
}

// FindFuzzyMatches returns slugs that fuzzy match the query using Jaro-Winkler similarity.
// Jaro-Winkler is optimized for short strings and heavily weights matching prefixes,
// making it ideal for game titles where users typically get the start correct.
// It also naturally handles British/American spelling variations (e.g., "colour" vs "color").
// Results are filtered by maxDistance and minSimilarity, sorted by similarity (best first).
func FindFuzzyMatches(query string, candidates []string, maxDistance int, minSimilarity float32) []FuzzyMatch {
	var matches []FuzzyMatch

	for _, candidate := range candidates {
		// Skip exact matches (already handled by earlier strategies)
		if candidate == query {
			continue
		}

		// Length pre-filter: skip candidates with length difference > maxDistance
		lenDiff := len(query) - len(candidate)
		if lenDiff < 0 {
			lenDiff = -lenDiff
		}
		if lenDiff > maxDistance {
			continue
		}

		// Calculate Jaro-Winkler similarity (0.0 to 1.0)
		similarity := edlib.JaroWinklerSimilarity(query, candidate)

		// Debug logging for close matches (helps troubleshoot fuzzy matching)
		if similarity > 0.7 {
			log.Debug().
				Str("query", query).
				Str("candidate", candidate).
				Float32("similarity", similarity).
				Float32("minSimilarity", minSimilarity).
				Msg("fuzzy match candidate evaluation")
		}

		// Filter by minimum similarity threshold
		if similarity >= minSimilarity {
			matches = append(matches, FuzzyMatch{
				Slug:       candidate,
				Similarity: similarity,
			})
		}
	}

	// Sort by similarity (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})

	return matches
}

// GenerateTokenSignature creates a normalized, sorted token signature for word-order independent matching.
// Uses the same tokenization pipeline as slugification to ensure consistency.
//
// IMPORTANT: Requires input with word boundaries (e.g., "Super Mario World"), not slugs.
// Slugs have already lost word boundary information and will produce incorrect signatures.
//
// The mediaType parameter ensures media-type-specific parsing is applied (e.g., ParseGame for games),
// matching the indexing pipeline used in GenerateSlugWithMetadata.
//
// Example:
//
//	GenerateTokenSignature(slugs.MediaTypeGame, "Super Mario World") → "mario_super_world"
//	GenerateTokenSignature(slugs.MediaTypeGame, "Mario World Super") → "mario_super_world"
func GenerateTokenSignature(mediaType slugs.MediaType, gameName string) string {
	// Slugify to get the normalized tokens (same pipeline as database indexing)
	// This now applies media-type-specific parsing internally
	result := slugs.SlugifyWithTokens(mediaType, gameName)

	// Sort tokens alphabetically for order-independent matching
	sort.Strings(result.Tokens)

	// Join with underscore delimiter
	return strings.Join(result.Tokens, "_")
}

// FindTokenSignatureMatches finds candidates where the token signature exactly matches the query signature.
// This enables word-order independent matching: "Crystal Space Quest" matches "Quest Space Crystal".
//
// The query and candidates must have word boundaries (e.g., "Super Mario World"), not slugs.
// The mediaType ensures consistent parsing between query and indexed candidates.
// Returns the slugs of matched titles.
func FindTokenSignatureMatches(
	mediaType slugs.MediaType,
	queryName string,
	candidates []database.MediaTitle,
) []string {
	querySignature := GenerateTokenSignature(mediaType, queryName)

	var matches []string
	for _, candidate := range candidates {
		candidateSignature := GenerateTokenSignature(mediaType, candidate.Name)

		// Exact match ensures all tokens match (order-independent)
		if candidateSignature == querySignature {
			matches = append(matches, candidate.Slug)
		}
	}

	return matches
}

// ApplyDamerauLevenshteinTieBreaker refines fuzzy matches using Damerau-Levenshtein distance
// to handle transposition errors (e.g., "crono tigger" → "Chrono Trigger").
//
// It takes the top N candidates from Jaro-Winkler and re-ranks them by edit distance.
// This two-stage approach is more accurate than either algorithm alone while remaining fast.
func ApplyDamerauLevenshteinTieBreaker(query string, matches []FuzzyMatch, topN int) []FuzzyMatch {
	if len(matches) == 0 {
		return matches
	}

	// Only apply tie-breaking if we have multiple candidates
	if len(matches) == 1 {
		return matches
	}

	// Limit to top N candidates to keep performance fast
	candidates := matches
	if topN > 0 && len(matches) > topN {
		candidates = matches[:topN]
	}

	type dlScore struct {
		match    FuzzyMatch
		distance int
	}

	// Calculate Damerau-Levenshtein distance for top candidates
	scored := make([]dlScore, len(candidates))
	for i, candidate := range candidates {
		dist := edlib.DamerauLevenshteinDistance(query, candidate.Slug)
		scored[i] = dlScore{
			match:    candidate,
			distance: dist,
		}
	}

	// Sort by distance (lower is better)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].distance < scored[j].distance
	})

	// Return re-ranked matches
	result := make([]FuzzyMatch, len(scored))
	for i, s := range scored {
		result[i] = s.match
	}

	return result
}
