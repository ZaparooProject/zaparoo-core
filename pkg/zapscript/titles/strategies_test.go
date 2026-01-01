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
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTryMainTitleOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock        func(*helpers.MockMediaDBI)
		name             string
		slug             string
		systemID         string
		expectedStrategy string
		matchInfo        GameMatchInfo
		expectedCount    int
		shouldError      bool
	}{
		{
			name: "exact match: query has secondary, DB doesn't",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "somegame",
				SecondaryTitleSlug: "thenextgen",
				CanonicalSlug:      "somegamethenextgen",
			},
			slug:     "somegamethenextgen",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				// Searches with MainTitleSlug, not full slug
				m.On("SearchMediaBySlugPrefix", mock.Anything, "SNES", "somegame", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "SNES", Name: "Some Game", Path: "/somegame.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyMainTitleOnly,
			shouldError:      false,
		},
		{
			name: "partial match: query simple, DB has secondary",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "somegame",
				CanonicalSlug:     "somegame",
			},
			slug:     "somegame",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugPrefix", mock.Anything, "SNES", "somegame", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "SNES", Name: "Some Game: The Next Gen", Path: "/somegame2.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyMainTitleOnly,
			shouldError:      false,
		},
		{
			name: "prefer exact over partial matches",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "mario",
				SecondaryTitleSlug: "bros",
				CanonicalSlug:      "mariobros",
			},
			slug:     "mariobros",
			systemID: "NES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugPrefix", mock.Anything, "NES", "mario", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "NES", Name: "Mario", Path: "/mario.rom"},                 // Exact match
						{SystemID: "NES", Name: "Mario: Lost Levels", Path: "/mario2.rom"},   // Partial match
						{SystemID: "NES", Name: "Mario: Super Show", Path: "/marioshow.rom"}, // Partial match
					}, nil)
			},
			expectedCount:    1, // Should only return exact match
			expectedStrategy: StrategyMainTitleOnly,
			shouldError:      false,
		},
		{
			name: "no results found",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "nonexistent",
				SecondaryTitleSlug: "game",
				CanonicalSlug:      "nonexistentgame",
			},
			slug:     "nonexistentgame",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugPrefix", mock.Anything, "SNES", "nonexistent", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "search returns error",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "error",
				SecondaryTitleSlug: "test",
				CanonicalSlug:      "errortest",
			},
			slug:     "errortest",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugPrefix", mock.Anything, "SNES", "error", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, errors.New("database error"))
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      true,
		},
		{
			name: "no valid matches after filtering - DB has unrelated prefix",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "mario",
				CanonicalSlug:     "mario",
			},
			slug:     "mario",
			systemID: "NES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugPrefix", mock.Anything, "NES", "mario", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						// This matches the prefix but is not a valid match:
						// Query is simple "mario", DB is "MarioKart" which also has no secondary
						// but CanonicalSlug is "mariokart" not "mario", so no match
						{SystemID: "NES", Name: "MarioKart", Path: "/mariokart.rom"},
					}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockDB := helpers.NewMockMediaDBI()
			tt.setupMock(mockDB)

			results, strategy, err := TryMainTitleOnly(
				context.Background(),
				mockDB,
				tt.systemID,
				tt.slug,
				tt.matchInfo,
				nil,
				"Game",
			)

			if tt.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, results, tt.expectedCount)
				assert.Equal(t, tt.expectedStrategy, strategy)
			}

			mockDB.AssertExpectations(t)
		})
	}
}

func TestTrySecondaryTitleExact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock        func(*helpers.MockMediaDBI)
		name             string
		slug             string
		systemID         string
		expectedStrategy string
		matchInfo        GameMatchInfo
		expectedCount    int
		shouldError      bool
	}{
		{
			name: "exact match: input has secondary, DB doesn't",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "legendofzelda",
				SecondaryTitleSlug: "ocarinaoftime",
				CanonicalSlug:      "legendofzeldaocarinaoftime",
			},
			slug:     "legendofzeldaocarinaoftime",
			systemID: "Nintendo64",
			setupMock: func(m *helpers.MockMediaDBI) {
				// Searches with SecondaryTitleSlug
				m.On("SearchMediaBySlug", mock.Anything, "Nintendo64", "ocarinaoftime", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "Nintendo64", Name: "Ocarina of Time", Path: "/oot.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategySecondaryTitleExact,
			shouldError:      false,
		},
		{
			name: "partial match: input simple, DB has secondary",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "ocarinaoftime",
				CanonicalSlug:     "ocarinaoftime",
			},
			slug:     "ocarinaoftime",
			systemID: "Nintendo64",
			setupMock: func(m *helpers.MockMediaDBI) {
				// First tries exact match (SearchMediaBySlug)
				m.On(
					"SearchMediaBySlug", mock.Anything, "Nintendo64", "ocarinaoftime", []database.TagFilter(nil),
				).Return([]database.SearchResultWithCursor{}, nil)
				// Then tries partial match (SearchMediaBySecondarySlug)
				m.On(
					"SearchMediaBySecondarySlug",
					mock.Anything,
					"Nintendo64",
					"ocarinaoftime",
					[]database.TagFilter(nil),
				).Return([]database.SearchResultWithCursor{
					{SystemID: "Nintendo64", Name: "Legend of Zelda: Ocarina of Time", Path: "/zelda-oot.rom"},
				}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategySecondaryTitleExact,
			shouldError:      false,
		},
		{
			name: "prefer exact over partial matches",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "streetfighter",
				SecondaryTitleSlug: "turbo",
				CanonicalSlug:      "streetfighterturbo",
			},
			slug:     "streetfighterturbo",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				// Exact match search finds simple "Turbo" game
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "turbo", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "SNES", Name: "Turbo", Path: "/turbo.rom"}, // Exact match
					}, nil)
				// Should not call SearchMediaBySecondarySlug since exact match was found
			},
			expectedCount:    1,
			expectedStrategy: StrategySecondaryTitleExact,
			shouldError:      false,
		},
		{
			name: "secondary slug too short - returns nil",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "game",
				SecondaryTitleSlug: "ii", // Very short secondary title
				CanonicalSlug:      "gameii",
			},
			slug:     "gameii",
			systemID: "NES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "NES", "ii", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
				m.On("SearchMediaBySecondarySlug", mock.Anything, "NES", "ii", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "input slug too short (no secondary) - returns nil",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "wwe",
				CanonicalSlug:     "wwe",
			},
			slug:     "wwe", // Very short slug
			systemID: "NES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "NES", "wwe", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
				m.On("SearchMediaBySecondarySlug", mock.Anything, "NES", "wwe", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "no results from either search",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "nonexistent",
				CanonicalSlug:     "nonexistent",
			},
			slug:     "nonexistent",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "nonexistent", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
				m.On("SearchMediaBySecondarySlug", mock.Anything, "SNES", "nonexistent", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "exact search returns error - continues to partial",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "testgame",
				CanonicalSlug:     "testgame",
			},
			slug:     "testgame",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "testgame", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, errors.New("database error"))
				// Should still try partial search
				m.On("SearchMediaBySecondarySlug", mock.Anything, "SNES", "testgame", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "SNES", Name: "Some Game: Test Game", Path: "/test.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategySecondaryTitleExact,
			shouldError:      false,
		},
		{
			name: "partial search returns error - returns nil",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "errorgame",
				CanonicalSlug:     "errorgame",
			},
			slug:     "errorgame",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "errorgame", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
				m.On("SearchMediaBySecondarySlug", mock.Anything, "SNES", "errorgame", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, errors.New("database error"))
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false, // Errors are logged, not returned
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockDB := helpers.NewMockMediaDBI()
			tt.setupMock(mockDB)

			results, strategy, err := TrySecondaryTitleExact(
				context.Background(),
				mockDB,
				tt.systemID,
				tt.slug,
				tt.matchInfo,
				nil,
				"Game",
			)

			if tt.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, results, tt.expectedCount)
				assert.Equal(t, tt.expectedStrategy, strategy)
			}

			mockDB.AssertExpectations(t)
		})
	}
}

func TestTryAdvancedFuzzyMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock        func(*helpers.MockMediaDBI)
		name             string
		gameName         string
		slug             string
		systemID         string
		expectedStrategy string
		expectedCount    int
		shouldError      bool
	}{
		{
			name:             "slug too short - returns nil",
			gameName:         "Test",
			slug:             "test", // len=4, min is 5
			systemID:         "NES",
			setupMock:        func(_ *helpers.MockMediaDBI) {},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name:     "fuzzy match found with similar slug",
			gameName: "Super Mario World",
			slug:     "mariosuperworld", // Different order
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetTitlesWithPreFilter", mock.Anything, "SNES",
					mock.AnythingOfType("int"), mock.AnythingOfType("int"),
					mock.AnythingOfType("int"), mock.AnythingOfType("int")).
					Return([]database.MediaTitle{
						{Slug: "supermarioworld"},
					}, nil)
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "supermarioworld", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "SNES", Name: "Super Mario World", Path: "/smw.smc"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyJaroWinklerDamerau, // JW finds it before token sig
			shouldError:      false,
		},
		{
			name:     "jaro-winkler fuzzy match found",
			gameName: "Zelad", // Typo
			slug:     "zelad",
			systemID: "NES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetTitlesWithPreFilter", mock.Anything, "NES",
					mock.AnythingOfType("int"), mock.AnythingOfType("int"),
					mock.AnythingOfType("int"), mock.AnythingOfType("int")).
					Return([]database.MediaTitle{
						{Slug: "zelda"},
						{Slug: "zeldaii"},
					}, nil)
				m.On("SearchMediaBySlug", mock.Anything, "NES", "zelda", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "NES", Name: "Zelda", Path: "/zelda.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyJaroWinklerDamerau,
			shouldError:      false,
		},
		{
			name:     "no candidates after prefilter",
			gameName: "Nonexistent Game",
			slug:     "nonexistentgame",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetTitlesWithPreFilter", mock.Anything, "SNES",
					mock.AnythingOfType("int"), mock.AnythingOfType("int"),
					mock.AnythingOfType("int"), mock.AnythingOfType("int")).
					Return([]database.MediaTitle{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name:     "prefilter returns error",
			gameName: "Error Test",
			slug:     "errortest",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("GetTitlesWithPreFilter", mock.Anything, "SNES",
					mock.AnythingOfType("int"), mock.AnythingOfType("int"),
					mock.AnythingOfType("int"), mock.AnythingOfType("int")).
					Return([]database.MediaTitle{}, errors.New("database error"))
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false, // Errors logged, not returned
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockDB := helpers.NewMockMediaDBI()
			tt.setupMock(mockDB)

			result, err := TryAdvancedFuzzyMatching(
				context.Background(),
				mockDB,
				tt.systemID,
				tt.gameName,
				tt.slug,
				nil,
				"Game",
			)

			if tt.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, result.Results, tt.expectedCount)
				assert.Equal(t, tt.expectedStrategy, result.Strategy)
			}

			mockDB.AssertExpectations(t)
		})
	}
}

func TestTryProgressiveTrim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock        func(*helpers.MockMediaDBI)
		name             string
		gameName         string
		slug             string
		systemID         string
		expectedStrategy string
		expectedCount    int
		shouldError      bool
	}{
		{
			name:     "successful progressive trim match",
			gameName: "Super Mario World Special Edition",
			slug:     "supermarioworldspecialedition",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugIn", mock.Anything, "SNES",
					mock.AnythingOfType("[]string"), []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "SNES", Name: "Super Mario World", Path: "/smw.smc"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyProgressiveTrim,
			shouldError:      false,
		},
		{
			name:             "title too short - returns nil",
			gameName:         "Short",
			slug:             "short",
			systemID:         "NES",
			setupMock:        func(_ *helpers.MockMediaDBI) {},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name:             "no candidates generated",
			gameName:         "AB CD", // Only 2 words after processing
			slug:             "abcd",
			systemID:         "NES",
			setupMock:        func(_ *helpers.MockMediaDBI) {},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name:     "search returns no results",
			gameName: "Nonexistent Long Title Name",
			slug:     "nonexistentlongtitlename",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugIn", mock.Anything, "SNES",
					mock.AnythingOfType("[]string"), []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name:     "search returns error",
			gameName: "Error Test Long Name",
			slug:     "errortestlongname",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlugIn", mock.Anything, "SNES",
					mock.AnythingOfType("[]string"), []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, errors.New("database error"))
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false, // Errors logged, not returned
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockDB := helpers.NewMockMediaDBI()
			tt.setupMock(mockDB)

			results, strategy, err := TryProgressiveTrim(
				context.Background(),
				mockDB,
				tt.systemID,
				tt.gameName,
				tt.slug,
				nil,
				"Game",
			)

			if tt.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, results, tt.expectedCount)
				assert.Equal(t, tt.expectedStrategy, strategy)
			}

			mockDB.AssertExpectations(t)
		})
	}
}

func TestTryWithoutAutoTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock         func(*helpers.MockMediaDBI)
		name              string
		slug              string
		systemID          string
		expectedStrategy  string
		autoExtractedTags []database.TagFilter
		advArgsTags       []database.TagFilter
		expectedCount     int
		shouldError       bool
	}{
		{
			name:              "no auto-extracted tags - returns nil",
			slug:              "mario",
			systemID:          "NES",
			autoExtractedTags: []database.TagFilter{},
			advArgsTags:       []database.TagFilter{},
			setupMock:         func(_ *helpers.MockMediaDBI) {},
			expectedCount:     0,
			expectedStrategy:  "",
			shouldError:       false,
		},
		{
			name:     "exact match without auto tags succeeds",
			slug:     "mario",
			systemID: "NES",
			autoExtractedTags: []database.TagFilter{
				{Type: "region", Value: "us"},
			},
			advArgsTags: []database.TagFilter{},
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "NES", "mario", mock.Anything).
					Return([]database.SearchResultWithCursor{
						{SystemID: "NES", Name: "Mario", Path: "/mario.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyExactMatchNoAutoTags,
			shouldError:      false,
		},
		{
			name:     "prefix match without auto tags succeeds",
			slug:     "mario",
			systemID: "NES",
			autoExtractedTags: []database.TagFilter{
				{Type: "region", Value: "us"},
			},
			advArgsTags: []database.TagFilter{},
			setupMock: func(m *helpers.MockMediaDBI) {
				// Exact match fails
				m.On("SearchMediaBySlug", mock.Anything, "NES", "mario", mock.Anything).
					Return([]database.SearchResultWithCursor{}, nil)
				// Prefix match succeeds
				m.On("SearchMediaBySlugPrefix", mock.Anything, "NES", "mario", mock.Anything).
					Return([]database.SearchResultWithCursor{
						{SystemID: "NES", Name: "Mario Bros", Path: "/mariobros.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyPrefixMatchNoAutoTags,
			shouldError:      false,
		},
		{
			name:     "both searches fail - returns nil",
			slug:     "nonexistent",
			systemID: "NES",
			autoExtractedTags: []database.TagFilter{
				{Type: "region", Value: "us"},
			},
			advArgsTags: []database.TagFilter{},
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "NES", "nonexistent", mock.Anything).
					Return([]database.SearchResultWithCursor{}, nil)
				m.On("SearchMediaBySlugPrefix", mock.Anything, "NES", "nonexistent", mock.Anything).
					Return([]database.SearchResultWithCursor{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name:     "uses advArgs tags when provided",
			slug:     "mario",
			systemID: "NES",
			autoExtractedTags: []database.TagFilter{
				{Type: "region", Value: "us"},
			},
			advArgsTags: []database.TagFilter{
				{Type: "lang", Value: "en"},
			},
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "NES", "mario",
					[]database.TagFilter{{Type: "lang", Value: "en"}}).
					Return([]database.SearchResultWithCursor{
						{SystemID: "NES", Name: "Mario (English)", Path: "/mario.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyExactMatchNoAutoTags,
			shouldError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockDB := helpers.NewMockMediaDBI()
			tt.setupMock(mockDB)

			results, strategy, err := TryWithoutAutoTags(
				context.Background(),
				mockDB,
				tt.systemID,
				tt.slug,
				tt.autoExtractedTags,
				tt.advArgsTags,
			)

			if tt.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, results, tt.expectedCount)
				assert.Equal(t, tt.expectedStrategy, strategy)
			}

			mockDB.AssertExpectations(t)
		})
	}
}
