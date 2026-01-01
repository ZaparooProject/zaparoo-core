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

package zapscript

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	advargtypes "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/advargs/types"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
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
	valid, systemID, gameName := isValidTitleFormat(query)
	if !valid {
		return platforms.CmdResult{}, fmt.Errorf(
			"invalid title format: %s (expected SystemID/GameName)", query)
	}

	// Validate system ID
	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to lookup system '%s': %w", systemID, err)
	}
	if system == nil {
		return platforms.CmdResult{}, fmt.Errorf("system not found: %s", systemID)
	}

	var args advargtypes.LaunchTitleArgs
	if parseErr := ParseAdvArgs(pl, &env, &args); parseErr != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid advanced arguments: %w", parseErr)
	}

	args.Launcher = applySystemDefaultLauncher(&env, system.ID)
	launch := getLaunchClosure(pl, &env)

	// Collect all launchers for this system to enable file type prioritization
	// during result selection. If user specified an alt launcher explicitly,
	// use only that one. Otherwise, get all launchers for the system.
	var launchersForSystem []platforms.Launcher
	if args.Launcher != "" {
		// User specified launcher explicitly via advanced args
		// Find the specific launcher by ID
		allLaunchers := pl.Launchers(env.Cfg)
		for i := range allLaunchers {
			if allLaunchers[i].ID == args.Launcher {
				launchersForSystem = []platforms.Launcher{allLaunchers[i]}
				log.Debug().Msgf("using explicitly specified launcher: %s", allLaunchers[i].ID)
				break
			}
		}
		if len(launchersForSystem) == 0 {
			// This shouldn't happen since validation already checked the launcher
			log.Warn().Msgf("explicitly specified launcher not found: %s, using all system launchers",
				args.Launcher)
			launchersForSystem = helpers.GlobalLauncherCache.GetLaunchersBySystem(system.ID)
		}
	} else {
		// Get all launchers for this system
		launchersForSystem = helpers.GlobalLauncherCache.GetLaunchersBySystem(system.ID)
	}
	log.Debug().Msgf("using %d launcher(s) for file type prioritization", len(launchersForSystem))

	// Two-stage tag extraction:
	// 1. Extract explicit canonical tags with operators from parentheses: (-unfinished:beta) (+region:us)
	// 2. Extract filename metadata tags from remaining parentheses: (USA) (1996) (Rev A)

	// Stage 1: Extract canonical tags with operators (e.g., "(-unfinished:beta) (+region:us)")
	canonicalTagFilters, remainingTitle := titles.ExtractCanonicalTagsFromParens(gameName)

	// Stage 2: Extract filename metadata tags from remaining string (e.g., "(USA) (1996)")
	filenameTags := tags.ParseFilenameToCanonicalTags(remainingTitle)

	// Convert filename tags to tag filters (always AND operator)
	// Skip inferred tags (e.g., "Edition" from plain text) - only use tags from brackets
	filenameTagFilters := make([]database.TagFilter, 0, len(filenameTags))
	for _, tag := range filenameTags {
		if tag.Source == tags.TagSourceInferred {
			continue // Skip inferred tags - don't use as search filters
		}
		filenameTagFilters = append(filenameTagFilters, database.TagFilter{
			Type:     string(tag.Type),
			Value:    string(tag.Value),
			Operator: database.TagOperatorAND,
		})
	}

	// Keep auto-extracted tags separate for fallback strategy
	autoExtractedTags := titles.MergeTagFilters(filenameTagFilters, canonicalTagFilters)

	// Use tags from validated advanced args (these are explicit user requirements)
	advArgsTagFilters := args.Tags

	// Merge all tags for initial search attempt
	// Priority: advanced args > canonical tags > filename tags
	tagFilters := titles.MergeTagFilters(autoExtractedTags, advArgsTagFilters)

	// Slugify the game name with media-type-aware normalization
	// e.g., "Sonic Spinball (USA) (year:1994)" → "sonicspinball"
	// For TV shows: "Breaking Bad - S01E02" and "Breaking Bad - 1x02" → same slug
	slug := slugs.Slugify(system.GetMediaType(), gameName)
	if slug == "" {
		return platforms.CmdResult{}, fmt.Errorf("game name slugified to empty string: %s", gameName)
	}

	mediadb := env.Database.MediaDB
	log.Info().Msgf("searching for slug '%s' in system '%s'", slug, system.ID)

	ctx := context.Background() // TODO: use proper context from env when available

	// Check slug resolution cache first
	cachedMediaID, cachedStrategy, cacheHit := mediadb.GetCachedSlugResolution(
		ctx, system.ID, slug, tagFilters)
	if cacheHit {
		log.Info().Msgf("slug resolution cache hit (strategy: %s)", cachedStrategy)
		// Retrieve full result from cached Media DBID
		result, cacheErr := mediadb.GetMediaByDBID(ctx, cachedMediaID)
		if cacheErr != nil {
			log.Warn().Err(cacheErr).Msg("failed to retrieve cached media, falling back to full resolution")
		}
		if cacheErr == nil {
			log.Info().Msgf("resolved from cache: %s (%s)", result.Name, result.Path)
			return platforms.CmdResult{
				MediaChanged: true,
				Strategy:     cachedStrategy,
				Confidence:   1.0, // Cached results are trusted
			}, launch(result.Path)
		}
	}

	// Generate match info once for secondary/main title strategies
	matchInfo := titles.GenerateMatchInfo(system.GetMediaType(), gameName)

	// Track the best candidate found across all strategies
	type candidate struct {
		strategy   string
		result     database.SearchResultWithCursor
		confidence float64
	}
	var bestCandidate *candidate
	var results []database.SearchResultWithCursor

	// Strategy 1: Exact match WITH tags (fast path for perfect matches)
	results, err = mediadb.SearchMediaBySlug(ctx, system.ID, slug, tagFilters)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to search for slug '%s': %w", slug, err)
	}
	if len(results) > 0 {
		selectedResult, confidence := titles.SelectBestResult(
			results, tagFilters, env.Cfg, titles.MatchQualityExact, launchersForSystem)
		log.Debug().
			Str("strategy", titles.StrategyExactMatch).
			Str("query", slug).
			Int("result_count", len(results)).
			Float64("confidence", confidence).
			Msg("match found via exact match strategy")

		// High confidence match - return immediately (only early exit case)
		if confidence >= titles.ConfidenceHigh {
			log.Info().Msgf("high confidence match (%.2f), launching immediately", confidence)
			if cacheErr := mediadb.SetCachedSlugResolution(
				ctx, system.ID, slug, tagFilters, selectedResult.MediaID, titles.StrategyExactMatch,
			); cacheErr != nil {
				log.Warn().Err(cacheErr).Msg("failed to cache slug resolution")
			}
			return platforms.CmdResult{
				MediaChanged: true,
				Strategy:     titles.StrategyExactMatch,
				Confidence:   confidence,
			}, launch(selectedResult.Path)
		}

		// Track as best candidate (only if valid result with non-zero confidence)
		if confidence > 0.0 {
			bestCandidate = &candidate{
				result:     selectedResult,
				confidence: confidence,
				strategy:   titles.StrategyExactMatch,
			}
			log.Info().Msgf("found candidate via exact match (confidence: %.2f)", confidence)
		}
	}

	// Strategy 2: Exact match WITHOUT tags (catches tag mismatches quickly)
	// Tags become soft preferences for result selection
	if bestCandidate == nil {
		log.Info().Msg("no results with tags, trying exact match without tag filters")
		results, err = mediadb.SearchMediaBySlug(ctx, system.ID, slug, nil)
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to search for slug '%s' without tags: %w", slug, err)
		}
		if len(results) > 0 {
			selectedResult, confidence := titles.SelectBestResult(
				results, tagFilters, env.Cfg, titles.MatchQualityExact, launchersForSystem,
			)
			log.Debug().
				Str("strategy", titles.StrategyExactMatch).
				Str("query", slug).
				Int("result_count", len(results)).
				Float64("confidence", confidence).
				Msg("match found via exact match without tags")

			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   titles.StrategyExactMatch,
				}
				log.Info().Msgf("found candidate via exact match without tags (confidence: %.2f)", confidence)
			}
		}
	}

	// Strategy 3: Secondary title match
	if bestCandidate == nil {
		var strategyErr error
		var resolvedStrategy string
		results, resolvedStrategy, strategyErr = titles.TrySecondaryTitleExact(
			ctx, mediadb, system.ID, slug, matchInfo, nil, system.GetMediaType())
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("secondary title exact match failed: %w", strategyErr)
		}
		if len(results) > 0 {
			selectedResult, confidence := titles.SelectBestResult(
				results, tagFilters, env.Cfg, titles.MatchQualitySecondaryTitle, launchersForSystem,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   resolvedStrategy,
				}
				log.Info().Msgf("found candidate via secondary title (confidence: %.2f)", confidence)
			}
		}
	}

	// Strategy 4: Advanced fuzzy matching with single prefilter
	// Uses a single prefilter query, then tries three algorithms in sequence:
	// 1. Token signature (word-order independent)
	// 2. Jaro-Winkler (typo tolerance, prefix matching)
	// 3. Damerau-Levenshtein tie-breaking (transposition handling)
	if bestCandidate == nil {
		fuzzyResult, strategyErr := titles.TryAdvancedFuzzyMatching(
			ctx, mediadb, system.ID, gameName, slug, nil, system.GetMediaType())
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("advanced fuzzy matching failed: %w", strategyErr)
		}
		if len(fuzzyResult.Results) > 0 {
			matchQuality := fuzzyResult.Similarity // Use actual fuzzy match similarity score
			selectedResult, confidence := titles.SelectBestResult(
				fuzzyResult.Results, tagFilters, env.Cfg, matchQuality, launchersForSystem)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   fuzzyResult.Strategy,
				}
				log.Info().Msgf("found candidate via fuzzy matching (similarity: %.2f, confidence: %.2f)",
					fuzzyResult.Similarity, confidence)
			}
		}
	}

	// Strategy 5: Main title search
	if bestCandidate == nil {
		var strategyErr error
		var resolvedStrategy string
		results, resolvedStrategy, strategyErr = titles.TryMainTitleOnly(
			ctx, mediadb, system.ID, slug, matchInfo, nil, system.GetMediaType())
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("main title only search failed: %w", strategyErr)
		}
		if len(results) > 0 {
			selectedResult, confidence := titles.SelectBestResult(
				results, tagFilters, env.Cfg, titles.MatchQualityMainTitle, launchersForSystem,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   resolvedStrategy,
				}
				log.Info().Msgf("found candidate via main title only (confidence: %.2f)", confidence)
			}
		}
	}

	// Strategy 6: Progressive trim candidates (last resort)
	// Handles overly-verbose queries by progressively trimming words from the end
	// Uses a single IN query for all candidates (max depth: 3)
	if bestCandidate == nil {
		var strategyErr error
		var resolvedStrategy string
		results, resolvedStrategy, strategyErr = titles.TryProgressiveTrim(
			ctx, mediadb, system.ID, gameName, slug, nil, system.GetMediaType())
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("progressive trim strategy failed: %w", strategyErr)
		}
		if len(results) > 0 {
			selectedResult, confidence := titles.SelectBestResult(
				results, tagFilters, env.Cfg, titles.MatchQualityProgressiveTrim, launchersForSystem,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   resolvedStrategy,
				}
				log.Info().Msgf("found candidate via progressive trim (confidence: %.2f)", confidence)
			}
		}
	}

	// No results found
	if bestCandidate == nil {
		return platforms.CmdResult{}, fmt.Errorf("no results found for title: %s/%s", system.ID, gameName)
	}

	// Have results but confidence is too low
	if bestCandidate.confidence < titles.ConfidenceMinimum {
		return platforms.CmdResult{}, fmt.Errorf(
			"found match for '%s/%s' but confidence too low (%.2f < %.2f): %s",
			system.ID, gameName, bestCandidate.confidence, titles.ConfidenceMinimum, bestCandidate.result.Name)
	}

	// Launch with the best candidate found
	if bestCandidate.confidence < titles.ConfidenceAcceptable {
		log.Warn().Msgf("launching with low confidence (%.2f): %s", bestCandidate.confidence, bestCandidate.result.Name)
	} else {
		log.Info().Msgf("launching with confidence %.2f: %s", bestCandidate.confidence, bestCandidate.result.Name)
	}

	// Cache the successful resolution (best effort - don't fail if caching fails)
	if cacheErr := mediadb.SetCachedSlugResolution(
		ctx, system.ID, slug, tagFilters, bestCandidate.result.MediaID, bestCandidate.strategy,
	); cacheErr != nil {
		log.Warn().Err(cacheErr).Msg("failed to cache slug resolution")
	}

	return platforms.CmdResult{
		MediaChanged: true,
		Strategy:     bestCandidate.strategy,
		Confidence:   bestCandidate.confidence,
	}, launch(bestCandidate.result.Path)
}

// mightBeTitle checks if input might be a title format for routing purposes in cmdLaunch to cmdTitle.
// Returns false for paths with file extensions, wildcards, or Windows-style backslashes.
func mightBeTitle(input string) bool {
	valid, _, game := isValidTitleFormat(input)
	if !valid {
		return false
	}

	// Reject wildcard patterns which should go to search instead
	if strings.Contains(game, "*") {
		return false
	}

	// Game part should not contain backslashes (Windows file path indicator)
	if strings.Contains(game, "\\") {
		return false
	}

	// Game part should not contain file extensions (path indicator)
	ext := filepath.Ext(game)
	return !helpers.IsValidExtension(ext)
}

// isValidTitleFormat checks if the input string is valid title format for cmdTitle.
// This is a lenient validation - just ensures basic SystemID/GameName format.
// The command itself handles all parsing complexity.
func isValidTitleFormat(input string) (valid bool, systemID, gameName string) {
	// Must contain at least one slash
	if !strings.Contains(input, "/") {
		return false, "", ""
	}

	// Split into system and game parts (only on first slash)
	parts := strings.SplitN(input, "/", 2)
	if len(parts) != 2 {
		return false, "", ""
	}

	system, game := parts[0], parts[1]

	// Both parts must be non-empty
	if system == "" || game == "" {
		return false, "", ""
	}

	return true, system, game
}
