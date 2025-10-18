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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
	canonicalTagFilters, remainingTitle := titles.ExtractCanonicalTagsFromParens(gameName)

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
	autoExtractedTags := titles.MergeTagFilters(filenameTagFilters, canonicalTagFilters)

	// Parse tags from advanced args (these are explicit user requirements)
	advArgsTagFilters := parseTagsAdvArg(env.Cmd.AdvArgs["tags"])

	// Merge all tags for initial search attempt
	// Priority: advanced args > canonical tags > filename tags
	tagFilters := titles.MergeTagFilters(autoExtractedTags, advArgsTagFilters)

	// Slugify the game name (SlugifyString handles metadata stripping in Stage 4)
	// e.g., "Sonic Spinball (USA) (year:1994)" → "sonicspinball"
	slug := slugs.SlugifyString(gameName)
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
			}, launch(result.Path)
		}
	}

	// Generate match info once for secondary/main title strategies
	matchInfo := titles.GenerateMatchInfo(gameName)

	// Track which strategy succeeds for cache storage
	var resolvedStrategy string

	// Strategy 1: Exact match
	results, err := mediadb.SearchMediaBySlug(ctx, system.ID, slug, tagFilters)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to search for slug '%s': %w", slug, err)
	}
	if len(results) > 0 {
		resolvedStrategy = titles.StrategyExactMatch
		log.Debug().
			Str("strategy", resolvedStrategy).
			Str("query", slug).
			Int("result_count", len(results)).
			Msg("match found via exact match strategy")
	}

	// Strategy 2: Secondary title-only exact match (only if has secondary title)
	if len(results) == 0 && matchInfo.HasSecondaryTitle {
		var strategyErr error
		results, resolvedStrategy, strategyErr = titles.TrySecondaryTitleExact(
			ctx, mediadb, system.ID, slug, matchInfo, tagFilters)
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("secondary title exact match failed: %w", strategyErr)
		}
	}

	// Strategy 3: Advanced fuzzy matching with single prefilter
	// Uses a single prefilter query, then tries three algorithms in sequence:
	// 1. Token signature (word-order independent)
	// 2. Jaro-Winkler (typo tolerance, prefix matching)
	// 3. Damerau-Levenshtein tie-breaking (transposition handling)
	if len(results) == 0 {
		var strategyErr error
		results, resolvedStrategy, strategyErr = titles.TryAdvancedFuzzyMatching(
			ctx, mediadb, system.ID, gameName, slug, tagFilters)
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("advanced fuzzy matching failed: %w", strategyErr)
		}
	}

	// Strategy 4: Main title-only search (drops secondary title, only if has secondary title)
	if len(results) == 0 && matchInfo.HasSecondaryTitle {
		var strategyErr error
		results, resolvedStrategy, strategyErr = titles.TryMainTitleOnly(
			ctx, mediadb, system.ID, slug, matchInfo, tagFilters)
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("main title only search failed: %w", strategyErr)
		}
	}

	// Strategy 5: Progressive trim candidates (last resort)
	// Handles overly-verbose queries by progressively trimming words from the end
	// Uses a single IN query for all candidates (max depth: 3)
	if len(results) == 0 {
		var strategyErr error
		results, resolvedStrategy, strategyErr = titles.TryProgressiveTrim(
			ctx, mediadb, system.ID, gameName, slug, tagFilters)
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("progressive trim strategy failed: %w", strategyErr)
		}
	}

	// Fallback strategy: If no results with auto-extracted tags, retry without them
	if len(results) == 0 {
		var strategyErr error
		results, resolvedStrategy, strategyErr = titles.TryWithoutAutoTags(
			ctx, mediadb, system.ID, slug, autoExtractedTags, advArgsTagFilters)
		if strategyErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("fallback without auto-tags failed: %w", strategyErr)
		}
		if len(results) > 0 {
			log.Info().Msg("fallback successful: found results by ignoring auto-extracted tag filters")
		}
	}

	if len(results) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no results found for title: %s/%s", system.ID, gameName)
	}

	// If multiple results, apply intelligent selection using ALL tags as preferences
	// This includes both user-provided and auto-extracted tags
	selectedResult := titles.SelectBestResult(results, tagFilters, env.Cfg)
	log.Info().Msgf("selected result: %s (%s)", selectedResult.Name, selectedResult.Path)

	// Cache the successful resolution (best effort - don't fail if caching fails)
	if resolvedStrategy != "" {
		if cacheErr := mediadb.SetCachedSlugResolution(
			ctx, system.ID, slug, tagFilters, selectedResult.MediaID, resolvedStrategy,
		); cacheErr != nil {
			log.Warn().Err(cacheErr).Msg("failed to cache slug resolution")
		}
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, launch(selectedResult.Path)
}

// mightBeTitle checks if input might be a title format for routing purposes in cmdLaunch to cmdTitle.
// It already assumes things like file extensions have been ruled out.
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

	return true
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
