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
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeTagFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		extracted       []zapscript.TagFilter
		advArgs         []zapscript.TagFilter
		expectedTags    []zapscript.TagFilter
		expectedCount   int
		shouldReturnNil bool
	}{
		{
			name:            "both empty - returns nil",
			extracted:       []zapscript.TagFilter{},
			advArgs:         []zapscript.TagFilter{},
			shouldReturnNil: true,
		},
		{
			name: "only extracted tags",
			extracted: []zapscript.TagFilter{
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
			},
			advArgs:       []zapscript.TagFilter{},
			expectedCount: 2,
			expectedTags: []zapscript.TagFilter{
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
			},
		},
		{
			name:      "only advArgs tags",
			extracted: []zapscript.TagFilter{},
			advArgs: []zapscript.TagFilter{
				{Type: "region", Value: "jp"},
				{Type: "year", Value: "1994"},
			},
			expectedCount: 2,
			expectedTags: []zapscript.TagFilter{
				{Type: "region", Value: "jp"},
				{Type: "year", Value: "1994"},
			},
		},
		{
			name: "no overlap - both included",
			extracted: []zapscript.TagFilter{
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
			},
			advArgs: []zapscript.TagFilter{
				{Type: "year", Value: "1994"},
				{Type: "genre", Value: "rpg"},
			},
			expectedCount: 4,
			expectedTags: []zapscript.TagFilter{
				{Type: "year", Value: "1994"},
				{Type: "genre", Value: "rpg"},
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
			},
		},
		{
			name: "advArgs takes precedence over extracted",
			extracted: []zapscript.TagFilter{
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
			},
			advArgs: []zapscript.TagFilter{
				{Type: "region", Value: "jp"}, // Overrides "us"
			},
			expectedCount: 2,
			expectedTags: []zapscript.TagFilter{
				{Type: "region", Value: "jp"}, // advArgs value
				{Type: "lang", Value: "en"},   // extracted value (no conflict)
			},
		},
		{
			name: "multiple conflicts - advArgs wins all",
			extracted: []zapscript.TagFilter{
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
				{Type: "year", Value: "1990"},
			},
			advArgs: []zapscript.TagFilter{
				{Type: "region", Value: "jp"},
				{Type: "lang", Value: "ja"},
			},
			expectedCount: 3,
			expectedTags: []zapscript.TagFilter{
				{Type: "region", Value: "jp"}, // advArgs wins
				{Type: "lang", Value: "ja"},   // advArgs wins
				{Type: "year", Value: "1990"}, // no conflict
			},
		},
		{
			name: "different operators preserved",
			extracted: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
				{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
			},
			advArgs: []zapscript.TagFilter{
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
			},
			expectedCount: 3,
			expectedTags: []zapscript.TagFilter{
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
				{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
			},
		},
		{
			name: "advArgs operators override extracted operators",
			extracted: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			},
			advArgs: []zapscript.TagFilter{
				{Type: "region", Value: "jp", Operator: zapscript.TagOperatorOR},
			},
			expectedCount: 1,
			expectedTags: []zapscript.TagFilter{
				{Type: "region", Value: "jp", Operator: zapscript.TagOperatorOR}, // advArgs wins completely
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := MergeTagFilters(tt.extracted, tt.advArgs)

			if tt.shouldReturnNil {
				assert.Nil(t, result)
				return
			}

			assert.Len(t, result, tt.expectedCount)

			// Verify all expected tags are present (order doesn't matter for some tests)
			if len(tt.expectedTags) > 0 {
				for _, expectedTag := range tt.expectedTags {
					found := false
					for _, resultTag := range result {
						if resultTag.Type == expectedTag.Type &&
							resultTag.Value == expectedTag.Value &&
							resultTag.Operator == expectedTag.Operator {
							found = true
							break
						}
					}
					assert.True(t, found, "expected tag not found: %+v", expectedTag)
				}
			}
		})
	}
}

func TestMergeTagFiltersPreservesAdvArgsOrder(t *testing.T) {
	t.Parallel()

	extracted := []zapscript.TagFilter{
		{Type: "region", Value: "us"},
		{Type: "lang", Value: "en"},
	}

	advArgs := []zapscript.TagFilter{
		{Type: "year", Value: "1994"},
		{Type: "genre", Value: "rpg"},
		{Type: "platform", Value: "snes"},
	}

	result := MergeTagFilters(extracted, advArgs)

	// First N items should be advArgs in order
	assert.Len(t, result, 5)
	assert.Equal(t, "year", result[0].Type)
	assert.Equal(t, "genre", result[1].Type)
	assert.Equal(t, "platform", result[2].Type)
	// Remaining are extracted (order not guaranteed)
}

func TestExtractCanonicalTagsFromParensEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		input             string
		expectedRemaining string
		expectedCount     int
	}{
		{
			name:              "tag type with underscores",
			input:             "Game (tag_type:value)",
			expectedRemaining: "Game",
			expectedCount:     1,
		},
		{
			name:              "tag type with hyphens",
			input:             "Game (tag-type:value)",
			expectedRemaining: "Game",
			expectedCount:     1,
		},
		{
			name:              "tag type with numbers",
			input:             "Game (tag2:value)",
			expectedRemaining: "Game",
			expectedCount:     1,
		},
		{
			name:              "tag value with spaces",
			input:             "Game (region:north america)",
			expectedRemaining: "Game",
			expectedCount:     1,
		},
		{
			name:              "tag value with special chars",
			input:             "Game (name:foo-bar_baz)",
			expectedRemaining: "Game",
			expectedCount:     1,
		},
		{
			name:              "multiple consecutive tags",
			input:             "Game (region:us)(lang:en)(year:1994)",
			expectedRemaining: "Game",
			expectedCount:     3,
		},
		{
			name:              "tags with whitespace between",
			input:             "Game (region:us) (lang:en)",
			expectedRemaining: "Game",
			expectedCount:     2,
		},
		{
			name:              "invalid tag - missing colon",
			input:             "Game (invalidtag)",
			expectedRemaining: "Game (invalidtag)",
			expectedCount:     0,
		},
		{
			name:              "invalid tag - starts with number",
			input:             "Game (1invalid:value)",
			expectedRemaining: "Game (1invalid:value)",
			expectedCount:     0,
		},
		{
			name:              "invalid tag - empty type",
			input:             "Game (:value)",
			expectedRemaining: "Game (:value)",
			expectedCount:     0,
		},
		{
			name:              "invalid tag - empty value",
			input:             "Game (type:)",
			expectedRemaining: "Game (type:)",
			expectedCount:     0,
		},
		{
			name:              "mixed valid and invalid tags",
			input:             "Game (region:us) (invalid) (lang:en)",
			expectedRemaining: "Game (invalid)",
			expectedCount:     2,
		},
		{
			name:              "tag at start of string",
			input:             "(region:us) Game Name",
			expectedRemaining: "Game Name",
			expectedCount:     1,
		},
		{
			name:              "tag at end of string",
			input:             "Game Name (region:us)",
			expectedRemaining: "Game Name",
			expectedCount:     1,
		},
		{
			name:              "extra whitespace cleanup",
			input:             "Game   (region:us)   Name   (lang:en)",
			expectedRemaining: "Game Name",
			expectedCount:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tagFilters, remaining := ExtractCanonicalTagsFromParens(tt.input)

			assert.Equal(t, tt.expectedRemaining, remaining, "remaining string mismatch")
			assert.Len(t, tagFilters, tt.expectedCount, "extracted tag count mismatch")
		})
	}
}

func TestExtractCanonicalTagsFromParensOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		input            string
		expectedOperator zapscript.TagOperator
	}{
		{
			name:             "AND operator explicit",
			input:            "Game (+region:us)",
			expectedOperator: zapscript.TagOperatorAND,
		},
		{
			name:             "NOT operator",
			input:            "Game (-unfinished:beta)",
			expectedOperator: zapscript.TagOperatorNOT,
		},
		{
			name:             "OR operator",
			input:            "Game (~lang:en)",
			expectedOperator: zapscript.TagOperatorOR,
		},
		{
			name:             "default to AND when no operator",
			input:            "Game (region:us)",
			expectedOperator: zapscript.TagOperatorAND,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tagFilters, _ := ExtractCanonicalTagsFromParens(tt.input)

			require.Len(t, tagFilters, 1)
			assert.Equal(t, tt.expectedOperator, tagFilters[0].Operator)
		})
	}
}
