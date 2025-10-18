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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
)

func TestGetRegionMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		preferredRegions []string
		result           database.SearchResultWithCursor
		expectedMatch    tagMatch
	}{
		{
			name: "matches preferred region",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			preferredRegions: []string{"us", "world"},
			expectedMatch:    tagMatchPreferred,
		},
		{
			name: "matches second preferred region",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "world"}},
			},
			preferredRegions: []string{"us", "world"},
			expectedMatch:    tagMatchPreferred,
		},
		{
			name: "no region tag",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "lang", Tag: "en"}},
			},
			preferredRegions: []string{"us"},
			expectedMatch:    tagMatchUntagged,
		},
		{
			name: "non-preferred region",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "jp"}},
			},
			preferredRegions: []string{"us", "world"},
			expectedMatch:    tagMatchOther,
		},
		{
			name:             "no tags at all",
			result:           database.SearchResultWithCursor{Tags: []database.TagInfo{}},
			preferredRegions: []string{"us"},
			expectedMatch:    tagMatchUntagged,
		},
		{
			name: "empty preferred regions",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			preferredRegions: []string{},
			expectedMatch:    tagMatchOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := getRegionMatch(&tt.result, tt.preferredRegions)
			assert.Equal(t, tt.expectedMatch, result)
		})
	}
}

func TestGetLanguageMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		preferredLangs []string
		result         database.SearchResultWithCursor
		expectedMatch  tagMatch
	}{
		{
			name: "matches preferred language",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "lang", Tag: "en"}},
			},
			preferredLangs: []string{"en", "es"},
			expectedMatch:  tagMatchPreferred,
		},
		{
			name: "matches second preferred language",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "lang", Tag: "es"}},
			},
			preferredLangs: []string{"en", "es"},
			expectedMatch:  tagMatchPreferred,
		},
		{
			name: "no language tag",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			preferredLangs: []string{"en"},
			expectedMatch:  tagMatchUntagged,
		},
		{
			name: "non-preferred language",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "lang", Tag: "ja"}},
			},
			preferredLangs: []string{"en", "es"},
			expectedMatch:  tagMatchOther,
		},
		{
			name:           "no tags at all",
			result:         database.SearchResultWithCursor{Tags: []database.TagInfo{}},
			preferredLangs: []string{"en"},
			expectedMatch:  tagMatchUntagged,
		},
		{
			name: "empty preferred languages",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "lang", Tag: "en"}},
			},
			preferredLangs: []string{},
			expectedMatch:  tagMatchOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := getLanguageMatch(&tt.result, tt.preferredLangs)
			assert.Equal(t, tt.expectedMatch, result)
		})
	}
}

func TestSelectAlphabeticallyByFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		expectedPath string
		results      []database.SearchResultWithCursor
	}{
		{
			name: "selects first alphabetically",
			results: []database.SearchResultWithCursor{
				{Path: "/games/zelda.rom"},
				{Path: "/games/mario.rom"},
				{Path: "/games/sonic.rom"},
			},
			expectedPath: "/games/mario.rom",
		},
		{
			name: "handles leading numbers",
			results: []database.SearchResultWithCursor{
				{Path: "/games/9game.rom"},
				{Path: "/games/1game.rom"},
				{Path: "/games/5game.rom"},
			},
			expectedPath: "/games/1game.rom",
		},
		{
			name: "case sensitive sorting",
			results: []database.SearchResultWithCursor{
				{Path: "/games/Zelda.rom"},
				{Path: "/games/mario.rom"},
				{Path: "/games/Sonic.rom"},
			},
			expectedPath: "/games/Sonic.rom", // Capital letters sort before lowercase
		},
		{
			name: "single result",
			results: []database.SearchResultWithCursor{
				{Path: "/games/only.rom"},
			},
			expectedPath: "/games/only.rom",
		},
		{
			name:         "empty results",
			results:      []database.SearchResultWithCursor{},
			expectedPath: "", // returns zero value
		},
		{
			name: "same directory different files",
			results: []database.SearchResultWithCursor{
				{Path: "/games/z.rom"},
				{Path: "/games/a.rom"},
				{Path: "/games/m.rom"},
			},
			expectedPath: "/games/a.rom",
		},
		{
			name: "different directories same filename",
			results: []database.SearchResultWithCursor{
				{Path: "/z/game.rom"},
				{Path: "/a/game.rom"},
				{Path: "/m/game.rom"},
			},
			expectedPath: "/z/game.rom", // Returns first when filenames are equal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := selectAlphabeticallyByFilename(tt.results)
			assert.Equal(t, tt.expectedPath, result.Path)
		})
	}
}

func TestHasAllTagsOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tagFilters []database.TagFilter
		result     database.SearchResultWithCursor
		expected   bool
	}{
		{
			name: "AND operator - all required tags present",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
					{Type: "lang", Tag: "en"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: database.TagOperatorAND},
			},
			expected: true,
		},
		{
			name: "AND operator - missing required tag",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: database.TagOperatorAND},
			},
			expected: false,
		},
		{
			name: "NOT operator - excluded tag present",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "unfinished", Tag: "beta"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
			},
			expected: false,
		},
		{
			name: "NOT operator - excluded tag absent",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
			},
			expected: true,
		},
		{
			name: "OR operator - has one of multiple options",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorOR},
				{Type: "region", Value: "eu", Operator: database.TagOperatorOR},
			},
			expected: true,
		},
		{
			name: "OR operator - has none of the options",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "jp"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorOR},
				{Type: "region", Value: "eu", Operator: database.TagOperatorOR},
			},
			expected: false,
		},
		{
			name: "mixed operators - AND + NOT",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
					{Type: "lang", Tag: "en"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
			},
			expected: true,
		},
		{
			name: "mixed operators - AND + NOT (fails NOT)",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
					{Type: "unfinished", Tag: "beta"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
			},
			expected: false,
		},
		{
			name: "mixed operators - AND + OR",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
					{Type: "lang", Tag: "en"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: database.TagOperatorOR},
				{Type: "lang", Value: "es", Operator: database.TagOperatorOR},
			},
			expected: true,
		},
		{
			name: "empty filters - matches all",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			tagFilters: []database.TagFilter{},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := HasAllTags(&tt.result, tt.tagFilters)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsVariantEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   database.SearchResultWithCursor
		expected bool
	}{
		{
			name: "alpha version is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedAlpha)}},
			},
			expected: true,
		},
		{
			name: "sample is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedSample)}},
			},
			expected: true,
		},
		{
			name: "preview is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedPreview)},
				},
			},
			expected: true,
		},
		{
			name: "prerelease is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedPrerelease)},
				},
			},
			expected: true,
		},
		{
			name: "bootleg is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedBootleg)},
				},
			},
			expected: true,
		},
		{
			name: "clone is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedClone)}},
			},
			expected: true,
		},
		{
			name: "bad dump is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpBad)}},
			},
			expected: true,
		},
		{
			name: "multiple variant tags",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedBeta)},
					{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedHack)},
				},
			},
			expected: true,
		},
		{
			name: "normal tags not variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
					{Type: "lang", Tag: "en"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := IsVariant(&tt.result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterByTagsMultipleResults(t *testing.T) {
	t.Parallel()

	results := []database.SearchResultWithCursor{
		{
			Name: "Game (USA)",
			Tags: []database.TagInfo{
				{Type: "region", Tag: "us"},
				{Type: "lang", Tag: "en"},
			},
		},
		{
			Name: "Game (Japan)",
			Tags: []database.TagInfo{
				{Type: "region", Tag: "jp"},
				{Type: "lang", Tag: "ja"},
			},
		},
		{
			Name: "Game (Europe)",
			Tags: []database.TagInfo{
				{Type: "region", Tag: "eu"},
				{Type: "lang", Tag: "en"},
			},
		},
	}

	tests := []struct {
		name          string
		tagFilters    []database.TagFilter
		expectedNames []string
		expectedCount int
	}{
		{
			name: "filter by single tag",
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
			},
			expectedCount: 1,
			expectedNames: []string{"Game (USA)"},
		},
		{
			name: "filter by multiple AND tags",
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "eu", Operator: database.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: database.TagOperatorAND},
			},
			expectedCount: 1,
			expectedNames: []string{"Game (Europe)"},
		},
		{
			name: "filter with OR - matches multiple",
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorOR},
				{Type: "region", Value: "eu", Operator: database.TagOperatorOR},
			},
			expectedCount: 2,
			expectedNames: []string{"Game (USA)", "Game (Europe)"},
		},
		{
			name: "filter with NOT - excludes",
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "jp", Operator: database.TagOperatorNOT},
			},
			expectedCount: 2,
			expectedNames: []string{"Game (USA)", "Game (Europe)"},
		},
		{
			name:          "no filters - returns all",
			tagFilters:    []database.TagFilter{},
			expectedCount: 3,
			expectedNames: []string{"Game (USA)", "Game (Japan)", "Game (Europe)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filtered := FilterByTags(results, tt.tagFilters)
			assert.Len(t, filtered, tt.expectedCount)

			if len(tt.expectedNames) > 0 {
				names := make([]string, len(filtered))
				for i, r := range filtered {
					names[i] = r.Name
				}
				assert.ElementsMatch(t, tt.expectedNames, names)
			}
		})
	}
}
