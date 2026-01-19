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
	"regexp"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/rs/zerolog/log"
)

// Regex to match canonical tag syntax in parentheses: (operator?type:value)
// - ([+~-]?) - optional operator prefix (+, ~, or -). Note: - is last to avoid being interpreted as range
// - ([a-zA-Z][a-zA-Z0-9_-]*) - tag type (starts with letter, can contain letters/numbers/hyphens/underscores)
// - : - separator
// - ([^)]+) - tag value (anything except closing paren)
var reCanonicalTag = regexp.MustCompile(`\(([+~-]?)([a-zA-Z][a-zA-Z0-9_-]*):([^)]+)\)`)

// ExtractCanonicalTagsFromParens extracts explicit canonical tag syntax from parentheses.
// Matches format: (operator?type:value) where operator is -, +, or ~ (optional, defaults to AND)
// Examples: (-unfinished:beta), (+region:us), (year:1994), (~lang:en)
//
// This is used to support operator-based tag filtering in media titles, separate from
// filename metadata tags which don't support operators.
//
// Returns the extracted tag filters and the input string with matched tags removed.
func ExtractCanonicalTagsFromParens(input string) (tagFilters []zapscript.TagFilter, remaining string) {
	var extractedTags []zapscript.TagFilter
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

// MergeTagFilters merges extracted tags with advanced args tags.
// Advanced args tags take precedence - if the same tag type exists in both,
// the advanced args value is used.
// Returns nil if the result would be empty.
func MergeTagFilters(extracted, advArgs []zapscript.TagFilter) []zapscript.TagFilter {
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
	advArgsMap := make(map[string]zapscript.TagFilter)
	for _, tag := range advArgs {
		advArgsMap[tag.Type] = tag
	}

	// Start with advanced args tags (they take precedence)
	result := make([]zapscript.TagFilter, 0, len(extracted)+len(advArgs))
	result = append(result, advArgs...)

	// Add extracted tags that don't conflict with advanced args
	for _, tag := range extracted {
		if _, exists := advArgsMap[tag.Type]; !exists {
			result = append(result, tag)
		}
	}

	return result
}
