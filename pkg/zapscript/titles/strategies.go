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
	"context"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/rs/zerolog/log"
)

// FuzzyMatchResult contains the results of a fuzzy matching strategy.
type FuzzyMatchResult struct {
	Strategy   string
	Results    []database.SearchResultWithCursor
	Similarity float64
}

// TryMainTitleOnly attempts main title-only search (drops secondary title).
// Expects matchInfo to be pre-generated to avoid redundant computation.
func TryMainTitleOnly(
	ctx context.Context,
	gamesdb database.MediaDBI,
	systemID string,
	slug string,
	matchInfo GameMatchInfo,
	tagFilters []database.TagFilter,
) ([]database.SearchResultWithCursor, string, error) {
	// Safety check (should be pre-filtered by caller)
	if !matchInfo.HasSecondaryTitle || matchInfo.MainTitleSlug == "" || matchInfo.MainTitleSlug == slug {
		return nil, "", nil
	}

	log.Info().Msgf("no results for '%s', trying main title only: '%s'", slug, matchInfo.MainTitleSlug)
	results, err := gamesdb.SearchMediaBySlug(ctx, systemID, matchInfo.MainTitleSlug, tagFilters)
	if err != nil {
		return nil, "", fmt.Errorf("failed to search for main title slug '%s': %w", matchInfo.MainTitleSlug, err)
	}

	if len(results) > 0 {
		log.Debug().
			Str("strategy", StrategyMainTitleOnly).
			Str("query", slug).
			Str("main_title_slug", matchInfo.MainTitleSlug).
			Int("result_count", len(results)).
			Msg("match found via main title only strategy")
		return results, StrategyMainTitleOnly, nil
	}

	return nil, "", nil
}

// TrySecondaryTitleExact attempts secondary title-only exact match.
// Expects matchInfo to be pre-generated to avoid redundant computation.
func TrySecondaryTitleExact(
	ctx context.Context,
	gamesdb database.MediaDBI,
	systemID string,
	slug string,
	matchInfo GameMatchInfo,
	tagFilters []database.TagFilter,
) ([]database.SearchResultWithCursor, string, error) {
	// Safety check (should be pre-filtered by caller)
	if !matchInfo.HasSecondaryTitle ||
		matchInfo.SecondaryTitleSlug == "" ||
		len(matchInfo.SecondaryTitleSlug) < MinSecondaryTitleSlugLength {
		return nil, "", nil
	}

	secondarySlug := matchInfo.SecondaryTitleSlug
	log.Info().Msgf("no results, trying secondary title-only search: '%s'", secondarySlug)

	results, err := gamesdb.SearchMediaBySlug(ctx, systemID, secondarySlug, tagFilters)
	if err != nil {
		log.Warn().Err(err).Msgf("secondary title-only exact search failed for '%s'", secondarySlug)
		return nil, "", nil
	}

	if len(results) > 0 {
		log.Info().Msgf("found %d results using secondary title-only exact: '%s'",
			len(results), secondarySlug)
		log.Debug().
			Str("strategy", StrategySecondaryTitleExact).
			Str("query", slug).
			Str("secondary_slug", secondarySlug).
			Int("result_count", len(results)).
			Msg("match found via secondary title exact strategy")
		return results, StrategySecondaryTitleExact, nil
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
	tagFilters []database.TagFilter,
) (FuzzyMatchResult, error) {
	if len(slug) < MinSlugLengthForFuzzy {
		return FuzzyMatchResult{}, nil
	}

	log.Info().Msgf("no results yet, trying advanced fuzzy matching for '%s'", slug)

	// Generate metadata for the query to build pre-filter parameters
	// This uses the exact same slugification and tokenization as the indexed data
	metadata := mediadb.GenerateSlugWithMetadata(gameName)

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
	tokenMatches := matcher.FindTokenSignatureMatches(gameName, candidateTitles)

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
	tagFilters []database.TagFilter,
) ([]database.SearchResultWithCursor, string, error) {
	log.Info().Msgf("all advanced strategies failed, trying progressive truncation as last resort")
	const maxTrimDepth = 3
	candidates := GenerateProgressiveTrimCandidates(gameName, maxTrimDepth)

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
	autoExtractedTags []database.TagFilter,
	advArgsTagFilters []database.TagFilter,
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
