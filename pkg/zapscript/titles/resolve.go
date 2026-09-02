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
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/container"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

var (
	ErrNoMatch       = errors.New("no matching title found")
	ErrLowConfidence = errors.New("match confidence below minimum threshold")
)

const slugResolutionCacheWriteTimeout = 100 * time.Millisecond

// ResolveResult contains the output of a title resolution.
type ResolveResult struct {
	Strategy   string
	Result     database.SearchResultWithCursor
	Confidence float64
}

// ResolveParams contains the input parameters for title resolution.
type ResolveParams struct {
	MediaDB        database.MediaDBI
	Cfg            *config.Instance
	SystemID       string
	GameName       string
	MediaType      slugs.MediaType
	AdditionalTags []zapscript.TagFilter
	Launchers      []platforms.Launcher
}

// cacheSlugResolution writes the resolution result to the cache in the
// background. SetCachedSlugResolution can block for seconds waiting on
// SQLite's WAL writer lock while a long-running indexing transaction holds
// it, and that wait is not reliably cut short by ctx cancellation - so this
// must run detached from the caller to keep launch-critical title
// resolution instant.
func cacheSlugResolution(
	ctx context.Context,
	mediadb database.MediaDBI,
	systemID string,
	slug string,
	tagFilters []zapscript.TagFilter,
	mediaID int64,
	strategy string,
) {
	go func() {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), slugResolutionCacheWriteTimeout)
		defer cancel()
		cacheErr := mediadb.SetCachedSlugResolution(cacheCtx, systemID, slug, tagFilters, mediaID, strategy)
		if cacheErr != nil {
			log.Warn().Err(cacheErr).Msg("failed to cache slug resolution")
		}
	}()
}

// promotionLosesRequestedTag reports whether swapping selected for promoted
// would give up a tag the query asked for and the selection actually carried.
// A playlist holds no disc tag, so "Game (Disc 2)" keeps the disc it named,
// while a region both rows carry promotes normally and a tag neither row
// carries was never a distinction between them. HasAllTags cannot answer this:
// it treats an absent tag type as unevaluable rather than as a mismatch, so a
// playlist passes every filter its own tracks were selected by.
func promotionLosesRequestedTag(
	selected, promoted *database.SearchResultWithCursor,
	tagFilters []zapscript.TagFilter,
) bool {
	if len(tagFilters) == 0 {
		return false
	}

	selectedTags := tagValueSets(selected)
	promotedTags := tagValueSets(promoted)
	carries := func(sets map[string]map[string]struct{}, filter zapscript.TagFilter) bool {
		values, ok := sets[filter.Type]
		if !ok {
			return false
		}
		_, ok = values[filter.Value]
		return ok
	}

	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(tagFilters)
	for _, filter := range andFilters {
		if carries(selectedTags, filter) && !carries(promotedTags, filter) {
			return true
		}
	}
	for _, filter := range notFilters {
		if !carries(selectedTags, filter) && carries(promotedTags, filter) {
			return true
		}
	}

	// An OR group is satisfied by any one of its filters, so it is only lost
	// when the selection matched something in the group and the promotion
	// matches nothing in it. Comparing filter by filter would refuse a
	// promotion that simply satisfies the group a different way.
	if len(orFilters) > 0 {
		selectedMatches, promotedMatches := false, false
		for _, filter := range orFilters {
			if carries(selectedTags, filter) {
				selectedMatches = true
			}
			if carries(promotedTags, filter) {
				promotedMatches = true
			}
		}
		if selectedMatches && !promotedMatches {
			return true
		}
	}
	return false
}

// promoteToContainerLaunchMedia swaps a resolved media for the launch target of
// the directory holding it, so a title that matched one of a disc folder's
// tracks launches the cue sheet or playlist that browse, media.meta and the
// scrapers all name for that folder.
//
// The question goes to the database rather than to the candidates in hand:
// every file in such a folder shares one title, the slug search is capped, and
// the container's own launch target is routinely outside that cap. Selecting
// among the rows already fetched cannot reach it.
//
// Anything unexpected keeps the original selection. A container lookup exists
// to launch the better file, never to fail a launch that would otherwise work.
func promoteToContainerLaunchMedia(
	ctx context.Context,
	mediadb database.MediaDBI,
	systemID string,
	selected *database.SearchResultWithCursor,
	tagFilters []zapscript.TagFilter,
) database.SearchResultWithCursor {
	if !container.MayHaveContainerTarget(selected.Path) {
		return *selected
	}
	containerPath := container.ParentDir(selected.Path)
	if containerPath == "" {
		return *selected
	}

	launch, err := mediadb.FindSingleContainerLaunchMediaBySystemID(ctx, systemID, containerPath)
	if err != nil {
		log.Debug().Err(err).Str("path", selected.Path).
			Msg("container launch lookup failed, keeping resolved media")
		return *selected
	}
	if launch == nil || launch.DBID == selected.MediaID {
		return *selected
	}

	promoted, err := mediadb.GetMediaByDBID(ctx, launch.DBID)
	if err != nil {
		log.Debug().Err(err).Int64("mediaID", launch.DBID).
			Msg("failed to read container launch media, keeping resolved media")
		return *selected
	}

	if promotionLosesRequestedTag(selected, &promoted, tagFilters) {
		return *selected
	}

	log.Info().Str("from", selected.Path).Str("to", promoted.Path).
		Msg("promoted resolved title to its container launch media")
	return promoted
}

// ResolveTitle runs the full title resolution pipeline against the media database.
// Returns nil if no match is found above ConfidenceMinimum.
func ResolveTitle(ctx context.Context, params *ResolveParams) (*ResolveResult, error) {
	gameName := params.GameName
	mediadb := params.MediaDB
	systemID := params.SystemID
	mediaType := params.MediaType

	// Two-stage tag extraction:
	// 1. Extract explicit canonical tags with operators from parentheses
	// 2. Extract filename metadata tags from remaining parentheses
	canonicalTagFilters, remainingTitle := ExtractCanonicalTagsFromParens(gameName)
	filenameTags := tags.ParseFilenameToCanonicalTags(remainingTitle)

	filenameTagFilters := make([]zapscript.TagFilter, 0, len(filenameTags))
	for _, tag := range filenameTags {
		if tag.Source == tags.TagSourceInferred {
			continue
		}
		filenameTagFilters = append(filenameTagFilters, zapscript.TagFilter{
			Type:     string(tag.Type),
			Value:    string(tag.Value),
			Operator: zapscript.TagOperatorAND,
		})
	}

	autoExtractedTags := MergeTagFilters(filenameTagFilters, canonicalTagFilters)
	tagFilters := MergeTagFilters(autoExtractedTags, params.AdditionalTags)

	slug := slugs.Slugify(mediaType, gameName)
	if slug == "" {
		return nil, fmt.Errorf("game name slugified to empty string: %s", gameName)
	}

	log.Info().Msgf("resolving title slug '%s' in system '%s'", slug, systemID)

	// Check slug resolution cache first
	cachedMediaID, cachedStrategy, cacheHit := mediadb.GetCachedSlugResolution(
		ctx, systemID, slug, tagFilters)
	if cacheHit {
		result, cacheErr := mediadb.GetMediaByDBID(ctx, cachedMediaID)
		if cacheErr == nil {
			return &ResolveResult{
				Result:     result,
				Strategy:   cachedStrategy,
				Confidence: 1.0,
			}, nil
		}
		log.Warn().Err(cacheErr).Msg("failed to retrieve cached media, falling back to full resolution")
	}

	matchInfo := GenerateMatchInfo(mediaType, gameName)

	type candidate struct {
		strategy   string
		result     database.SearchResultWithCursor
		confidence float64
	}
	var bestCandidate *candidate
	var results []database.SearchResultWithCursor

	// Strategy 1: Exact match WITH tags
	results, err := mediadb.SearchMediaBySlug(ctx, systemID, slug, tagFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to search for slug '%s': %w", slug, err)
	}
	if len(results) > 0 {
		selectedResult, confidence := SelectBestResult(
			results, tagFilters, params.Cfg, MatchQualityExact, params.Launchers)

		if confidence >= ConfidenceHigh {
			selectedResult = promoteToContainerLaunchMedia(
				ctx, mediadb, systemID, &selectedResult, tagFilters)
			cacheSlugResolution(ctx, mediadb, systemID, slug, tagFilters, selectedResult.MediaID, StrategyExactMatch)
			return &ResolveResult{
				Result:     selectedResult,
				Strategy:   StrategyExactMatch,
				Confidence: confidence,
			}, nil
		}

		if confidence > 0.0 {
			bestCandidate = &candidate{
				result:     selectedResult,
				confidence: confidence,
				strategy:   StrategyExactMatch,
			}
		}
	}

	// Strategy 2: Exact match WITHOUT tags
	if bestCandidate == nil {
		results, err = mediadb.SearchMediaBySlug(ctx, systemID, slug, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to search for slug '%s' without tags: %w", slug, err)
		}
		if len(results) > 0 {
			selectedResult, confidence := SelectBestResult(
				results, tagFilters, params.Cfg, MatchQualityExact, params.Launchers,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   StrategyExactMatch,
				}
			}
		}
	}

	// Strategy 3: Secondary title match
	if bestCandidate == nil {
		var strategyErr error
		var resolvedStrategy string
		results, resolvedStrategy, strategyErr = TrySecondaryTitleExact(
			ctx, mediadb, systemID, slug, matchInfo, nil, mediaType)
		if strategyErr != nil {
			return nil, fmt.Errorf("secondary title exact match failed: %w", strategyErr)
		}
		if len(results) > 0 {
			selectedResult, confidence := SelectBestResult(
				results, tagFilters, params.Cfg, MatchQualitySecondaryTitle, params.Launchers,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   resolvedStrategy,
				}
			}
		}
	}

	// Strategy 4: Shared secondary title — both titles have the same secondary slug but different
	// main titles, e.g. "Touhou 06: The Embodiment of Scarlet Devil" vs
	// "Touhou Koumakyou: The Embodiment of Scarlet Devil".
	if bestCandidate == nil {
		var strategyErr error
		var resolvedStrategy string
		results, resolvedStrategy, strategyErr = TrySharedSecondaryTitle(
			ctx, mediadb, systemID, slug, matchInfo, nil, mediaType)
		if strategyErr != nil {
			return nil, fmt.Errorf("shared secondary title match failed: %w", strategyErr)
		}
		if len(results) > 0 {
			selectedResult, confidence := SelectBestResult(
				results, tagFilters, params.Cfg, MatchQualitySecondaryTitle, params.Launchers,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   resolvedStrategy,
				}
			}
		}
	}

	// Strategy 5: Advanced fuzzy matching
	if bestCandidate == nil {
		fuzzyResult, strategyErr := TryAdvancedFuzzyMatching(
			ctx, mediadb, systemID, gameName, slug, nil, mediaType)
		if strategyErr != nil {
			return nil, fmt.Errorf("advanced fuzzy matching failed: %w", strategyErr)
		}
		if len(fuzzyResult.Results) > 0 {
			matchQuality := fuzzyResult.Similarity
			selectedResult, confidence := SelectBestResult(
				fuzzyResult.Results, tagFilters, params.Cfg, matchQuality, params.Launchers)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   fuzzyResult.Strategy,
				}
			}
		}
	}

	// Strategy 6: Main title search
	if bestCandidate == nil {
		var strategyErr error
		var resolvedStrategy string
		results, resolvedStrategy, strategyErr = TryMainTitleOnly(
			ctx, mediadb, systemID, slug, matchInfo, nil, mediaType)
		if strategyErr != nil {
			return nil, fmt.Errorf("main title only search failed: %w", strategyErr)
		}
		if len(results) > 0 {
			selectedResult, confidence := SelectBestResult(
				results, tagFilters, params.Cfg, MatchQualityMainTitle, params.Launchers,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   resolvedStrategy,
				}
			}
		}
	}

	// Strategy 7: Progressive trim
	if bestCandidate == nil {
		var strategyErr error
		var resolvedStrategy string
		results, resolvedStrategy, strategyErr = TryProgressiveTrim(
			ctx, mediadb, systemID, gameName, slug, nil, mediaType)
		if strategyErr != nil {
			return nil, fmt.Errorf("progressive trim strategy failed: %w", strategyErr)
		}
		if len(results) > 0 {
			selectedResult, confidence := SelectBestResult(
				results, tagFilters, params.Cfg, MatchQualityProgressiveTrim, params.Launchers,
			)
			if confidence > 0.0 {
				bestCandidate = &candidate{
					result:     selectedResult,
					confidence: confidence,
					strategy:   resolvedStrategy,
				}
			}
		}
	}

	if bestCandidate == nil {
		return nil, ErrNoMatch
	}

	if bestCandidate.confidence < ConfidenceMinimum {
		return nil, ErrLowConfidence
	}

	bestCandidate.result = promoteToContainerLaunchMedia(
		ctx, mediadb, systemID, &bestCandidate.result, tagFilters)

	// Cache the successful resolution without letting cache bookkeeping block
	// launch-critical title resolution. Promotion runs first so the cached ID is
	// the container's launch target, not the sibling the search happened to pick.
	cacheSlugResolution(ctx, mediadb, systemID, slug, tagFilters, bestCandidate.result.MediaID, bestCandidate.strategy)

	return &ResolveResult{
		Result:     bestCandidate.result,
		Strategy:   bestCandidate.strategy,
		Confidence: bestCandidate.confidence,
	}, nil
}
