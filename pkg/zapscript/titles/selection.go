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

// SelectBestResult implements intelligent selection when multiple media match a slug.
// Returns the best result and a confidence score (0.0-1.0) based on match quality and tag match quality.
// matchQuality should be 1.0 for exact matches, or the similarity score (0.0-1.0) for fuzzy matches.
func SelectBestResult(
	results []database.SearchResultWithCursor,
	tagFilters []database.TagFilter,
	cfg *config.Instance,
	matchQuality float64,
) (result database.SearchResultWithCursor, confidence float64) {
	if len(results) == 1 {
		// Check if the single result is a variant (and user didn't explicitly request it)
		if IsVariant(&results[0]) && !hasVariantTagFilter(tagFilters) {
			log.Info().Msg("single result is a variant (demo/beta/proto), excluding")
			return database.SearchResultWithCursor{}, 0.0
		}
		tagConfidence := CalculateTagMatchConfidence(&results[0], tagFilters)
		confidence = matchQuality * tagConfidence
		log.Info().Msgf("single result, confidence: %.2f (match: %.2f, tags: %.2f)",
			confidence, matchQuality, tagConfidence)
		return results[0], confidence
	}

	initialCount := len(results)
	log.Info().Msgf("multiple results found (%d), picking best match", initialCount)

	// Priority 1: If user provided specific tags, filter by those first
	if len(tagFilters) > 0 {
		filtered := FilterByTags(results, tagFilters)
		if len(filtered) == 1 {
			tagConfidence := CalculateTagMatchConfidence(&filtered[0], tagFilters)
			confidence = matchQuality * tagConfidence
			log.Info().Msgf("selected result based on user-specified tags, confidence: %.2f (match: %.2f, tags: %.2f)",
				confidence, matchQuality, tagConfidence)
			return filtered[0], confidence
		}
		if len(filtered) > 0 {
			log.Info().Msgf("tag filtering reduced candidates from %d to %d", len(results), len(filtered))
			results = filtered
		}
	}

	// Priority 2: Prefer main game over variants (exclude demos, betas, prototypes, hacks)
	// But only if user didn't explicitly request a variant via tags
	if !hasVariantTagFilter(tagFilters) {
		mainGames := FilterOutVariants(results)
		if len(mainGames) == 1 {
			tagConfidence := CalculateTagMatchConfidence(&mainGames[0], tagFilters)
			confidence = matchQuality * tagConfidence
			log.Info().Msgf("selected main game (filtered out variants), confidence: %.2f (match: %.2f, tags: %.2f)",
				confidence, matchQuality, tagConfidence)
			return mainGames[0], confidence
		}
		if len(mainGames) > 0 {
			results = mainGames
		} else if len(results) > 0 {
			// All results are variants - reject them
			log.Info().Msgf("all %d results are variants (demo/beta/proto), excluding all", len(results))
			return database.SearchResultWithCursor{}, 0.0
		}
	}

	// Priority 3: Prefer original releases (exclude re-releases, reboxed)
	originals := FilterOutRereleases(results)
	if len(originals) == 1 {
		tagConfidence := CalculateTagMatchConfidence(&originals[0], tagFilters)
		confidence = matchQuality * tagConfidence
		log.Info().Msgf("selected original release, confidence: %.2f (match: %.2f, tags: %.2f)",
			confidence, matchQuality, tagConfidence)
		return originals[0], confidence
	}
	if len(originals) > 0 {
		results = originals
	}

	// Priority 4: Prefer configured regions
	preferredRegions := FilterByPreferredRegions(results, cfg.DefaultRegions())
	if len(preferredRegions) == 1 {
		tagConfidence := CalculateTagMatchConfidence(&preferredRegions[0], tagFilters)
		confidence = matchQuality * tagConfidence
		log.Info().Msgf("selected preferred region, confidence: %.2f (match: %.2f, tags: %.2f)",
			confidence, matchQuality, tagConfidence)
		return preferredRegions[0], confidence
	}
	if len(preferredRegions) > 0 {
		results = preferredRegions
	}

	// Priority 5: Prefer configured languages
	preferredLanguages := FilterByPreferredLanguages(results, cfg.DefaultLangs())
	if len(preferredLanguages) == 1 {
		tagConfidence := CalculateTagMatchConfidence(&preferredLanguages[0], tagFilters)
		confidence = matchQuality * tagConfidence
		log.Info().Msgf("selected preferred language, confidence: %.2f (match: %.2f, tags: %.2f)",
			confidence, matchQuality, tagConfidence)
		return preferredLanguages[0], confidence
	}
	if len(preferredLanguages) > 0 {
		results = preferredLanguages
	}

	// Priority 6: If still multiple, pick the first alphabetically by filename
	selected := selectAlphabeticallyByFilename(results)
	tagConfidence := CalculateTagMatchConfidence(&selected, tagFilters)
	confidence = matchQuality * tagConfidence
	log.Info().Msgf(
		"multiple results (%d), selecting alphabetically, confidence: %.2f (match: %.2f, tags: %.2f)",
		len(results), confidence, matchQuality, tagConfidence)
	return selected, confidence
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

// hasVariantTagFilter checks if the user explicitly requested a variant via tag filters
func hasVariantTagFilter(tagFilters []database.TagFilter) bool {
	for _, filter := range tagFilters {
		// Only consider AND filters (not NOT filters)
		if filter.Operator != database.TagOperatorNOT {
			if filter.Type == string(tags.TagTypeUnfinished) ||
				filter.Type == string(tags.TagTypeUnlicensed) {
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

// CalculateTagMatchConfidence calculates a confidence score based on how well
// a result's tags match the requested tag filters.
// Returns a value between 0.0 and 1.0, where:
// - 1.0 = perfect match (all tags match or no tags required)
// - 0.7-0.9 = good match (most tags match)
// - 0.5-0.7 = partial match (some tags match or no tags on result)
// - <0.5 = poor match (few tags match or conflicts exist)
func CalculateTagMatchConfidence(result *database.SearchResultWithCursor, tagFilters []database.TagFilter) float64 {
	// No tag requirements = perfect match
	if len(tagFilters) == 0 {
		return 1.0
	}

	// Create a map of result's tags for fast lookup
	resultTags := make(map[string]string) // type -> value
	for _, tag := range result.Tags {
		resultTags[tag.Type] = tag.Tag
	}

	// If result has no tags at all, give moderate confidence (0.65)
	// This handles database entries with incomplete tag information
	if len(resultTags) == 0 {
		return 0.65
	}

	matched := 0
	conflicts := 0

	// Group filters by operator
	andFilters, notFilters, orFilters := database.GroupTagFiltersByOperator(tagFilters)

	// Check AND filters
	for _, requiredTag := range andFilters {
		if value, exists := resultTags[requiredTag.Type]; exists && value == requiredTag.Value {
			matched++
		} else if exists {
			// Has a different value for this tag type (e.g., wants USA, has Japan)
			conflicts++
		}
	}

	// Check NOT filters
	for _, excludedTag := range notFilters {
		if value, exists := resultTags[excludedTag.Type]; exists && value == excludedTag.Value {
			// Has a tag that should be excluded - major penalty
			conflicts += 2
		} else {
			// Correctly doesn't have the excluded tag
			matched++
		}
	}

	// Check OR filters (need at least one match)
	if len(orFilters) > 0 {
		hasAtLeastOne := false
		for _, orTag := range orFilters {
			if value, exists := resultTags[orTag.Type]; exists && value == orTag.Value {
				hasAtLeastOne = true
				matched++
				break
			}
		}
		if !hasAtLeastOne {
			conflicts++
		}
	}

	totalFilters := len(andFilters) + len(notFilters)
	if len(orFilters) > 0 {
		totalFilters++ // OR group counts as one requirement
	}

	if totalFilters == 0 {
		return 1.0
	}

	// Calculate confidence: matched ratio minus conflict penalty
	matchRatio := float64(matched) / float64(totalFilters)
	conflictPenalty := float64(conflicts) * 0.2

	confidence := matchRatio - conflictPenalty
	if confidence < 0.0 {
		confidence = 0.0
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}
