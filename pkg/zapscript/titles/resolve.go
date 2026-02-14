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

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

var (
	ErrNoMatch       = errors.New("no matching title found")
	ErrLowConfidence = errors.New("match confidence below minimum threshold")
)

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
			if cacheErr := mediadb.SetCachedSlugResolution(
				ctx, systemID, slug, tagFilters, selectedResult.MediaID, StrategyExactMatch,
			); cacheErr != nil {
				log.Warn().Err(cacheErr).Msg("failed to cache slug resolution")
			}
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

	// Strategy 4: Advanced fuzzy matching
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

	// Strategy 5: Main title search
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

	// Strategy 6: Progressive trim
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

	// Cache the successful resolution
	if cacheErr := mediadb.SetCachedSlugResolution(
		ctx, systemID, slug, tagFilters, bestCandidate.result.MediaID, bestCandidate.strategy,
	); cacheErr != nil {
		log.Warn().Err(cacheErr).Msg("failed to cache slug resolution")
	}

	return &ResolveResult{
		Result:     bestCandidate.result,
		Strategy:   bestCandidate.strategy,
		Confidence: bestCandidate.confidence,
	}, nil
}
