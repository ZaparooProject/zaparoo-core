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
// Handles various episode number formats and reorders components to:
// "Show Name - s##e## - Episode Title"
//
// Supported formats:
// - S01E02, s01e02 (uppercase/lowercase)
// - 1x02, 01x02 (with or without zero-padding)
// - S01E01-E02, S01E01E02 (multi-episode)
// - Various delimiter variations (-, ., _, space)
//
// Component ordering:
// - "Show - S01E02 - Title" (canonical, no change)
// - "S01E02 - Show - Title" → "Show - s01e02 - Title"
// - "Show - Title - S01E02" → "Show - s01e02 - Title"
func ParseTVShow(title string) string {
	// First, normalize the episode format (S01E02 or 1x02 → s##e##)
	normalized := title

	// Check for standard S##E## format
	if match := tvEpisodePattern.FindStringSubmatchIndex(normalized); match != nil {
		normalized = normalizeTVEpisodeFormat(normalized, match, false)
	}

	// Check for alternative #x## format
	if match := tvEpisodeAltPattern.FindStringSubmatchIndex(normalized); match != nil {
		normalized = normalizeTVEpisodeFormat(normalized, match, true)
	}

	// TODO: Component reordering - detect and reorder to canonical format
	// This would involve parsing out show name, episode marker, and episode title,
	// then reassembling in canonical order.

	return normalized
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

// ParseWithMediaType is the entry point for media-type-aware parsing.
// It delegates to the appropriate parser based on media type.
// mediaType should be one of: "TVShow", "Movie", "Music", "Audio", "Video", "Game", "Image"
func ParseWithMediaType(title, mediaType string) string {
	switch mediaType {
	case "TVShow":
		return ParseTVShow(title)
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
	case "Game", "Image", "Application":
		// No special parsing needed for games, images, and applications
		return title
	default:
		return title
	}
}
