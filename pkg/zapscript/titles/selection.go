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
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// SelectBestResult implements intelligent selection when multiple media match a slug
func SelectBestResult(
	results []database.SearchResultWithCursor, tagFilters []database.TagFilter, cfg *config.Instance,
) database.SearchResultWithCursor {
	if len(results) == 1 {
		log.Info().Msg("single result, confidence: high")
		return results[0]
	}

	initialCount := len(results)
	log.Info().Msgf("multiple results found (%d), picking best match", initialCount)

	// Priority 1: If user provided specific tags, filter by those first
	if len(tagFilters) > 0 {
		filtered := FilterByTags(results, tagFilters)
		if len(filtered) == 1 {
			log.Info().Msg("selected result based on user-specified tags, confidence: high")
			return filtered[0]
		}
		if len(filtered) > 0 {
			log.Info().Msgf("tag filtering reduced candidates from %d to %d", len(results), len(filtered))
			results = filtered
		}
	}

	// Priority 2: Prefer main game over variants (exclude demos, betas, prototypes, hacks)
	mainGames := FilterOutVariants(results)
	if len(mainGames) == 1 {
		log.Info().Msg("selected main game (filtered out variants), confidence: medium-high")
		return mainGames[0]
	}
	if len(mainGames) > 0 {
		results = mainGames
	}

	// Priority 3: Prefer original releases (exclude re-releases, reboxed)
	originals := FilterOutRereleases(results)
	if len(originals) == 1 {
		log.Info().Msg("selected original release (filtered out re-releases), confidence: medium-high")
		return originals[0]
	}
	if len(originals) > 0 {
		results = originals
	}

	// Priority 4: Prefer configured regions
	preferredRegions := FilterByPreferredRegions(results, cfg.DefaultRegions())
	if len(preferredRegions) == 1 {
		log.Info().Msg("selected preferred region, confidence: medium")
		return preferredRegions[0]
	}
	if len(preferredRegions) > 0 {
		results = preferredRegions
	}

	// Priority 5: Prefer configured languages
	preferredLanguages := FilterByPreferredLanguages(results, cfg.DefaultLangs())
	if len(preferredLanguages) == 1 {
		log.Info().Msg("selected preferred language, confidence: medium")
		return preferredLanguages[0]
	}
	if len(preferredLanguages) > 0 {
		results = preferredLanguages
	}

	// Priority 6: If still multiple, pick the first alphabetically by filename
	confidence := "low"
	if len(results) <= 3 {
		confidence = "medium-low"
	}
	log.Info().Msgf("multiple results remain (%d), selecting first alphabetically by filename, confidence: %s",
		len(results), confidence)
	return selectAlphabeticallyByFilename(results)
}

// FilterByTags filters results that match all specified tags
func FilterByTags(
	results []database.SearchResultWithCursor, tagFilters []database.TagFilter,
) []database.SearchResultWithCursor {
	var filtered []database.SearchResultWithCursor

	for _, result := range results {
		if HasAllTags(&result, tagFilters) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// HasAllTags checks if a result matches the specified tag filters
// Respects operator logic: AND (must have), NOT (must not have), OR (at least one)
func HasAllTags(result *database.SearchResultWithCursor, tagFilters []database.TagFilter) bool {
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

// FilterOutVariants removes demos, betas, prototypes, hacks, and other variants
func FilterOutVariants(results []database.SearchResultWithCursor) []database.SearchResultWithCursor {
	var filtered []database.SearchResultWithCursor

	for _, result := range results {
		if !IsVariant(&result) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// IsVariant checks if a result is a variant (demo, beta, prototype, hack, etc.)
func IsVariant(result *database.SearchResultWithCursor) bool {
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

// FilterOutRereleases removes re-releases and reboxed versions
func FilterOutRereleases(results []database.SearchResultWithCursor) []database.SearchResultWithCursor {
	var filtered []database.SearchResultWithCursor

	for _, result := range results {
		if !IsRerelease(&result) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// IsRerelease checks if a result is a re-release
func IsRerelease(result *database.SearchResultWithCursor) bool {
	for _, tag := range result.Tags {
		switch tag.Type {
		case string(tags.TagTypeRerelease), string(tags.TagTypeReboxed):
			return true
		}
	}
	return false
}

// FilterByPreferredRegions prefers configured regions over others
func FilterByPreferredRegions(
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

// FilterByPreferredLanguages prefers configured languages over others
func FilterByPreferredLanguages(
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
