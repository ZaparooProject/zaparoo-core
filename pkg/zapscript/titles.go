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

package zapscript

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// reMultiSpace normalizes multiple consecutive spaces to a single space
var reMultiSpace = regexp.MustCompile(`\s+`)

const (
	// Fuzzy matching thresholds
	minSlugLengthForFuzzy   = 5
	fuzzyMatchMaxLengthDiff = 2
	fuzzyMatchMinSimilarity = 0.85

	// Secondary title minimum length for search
	minSecondaryTitleSlugLength = 4
)

// cmdTitle implements the launch.title command for media title-based game launching
//
//nolint:gocritic // single-use parameter in command handler
func cmdTitle(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}

	query := env.Cmd.Args[0]
	if query == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	// Validate title format
	if !isValidTitleFormat(query) {
		return platforms.CmdResult{}, fmt.Errorf(
			"invalid title format: %s (expected SystemID/GameName)", query)
	}

	// Parse SystemID/GameName format
	ps := strings.SplitN(query, "/", 2)
	if len(ps) < 2 {
		return platforms.CmdResult{}, fmt.Errorf("invalid title format: %s (expected SystemID/GameName)", query)
	}

	systemID, gameName := ps[0], ps[1]
	if systemID == "" || gameName == "" {
		return platforms.CmdResult{}, fmt.Errorf(
			"invalid title format: %s (both SystemID and GameName required)", query)
	}

	// Validate system ID
	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to lookup system '%s': %w", systemID, err)
	}
	if system == nil {
		return platforms.CmdResult{}, fmt.Errorf("system not found: %s", systemID)
	}

	// Check system defaults for launcher if not already specified
	if env.Cmd.AdvArgs["launcher"] == "" {
		if systemDefaults, ok := env.Cfg.LookupSystemDefaults(system.ID); ok && systemDefaults.Launcher != "" {
			log.Info().Msgf("using system default launcher for %s: %s", system.ID, systemDefaults.Launcher)
			if env.Cmd.AdvArgs == nil {
				env.Cmd.AdvArgs = make(map[string]string)
			}
			env.Cmd.AdvArgs["launcher"] = systemDefaults.Launcher
		}
	}

	launch, err := getAltLauncher(pl, env)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	// Two-stage tag extraction:
	// 1. Extract explicit canonical tags with operators from parentheses: (-unfinished:beta) (+region:us)
	// 2. Extract filename metadata tags from remaining parentheses: (USA) (1996) (Rev A)

	// Stage 1: Extract canonical tags with operators (e.g., "(-unfinished:beta) (+region:us)")
	canonicalTagFilters, remainingTitle := extractCanonicalTagsFromParens(gameName)

	// Stage 2: Extract filename metadata tags from remaining string (e.g., "(USA) (1996)")
	filenameTags := tags.ParseFilenameToCanonicalTags(remainingTitle)

	// Convert filename tags to tag filters (always AND operator)
	filenameTagFilters := make([]database.TagFilter, 0, len(filenameTags))
	for _, tag := range filenameTags {
		filenameTagFilters = append(filenameTagFilters, database.TagFilter{
			Type:     string(tag.Type),
			Value:    string(tag.Value),
			Operator: database.TagOperatorAND,
		})
	}

	// Keep auto-extracted tags separate for fallback strategy
	autoExtractedTags := mergeTagFilters(filenameTagFilters, canonicalTagFilters)

	// Parse tags from advanced args (these are explicit user requirements)
	advArgsTagFilters := parseTagsAdvArg(env.Cmd.AdvArgs["tags"])

	// Merge all tags for initial search attempt
	// Priority: advanced args > canonical tags > filename tags
	tagFilters := mergeTagFilters(autoExtractedTags, advArgsTagFilters)

	// Slugify the game name (SlugifyString handles metadata stripping in Stage 4)
	// e.g., "Sonic Spinball (USA) (year:1994)" → "sonicspinball"
	slug := slugs.SlugifyString(gameName)
	if slug == "" {
		return platforms.CmdResult{}, fmt.Errorf("game name slugified to empty string: %s", gameName)
	}

	gamesdb := env.Database.MediaDB
	log.Info().Msgf("searching for slug '%s' in system '%s'", slug, system.ID)

	// Check slug resolution cache first
	ctx := context.Background()
	cachedMediaID, cachedStrategy, cacheHit := gamesdb.GetCachedSlugResolution(
		ctx, system.ID, slug, tagFilters)
	if cacheHit {
		log.Info().Msgf("slug resolution cache hit (strategy: %s)", cachedStrategy)
		// Retrieve full result from cached Media DBID
		result, cacheErr := gamesdb.GetMediaByDBID(ctx, cachedMediaID)
		if cacheErr != nil {
			log.Warn().Err(cacheErr).Msg("failed to retrieve cached media, falling back to full resolution")
		}
		if cacheErr == nil {
			log.Info().Msgf("resolved from cache: %s (%s)", result.Name, result.Path)
			return platforms.CmdResult{
				MediaChanged: true,
			}, launch(result.Path)
		}
	}

	// Track which strategy succeeds for cache storage
	var resolvedStrategy string

	// Strategy 0: Exact match
	results, err := gamesdb.SearchMediaBySlug(ctx, system.ID, slug, tagFilters)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to search for slug '%s': %w", slug, err)
	}
	if len(results) > 0 {
		resolvedStrategy = "exact_match"
		log.Debug().
			Str("strategy", resolvedStrategy).
			Str("query", slug).
			Int("result_count", len(results)).
			Msg("match found via exact match strategy")
	}

	// Fallback 1: Prefix search on full normalized title with edition-aware ranking
	if len(results) == 0 {
		log.Info().Msgf("no exact match for '%s', trying prefix search with ranking", slug)
		prefixResults, prefixErr := gamesdb.SearchMediaBySlugPrefix(context.Background(), system.ID, slug, tagFilters)
		if prefixErr != nil {
			log.Warn().Err(prefixErr).Msg("prefix search failed")
		} else if len(prefixResults) > 0 {
			queryWords := slugs.NormalizeToWords(gameName)
			var validCandidates []matcher.PrefixMatchCandidate

			for _, result := range prefixResults {
				candidateWords := slugs.NormalizeToWords(result.Name)

				if len(queryWords) >= 2 && !matcher.StartsWithWordSequence(candidateWords, queryWords) {
					continue
				}

				candidateSlug := slugs.SlugifyString(result.Name)
				score := matcher.ScorePrefixCandidate(slug, candidateSlug)
				validCandidates = append(validCandidates, matcher.PrefixMatchCandidate{
					Slug:  candidateSlug,
					Score: score,
				})
			}

			if len(validCandidates) > 0 {
				bestScore := validCandidates[0].Score
				bestIdx := 0
				for i := 1; i < len(validCandidates); i++ {
					if validCandidates[i].Score > bestScore {
						bestScore = validCandidates[i].Score
						bestIdx = i
					}
				}

				resolvedStrategy = "prefix_match"
				log.Info().Msgf("found %d prefix matches, selected best: '%s' (score=%d)",
					len(validCandidates), validCandidates[bestIdx].Slug, bestScore)
				log.Debug().
					Str("strategy", resolvedStrategy).
					Str("query", slug).
					Str("match", prefixResults[bestIdx].Name).
					Int("score", bestScore).
					Int("result_count", len(validCandidates)).
					Msg("match found via prefix match strategy")
				results = []database.SearchResultWithCursor{prefixResults[bestIdx]}
			} else if len(prefixResults) > 0 {
				// Strategy 1.5: Token-based similarity matching (word-order independent)
				// If word sequence validation filtered out all candidates, try token matching
				log.Info().Msgf(
					"no valid prefix candidates, trying token-based matching on %d results",
					len(prefixResults),
				)

				type tokenMatchCandidate struct {
					result database.SearchResultWithCursor
					score  float64
				}
				var tokenCandidates []tokenMatchCandidate

				for _, result := range prefixResults {
					tokenScore := matcher.ScoreTokenMatch(gameName, result.Name)
					setScore := matcher.ScoreTokenSetRatio(gameName, result.Name)

					bestScore := tokenScore
					if setScore > bestScore {
						bestScore = setScore
					}

					if bestScore > matcher.TokenMatchMinScore {
						tokenCandidates = append(tokenCandidates, tokenMatchCandidate{
							result: result,
							score:  bestScore,
						})
					}
				}

				if len(tokenCandidates) > 0 {
					bestScore := tokenCandidates[0].score
					bestIdx := 0
					for i := 1; i < len(tokenCandidates); i++ {
						if tokenCandidates[i].score > bestScore {
							bestScore = tokenCandidates[i].score
							bestIdx = i
						}
					}

					resolvedStrategy = "token_match"
					log.Info().Msgf("found %d token matches, selected best: '%s' (score=%.2f)",
						len(tokenCandidates), tokenCandidates[bestIdx].result.Name, bestScore)
					log.Debug().
						Str("strategy", resolvedStrategy).
						Str("query", slug).
						Str("match", tokenCandidates[bestIdx].result.Name).
						Float64("score", bestScore).
						Int("result_count", len(tokenCandidates)).
						Msg("match found via token-based matching strategy")
					results = []database.SearchResultWithCursor{tokenCandidates[bestIdx].result}
				}
			}
		}
	}

	// Strategy 2: Secondary title-dropping main title search
	if len(results) == 0 {
		matchInfo := matcher.GenerateMatchInfo(gameName)
		if matchInfo.HasSecondaryTitle && matchInfo.MainTitleSlug != "" && matchInfo.MainTitleSlug != slug {
			log.Info().Msgf("no results for '%s', trying main title only: '%s'", slug, matchInfo.MainTitleSlug)
			results, err = gamesdb.SearchMediaBySlug(
				context.Background(), system.ID, matchInfo.MainTitleSlug, tagFilters)
			if err != nil {
				return platforms.CmdResult{},
					fmt.Errorf("failed to search for main title slug '%s': %w", matchInfo.MainTitleSlug, err)
			}
			if len(results) > 0 {
				resolvedStrategy = "main_title_only"
				log.Debug().
					Str("strategy", resolvedStrategy).
					Str("query", slug).
					Str("main_title_slug", matchInfo.MainTitleSlug).
					Int("result_count", len(results)).
					Msg("match found via main title only strategy")
			}
		}
	}

	// Fallback 3: Secondary title-only literal search
	if len(results) == 0 {
		matchInfo := matcher.GenerateMatchInfo(gameName)
		if matchInfo.HasSecondaryTitle &&
			matchInfo.SecondaryTitleSlug != "" &&
			len(matchInfo.SecondaryTitleSlug) >= minSecondaryTitleSlugLength {
			secondarySlug := matchInfo.SecondaryTitleSlug
			log.Info().Msgf("no results, trying secondary title-only search: '%s'", secondarySlug)

			results, err = gamesdb.SearchMediaBySlug(
				context.Background(), system.ID, secondarySlug, tagFilters)
			if err != nil {
				log.Warn().Err(err).Msgf("secondary title-only exact search failed for '%s'", secondarySlug)
			}

			if len(results) == 0 {
				results, err = gamesdb.SearchMediaBySlugPrefix(
					context.Background(), system.ID, secondarySlug, tagFilters)
				if err != nil {
					log.Warn().Err(err).Msgf(
						"secondary title-only prefix search failed for '%s'", secondarySlug)
				} else if len(results) > 0 {
					resolvedStrategy = "secondary_title_prefix"
					log.Info().Msgf("found %d results using secondary title-only prefix: '%s'",
						len(results), secondarySlug)
					log.Debug().
						Str("strategy", resolvedStrategy).
						Str("query", slug).
						Str("secondary_slug", secondarySlug).
						Int("result_count", len(results)).
						Msg("match found via secondary title prefix strategy")
				}
			} else {
				resolvedStrategy = "secondary_title_exact"
				log.Info().Msgf("found %d results using secondary title-only exact: '%s'",
					len(results), secondarySlug)
				log.Debug().
					Str("strategy", resolvedStrategy).
					Str("query", slug).
					Str("secondary_slug", secondarySlug).
					Int("result_count", len(results)).
					Msg("match found via secondary title exact strategy")
			}
		}
	}

	// Strategy 4: Jaro-Winkler fuzzy matching (typo tolerance, US/UK spelling)
	if len(results) == 0 && len(slug) >= minSlugLengthForFuzzy {
		log.Info().Msgf("no results yet, trying fuzzy matching for '%s'", slug)

		allSlugs, fetchErr := gamesdb.GetAllSlugsForSystem(context.Background(), system.ID)
		if fetchErr != nil {
			log.Warn().Err(fetchErr).Msg("failed to fetch all slugs for fuzzy matching")
		} else if len(allSlugs) > 0 {
			fuzzyMatches := matcher.FindFuzzyMatches(slug, allSlugs, fuzzyMatchMaxLengthDiff, fuzzyMatchMinSimilarity)

			if len(fuzzyMatches) > 0 {
				log.Debug().Int("count", len(fuzzyMatches)).Msg("found fuzzy match candidates")
				for _, match := range fuzzyMatches {
					log.Debug().
						Str("slug", match.Slug).
						Float32("similarity", match.Similarity).
						Msg("attempting fuzzy match")
					results, err = gamesdb.SearchMediaBySlug(
						context.Background(), system.ID, match.Slug, tagFilters)
					if err == nil && len(results) > 0 {
						resolvedStrategy = "jarowinkler_fuzzy"
						log.Info().Msgf("found match via fuzzy search: '%s' (similarity=%.2f)",
							match.Slug, match.Similarity)
						log.Debug().
							Str("strategy", resolvedStrategy).
							Str("query", slug).
							Str("match", match.Slug).
							Float64("similarity", float64(match.Similarity)).
							Int("result_count", len(results)).
							Msg("match found via Jaro-Winkler fuzzy matching")
						break
					}
				}
			}
		}
	}

	// Strategy 5: Progressive trim candidates (last resort)
	// Aggressively removes words from the end - handles overly-verbose queries
	if len(results) == 0 {
		log.Info().Msgf("all strategies failed, trying progressive truncation as last resort")
		candidates := matcher.GenerateProgressiveTrimCandidates(gameName)
		for _, candidate := range candidates {
			log.Info().Msgf("trying progressive trim candidate: '%s' (exact=%v, prefix=%v)",
				candidate.Slug, candidate.IsExactMatch, candidate.IsPrefixMatch)

			if candidate.IsExactMatch {
				results, err = gamesdb.SearchMediaBySlug(
					context.Background(), system.ID, candidate.Slug, tagFilters)
			} else if candidate.IsPrefixMatch {
				results, err = gamesdb.SearchMediaBySlugPrefix(
					context.Background(), system.ID, candidate.Slug, tagFilters)
			}

			if err != nil {
				log.Warn().Err(err).Msgf("failed to search with candidate '%s'", candidate.Slug)
				continue
			}

			if len(results) > 0 {
				resolvedStrategy = "progressive_trim"
				log.Info().Msgf("found %d results using progressive trim: '%s'", len(results), candidate.Slug)
				log.Debug().
					Str("strategy", resolvedStrategy).
					Str("query", slug).
					Str("trim_slug", candidate.Slug).
					Bool("exact", candidate.IsExactMatch).
					Bool("prefix", candidate.IsPrefixMatch).
					Int("result_count", len(results)).
					Msg("match found via progressive trim strategy")
				break
			}
		}
	}

	// Fallback strategy: If no results with auto-extracted tags, retry without them
	if len(results) == 0 && len(autoExtractedTags) > 0 {
		log.Info().Msgf("no results found with auto-extracted tags, retrying without them")

		// Retry with only explicit user tags (from advArgs)
		fallbackTags := advArgsTagFilters

		// Re-run exact match strategy without auto-extracted tags
		results, err = gamesdb.SearchMediaBySlug(ctx, system.ID, slug, fallbackTags)
		if err == nil && len(results) > 0 {
			resolvedStrategy = "exact_match_no_auto_tags"
			log.Info().Msgf("found %d results without auto-extracted tags", len(results))
		}

		// If still no results, try prefix match without auto-extracted tags
		if len(results) == 0 {
			prefixResults, prefixErr := gamesdb.SearchMediaBySlugPrefix(ctx, system.ID, slug, fallbackTags)
			if prefixErr == nil && len(prefixResults) > 0 {
				results = prefixResults
				resolvedStrategy = "prefix_match_no_auto_tags"
				log.Info().Msgf("found %d prefix matches without auto-extracted tags", len(results))
			}
		}

		// If we found results without auto-extracted tags, use ALL tags for selection preferences
		if len(results) > 0 {
			log.Info().Msg("fallback successful: found results by ignoring auto-extracted tag filters")
		}
	}

	if len(results) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no results found for title: %s/%s", system.ID, gameName)
	}

	// If multiple results, apply intelligent selection using ALL tags as preferences
	// This includes both user-provided and auto-extracted tags
	selectedResult := selectBestResult(results, tagFilters, env.Cfg)
	log.Info().Msgf("selected result: %s (%s)", selectedResult.Name, selectedResult.Path)

	// Cache the successful resolution (best effort - don't fail if caching fails)
	if resolvedStrategy != "" {
		if cacheErr := gamesdb.SetCachedSlugResolution(
			ctx, system.ID, slug, tagFilters, selectedResult.MediaID, resolvedStrategy,
		); cacheErr != nil {
			log.Warn().Err(cacheErr).Msg("failed to cache slug resolution")
		}
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, launch(selectedResult.Path)
}

// selectBestResult implements intelligent selection when multiple media match a slug
func selectBestResult(
	results []database.SearchResultWithCursor, tagFilters []database.TagFilter, cfg *config.Instance,
) database.SearchResultWithCursor {
	if len(results) == 1 {
		return results[0]
	}

	log.Info().Msgf("multiple results found (%d), picking best match", len(results))

	// Priority 1: If user provided specific tags, filter by those first
	if len(tagFilters) > 0 {
		filtered := filterByTags(results, tagFilters)
		if len(filtered) == 1 {
			log.Info().Msg("selected result based on user-specified tags")
			return filtered[0]
		}
		if len(filtered) > 0 {
			results = filtered
		}
	}

	// Priority 2: Prefer main game over variants (exclude demos, betas, prototypes, hacks)
	mainGames := filterOutVariants(results)
	if len(mainGames) == 1 {
		log.Info().Msg("selected main game (filtered out variants)")
		return mainGames[0]
	}
	if len(mainGames) > 0 {
		results = mainGames
	}

	// Priority 3: Prefer original releases (exclude re-releases, reboxed)
	originals := filterOutRereleases(results)
	if len(originals) == 1 {
		log.Info().Msg("selected original release (filtered out re-releases)")
		return originals[0]
	}
	if len(originals) > 0 {
		results = originals
	}

	// Priority 4: Prefer configured regions
	preferredRegions := filterByPreferredRegions(results, cfg.DefaultRegions())
	if len(preferredRegions) == 1 {
		log.Info().Msg("selected preferred region")
		return preferredRegions[0]
	}
	if len(preferredRegions) > 0 {
		results = preferredRegions
	}

	// Priority 5: Prefer configured languages
	preferredLanguages := filterByPreferredLanguages(results, cfg.DefaultLangs())
	if len(preferredLanguages) == 1 {
		log.Info().Msg("selected preferred language")
		return preferredLanguages[0]
	}
	if len(preferredLanguages) > 0 {
		results = preferredLanguages
	}

	// Priority 6: If still multiple, pick the first alphabetically by filename
	log.Info().Msg("multiple results remain, selecting first alphabetically by filename")
	return selectAlphabeticallyByFilename(results)
}

// filterByTags filters results that match all specified tags
func filterByTags(
	results []database.SearchResultWithCursor, tagFilters []database.TagFilter,
) []database.SearchResultWithCursor {
	var filtered []database.SearchResultWithCursor

	for _, result := range results {
		if hasAllTags(&result, tagFilters) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// hasAllTags checks if a result matches the specified tag filters
// Respects operator logic: AND (must have), NOT (must not have), OR (at least one)
func hasAllTags(result *database.SearchResultWithCursor, tagFilters []database.TagFilter) bool {
	if len(tagFilters) == 0 {
		return true
	}

	// Create a map of result's tags for fast lookup
	resultTags := make(map[string]string) // type -> value
	for _, tag := range result.Tags {
		resultTags[tag.Type] = tag.Tag
	}

	// Group filters by operator using shared logic
	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(tagFilters)

	// Check AND filters: must have ALL
	for _, requiredTag := range andFilters {
		if value, exists := resultTags[requiredTag.Type]; !exists || value != requiredTag.Value {
			return false
		}
	}

	// Check NOT filters: must NOT have ANY
	for _, excludedTag := range notFilters {
		if value, exists := resultTags[excludedTag.Type]; exists && value == excludedTag.Value {
			return false // Has a tag that should be excluded
		}
	}

	// Check OR filters: must have AT LEAST ONE
	if len(orFilters) > 0 {
		hasAtLeastOne := false
		for _, orTag := range orFilters {
			if value, exists := resultTags[orTag.Type]; exists && value == orTag.Value {
				hasAtLeastOne = true
				break
			}
		}
		if !hasAtLeastOne {
			return false
		}
	}

	return true
}

// filterOutVariants removes demos, betas, prototypes, hacks, and other variants
func filterOutVariants(results []database.SearchResultWithCursor) []database.SearchResultWithCursor {
	var filtered []database.SearchResultWithCursor

	for _, result := range results {
		if !isVariant(&result) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// isVariant checks if a result is a variant (demo, beta, prototype, hack, etc.)
func isVariant(result *database.SearchResultWithCursor) bool {
	for _, tag := range result.Tags {
		switch tag.Type {
		case string(tags.TagTypeUnfinished):
			// Exclude demos, betas, prototypes, samples, previews, prereleases
			if strings.HasPrefix(tag.Tag, string(tags.TagUnfinishedDemo)) ||
				strings.HasPrefix(tag.Tag, string(tags.TagUnfinishedBeta)) ||
				strings.HasPrefix(tag.Tag, string(tags.TagUnfinishedProto)) ||
				strings.HasPrefix(tag.Tag, string(tags.TagUnfinishedAlpha)) ||
				tag.Tag == string(tags.TagUnfinishedSample) ||
				tag.Tag == string(tags.TagUnfinishedPreview) ||
				tag.Tag == string(tags.TagUnfinishedPrerelease) {
				return true
			}
		case string(tags.TagTypeUnlicensed):
			// Exclude hacks, translations, bootlegs
			if tag.Tag == string(tags.TagUnlicensedHack) ||
				tag.Tag == string(tags.TagUnlicensedTranslation) ||
				tag.Tag == string(tags.TagUnlicensedBootleg) ||
				tag.Tag == string(tags.TagUnlicensedClone) {
				return true
			}
		case string(tags.TagTypeDump):
			// Exclude bad dumps
			if tag.Tag == string(tags.TagDumpBad) {
				return true
			}
		}
	}
	return false
}

// filterOutRereleases removes re-releases and reboxed versions
func filterOutRereleases(results []database.SearchResultWithCursor) []database.SearchResultWithCursor {
	var filtered []database.SearchResultWithCursor

	for _, result := range results {
		if !isRerelease(&result) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// isRerelease checks if a result is a re-release
func isRerelease(result *database.SearchResultWithCursor) bool {
	for _, tag := range result.Tags {
		switch tag.Type {
		case string(tags.TagTypeRerelease), string(tags.TagTypeReboxed):
			return true
		}
	}
	return false
}

// filterByPreferredRegions prefers configured regions over others
func filterByPreferredRegions(
	results []database.SearchResultWithCursor, preferredRegions []string,
) []database.SearchResultWithCursor {
	var preferred []database.SearchResultWithCursor
	var untagged []database.SearchResultWithCursor
	var others []database.SearchResultWithCursor

	for _, result := range results {
		regionMatch := getRegionMatch(&result, preferredRegions)
		switch regionMatch {
		case tagMatchPreferred:
			preferred = append(preferred, result)
		case tagMatchUntagged:
			untagged = append(untagged, result)
		case tagMatchOther:
			others = append(others, result)
		}
	}

	if len(preferred) > 0 {
		return preferred
	}
	if len(untagged) > 0 {
		return untagged
	}
	return others
}

type tagMatch int

const (
	tagMatchPreferred tagMatch = iota
	tagMatchUntagged
	tagMatchOther
)

// getRegionMatch checks if a result matches preferred regions
func getRegionMatch(result *database.SearchResultWithCursor, preferredRegions []string) tagMatch {
	hasRegionTag := false
	for _, tag := range result.Tags {
		if tag.Type == string(tags.TagTypeRegion) {
			hasRegionTag = true
			for _, preferred := range preferredRegions {
				if tag.Tag == preferred {
					return tagMatchPreferred
				}
			}
		}
	}
	if !hasRegionTag {
		return tagMatchUntagged
	}
	return tagMatchOther
}

// filterByPreferredLanguages prefers configured languages over others
func filterByPreferredLanguages(
	results []database.SearchResultWithCursor, preferredLangs []string,
) []database.SearchResultWithCursor {
	var preferred []database.SearchResultWithCursor
	var untagged []database.SearchResultWithCursor
	var others []database.SearchResultWithCursor

	for _, result := range results {
		langMatch := getLanguageMatch(&result, preferredLangs)
		switch langMatch {
		case tagMatchPreferred:
			preferred = append(preferred, result)
		case tagMatchUntagged:
			untagged = append(untagged, result)
		case tagMatchOther:
			others = append(others, result)
		}
	}

	if len(preferred) > 0 {
		return preferred
	}
	if len(untagged) > 0 {
		return untagged
	}
	return others
}

// getLanguageMatch checks if a result matches preferred languages
func getLanguageMatch(result *database.SearchResultWithCursor, preferredLangs []string) tagMatch {
	hasLangTag := false
	for _, tag := range result.Tags {
		if tag.Type == string(tags.TagTypeLang) {
			hasLangTag = true
			for _, preferred := range preferredLangs {
				if tag.Tag == preferred {
					return tagMatchPreferred
				}
			}
		}
	}
	if !hasLangTag {
		return tagMatchUntagged
	}
	return tagMatchOther
}

// selectAlphabeticallyByFilename selects the first result alphabetically by filename
func selectAlphabeticallyByFilename(results []database.SearchResultWithCursor) database.SearchResultWithCursor {
	if len(results) == 0 {
		return database.SearchResultWithCursor{}
	}

	best := results[0]
	bestFilename := filepath.Base(best.Path)

	for _, result := range results[1:] {
		resultFilename := filepath.Base(result.Path)
		if resultFilename < bestFilename {
			best = result
			bestFilename = resultFilename
		}
	}
	return best
}

// mightBeTitle checks if input might be a title format for routing purposes in cmdLaunch to cmdTitle.
// This is a lenient check that allows characters that will be normalized during slugification.
func mightBeTitle(input string) bool {
	// Must contain at least one slash
	if !strings.Contains(input, "/") {
		return false
	}

	// Split into system and game parts (only on first slash)
	parts := strings.SplitN(input, "/", 2)
	if len(parts) != 2 {
		return false
	}

	system, game := parts[0], parts[1]

	// Both parts must be non-empty
	if system == "" || game == "" {
		return false
	}

	// Reject obvious wildcard patterns which should go to search instead (but allow Q*bert)
	if strings.HasPrefix(game, "*") || strings.HasSuffix(game, "*") {
		return false
	}

	// Game part should not contain backslashes (Windows file path indicator)
	if strings.Contains(game, "\\") {
		return false
	}

	return true
}

// isValidTitleFormat checks if the input string is valid title format for cmdTitle.
// This is a strict validation used after routing to cmdTitle.
func isValidTitleFormat(input string) bool {
	// Must contain at least one slash
	if !strings.Contains(input, "/") {
		return false
	}

	// Split into system and game parts (only on first slash)
	parts := strings.SplitN(input, "/", 2)
	if len(parts) != 2 {
		return false
	}

	system, game := parts[0], parts[1]

	// Both parts must be non-empty
	if system == "" || game == "" {
		return false
	}

	// Reject wildcard patterns (but allow Q*bert)
	if strings.HasPrefix(game, "*") || strings.HasSuffix(game, "*") {
		return false
	}

	// Game part should not contain backslashes (Windows file path indicator)
	if strings.Contains(game, "\\") {
		return false
	}

	return true
}

// extractCanonicalTagsFromParens extracts explicit canonical tag syntax from parentheses.
// Matches format: (operator?type:value) where operator is -, +, or ~ (optional, defaults to AND)
// Examples: (-unfinished:beta), (+region:us), (year:1994), (~lang:en)
//
// This is used to support operator-based tag filtering in media titles, separate from
// filename metadata tags which don't support operators.
//
// Returns the extracted tag filters and the input string with matched tags removed.
func extractCanonicalTagsFromParens(input string) (tagFilters []database.TagFilter, remaining string) {
	// Regex to match canonical tag syntax in parentheses: (operator?type:value)
	// - ([+~-]?) - optional operator prefix (+, ~, or -). Note: - is last to avoid being interpreted as range
	// - ([a-zA-Z][a-zA-Z0-9_-]*) - tag type (starts with letter, can contain letters/numbers/hyphens/underscores)
	// - : - separator
	// - ([^)]+) - tag value (anything except closing paren)
	reCanonicalTag := regexp.MustCompile(`\(([+~-]?)([a-zA-Z][a-zA-Z0-9_-]*):([^)]+)\)`)

	var extractedTags []database.TagFilter
	remaining = input

	// Find all matches
	matches := reCanonicalTag.FindAllStringSubmatch(input, -1)

	for _, match := range matches {
		fullMatch := match[0] // "(+region:us)"
		operator := match[1]  // "+"
		tagType := match[2]   // "region"
		tagValue := match[3]  // "us"

		// Construct tag string with operator for parsing
		tagStr := operator + tagType + ":" + tagValue

		// Parse using existing filter parser (handles normalization and validation)
		parsedFilters, err := filters.ParseTagFilters([]string{tagStr})
		if err != nil {
			log.Warn().Err(err).Str("tag", tagStr).Msg("failed to parse canonical tag from parentheses")
			continue
		}

		if len(parsedFilters) > 0 {
			extractedTags = append(extractedTags, parsedFilters[0])
			// Remove this tag from the string
			remaining = strings.Replace(remaining, fullMatch, "", 1)
		}
	}

	// Clean up extra spaces left by removed tags
	remaining = strings.TrimSpace(remaining)
	remaining = reMultiSpace.ReplaceAllString(remaining, " ")

	tagFilters = extractedTags
	return tagFilters, remaining
}

// mergeTagFilters merges extracted tags with advanced args tags.
// Advanced args tags take precedence - if the same tag type exists in both,
// the advanced args value is used.
// Returns nil if the result would be empty.
func mergeTagFilters(extracted, advArgs []database.TagFilter) []database.TagFilter {
	if len(advArgs) == 0 && len(extracted) == 0 {
		return nil
	}

	if len(advArgs) == 0 {
		return extracted
	}

	if len(extracted) == 0 {
		return advArgs
	}

	// Create a map of advanced args tags by type for quick lookup
	advArgsMap := make(map[string]database.TagFilter)
	for _, tag := range advArgs {
		advArgsMap[tag.Type] = tag
	}

	// Start with advanced args tags (they take precedence)
	result := make([]database.TagFilter, 0, len(extracted)+len(advArgs))
	result = append(result, advArgs...)

	// Add extracted tags that don't conflict with advanced args
	for _, tag := range extracted {
		if _, exists := advArgsMap[tag.Type]; !exists {
			result = append(result, tag)
		}
	}

	return result
}
