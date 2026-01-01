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
	// Matches S01.E02, s01.e02 formats (dot separator)
	tvEpisodeDotPattern = regexp.MustCompile(`(?i)s(\d+)\.e(\d+)(?:[-.]?e?(\d+))?`)
	// Matches S01_E02, s01_e02 formats (underscore separator)
	tvEpisodeUnderscorePattern = regexp.MustCompile(`(?i)s(\d+)_e(\d+)(?:[-_]?e?(\d+))?`)

	// Date-based episode formats (for daily shows)
	// Matches YYYY-MM-DD, YYYY.MM.DD, YYYY/MM/DD
	tvDateYMDPattern = regexp.MustCompile(`(\d{4})[-./](\d{2})[-./](\d{2})`)
	// Matches DD-MM-YYYY, DD.MM.YYYY, DD/MM/YYYY
	tvDateDMYPattern = regexp.MustCompile(`\b(\d{2})[-./](\d{2})[-./](\d{4})\b`)

	// Absolute numbering formats (for anime)
	// Matches "Episode 001", "Ep 42", "E001" formats
	tvAbsolutePattern = regexp.MustCompile(`(?i)\b(?:episode|ep|e)[\s._-]*(\d{1,4})\b`)
	// Matches "#001", "#42" formats
	tvAbsoluteHashPattern = regexp.MustCompile(`#(\d{1,4})\b`)
	// Matches leading number format: "001 - Show - Title"
	tvAbsoluteLeadingPattern = regexp.MustCompile(`^\s*(\d{2,4})[\s._-]`)
)

// ParseTVShow normalizes TV show titles to a canonical format.
// Handles various episode number formats, scene release tags, and reorders components.
//
// Transformations applied (in order):
//  1. Width normalization: Convert fullwidth characters to ASCII
//  2. Scene tag stripping: Remove quality, codec, source tags (1080p, x264, BluRay, etc.)
//  3. Dot normalization: Convert scene release dots to spaces
//  4. Split titles and strip articles: "The Show: Episode Title" → "Show Episode Title"
//  5. Strip trailing articles: "Show, The" → "Show"
//  6. Strip metadata brackets: [720p], (extended), etc. → removed
//  7. Normalize episode formats: S01E02, 1x02, dates, absolute → canonical formats
//  8. Component reordering: Place episode marker in consistent position
//
// Supported episode formats:
// - Season-based: S01E02, s01e02, 1x02, S01.E02, S01_E02, 102 (multi-episode supported)
// - Date-based: YYYY-MM-DD, DD-MM-YYYY, various separators (-, ., /)
// - Absolute: Episode 001, Ep 42, E001, #001 (anime)
// - Various delimiter variations (-, ., _, space)
//
// Examples:
//   - "Breaking.Bad.S01E02.1080p.BluRay.x264-GROUP" → "Breaking Bad s01e02"
//   - "Show - S01E02 [720p]" → "Show s01e02"
//   - "S01E02 - Show - Episode Title" → "Show s01e02 Episode Title"
//   - "Attack on Titan - 1x02 - Title" → "Attack on Titan s01e02 Title"
//   - "Daily Show - 2024-01-15" → "Daily Show 2024-01-15"
//   - "One Piece - Episode 001" → "One Piece e001"
func ParseTVShow(title string) string {
	s := title

	// 1. Normalize width first so fullwidth separators are detected by later functions
	// This converts "：" (fullwidth colon) to ":" (ASCII colon)
	s = NormalizeWidth(s)
	s = strings.TrimSpace(s)

	// 2. Strip scene release tags EARLY (before dot normalization)
	// This removes quality, codec, source tags like "1080p", "x264", "BluRay", etc.
	s = StripSceneTags(s)
	s = strings.TrimSpace(s)

	// 3. Normalize dot separators (scene releases use dots)
	// "Show.Name.S01E02" → "Show Name S01E02"
	s = NormalizeDotSeparators(s)
	s = strings.TrimSpace(s)

	// 4. Strip metadata brackets EARLY (before episode normalization)
	// This removes quality tags, edition markers, format info
	s = StripMetadataBrackets(s)
	s = strings.TrimSpace(s)

	// 5. Normalize dates EARLY (before episode format normalization)
	// This prevents dates from being confused with episode numbers
	s = normalizeDateEpisode(s)
	s = strings.TrimSpace(s)

	// 6. Normalize ALL episode formats to canonical forms
	// Priority: Check season-based first, then absolute (dates already normalized)

	// 6a. Check for standard S##E## format
	if match := tvEpisodePattern.FindStringSubmatchIndex(s); match != nil {
		s = normalizeTVEpisodeFormat(s, match)
	}

	// 6b. Check for alternative #x## format
	if match := tvEpisodeAltPattern.FindStringSubmatchIndex(s); match != nil {
		s = normalizeTVEpisodeFormat(s, match)
	}

	// 6c. Check for S##.E## format (dot separator)
	if match := tvEpisodeDotPattern.FindStringSubmatchIndex(s); match != nil {
		s = normalizeTVEpisodeFormat(s, match)
	}

	// 6d. Check for S##_E## format (underscore separator)
	if match := tvEpisodeUnderscorePattern.FindStringSubmatchIndex(s); match != nil {
		s = normalizeTVEpisodeFormat(s, match)
	}

	// 6e. Check for absolute numbering (anime)
	// Only normalize if no season marker and no date marker was found
	hasSeasonMarker := regexp.MustCompile(`\bs\d+e\d+`).MatchString(strings.ToLower(s))
	hasDateMarker := tvDateYMDPattern.MatchString(s) || tvDateDMYPattern.MatchString(s)

	if !hasSeasonMarker && !hasDateMarker {
		s = normalizeAbsoluteEpisode(s)
	}

	// 7. Component reordering - BEFORE article stripping!
	// This ensures all " - " delimiters are intact for proper component identification
	// This solves the ordering issue: "S01E02 - Show" vs "Show - S01E02"
	hasAbsoluteMarker := regexp.MustCompile(`\be\d{2,4}\b`).MatchString(strings.ToLower(s))

	// Reorder if we have any episode marker (season, date, or absolute)
	if hasSeasonMarker || hasAbsoluteMarker || hasDateMarker {
		s = reorderTVComponents(s)
	}

	// 8. NOW split titles and strip articles (after reordering)
	// At this point, the string is in canonical order: "Show marker Title"
	s = SplitAndStripArticles(s)
	s = strings.TrimSpace(s)

	// 9. Strip trailing articles
	s = StripTrailingArticle(s)
	s = strings.TrimSpace(s)

	return s
}

// normalizeTVEpisodeFormat converts episode markers to canonical lowercase s##e## format.
// Handles both S##E## and #x## input formats, including multi-episode ranges.
func normalizeTVEpisodeFormat(input string, matchIndices []int) string {
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

// normalizeDateEpisode converts date-based episode markers to canonical YYYY-MM-DD format.
// Handles both YYYY-MM-DD and DD-MM-YYYY input formats with various separators (-, ., /).
func normalizeDateEpisode(input string) string {
	// Try YYYY-MM-DD format first (matches YYYY-MM-DD, YYYY.MM.DD, YYYY/MM/DD)
	if match := tvDateYMDPattern.FindStringSubmatchIndex(input); match != nil {
		fullMatch := input[match[0]:match[1]]
		year := input[match[2]:match[3]]
		month := input[match[4]:match[5]]
		day := input[match[6]:match[7]]
		// Always normalize to YYYY-MM-DD format with hyphens
		canonical := fmt.Sprintf("%s-%s-%s", year, month, day)
		// Only replace if it's different (avoid unnecessary work)
		if fullMatch != canonical {
			return strings.Replace(input, fullMatch, canonical, 1)
		}
		return input
	}

	// Try DD-MM-YYYY format (matches DD-MM-YYYY, DD.MM.YYYY, DD/MM/YYYY)
	if match := tvDateDMYPattern.FindStringSubmatchIndex(input); match != nil {
		fullMatch := input[match[0]:match[1]]
		day := input[match[2]:match[3]]
		month := input[match[4]:match[5]]
		year := input[match[6]:match[7]]
		// Convert to canonical YYYY-MM-DD format
		canonical := fmt.Sprintf("%s-%s-%s", year, month, day)
		return strings.Replace(input, fullMatch, canonical, 1)
	}

	return input
}

// normalizeAbsoluteEpisode converts absolute numbering formats to canonical e### format.
// Handles "Episode 001", "Ep 42", "E001", "#001", and leading number formats.
func normalizeAbsoluteEpisode(input string) string {
	// Try standard absolute patterns first (Episode 001, Ep 42, E001)
	if match := tvAbsolutePattern.FindStringSubmatchIndex(input); match != nil {
		fullMatch := input[match[0]:match[1]]
		episodeStr := input[match[2]:match[3]]
		var episode int
		_, _ = fmt.Sscanf(episodeStr, "%d", &episode)
		canonical := fmt.Sprintf("e%03d", episode)
		return strings.Replace(input, fullMatch, canonical, 1)
	}

	// Try hash pattern (#001)
	if match := tvAbsoluteHashPattern.FindStringSubmatchIndex(input); match != nil {
		fullMatch := input[match[0]:match[1]]
		episodeStr := input[match[2]:match[3]]
		var episode int
		_, _ = fmt.Sscanf(episodeStr, "%d", &episode)
		canonical := fmt.Sprintf("e%03d", episode)
		return strings.Replace(input, fullMatch, canonical, 1)
	}

	// Try leading number pattern (001 - Show - Title)
	if match := tvAbsoluteLeadingPattern.FindStringSubmatchIndex(input); match != nil {
		fullMatch := input[match[0]:match[1]]
		episodeStr := input[match[2]:match[3]]
		var episode int
		_, _ = fmt.Sscanf(episodeStr, "%d", &episode)
		canonical := fmt.Sprintf("e%03d ", episode)
		return strings.Replace(input, fullMatch, canonical, 1)
	}

	return input
}

// reorderTVComponents reorders TV show components to canonical format: {show} {marker} {title}
// This solves the ordering issue where episode markers can appear in different positions.
//
// Algorithm:
// 1. Extract episode marker (s##e##, date, or e###) from anywhere in string
// 2. Split remaining text on common delimiters (` - `, ` . `, ` | `)
// 3. Identify show name and episode title
// 4. Reassemble in canonical order
//
// Examples:
//   - "S01E02 - Attack on Titan - That Day" → "Attack on Titan s01e02 That Day"
//   - "Attack on Titan - S01E02 - That Day" → "Attack on Titan s01e02 That Day"
//   - "Attack on Titan - That Day - S01E02" → "Attack on Titan s01e02 That Day"
func reorderTVComponents(s string) string {
	// Extract episode marker if present
	marker := ""
	markerIdx := -1

	// Check for season-based episode marker (s##e##) first
	if match := regexp.MustCompile(`\bs\d+e\d+(?:e\d+)?\b`).FindStringIndex(s); match != nil {
		marker = s[match[0]:match[1]]
		markerIdx = match[0]
	} else if match := tvDateYMDPattern.FindStringIndex(s); match != nil {
		// Check for date-based marker (YYYY-MM-DD, YYYY.MM.DD, YYYY/MM/DD)
		marker = s[match[0]:match[1]]
		markerIdx = match[0]
	} else if match := tvDateDMYPattern.FindStringIndex(s); match != nil {
		// Check for DD-MM-YYYY format dates
		marker = s[match[0]:match[1]]
		markerIdx = match[0]
	} else if match := regexp.MustCompile(`\be\d{2,4}\b`).FindStringIndex(s); match != nil {
		// Check for absolute numbering marker (e###) - only if no date found
		marker = s[match[0]:match[1]]
		markerIdx = match[0]
	}

	// If no marker found, return as-is
	if marker == "" {
		return s
	}

	// Remove the marker temporarily to work with components
	beforeMarker := strings.TrimSpace(s[:markerIdx])
	afterMarker := ""
	if markerIdx+len(marker) < len(s) {
		afterMarker = strings.TrimSpace(s[markerIdx+len(marker):])
	}

	// Combine and split on common delimiters
	// NOTE: Use a more specific delimiter pattern that won't split dates
	// We want to split on " - " (space-dash-space) or " | " but NOT on "-" within dates
	combined := beforeMarker
	if afterMarker != "" {
		if beforeMarker != "" {
			combined += " - " + afterMarker
		} else {
			combined = afterMarker
		}
	}

	// Split on explicit delimiters (space-dash-space, space-pipe-space)
	// Don't split on lone dashes (which would break dates)
	components := regexp.MustCompile(`\s+[-|]\s+`).Split(combined, -1)

	// Filter empty components and clean up separators
	var nonEmpty []string
	for _, comp := range components {
		comp = strings.TrimSpace(comp)
		// Also trim any trailing separators from components
		comp = strings.Trim(comp, "-|. ")
		if comp != "" {
			nonEmpty = append(nonEmpty, comp)
		}
	}

	if len(nonEmpty) == 0 {
		return marker
	}

	// Simple positional logic: first component is show name, rest is episode title
	showName := nonEmpty[0]
	episodeTitle := ""
	if len(nonEmpty) > 1 {
		episodeTitle = strings.Join(nonEmpty[1:], " ")
	}

	// Reassemble in canonical order: show marker title
	// Use spaces only, no dashes or other separators
	result := showName + " " + marker
	if episodeTitle != "" {
		result += " " + episodeTitle
	}

	return strings.TrimSpace(result)
}
