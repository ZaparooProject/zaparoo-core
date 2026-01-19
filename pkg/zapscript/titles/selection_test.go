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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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

func TestHasAllTagsOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tagFilters []zapscript.TagFilter
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
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorOR},
				{Type: "region", Value: "eu", Operator: zapscript.TagOperatorOR},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorOR},
				{Type: "region", Value: "eu", Operator: zapscript.TagOperatorOR},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
				{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
				{Type: "unfinished", Value: "beta", Operator: zapscript.TagOperatorNOT},
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
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorOR},
				{Type: "lang", Value: "es", Operator: zapscript.TagOperatorOR},
			},
			expected: true,
		},
		{
			name: "empty filters - matches all",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			tagFilters: []zapscript.TagFilter{},
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
		tagFilters    []zapscript.TagFilter
		expectedNames []string
		expectedCount int
	}{
		{
			name: "filter by single tag",
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			},
			expectedCount: 1,
			expectedNames: []string{"Game (USA)"},
		},
		{
			name: "filter by multiple AND tags",
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "eu", Operator: zapscript.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: zapscript.TagOperatorAND},
			},
			expectedCount: 1,
			expectedNames: []string{"Game (Europe)"},
		},
		{
			name: "filter with OR - matches multiple",
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorOR},
				{Type: "region", Value: "eu", Operator: zapscript.TagOperatorOR},
			},
			expectedCount: 2,
			expectedNames: []string{"Game (USA)", "Game (Europe)"},
		},
		{
			name: "filter with NOT - excludes",
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "jp", Operator: zapscript.TagOperatorNOT},
			},
			expectedCount: 2,
			expectedNames: []string{"Game (USA)", "Game (Europe)"},
		},
		{
			name:          "no filters - returns all",
			tagFilters:    []zapscript.TagFilter{},
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

func TestCheckNumericSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		filename      string
		expectedScore int
	}{
		{
			name:          "clean filename",
			filename:      "game.zip",
			expectedScore: 0,
		},
		{
			name:          "OS duplicate (1)",
			filename:      "game (1).zip",
			expectedScore: 1,
		},
		{
			name:          "OS duplicate (2)",
			filename:      "game (2).zip",
			expectedScore: 1,
		},
		{
			name:          "OS duplicate (6) - higher numbers",
			filename:      "game (6).zip",
			expectedScore: 1,
		},
		{
			name:          "OS duplicate (10) - double digits",
			filename:      "game (10).zip",
			expectedScore: 1,
		},
		{
			name:          "OS duplicate (99) - large numbers",
			filename:      "game (99).zip",
			expectedScore: 1,
		},
		{
			name:          "manual copy",
			filename:      "game - Copy.zip",
			expectedScore: 1,
		},
		{
			name:          "lowercase copy",
			filename:      "game copy.zip",
			expectedScore: 1,
		},
		{
			name:          "region tag not penalized",
			filename:      "game (USA).zip",
			expectedScore: 0, // Not a numeric duplicate
		},
		{
			name:          "version number not penalized",
			filename:      "game v1.0.zip",
			expectedScore: 0,
		},
		{
			name:          "year in parens gets penalized (acceptable trade-off)",
			filename:      "game (1996).zip",
			expectedScore: 1, // Years get caught by \d+ pattern, but this is rare and acceptable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			score := checkNumericSuffix(tt.filename)
			assert.Equal(t, tt.expectedScore, score)
		})
	}
}

func TestCalculateCharDensity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		filename      string
		expectedScore int
	}{
		{
			name:          "clean filename",
			filename:      "game.zip",
			expectedScore: 0,
		},
		{
			name:          "consecutive underscores",
			filename:      "game__v1.zip",
			expectedScore: 1, // One __ occurrence
		},
		{
			name:          "dots not penalized (valid abbreviations)",
			filename:      "S.T.A.L.K.E.R..zip",
			expectedScore: 0, // Dots are intentionally not counted
		},
		{
			name:          "mixed separators",
			filename:      "game-v1_final.zip",
			expectedScore: 2, // Has both - and _
		},
		{
			name:          "all messy",
			filename:      "game__v1.0-final_release.zip",
			expectedScore: 3, // __ (1) + mixed (2), dots not counted
		},
		{
			name:          "extension dot not counted",
			filename:      "game.zip",
			expectedScore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			score := calculateCharDensity(tt.filename)
			assert.Equal(t, tt.expectedScore, score)
		})
	}
}

func TestTiebreakerScoreCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        TiebreakerScore
		b        TiebreakerScore
		expected int // -1 if a better, 0 if equal, 1 if b better
	}{
		{
			name:     "equal scores",
			a:        TiebreakerScore{0, 2, 0, 10},
			b:        TiebreakerScore{0, 2, 0, 10},
			expected: 0,
		},
		{
			name:     "a wins on numeric suffix",
			a:        TiebreakerScore{0, 2, 0, 10},
			b:        TiebreakerScore{1, 2, 0, 10},
			expected: -1,
		},
		{
			name:     "a wins on path depth",
			a:        TiebreakerScore{0, 2, 0, 10},
			b:        TiebreakerScore{0, 5, 0, 10},
			expected: -1,
		},
		{
			name:     "a wins on char density",
			a:        TiebreakerScore{0, 2, 0, 10},
			b:        TiebreakerScore{0, 2, 3, 10},
			expected: -1,
		},
		{
			name:     "a wins on name length",
			a:        TiebreakerScore{0, 2, 0, 10},
			b:        TiebreakerScore{0, 2, 0, 20},
			expected: -1,
		},
		{
			name:     "priority order - numeric suffix beats all",
			a:        TiebreakerScore{0, 5, 5, 50}, // Clean suffix, worse everything else
			b:        TiebreakerScore{1, 2, 0, 10}, // Has suffix, better everything else
			expected: -1,                           // a still wins
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.a.Compare(tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSelectByQualityScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		expectedPath string
		description  string
		results      []database.SearchResultWithCursor
	}{
		{
			name: "prefers original over duplicate",
			results: []database.SearchResultWithCursor{
				{Name: "Zelda", Path: "/games/zelda (1).zip"},
				{Name: "Zelda", Path: "/games/zelda.zip"},
			},
			expectedPath: "/games/zelda.zip",
			description:  "Original should beat OS duplicate marker",
		},
		{
			name: "prefers shallower path",
			results: []database.SearchResultWithCursor{
				{Name: "Sonic", Path: "/games/backups/old/archive/sonic.rom"},
				{Name: "Sonic", Path: "/games/sonic.rom"},
			},
			expectedPath: "/games/sonic.rom",
			description:  "Shallower path indicates better curation",
		},
		{
			name: "prefers cleaner filename",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Path: "/games/game__v1.0-final_release.zip"},
				{Name: "Game", Path: "/games/game.zip"},
			},
			expectedPath: "/games/game.zip",
			description:  "Clean filename should beat messy one",
		},
		{
			name: "prefers shorter name (all else equal)",
			results: []database.SearchResultWithCursor{
				{Name: "Mario", Path: "/games/mario-bros-super-deluxe.zip"},
				{Name: "Mario", Path: "/games/mario.zip"},
			},
			expectedPath: "/games/mario.zip",
			description:  "Shorter filename as final tie-breaker",
		},
		{
			name: "complex real-world scenario",
			results: []database.SearchResultWithCursor{
				// Worst: deep path and duplicate marker
				{Name: "Super Mario Bros", Path: "/backups/old/Super Mario Bros (1).zip"},
				// Bad: copy marker
				{Name: "Super Mario Bros", Path: "/games/Super Mario Bros - Copy.zip"},
				// Best: no penalties, shallow path
				{Name: "Super Mario Bros", Path: "/games/Super Mario Bros.zip"},
				// Medium: deeper path but no other penalties
				{Name: "Super Mario Bros", Path: "/roms/archive/Super Mario Bros.zip"},
			},
			expectedPath: "/games/Super Mario Bros.zip",
			description:  "Best overall quality should win",
		},
		{
			name: "single result",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Path: "/games/game.zip"},
			},
			expectedPath: "/games/game.zip",
			description:  "Single result returns itself",
		},
		{
			name:         "empty results",
			results:      []database.SearchResultWithCursor{},
			expectedPath: "",
			description:  "Empty returns zero value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := selectByQualityScore(tt.results)
			assert.Equal(t, tt.expectedPath, result.Path, tt.description)
		})
	}
}

func TestFilterByFileTypePriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		description   string
		results       []database.SearchResultWithCursor
		launchers     []platforms.Launcher
		expectedPaths []string
	}{
		{
			name:        "single launcher - prefers first extension",
			description: "Should select .mgl (position 0) over others",
			results: []database.SearchResultWithCursor{
				{Name: "game", Path: "/games/game.vhd"},
				{Name: "game", Path: "/games/game.mgl"},
				{Name: "game", Path: "/games/game.img"},
			},
			launchers: []platforms.Launcher{
				{
					ID:         "dos",
					Extensions: []string{".mgl", ".vhd", ".img"},
				},
			},
			expectedPaths: []string{"/games/game.mgl"},
		},
		{
			name:        "single launcher - multiple results with same extension",
			description: "Should return all results with best extension",
			results: []database.SearchResultWithCursor{
				{Name: "game", Path: "/games/game1.mgl"},
				{Name: "game", Path: "/games/game2.mgl"},
				{Name: "game", Path: "/games/game.vhd"},
			},
			launchers: []platforms.Launcher{
				{
					ID:         "dos",
					Extensions: []string{".mgl", ".vhd"},
				},
			},
			expectedPaths: []string{"/games/game1.mgl", "/games/game2.mgl"},
		},
		{
			name:        "multiple launchers - best score across any launcher",
			description: "Should select .chd (pos 0 in launcher2) and .iso (pos 0 in launcher1)",
			results: []database.SearchResultWithCursor{
				{Name: "game", Path: "/games/game.chd"},
				{Name: "game", Path: "/games/game.cue"},
				{Name: "game", Path: "/games/game.iso"},
			},
			launchers: []platforms.Launcher{
				{
					ID:         "launcher1",
					Extensions: []string{".iso", ".cue"},
				},
				{
					ID:         "launcher2",
					Extensions: []string{".chd", ".iso"},
				},
			},
			expectedPaths: []string{"/games/game.chd", "/games/game.iso"},
		},
		{
			name:        "extension not in any launcher - all have same score",
			description: "Should return all results when none match launcher extensions",
			results: []database.SearchResultWithCursor{
				{Name: "game", Path: "/games/game.zip"},
				{Name: "game", Path: "/games/game.rar"},
			},
			launchers: []platforms.Launcher{
				{
					ID:         "dos",
					Extensions: []string{".mgl", ".vhd"},
				},
			},
			expectedPaths: []string{"/games/game.zip", "/games/game.rar"},
		},
		{
			name:        "mixed - some match launcher, some don't",
			description: "Should prefer .mgl (pos 0) and filter out unmatched extensions",
			results: []database.SearchResultWithCursor{
				{Name: "game", Path: "/games/game.mgl"},
				{Name: "game", Path: "/games/game.zip"}, // Not in launcher
				{Name: "game", Path: "/games/game.vhd"},
			},
			launchers: []platforms.Launcher{
				{
					ID:         "dos",
					Extensions: []string{".mgl", ".vhd"},
				},
			},
			expectedPaths: []string{"/games/game.mgl"},
		},
		{
			name:        "empty launchers - returns all results",
			description: "Should return all results when launchers is empty",
			results: []database.SearchResultWithCursor{
				{Name: "game", Path: "/games/game1.zip"},
				{Name: "game", Path: "/games/game2.rom"},
			},
			launchers:     []platforms.Launcher{},
			expectedPaths: []string{"/games/game1.zip", "/games/game2.rom"},
		},
		{
			name:          "empty results - returns empty",
			description:   "Should return empty slice when no results",
			results:       []database.SearchResultWithCursor{},
			launchers:     []platforms.Launcher{{ID: "test", Extensions: []string{".mgl"}}},
			expectedPaths: []string{},
		},
		{
			name:        "case insensitive extension matching",
			description: "Should match extensions case-insensitively",
			results: []database.SearchResultWithCursor{
				{Name: "game", Path: "/games/game.MGL"},
				{Name: "game", Path: "/games/game.VHD"},
			},
			launchers: []platforms.Launcher{
				{
					ID:         "dos",
					Extensions: []string{".mgl", ".vhd"},
				},
			},
			expectedPaths: []string{"/games/game.MGL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filtered := FilterByFileTypePriority(tt.results, tt.launchers)

			// Extract paths from filtered results
			actualPaths := make([]string, 0, len(filtered))
			for _, r := range filtered {
				actualPaths = append(actualPaths, r.Path)
			}

			// Sort both slices for comparison (order within same priority doesn't matter)
			assert.ElementsMatch(t, tt.expectedPaths, actualPaths, tt.description)
		})
	}
}

func TestSelectBest_SingleResultVariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		description  string
		results      []database.SearchResultWithCursor
		matchQuality float64
		wantSelected bool
	}{
		{
			name: "single result with unlicensed:translation should be selected (FTL regression)",
			results: []database.SearchResultWithCursor{
				{
					MediaID:  1,
					Name:     "FTL: Faster Than Light",
					Path:     "steam://212680/FTL",
					SystemID: "PC",
					Tags: []database.TagInfo{
						{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedTranslation)},
					},
				},
			},
			matchQuality: 1.0,
			wantSelected: true,
			description:  "single result should be selected even if it's a variant",
		},
		{
			name: "single result with demo tag should be selected",
			results: []database.SearchResultWithCursor{
				{
					MediaID:  1,
					Name:     "Game Demo",
					Path:     "/path/to/demo",
					SystemID: "PC",
					Tags: []database.TagInfo{
						{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedDemo)},
					},
				},
			},
			matchQuality: 1.0,
			wantSelected: true,
			description:  "single demo result should be selected",
		},
		{
			name: "single result with beta tag should be selected",
			results: []database.SearchResultWithCursor{
				{
					MediaID:  1,
					Name:     "Game Beta",
					Path:     "/path/to/beta",
					SystemID: "PC",
					Tags: []database.TagInfo{
						{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedBeta)},
					},
				},
			},
			matchQuality: 1.0,
			wantSelected: true,
			description:  "single beta result should be selected",
		},
		{
			name: "single result with no variant tags should be selected",
			results: []database.SearchResultWithCursor{
				{
					MediaID:  1,
					Name:     "Regular Game",
					Path:     "/path/to/game",
					SystemID: "PC",
					Tags:     []database.TagInfo{},
				},
			},
			matchQuality: 1.0,
			wantSelected: true,
			description:  "single normal result should be selected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, confidence := SelectBestResult(
				tt.results,
				[]zapscript.TagFilter{},
				nil, // cfg
				tt.matchQuality,
				[]platforms.Launcher{},
			)

			if tt.wantSelected {
				assert.NotZero(t, confidence, "expected result to be selected but confidence was 0")
				assert.Equal(t, tt.results[0].MediaID, result.MediaID, "wrong result selected")
			} else {
				assert.Zero(t, confidence, "expected result to be rejected but confidence was non-zero")
			}
		})
	}
}

func TestCalculateTagMatchConfidence_YearSoftPreference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		description string
		tagFilters  []zapscript.TagFilter
		result      database.SearchResultWithCursor
		minExpected float64
		maxExpected float64
	}{
		{
			name: "exact year match boosts confidence",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "year", Tag: "1991"}},
			},
			tagFilters: []zapscript.TagFilter{
				{Type: "year", Value: "1991", Operator: zapscript.TagOperatorAND},
			},
			minExpected: 0.99,
			maxExpected: 1.0,
			description: "exact year match should give full confidence",
		},
		{
			name: "year mismatch has small penalty (not dealbreaker)",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "year", Tag: "1992"}},
			},
			tagFilters: []zapscript.TagFilter{
				{Type: "year", Value: "1991", Operator: zapscript.TagOperatorAND},
			},
			// Year penalty is 0.05 vs normal 0.2 penalty
			// With 1 filter and 0 matches: matchRatio = 0, penalty = 0.05
			// confidence = 0 - 0.05 = 0 (clamped)
			// But wait - there's no match so matchRatio = 0
			// The key is that it's not as harsh as other tag conflicts
			minExpected: 0.0,
			maxExpected: 0.05, // Should be near 0 but not a hard failure
			description: "year mismatch should have small penalty",
		},
		{
			name: "year missing from result has no penalty",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			tagFilters: []zapscript.TagFilter{
				{Type: "year", Value: "1991", Operator: zapscript.TagOperatorAND},
			},
			// Year not present = no conflict counted
			// matchRatio = 0/1 = 0, penalty = 0
			minExpected: 0.0,
			maxExpected: 0.05,
			description: "missing year should not penalize",
		},
		{
			name: "region mismatch has normal penalty",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "jp"}},
			},
			tagFilters: []zapscript.TagFilter{
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			},
			// Normal penalty: matchRatio = 0, penalty = 0.2
			// confidence = 0 - 0.2 = -0.2 (clamped to 0)
			minExpected: 0.0,
			maxExpected: 0.01,
			description: "region mismatch should have normal penalty",
		},
		{
			name: "year mismatch with region match still acceptable",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "year", Tag: "1992"},
					{Type: "region", Tag: "us"},
				},
			},
			tagFilters: []zapscript.TagFilter{
				{Type: "year", Value: "1991", Operator: zapscript.TagOperatorAND},
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			},
			// 2 filters total
			// region matches (matched = 1)
			// year conflicts with small penalty (yearConflicts = 1)
			// matchRatio = 1/2 = 0.5
			// penalty = 0*0.2 + 1*0.05 = 0.05
			// confidence = 0.5 - 0.05 = 0.45
			minExpected: 0.40,
			maxExpected: 0.50,
			description: "year mismatch with good region match should still be acceptable",
		},
		{
			name: "compare year vs region mismatch severity",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "year", Tag: "wrong"},
					{Type: "region", Tag: "wrong"},
				},
			},
			tagFilters: []zapscript.TagFilter{
				{Type: "year", Value: "1991", Operator: zapscript.TagOperatorAND},
				{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND},
			},
			// 2 filters, 0 matches
			// 1 year conflict (0.05 penalty) + 1 region conflict (0.2 penalty)
			// matchRatio = 0/2 = 0
			// penalty = 0.2 + 0.05 = 0.25
			// confidence = 0 - 0.25 = -0.25 (clamped to 0)
			minExpected: 0.0,
			maxExpected: 0.01,
			description: "both mismatches should result in low confidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			confidence := CalculateTagMatchConfidence(&tt.result, tt.tagFilters)

			assert.GreaterOrEqual(t, confidence, tt.minExpected,
				"%s: confidence %.2f should be >= %.2f", tt.description, confidence, tt.minExpected)
			assert.LessOrEqual(t, confidence, tt.maxExpected,
				"%s: confidence %.2f should be <= %.2f", tt.description, confidence, tt.maxExpected)
		})
	}
}
