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
			name: "successful main title search",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "zelda",
				SecondaryTitleSlug: "ocarinaoftime",
				CanonicalSlug:      "zeldaocarinaoftime",
			},
			slug:     "zeldaocarinaoftime",
			systemID: "Nintendo64",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "Nintendo64", "zelda", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{
						{SystemID: "Nintendo64", Name: "Zelda", Path: "/zelda.rom"},
					}, nil)
			},
			expectedCount:    1,
			expectedStrategy: StrategyMainTitleOnly,
			shouldError:      false,
		},
		{
			name: "no secondary title - returns nil",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "mario",
				CanonicalSlug:     "mario",
			},
			slug:             "mario",
			systemID:         "NES",
			setupMock:        func(_ *helpers.MockMediaDBI) {},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "main title slug same as original - returns nil",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "zelda",
				SecondaryTitleSlug: "",
				CanonicalSlug:      "zelda",
			},
			slug:             "zelda",
			systemID:         "NES",
			setupMock:        func(_ *helpers.MockMediaDBI) {},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "search returns no results",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "nonexistent",
				SecondaryTitleSlug: "game",
				CanonicalSlug:      "nonexistentgame",
			},
			slug:     "nonexistentgame",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "nonexistent", []database.TagFilter(nil)).
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
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "error", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, errors.New("database error"))
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      true,
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
			name: "successful secondary title search",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "zelda",
				SecondaryTitleSlug: "ocarinaoftime",
				CanonicalSlug:      "zeldaocarinaoftime",
			},
			slug:     "zeldaocarinaoftime",
			systemID: "Nintendo64",
			setupMock: func(m *helpers.MockMediaDBI) {
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
			name: "no secondary title - returns nil",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle: false,
				MainTitleSlug:     "mario",
				CanonicalSlug:     "mario",
			},
			slug:             "mario",
			systemID:         "NES",
			setupMock:        func(_ *helpers.MockMediaDBI) {},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "secondary slug too short - returns nil",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "game",
				SecondaryTitleSlug: "ii", // Only 2 chars, min is 4
				CanonicalSlug:      "gameii",
			},
			slug:             "gameii",
			systemID:         "NES",
			setupMock:        func(_ *helpers.MockMediaDBI) {},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "search returns no results - no error",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "game",
				SecondaryTitleSlug: "nonexist",
				CanonicalSlug:      "gamenonexist",
			},
			slug:     "gamenonexist",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "nonexist", []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil)
			},
			expectedCount:    0,
			expectedStrategy: "",
			shouldError:      false,
		},
		{
			name: "search returns error - logged but no error returned",
			matchInfo: GameMatchInfo{
				HasSecondaryTitle:  true,
				MainTitleSlug:      "game",
				SecondaryTitleSlug: "error",
				CanonicalSlug:      "gameerror",
			},
			slug:     "gameerror",
			systemID: "SNES",
			setupMock: func(m *helpers.MockMediaDBI) {
				m.On("SearchMediaBySlug", mock.Anything, "SNES", "error", []database.TagFilter(nil)).
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
