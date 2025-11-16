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

package slugs

import (
	"fmt"
	"regexp"
	"strings"
)

// Regex patterns for TV show episode formats
var (
	// Matches S01E02, s01e02, S01E02-E03, S01E02E03 formats
	tvEpisodePattern = regexp.MustCompile(`(?i)s(\d+)e(\d+)(?:[-e]?e?(\d+))?`)
	// Matches 1x02, 01x02, 1x02-03 formats
	tvEpisodeAltPattern = regexp.MustCompile(`(?i)(\d+)x(\d+)(?:-?(\d+))?`)
)

// ParseTVShow normalizes TV show titles to a canonical format.
// Handles various episode number formats and strips metadata brackets.
//
// Transformations applied (in order):
//  1. Split titles and strip articles: "The Show: Episode Title" → "Show Episode Title"
//  2. Strip trailing articles: "Show, The" → "Show"
//  3. Strip metadata brackets: [720p], (extended), etc. → removed
//  4. Normalize episode formats: S01E02, 1x02 → s01e02
//
// Supported episode formats:
// - S01E02, s01e02 (uppercase/lowercase)
// - 1x02, 01x02 (with or without zero-padding)
// - S01E01-E02, S01E01E02 (multi-episode)
// - Various delimiter variations (-, ., _, space)
//
// Examples:
//   - "Show - S01E02 [720p]" → "Show - s01e02"
//   - "Show - 1x02 - Title (extended)" → "Show - s01e02 - Title"
func ParseTVShow(title string) string {
	s := title

	// Normalize width first so fullwidth separators are detected by SplitAndStripArticles
	// This converts "：" (fullwidth colon) to ":" (ASCII colon)
	s = NormalizeWidth(s)
	s = strings.TrimSpace(s)

	// MUST happen first: Split titles and strip articles
	// This preserves separators like ":" and "," which are needed for splitting
	s = SplitAndStripArticles(s)
	s = strings.TrimSpace(s)

	// Strip trailing articles (also needs "," intact)
	s = StripTrailingArticle(s)
	s = strings.TrimSpace(s)

	// Strip metadata brackets (quality tags, edition markers, format info)
	s = StripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	// Normalize the episode format (S01E02 or 1x02 → s##e##)
	// Check for standard S##E## format
	if match := tvEpisodePattern.FindStringSubmatchIndex(s); match != nil {
		s = normalizeTVEpisodeFormat(s, match, false)
	}

	// Check for alternative #x## format
	if match := tvEpisodeAltPattern.FindStringSubmatchIndex(s); match != nil {
		s = normalizeTVEpisodeFormat(s, match, true)
	}

	// TODO: Component reordering - detect and reorder to canonical format
	// This would involve parsing out show name, episode marker, and episode title,
	// then reassembling in canonical order.

	return s
}

// normalizeTVEpisodeFormat converts episode markers to canonical lowercase s##e## format.
// Handles both S##E## and #x## input formats, including multi-episode ranges.
func normalizeTVEpisodeFormat(input string, matchIndices []int, _ bool) string {
	if len(matchIndices) < 6 {
		return input
	}

	// Extract the matched substring
	fullMatch := input[matchIndices[0]:matchIndices[1]]

	// Extract season and episode numbers from capture groups
	seasonStr := input[matchIndices[2]:matchIndices[3]]
	episodeStr := input[matchIndices[4]:matchIndices[5]]

	// Parse numbers
	var season, episode int
	_, _ = fmt.Sscanf(seasonStr, "%d", &season)
	_, _ = fmt.Sscanf(episodeStr, "%d", &episode)

	// Build canonical format
	canonical := fmt.Sprintf("s%02de%02d", season, episode)

	// Handle multi-episode format (e.g., S01E01-E02 or S01E01E02)
	if len(matchIndices) >= 8 && matchIndices[6] != -1 {
		endEpisodeStr := input[matchIndices[6]:matchIndices[7]]
		var endEpisode int
		_, _ = fmt.Sscanf(endEpisodeStr, "%d", &endEpisode)
		canonical = fmt.Sprintf("s%02de%02de%02d", season, episode, endEpisode)
	}

	// Replace the original match with canonical format
	return strings.Replace(input, fullMatch, canonical, 1)
}

// ParseGame normalizes game titles by applying game-specific transformations.
// This handles common game title patterns and variations to ensure consistent matching.
//
// Transformations applied (in order):
//  1. Split titles and strip articles: "The Zelda: Link's Awakening" → "Zelda Link's Awakening"
//  2. Strip trailing articles: "Legend, The" → "Legend"
//  3. Strip metadata brackets: (USA), [!], {Europe}, <Beta> → removed
//  4. Strip edition/version suffixes: "Edition", "Version", v1.0 → removed
//  5. Normalize separators: Convert periods to spaces (for abbreviation matching)
//  6. Expand abbreviations: "Bros" → "brothers", "vs" → "versus", "Dr" → "doctor"
//  7. Expand number words: "one" → "1", "two" → "2"
//  8. Normalize ordinals: "1st" → "1", "2nd" → "2"
//  9. Convert roman numerals: "VII" → "7", "II" → "2" (preserves "X" for games like Mega Man X)
//
// Examples:
//   - "Super Mario Bros. III (USA) [!]" → "super mario brothers 3"
//   - "Street Fighter II Version" → "street fighter 2"
//   - "Mega Man X" → "mega man x" (X preserved)
//   - "Final Fantasy VII" → "final fantasy 7"
func ParseGame(title string) string {
	s := title

	// Normalize width first so fullwidth separators are detected by SplitAndStripArticles
	// This converts "：" (fullwidth colon) to ":" (ASCII colon)
	s = NormalizeWidth(s)
	s = strings.TrimSpace(s)

	// MUST happen first: Split titles and strip articles
	// This preserves separators like ":" and "," which are needed for splitting
	s = SplitAndStripArticles(s)
	s = strings.TrimSpace(s)

	// Strip trailing articles (also needs "," intact)
	s = StripTrailingArticle(s)
	s = strings.TrimSpace(s)

	// Strip metadata brackets (region codes, dump info, tags)
	s = StripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	// Strip edition and version suffixes
	s = StripEditionAndVersionSuffixes(s)
	s = strings.TrimSpace(s)

	// Trim trailing separators before abbreviation expansion
	// This ensures "Bros-" → "Bros" so abbreviation matching works
	s = strings.TrimRight(s, "-:;.,_/\\ ")

	// Normalize symbols and separators (BUT NOT commas - needed for trailing articles)
	// This is similar to NormalizeSymbolsAndSeparators but preserves commas
	// Conjunction normalization
	s = strings.ReplaceAll(s, " & ", " and ")
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.ReplaceAll(s, " + ", " and ")
	s = strings.ReplaceAll(s, "+", " plus ")
	// Separator normalization (: _ / \ ; but NOT commas)
	s = strings.ReplaceAll(s, ":", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.ReplaceAll(s, "\\", " ")
	s = strings.ReplaceAll(s, ";", " ")
	// Convert periods to spaces
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.TrimSpace(s)

	// Expand common abbreviations
	s = ExpandAbbreviations(s)

	// Expand number words
	s = ExpandNumberWords(s)

	// Normalize ordinals
	s = NormalizeOrdinals(s)

	// Convert roman numerals (includes lowercasing)
	s = ConvertRomanNumerals(s)

	return s
}

// ParseWithMediaType is the entry point for media-type-aware parsing.
// It delegates to the appropriate parser based on media type.
// Each parser applies media-specific normalization BEFORE the universal pipeline.
//
// mediaType should be one of: "TVShow", "Movie", "Music", "Audio", "Video", "Game", "Image", "Application"
func ParseWithMediaType(title, mediaType string) string {
	switch mediaType {
	case "TVShow":
		return ParseTVShow(title)
	case "Game":
		return ParseGame(title)
	case "Movie":
		// TODO: Implement ParseMovie
		return title
	case "Music":
		// TODO: Implement ParseMusic
		return title
	case "Audio":
		// TODO: Implement ParseAudiobook/ParsePodcast
		return title
	case "Video":
		// TODO: Implement ParseVideo (music videos)
		return title
	case "Image", "Application":
		// No special parsing needed for images and applications
		return title
	default:
		return title
	}
}
