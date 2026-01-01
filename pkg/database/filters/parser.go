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

package filters

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

const (
	maxTagsCount = 50
	maxTagLength = 128
)

// ParseTagFilters parses a slice of tag strings into TagFilter structs.
// Supports operator prefixes:
//   - "+" or no prefix: AND (default) - must have tag
//   - "-": NOT - must not have tag
//   - "~": OR - at least one OR tag must match
//
// Format: "type:value" or "+type:value" (AND), "-type:value" (NOT), "~type:value" (OR)
// Example: []string{"region:usa", "-unfinished:demo", "~lang:en", "~lang:es"}
// Returns normalized, deduplicated filters.
func ParseTagFilters(tagSlice []string) ([]database.TagFilter, error) {
	if len(tagSlice) > maxTagsCount {
		return nil, fmt.Errorf("exceeded maximum number of tags: %d (max: %d)", len(tagSlice), maxTagsCount)
	}

	// Use map for deduplication while maintaining order
	type filterKey struct {
		typ      string
		value    string
		operator database.TagOperator
	}
	seenFilters := make(map[filterKey]bool)
	result := make([]database.TagFilter, 0, len(tagSlice))

	for _, tagStr := range tagSlice {
		trimmedTag := strings.TrimSpace(tagStr)
		if trimmedTag == "" {
			continue
		}

		if len(trimmedTag) > maxTagLength {
			return nil, fmt.Errorf("tag too long: %q (max: %d characters)", trimmedTag, maxTagLength)
		}

		// Parse operator prefix
		operator := database.TagOperatorAND // default
		if trimmedTag != "" {
			switch trimmedTag[0] {
			case '+':
				operator = database.TagOperatorAND
				trimmedTag = trimmedTag[1:] // Remove operator prefix
			case '-':
				operator = database.TagOperatorNOT
				trimmedTag = trimmedTag[1:] // Remove operator prefix
			case '~':
				operator = database.TagOperatorOR
				trimmedTag = trimmedTag[1:] // Remove operator prefix
			}
		}

		// Validate type:value format
		parts := strings.SplitN(trimmedTag, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid tag format for %q: must be in 'type:value' format", tagStr)
		}

		tagType := strings.TrimSpace(parts[0])
		tagValue := strings.TrimSpace(parts[1])

		// Apply full normalization FIRST
		normalizedType := tags.NormalizeTag(tagType)
		normalizedValue := tags.NormalizeTag(tagValue)

		// Validate AFTER normalization to catch cases where normalization strips everything
		if normalizedType == "" || normalizedValue == "" {
			return nil, fmt.Errorf("invalid tag %q: type and value cannot be empty after normalization", tagStr)
		}

		filter := database.TagFilter{
			Type:     normalizedType,
			Value:    normalizedValue,
			Operator: operator,
		}

		// Deduplicate by normalized key (including operator), preserving order
		key := filterKey{
			typ:      filter.Type,
			value:    filter.Value,
			operator: operator,
		}
		if !seenFilters[key] {
			seenFilters[key] = true
			result = append(result, filter)
		}
	}

	return result, nil
}
