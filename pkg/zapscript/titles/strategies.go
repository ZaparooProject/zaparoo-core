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

package titles

import (
	"context"
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/rs/zerolog/log"
)

// FuzzyMatchResult contains the results of a fuzzy matching strategy.
type FuzzyMatchResult struct {
	Strategy   string
	Results    []database.SearchResultWithCursor
	Similarity float64
}

// TryMainTitleOnly attempts main title-only search when query and DB have mismatched secondary titles.
// Handles two cases:
// 1. Query has secondary title, DB doesn't: "Some Game: The Next Gen" → "Some Game" (exact match on main)
// 2. Query lacks secondary title, DB has one: "Some Game" → "Some Game: The Next Gen" (partial match)
// Expects matchInfo to be pre-generated to avoid redundant computation.
func TryMainTitleOnly(
	ctx context.Context,
	gamesdb database.MediaDBI,
	systemID string,
	slug string,
	matchInfo GameMatchInfo,
	tagFilters []zapscript.TagFilter,
	mediaType slugs.MediaType,
) ([]database.SearchResultWithCursor, string, error) {
	// Search using the main title slug (not the full slug)
	mainSlug := matchInfo.MainTitleSlug
	log.Info().Msgf("trying main title DB column prefix search: '%s' (from full slug: '%s')", mainSlug, slug)

	// Prefix search returns all titles starting with the main title slug
	results, err := gamesdb.SearchMediaBySlugPrefix(ctx, systemID, mainSlug, tagFilters)
	if err != nil {
		return nil, "", fmt.Errorf("failed to prefix search for main slug '%s': %w", mainSlug, err)
	}

	if len(results) == 0 {
		return nil, "", nil
	}

	// Post-filter into two categories, preferring exact matches
	var exactMatches []database.SearchResultWithCursor
	var partialMatches []database.SearchResultWithCursor

	for _, result := range results {
		dbMatchInfo := GenerateMatchInfo(mediaType, result.Name)

		// Exact match: Query has secondary title, DB doesn't - DB's full slug matches query's main title
		if dbMatchInfo.CanonicalSlug == mainSlug && !dbMatchInfo.HasSecondaryTitle {
			exactMatches = append(exactMatches, result)
			continue
		}

		// Partial match case 1: Query simple, DB has secondary - DB's main title starts with query's main
		if strings.HasPrefix(dbMatchInfo.MainTitleSlug, mainSlug) &&
			!matchInfo.HasSecondaryTitle && dbMatchInfo.HasSecondaryTitle {
			partialMatches = append(partialMatches, result)
			continue
		}

		// Partial match case 2: Delimiter priority conflict - query's full slug matches DB's main
		// Handles cases like "Hero's Adventure" matching "Hero's Adventure: Crystal Temple"
		// where different delimiters are present causing different split points
		// Uses prefix to handle the delimiter mismatch, but only if query's full slug aligns with DB main
		if strings.HasPrefix(dbMatchInfo.MainTitleSlug, slug) &&
			matchInfo.HasSecondaryTitle && dbMatchInfo.HasSecondaryTitle {
			partialMatches = append(partialMatches, result)
		}
	}

	// Prefer exact matches over partial matches
	if len(exactMatches) > 0 {
		log.Info().Msgf("found %d exact main title matches (filtered from %d): '%s'",
			len(exactMatches), len(results), mainSlug)
		log.Debug().
			Str("strategy", StrategyMainTitleOnly).
			Str("query", mainSlug).
			Int("result_count", len(exactMatches)).
			Msg("exact match found via main title only strategy")
		return exactMatches, StrategyMainTitleOnly, nil
	}

	if len(partialMatches) > 0 {
		log.Info().Msgf("found %d partial main title matches (filtered from %d): '%s'",
			len(partialMatches), len(results), mainSlug)
		log.Debug().
			Str("strategy", StrategyMainTitleOnly).
			Str("query", mainSlug).
			Int("result_count", len(partialMatches)).
			Msg("partial match found via main title only strategy")
		return partialMatches, StrategyMainTitleOnly, nil
	}

	return nil, "", nil
}

// TrySecondaryTitleExact attempts secondary title-only matching when query and DB have mismatched secondary titles.
// Handles two cases:
// 1. Input has secondary title, DB doesn't: "Legend of Zelda: Ocarina of Time" → "Ocarina of Time" (exact match)
// 2. Input lacks secondary title, DB has one: "Ocarina of Time" → "Legend of Zelda: Ocarina of Time" (partial match)
// Expects matchInfo to be pre-generated to avoid redundant computation.
func TrySecondaryTitleExact(
	ctx context.Context,
	gamesdb database.MediaDBI,
	systemID string,
	slug string,
	matchInfo GameMatchInfo,
	tagFilters []zapscript.TagFilter,
	mediaType slugs.MediaType,
) ([]database.SearchResultWithCursor, string, error) {
	// Determine search slug: use secondary title slug if input has one, otherwise use full slug
	var searchSlug string
	if matchInfo.HasSecondaryTitle {
		searchSlug = matchInfo.SecondaryTitleSlug
	} else {
		searchSlug = slug
	}

	log.Info().Msgf("trying secondary title search: '%s' (from full slug: '%s')", searchSlug, slug)

	// Case 1 (Exact): Input has secondary, DB doesn't - search DB's Slug column
	// Example: Input "Legend of Zelda: Ocarina of Time" (secondary="ocarinaoftime")
	//          matches DB "Ocarina of Time" (slug="ocarinaoftime", no secondary)
	exactResults, exactErr := gamesdb.SearchMediaBySlug(ctx, systemID, searchSlug, tagFilters)
	if exactErr != nil {
		log.Warn().Err(exactErr).Msgf("exact secondary title search failed for '%s'", searchSlug)
	} else if len(exactResults) > 0 {
		// Post-filter: only keep DB entries WITHOUT secondary title
		var filtered []database.SearchResultWithCursor
		for _, result := range exactResults {
			dbMatchInfo := GenerateMatchInfo(mediaType, result.Name)
			if !dbMatchInfo.HasSecondaryTitle {
				filtered = append(filtered, result)
			}
		}

		if len(filtered) > 0 {
			log.Info().Msgf("found %d exact secondary title matches (filtered from %d): '%s'",
				len(filtered), len(exactResults), searchSlug)
			log.Debug().
				Str("strategy", StrategySecondaryTitleExact).
				Str("query", searchSlug).
				Int("result_count", len(filtered)).
				Msg("exact match found via secondary title exact strategy")
			return filtered, StrategySecondaryTitleExact, nil
		}
	}

	// Case 2 (Partial): Input simple, DB has secondary - search DB's SecondarySlug column
	// Example: Input "Ocarina of Time" (slug="ocarinaoftime")
	//          matches DB "Legend of Zelda: Ocarina of Time" (secondary_slug="ocarinaoftime")
	partialResults, partialErr := gamesdb.SearchMediaBySecondarySlug(ctx, systemID, searchSlug, tagFilters)
	if partialErr != nil {
		log.Warn().Err(partialErr).Msgf("partial secondary title search failed for '%s'", searchSlug)
		return nil, "", nil
	}

	if len(partialResults) > 0 {
		// Post-filter: only keep DB entries WITH secondary title
		var filtered []database.SearchResultWithCursor
		for _, result := range partialResults {
			dbMatchInfo := GenerateMatchInfo(mediaType, result.Name)
			if dbMatchInfo.HasSecondaryTitle {
				filtered = append(filtered, result)
			}
		}

		if len(filtered) > 0 {
			log.Info().Msgf("found %d partial secondary title matches (filtered from %d): '%s'",
				len(filtered), len(partialResults), searchSlug)
			log.Debug().
				Str("strategy", StrategySecondaryTitleExact).
				Str("query", searchSlug).
				Int("result_count", len(filtered)).
				Msg("partial match found via secondary title exact strategy")
			return filtered, StrategySecondaryTitleExact, nil
		}
	}

	return nil, "", nil
}

// TryAdvancedFuzzyMatching attempts Strategy 3: Advanced fuzzy matching with single prefilter.
// Uses a single prefilter query, then tries three algorithms in sequence:
// 1. Token signature (word-order independent)
// 2. Jaro-Winkler (typo tolerance, prefix matching)
// 3. Damerau-Levenshtein tie-breaking (transposition handling)
func TryAdvancedFuzzyMatching(
	ctx context.Context,
	gamesdb database.MediaDBI,
	systemID string,
	gameName string,
	slug string,
	tagFilters []zapscript.TagFilter,
	mediaType slugs.MediaType,
) (FuzzyMatchResult, error) {
	if len(slug) < MinSlugLengthForFuzzy {
		return FuzzyMatchResult{}, nil
	}

	log.Info().Msgf("no results yet, trying advanced fuzzy matching for '%s'", slug)

	// Generate metadata for the query to build pre-filter parameters
	// This uses the exact same slugification and tokenization as the indexed data
	metadata := mediadb.GenerateSlugWithMetadata(mediaType, gameName)

	// Build pre-filter query with tolerance thresholds:
	// ±3 characters for edit distance, ±1 word for token count
	minLength := metadata.SlugLength - 3
	if minLength < 0 {
		minLength = 0
	}
	maxLength := metadata.SlugLength + 3
	minWordCount := metadata.SlugWordCount - 1
	if minWordCount < 1 {
		minWordCount = 1
	}
	maxWordCount := metadata.SlugWordCount + 1

	log.Debug().
		Int("slug_length", metadata.SlugLength).
		Int("slug_word_count", metadata.SlugWordCount).
		Int("min_length", minLength).
		Int("max_length", maxLength).
		Int("min_word_count", minWordCount).
		Int("max_word_count", maxWordCount).
		Msg("using pre-filter for advanced fuzzy matching")

	// Fetch pre-filtered candidates once for all fuzzy strategies
	candidateTitles, err := gamesdb.GetTitlesWithPreFilter(
		ctx, systemID, minLength, maxLength, minWordCount, maxWordCount)
	if err != nil {
		log.Warn().Err(err).Msg("failed to fetch pre-filtered candidates")
		return FuzzyMatchResult{}, nil
	}

	if len(candidateTitles) == 0 {
		return FuzzyMatchResult{}, nil
	}

	log.Info().Msgf("pre-filter reduced candidate set to %d titles", len(candidateTitles))

	// Extract slugs from MediaTitle objects for fuzzy matching
	candidateSlugs := make([]string, 0, len(candidateTitles))
	for _, title := range candidateTitles {
		candidateSlugs = append(candidateSlugs, title.Slug)
	}

	// Sub-strategy 3a: Token signature matching (word-order independent)
	// Uses original game names (not slugs) to preserve word boundaries
	log.Info().Msg("trying token signature matching")
	tokenMatches := matcher.FindTokenSignatureMatches(mediaType, gameName, candidateTitles)

	if len(tokenMatches) > 0 {
		log.Debug().Int("count", len(tokenMatches)).Msg("found token signature matches")
		// Try each token match
		for _, matchSlug := range tokenMatches {
			results, err := gamesdb.SearchMediaBySlug(ctx, systemID, matchSlug, tagFilters)
			if err == nil && len(results) > 0 {
				log.Info().Msgf("found match via token signature: '%s'", matchSlug)
				log.Debug().
					Str("strategy", StrategyTokenSignature).
					Str("query", slug).
					Str("match", matchSlug).
					Int("result_count", len(results)).
					Msg("match found via token signature strategy")
				return FuzzyMatchResult{
					Results:    results,
					Strategy:   StrategyTokenSignature,
					Similarity: 1.0, // Token signature = exact match on all tokens
				}, nil
			}
		}
	}

	// Sub-strategy 3b: Jaro-Winkler fuzzy matching
	log.Info().Msg("trying Jaro-Winkler fuzzy matching")
	fuzzyMatches := matcher.FindFuzzyMatches(
		slug, candidateSlugs, FuzzyMatchMaxLengthDiff, FuzzyMatchMinSimilarity)

	if len(fuzzyMatches) > 0 {
		log.Debug().Int("count", len(fuzzyMatches)).Msg("found Jaro-Winkler candidates")

		// Sub-strategy 3c: Apply Damerau-Levenshtein tie-breaking
		// Only run on top 5 candidates for performance
		const dlTopN = 5
		fuzzyMatches = matcher.ApplyDamerauLevenshteinTieBreaker(slug, fuzzyMatches, dlTopN)
		log.Debug().Msg("applied Damerau-Levenshtein tie-breaking")

		// Try matches in order (best first)
		for _, match := range fuzzyMatches {
			log.Debug().
				Str("slug", match.Slug).
				Float32("similarity", match.Similarity).
				Msg("attempting fuzzy match")
			results, err := gamesdb.SearchMediaBySlug(ctx, systemID, match.Slug, tagFilters)
			if err == nil && len(results) > 0 {
				log.Info().Msgf("found match via fuzzy search with DL tie-breaking: '%s' (similarity=%.2f)",
					match.Slug, match.Similarity)
				log.Debug().
					Str("strategy", StrategyJaroWinklerDamerau).
					Str("query", slug).
					Str("match", match.Slug).
					Float64("similarity", float64(match.Similarity)).
					Int("result_count", len(results)).
					Msg("match found via Jaro-Winkler + Damerau-Levenshtein strategy")
				return FuzzyMatchResult{
					Results:    results,
					Strategy:   StrategyJaroWinklerDamerau,
					Similarity: float64(match.Similarity),
				}, nil
			}
		}
	}

	return FuzzyMatchResult{}, nil
}

// TryProgressiveTrim attempts Strategy 5: Progressive trim candidates (last resort).
// Handles overly-verbose queries by progressively trimming words from the end.
// Uses a single IN query for all candidates (max depth: 3).
func TryProgressiveTrim(
	ctx context.Context,
	gamesdb database.MediaDBI,
	systemID string,
	gameName string,
	slug string,
	tagFilters []zapscript.TagFilter,
	mediaType slugs.MediaType,
) ([]database.SearchResultWithCursor, string, error) {
	log.Info().Msgf("all advanced strategies failed, trying progressive truncation as last resort")
	const maxTrimDepth = 3
	candidates := GenerateProgressiveTrimCandidates(mediaType, gameName, maxTrimDepth)

	if len(candidates) == 0 {
		return nil, "", nil
	}

	// Collect unique slugs for single IN query (only exact matches for now)
	seenSlugs := make(map[string]bool)
	candidateSlugs := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		if candidate.IsExactMatch && !seenSlugs[candidate.Slug] {
			candidateSlugs = append(candidateSlugs, candidate.Slug)
			seenSlugs[candidate.Slug] = true
		}
	}

	if len(candidateSlugs) == 0 {
		return nil, "", nil
	}

	log.Info().Msgf("searching %d trim candidates with single query", len(candidateSlugs))
	results, err := gamesdb.SearchMediaBySlugIn(ctx, systemID, candidateSlugs, tagFilters)
	if err != nil {
		log.Warn().Err(err).Msg("failed to search with progressive trim candidates")
		return nil, "", nil
	}

	if len(results) > 0 {
		log.Info().Msgf("found %d results using progressive trim", len(results))
		log.Debug().
			Str("strategy", StrategyProgressiveTrim).
			Str("query", slug).
			Int("candidate_count", len(candidateSlugs)).
			Int("result_count", len(results)).
			Msg("match found via progressive trim strategy")
		return results, StrategyProgressiveTrim, nil
	}

	return nil, "", nil
}

// TryWithoutAutoTags attempts fallback strategy: retry without auto-extracted tags
func TryWithoutAutoTags(
	ctx context.Context,
	gamesdb database.MediaDBI,
	systemID string,
	slug string,
	autoExtractedTags []zapscript.TagFilter,
	advArgsTagFilters []zapscript.TagFilter,
) ([]database.SearchResultWithCursor, string, error) {
	if len(autoExtractedTags) == 0 {
		return nil, "", nil
	}

	log.Info().Msgf("no results found with auto-extracted tags, retrying without them")

	// Retry with only explicit user tags (from advArgs)
	fallbackTags := advArgsTagFilters

	// Re-run exact match strategy without auto-extracted tags
	results, err := gamesdb.SearchMediaBySlug(ctx, systemID, slug, fallbackTags)
	if err == nil && len(results) > 0 {
		log.Info().Msgf("found %d results without auto-extracted tags", len(results))
		return results, StrategyExactMatchNoAutoTags, nil
	}

	// If still no results, try prefix match without auto-extracted tags
	prefixResults, prefixErr := gamesdb.SearchMediaBySlugPrefix(ctx, systemID, slug, fallbackTags)
	if prefixErr == nil && len(prefixResults) > 0 {
		log.Info().Msgf("found %d prefix matches without auto-extracted tags", len(prefixResults))
		return prefixResults, StrategyPrefixMatchNoAutoTags, nil
	}

	return nil, "", nil
}
