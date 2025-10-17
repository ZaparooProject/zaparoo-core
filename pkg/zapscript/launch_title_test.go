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

package zapscript

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCmdTitle(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedSystem string
		expectedSlug   string
		shouldError    bool
	}{
		{
			name:           "valid slug format",
			input:          "snes/Super Mario World",
			expectedSystem: "SNES",
			expectedSlug:   "supermarioworld",
			shouldError:    false,
		},
		{
			name:           "slug with special characters",
			input:          "genesis/Sonic & Knuckles",
			expectedSystem: "Genesis",
			expectedSlug:   "sonicandknuckles",
			shouldError:    false,
		},
		{
			name:           "slug with subtitle",
			input:          "n64/The Legend of Zelda: Ocarina of Time",
			expectedSystem: "Nintendo64",
			expectedSlug:   "legendofzeldaocarinaoftime",
			shouldError:    false,
		},
		{
			name:        "invalid format - no slash",
			input:       "Super Mario World",
			shouldError: true,
		},
		{
			name:           "multiple slashes in game title - valid (WCW/nWo)",
			input:          "ps1/WCW/nWo Thunder",
			expectedSystem: "PSX",
			expectedSlug:   "wcwnwothunder",
			shouldError:    false,
		},
		{
			name:           "has extension - passes slug format validation",
			input:          "snes/game.smc",
			expectedSystem: "SNES",
			expectedSlug:   "gamesmc",
			shouldError:    false,
		},
		{
			name:           "suffix wildcard - passes validation but no results",
			input:          "snes/game*",
			expectedSystem: "SNES",
			expectedSlug:   "game",
			shouldError:    true, // Will error when no results found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := helpers.NewMockMediaDBI()
			mockPlatform := mocks.NewMockPlatform()
			mockPlaylistController := playlists.PlaylistController{}
			mockConfig := &config.Instance{}

			db := &database.Database{
				MediaDB: mockMediaDB,
			}

			cmd := parser.Command{
				Name:    "launch.title",
				Args:    []string{tt.input},
				AdvArgs: map[string]string{},
			}

			env := platforms.CmdEnv{
				Playlist: mockPlaylistController,
				Cfg:      mockConfig,
				Database: db,
				Cmd:      cmd,
			}

			if !tt.shouldError {
				// Mock cache miss
				mockMediaDB.On("GetCachedSlugResolution",
					mock.Anything, tt.expectedSystem, tt.expectedSlug, []database.TagFilter(nil)).
					Return(int64(0), "", false)

				expectedResults := []database.SearchResultWithCursor{
					{
						SystemID: tt.expectedSystem,
						Name:     "Test Game",
						Path:     "/test/path",
					},
				}
				mockMediaDB.On("SearchMediaBySlug",
					context.Background(), tt.expectedSystem, tt.expectedSlug, []database.TagFilter(nil)).
					Return(expectedResults, nil)
				mockMediaDB.On("SetCachedSlugResolution",
					mock.Anything, tt.expectedSystem, tt.expectedSlug, []database.TagFilter(nil),
					mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
					Return(nil).Maybe()
				mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			} else if tt.expectedSystem != "" && tt.expectedSlug != "" {
				// shouldError but validation passes - set up mocks to return no results
				mockMediaDB.On("GetCachedSlugResolution",
					mock.Anything, tt.expectedSystem, tt.expectedSlug, []database.TagFilter(nil)).
					Return(int64(0), "", false)
				mockMediaDB.On("SearchMediaBySlug",
					mock.Anything, tt.expectedSystem, mock.AnythingOfType("string"), mock.Anything).
					Return([]database.SearchResultWithCursor{}, nil).Maybe()
				mockMediaDB.On("SearchMediaBySlugPrefix",
					mock.Anything, tt.expectedSystem, mock.AnythingOfType("string"), mock.Anything).
					Return([]database.SearchResultWithCursor{}, nil).Maybe()
			}

			result, err := cmdTitle(mockPlatform, env)

			if tt.shouldError {
				require.Error(t, err)
				if tt.expectedSystem == "" || tt.expectedSlug == "" {
					assert.Contains(t, err.Error(), "invalid title format")
				} else {
					assert.Contains(t, err.Error(), "no results found")
				}
			} else {
				require.NoError(t, err)
				mockMediaDB.AssertExpectations(t)
				if !tt.shouldError {
					mockPlatform.AssertExpectations(t)
				}
				assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
			}
		})
	}
}

func TestExtractCanonicalTagsFromParens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		input             string
		expectedRemaining string
		expectedTags      []database.TagFilter
	}{
		{
			name:              "no canonical tags",
			input:             "Super Mario (USA) (1996)",
			expectedTags:      nil,
			expectedRemaining: "Super Mario (USA) (1996)",
		},
		{
			name:  "single canonical tag with AND operator",
			input: "Game (region:us)",
			expectedTags: []database.TagFilter{
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
			},
			expectedRemaining: "Game",
		},
		{
			name:  "single canonical tag with NOT operator",
			input: "Game (-unfinished:beta)",
			expectedTags: []database.TagFilter{
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
			},
			expectedRemaining: "Game",
		},
		{
			name:  "single canonical tag with OR operator",
			input: "Game (~lang:en)",
			expectedTags: []database.TagFilter{
				{Type: "lang", Value: "en", Operator: database.TagOperatorOR},
			},
			expectedRemaining: "Game",
		},
		{
			name:  "multiple canonical tags with operators",
			input: "Game (-unfinished:beta) (+region:us) (~lang:en)",
			expectedTags: []database.TagFilter{
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
				{Type: "region", Value: "us", Operator: database.TagOperatorAND},
				{Type: "lang", Value: "en", Operator: database.TagOperatorOR},
			},
			expectedRemaining: "Game",
		},
		{
			name:  "canonical tags mixed with filename metadata",
			input: "Game (-unfinished:beta) (USA) (year:1996)",
			expectedTags: []database.TagFilter{
				{Type: "unfinished", Value: "beta", Operator: database.TagOperatorNOT},
				{Type: "year", Value: "1996", Operator: database.TagOperatorAND},
			},
			expectedRemaining: "Game (USA)",
		},
		{
			name:  "canonical tag without operator defaults to AND",
			input: "Game (year:1996)",
			expectedTags: []database.TagFilter{
				{Type: "year", Value: "1996", Operator: database.TagOperatorAND},
			},
			expectedRemaining: "Game",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tagFilters, remaining := extractCanonicalTagsFromParens(tt.input)

			assert.Equal(t, tt.expectedRemaining, remaining, "remaining string mismatch")
			assert.Len(t, tagFilters, len(tt.expectedTags), "number of extracted tags mismatch")

			if len(tt.expectedTags) > 0 {
				for i, expectedTag := range tt.expectedTags {
					assert.Equal(t, expectedTag.Type, tagFilters[i].Type, "tag type mismatch at index %d", i)
					assert.Equal(t, expectedTag.Value, tagFilters[i].Value, "tag value mismatch at index %d", i)
					assert.Equal(t, expectedTag.Operator, tagFilters[i].Operator,
						"tag operator mismatch at index %d", i)
				}
			}
		})
	}
}

func TestCmdTitleWithTags(t *testing.T) {
	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlaylistController := playlists.PlaylistController{}
	mockConfig := &config.Instance{}

	db := &database.Database{
		MediaDB: mockMediaDB,
	}

	input := "snes/Super Mario World"
	expectedSystem := "SNES"
	expectedSlug := "supermarioworld"
	expectedTags := []database.TagFilter{
		{Type: "region", Value: "usa", Operator: database.TagOperatorAND},
		{Type: "type", Value: "game", Operator: database.TagOperatorAND},
	}

	cmd := parser.Command{
		Name:    "launch.title",
		Args:    []string{input},
		AdvArgs: map[string]string{"tags": "region:usa,type:game"},
	}

	env := platforms.CmdEnv{
		Playlist: mockPlaylistController,
		Cfg:      mockConfig,
		Database: db,
		Cmd:      cmd,
	}

	// Mock cache miss
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, expectedSystem, expectedSlug, expectedTags).
		Return(int64(0), "", false)

	expectedResults := []database.SearchResultWithCursor{
		{
			SystemID: expectedSystem,
			Name:     "Super Mario World (USA)",
			Path:     "/test/path/super-mario-world.smc",
		},
	}
	mockMediaDB.On("SearchMediaBySlug", context.Background(), expectedSystem, expectedSlug, expectedTags).
		Return(expectedResults, nil)
	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, expectedSystem, expectedSlug, expectedTags,
		mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
		Return(nil).Maybe()
	mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := cmdTitle(mockPlatform, env)

	require.NoError(t, err)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
	assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
}

func TestCmdTitleWithSubtitleFallback(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		systemID           string
		initialSearchSlug  string
		fallbackSearchSlug string
		initialResults     []database.SearchResultWithCursor
		fallbackResults    []database.SearchResultWithCursor
		expectFallback     bool
		shouldError        bool
	}{
		{
			name:               "subtitle fallback triggers when no initial results",
			input:              "snes/zelda: ocarina",
			systemID:           "SNES",
			initialSearchSlug:  "zeldaocarina",
			fallbackSearchSlug: "zelda",
			initialResults:     []database.SearchResultWithCursor{},
			fallbackResults: []database.SearchResultWithCursor{
				{SystemID: "snes", Name: "The Legend of Zelda", Path: "/test/zelda.smc"},
			},
			expectFallback: true,
			shouldError:    false,
		},
		{
			name:               "subtitle fallback with colon separator",
			input:              "genesis/sonic: the hedgehog",
			systemID:           "Genesis",
			initialSearchSlug:  "sonichedgehog",
			fallbackSearchSlug: "sonic",
			initialResults:     []database.SearchResultWithCursor{},
			fallbackResults: []database.SearchResultWithCursor{
				{SystemID: "genesis", Name: "Sonic", Path: "/test/sonic.bin"},
			},
			expectFallback: true,
			shouldError:    false,
		},
		{
			name:               "subtitle fallback with dash separator",
			input:              "ps1/final fantasy - 7",
			systemID:           "PSX",
			initialSearchSlug:  "finalfantasy7",
			fallbackSearchSlug: "finalfantasy",
			initialResults:     []database.SearchResultWithCursor{},
			fallbackResults: []database.SearchResultWithCursor{
				{SystemID: "ps1", Name: "Final Fantasy VII", Path: "/test/ff7.bin"},
			},
			expectFallback: true,
			shouldError:    false,
		},
		{
			name:              "no fallback when initial search succeeds",
			input:             "n64/mario 64",
			systemID:          "Nintendo64",
			initialSearchSlug: "mario64",
			initialResults: []database.SearchResultWithCursor{
				{SystemID: "n64", Name: "Super Mario 64", Path: "/test/mario64.z64"},
			},
			expectFallback: false,
			shouldError:    false,
		},
		{
			name:               "error when both searches fail",
			input:              "snes/nonexistent: game",
			systemID:           "SNES",
			initialSearchSlug:  "nonexistentgame",
			fallbackSearchSlug: "nonexistent",
			initialResults:     []database.SearchResultWithCursor{},
			fallbackResults:    []database.SearchResultWithCursor{},
			expectFallback:     true,
			shouldError:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := helpers.NewMockMediaDBI()
			mockPlatform := mocks.NewMockPlatform()
			mockPlaylistController := playlists.PlaylistController{}
			mockConfig := &config.Instance{}

			db := &database.Database{
				MediaDB: mockMediaDB,
			}

			cmd := parser.Command{
				Name:    "launch.title",
				Args:    []string{tt.input},
				AdvArgs: map[string]string{},
			}

			env := platforms.CmdEnv{
				Playlist: mockPlaylistController,
				Cfg:      mockConfig,
				Database: db,
				Cmd:      cmd,
			}

			// Mock cache miss
			mockMediaDB.On("GetCachedSlugResolution",
				mock.Anything, tt.systemID, tt.initialSearchSlug, []database.TagFilter(nil)).
				Return(int64(0), "", false)

			mockMediaDB.On("SearchMediaBySlug",
				context.Background(), tt.systemID, tt.initialSearchSlug, []database.TagFilter(nil)).
				Return(tt.initialResults, nil).Once()

			if len(tt.initialResults) == 0 {
				mockMediaDB.On("SearchMediaBySlugPrefix",
					context.Background(), tt.systemID, tt.initialSearchSlug, []database.TagFilter(nil)).
					Return([]database.SearchResultWithCursor{}, nil).Once()

				if tt.expectFallback {
					mockMediaDB.On("SearchMediaBySlug",
						context.Background(), tt.systemID, tt.fallbackSearchSlug, []database.TagFilter(nil)).
						Return(tt.fallbackResults, nil).Once()

					if len(tt.fallbackResults) == 0 {
						mockMediaDB.On("SearchMediaBySlug",
							mock.Anything, tt.systemID, mock.AnythingOfType("string"), mock.Anything).
							Return([]database.SearchResultWithCursor{}, nil).Maybe()
						mockMediaDB.On("SearchMediaBySlugPrefix",
							mock.Anything, tt.systemID, mock.AnythingOfType("string"), mock.Anything).
							Return([]database.SearchResultWithCursor{}, nil).Maybe()
						mockMediaDB.On("GetTitlesWithPreFilter",
							mock.Anything, tt.systemID, mock.AnythingOfType("int"), mock.AnythingOfType("int"),
							mock.AnythingOfType("int"), mock.AnythingOfType("int")).
							Return([]database.MediaTitle{}, nil).Maybe()
					}
				}
			}

			if !tt.shouldError {
				mockMediaDB.On("SetCachedSlugResolution",
					mock.Anything, tt.systemID, mock.AnythingOfType("string"), []database.TagFilter(nil),
					mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
					Return(nil).Maybe()
				mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			}

			result, err := cmdTitle(mockPlatform, env)

			if tt.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
				mockPlatform.AssertExpectations(t)
			}

			mockMediaDB.AssertExpectations(t)
		})
	}
}

func TestCmdTitleTokenMatching(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		systemID       string
		slug           string
		expectedMatch  string
		prefixResults  []database.SearchResultWithCursor
		shouldUseToken bool
	}{
		{
			name:     "word order variation - awakening link matches links awakening",
			input:    "gbc/awakening link",
			systemID: "GameboyColor",
			slug:     "awakeninglink",
			prefixResults: []database.SearchResultWithCursor{
				{
					SystemID: "GameboyColor",
					Name:     "The Legend of Zelda: Link's Awakening DX",
					Path:     "/test/zelda-dx.gbc",
				},
			},
			expectedMatch:  "The Legend of Zelda: Link's Awakening DX",
			shouldUseToken: true,
		},
		{
			name:     "reversed words - mario super matches super mario",
			input:    "snes/mario super world",
			systemID: "SNES",
			slug:     "mariosuperworld",
			prefixResults: []database.SearchResultWithCursor{
				{SystemID: "SNES", Name: "Super Mario World", Path: "/test/smw.smc"},
			},
			expectedMatch:  "Super Mario World",
			shouldUseToken: true,
		},
		{
			name:     "partial word order - turtles ninja matches ninja turtles",
			input:    "nes/turtles ninja",
			systemID: "NES",
			slug:     "turtlesninja",
			prefixResults: []database.SearchResultWithCursor{
				{SystemID: "NES", Name: "Teenage Mutant Ninja Turtles", Path: "/test/tmnt.nes"},
			},
			expectedMatch:  "Teenage Mutant Ninja Turtles",
			shouldUseToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := helpers.NewMockMediaDBI()
			mockPlatform := mocks.NewMockPlatform()
			mockPlaylistController := playlists.PlaylistController{}
			mockConfig := &config.Instance{}

			db := &database.Database{
				MediaDB: mockMediaDB,
			}

			cmd := parser.Command{
				Name:    "launch.title",
				Args:    []string{tt.input},
				AdvArgs: map[string]string{},
			}

			env := platforms.CmdEnv{
				Playlist: mockPlaylistController,
				Cfg:      mockConfig,
				Database: db,
				Cmd:      cmd,
			}

			// Mock cache miss
			mockMediaDB.On("GetCachedSlugResolution",
				mock.Anything, tt.systemID, tt.slug, []database.TagFilter(nil)).
				Return(int64(0), "", false)

			mockMediaDB.On("SearchMediaBySlug",
				context.Background(), tt.systemID, tt.slug, []database.TagFilter(nil)).
				Return([]database.SearchResultWithCursor{}, nil).Once()

			mockMediaDB.On("SearchMediaBySlugPrefix",
				context.Background(), tt.systemID, tt.slug, []database.TagFilter(nil)).
				Return(tt.prefixResults, nil).Once()

			mockMediaDB.On("SetCachedSlugResolution",
				mock.Anything, tt.systemID, tt.slug, []database.TagFilter(nil),
				mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
				Return(nil).Maybe()
			mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			result, err := cmdTitle(mockPlatform, env)

			require.NoError(t, err)
			assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
			mockMediaDB.AssertExpectations(t)
			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestCmdTitleJaroWinklerFuzzy(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		systemID      string
		slug          string
		expectedMatch string
		allSlugs      []string
	}{
		{
			name:          "typo - missing character (zelad -> zelda)",
			input:         "nes/zelad",
			systemID:      "NES",
			slug:          "zelad",
			allSlugs:      []string{"zelda", "mario", "sonic"},
			expectedMatch: "zelda",
		},
		{
			name:          "typo - wrong character (sanic -> sonic)",
			input:         "genesis/sanic",
			systemID:      "Genesis",
			slug:          "sanic",
			allSlugs:      []string{"sonic", "sonicandknuckles", "streets"},
			expectedMatch: "sonic",
		},
		{
			name:          "typo - transposed characters (mraio -> mario)",
			input:         "snes/mraio",
			systemID:      "SNES",
			slug:          "mraio",
			allSlugs:      []string{"mario", "megaman", "metroid"},
			expectedMatch: "mario",
		},
		{
			name:          "spelling - british vs american (honour -> honor)",
			input:         "pc/honourguard",
			systemID:      "PC",
			slug:          "honourguard",
			allSlugs:      []string{"honorguard", "halflife", "halo"},
			expectedMatch: "honorguard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := helpers.NewMockMediaDBI()
			mockPlatform := mocks.NewMockPlatform()
			mockPlaylistController := playlists.PlaylistController{}
			mockConfig := &config.Instance{}

			db := &database.Database{
				MediaDB: mockMediaDB,
			}

			cmd := parser.Command{
				Name:    "launch.title",
				Args:    []string{tt.input},
				AdvArgs: map[string]string{},
			}

			env := platforms.CmdEnv{
				Playlist: mockPlaylistController,
				Cfg:      mockConfig,
				Database: db,
				Cmd:      cmd,
			}

			// Mock cache miss
			mockMediaDB.On("GetCachedSlugResolution",
				mock.Anything, tt.systemID, tt.slug, []database.TagFilter(nil)).
				Return(int64(0), "", false)

			// All earlier strategies fail
			mockMediaDB.On("SearchMediaBySlug",
				context.Background(), tt.systemID, tt.slug, []database.TagFilter(nil)).
				Return([]database.SearchResultWithCursor{}, nil).Once()
			mockMediaDB.On("SearchMediaBySlugPrefix",
				context.Background(), tt.systemID, tt.slug, []database.TagFilter(nil)).
				Return([]database.SearchResultWithCursor{}, nil).Once()

			// Fuzzy matching uses pre-filter to get candidate titles
			candidateTitles := make([]database.MediaTitle, len(tt.allSlugs))
			for i, slug := range tt.allSlugs {
				candidateTitles[i] = database.MediaTitle{
					Slug: slug,
				}
			}
			mockMediaDB.On("GetTitlesWithPreFilter",
				mock.Anything, tt.systemID, mock.AnythingOfType("int"), mock.AnythingOfType("int"),
				mock.AnythingOfType("int"), mock.AnythingOfType("int")).
				Return(candidateTitles, nil).Once()

			// Fuzzy match succeeds (MUST come before .Maybe() to take precedence)
			expectedResults := []database.SearchResultWithCursor{
				{
					SystemID: tt.systemID,
					Name:     tt.expectedMatch,
					Path:     "/test/" + tt.expectedMatch,
				},
			}
			mockMediaDB.On("SearchMediaBySlug",
				context.Background(), tt.systemID, tt.expectedMatch, []database.TagFilter(nil)).
				Return(expectedResults, nil).Once()

			// Secondary title searches also fail (no ':' or '-' in query)
			mockMediaDB.On("SearchMediaBySlug",
				mock.Anything, tt.systemID, mock.AnythingOfType("string"), mock.Anything).
				Return([]database.SearchResultWithCursor{}, nil).Maybe()
			mockMediaDB.On("SearchMediaBySlugPrefix",
				mock.Anything, tt.systemID, mock.AnythingOfType("string"), mock.Anything).
				Return([]database.SearchResultWithCursor{}, nil).Maybe()
			mockMediaDB.On("GetTitlesWithPreFilter",
				mock.Anything, tt.systemID, mock.AnythingOfType("int"), mock.AnythingOfType("int"),
				mock.AnythingOfType("int"), mock.AnythingOfType("int")).
				Return([]database.MediaTitle{}, nil).Maybe()

			mockMediaDB.On("SetCachedSlugResolution",
				mock.Anything, tt.systemID, mock.AnythingOfType("string"), []database.TagFilter(nil),
				mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
				Return(nil).Maybe()
			mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			result, err := cmdTitle(mockPlatform, env)

			require.NoError(t, err)
			assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
			mockMediaDB.AssertExpectations(t)
			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestCmdTitleEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		errorMsg    string
		args        []string
		shouldError bool
	}{
		{
			name:        "no arguments",
			args:        []string{},
			shouldError: true,
			errorMsg:    "invalid number of arguments",
		},
		{
			name:        "too many arguments",
			args:        []string{"snes/mario", "extra"},
			shouldError: true,
			errorMsg:    "invalid number of arguments",
		},
		{
			name:        "empty string argument",
			args:        []string{""},
			shouldError: true,
			errorMsg:    "required",
		},
		{
			name:        "only system no game",
			args:        []string{"snes/"},
			shouldError: true,
			errorMsg:    "invalid title format",
		},
		{
			name:        "only game no system",
			args:        []string{"/mario"},
			shouldError: true,
			errorMsg:    "invalid title format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := helpers.NewMockMediaDBI()
			mockPlatform := mocks.NewMockPlatform()
			mockPlaylistController := playlists.PlaylistController{}
			mockConfig := &config.Instance{}

			db := &database.Database{
				MediaDB: mockMediaDB,
			}

			cmd := parser.Command{
				Name:    "launch.title",
				Args:    tt.args,
				AdvArgs: map[string]string{},
			}

			env := platforms.CmdEnv{
				Playlist: mockPlaylistController,
				Cfg:      mockConfig,
				Database: db,
				Cmd:      cmd,
			}

			_, err := cmdTitle(mockPlatform, env)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

func TestMightBeTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid slug",
			input:    "snes/Super Mario World",
			expected: true,
		},
		{
			name:     "valid slug with special chars",
			input:    "genesis/Sonic & Knuckles",
			expected: true,
		},
		{
			name:     "no slash - not slug",
			input:    "Super Mario World",
			expected: false,
		},
		{
			name:     "multiple slashes in game title - IS a slug (WCW/nWo)",
			input:    "ps1/WCW/nWo Thunder",
			expected: true,
		},
		{
			name:     "has extension - might be slug (file check runs first)",
			input:    "snes/game.smc",
			expected: true,
		},
		{
			name:     "has suffix wildcard - not slug",
			input:    "snes/game*",
			expected: false,
		},
		{
			name:     "has prefix wildcard - not slug",
			input:    "snes/*game",
			expected: false,
		},
		{
			name:     "asterisk in middle of title - not slug (Q*bert)",
			input:    "atari2600/Q*bert",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "game with period in title - Ms. Pac-Man",
			input:    "arcade/Ms. Pac-Man",
			expected: true,
		},
		{
			name:     "game with Dr abbreviation",
			input:    "nes/Dr. Mario",
			expected: true,
		},
		{
			name:     "game with multiple periods",
			input:    "pc/S.T.A.L.K.E.R.",
			expected: true,
		},
		{
			name:     "game with period and subtitle",
			input:    "snes/Mega Man X2",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mightBeTitle(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSlugGeneration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple title",
			input:    "Super Mario World",
			expected: "supermarioworld",
		},
		{
			name:     "title with special characters",
			input:    "Sonic & Knuckles",
			expected: "sonicandknuckles",
		},
		{
			name:     "title with subtitle",
			input:    "The Legend of Zelda: Ocarina of Time",
			expected: "legendofzeldaocarinaoftime",
		},
		{
			name:     "title with leading article",
			input:    "The Simpsons",
			expected: "simpsons",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := slugs.SlugifyString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterByPreferredRegions(t *testing.T) {
	tests := []struct {
		name             string
		results          []database.SearchResultWithCursor
		preferredRegions []string
		expectedPaths    []string
		expectedCount    int
	}{
		{
			name: "no preferred regions returns all",
			results: []database.SearchResultWithCursor{
				{Tags: []database.TagInfo{{Type: "region", Tag: "us"}}},
				{Tags: []database.TagInfo{{Type: "region", Tag: "jp"}}},
			},
			preferredRegions: []string{},
			expectedCount:    2,
		},
		{
			name: "single preferred region filters correctly",
			results: []database.SearchResultWithCursor{
				{Tags: []database.TagInfo{{Type: "region", Tag: "us"}}},
				{Tags: []database.TagInfo{{Type: "region", Tag: "jp"}}},
				{Tags: []database.TagInfo{{Type: "region", Tag: "eu"}}},
			},
			preferredRegions: []string{"us"},
			expectedCount:    1,
		},
		{
			name: "multiple preferred regions",
			results: []database.SearchResultWithCursor{
				{Tags: []database.TagInfo{{Type: "region", Tag: "us"}}},
				{Tags: []database.TagInfo{{Type: "region", Tag: "jp"}}},
				{Tags: []database.TagInfo{{Type: "region", Tag: "world"}}},
			},
			preferredRegions: []string{"us", "world"},
			expectedCount:    2,
		},
		{
			name: "prefers tagged over untagged",
			results: []database.SearchResultWithCursor{
				{Path: "/untagged.rom", Tags: []database.TagInfo{{Type: "other", Tag: "test"}}},
				{Path: "/us.rom", Tags: []database.TagInfo{{Type: "region", Tag: "us"}}},
				{Path: "/jp.rom", Tags: []database.TagInfo{{Type: "region", Tag: "jp"}}},
			},
			preferredRegions: []string{"us"},
			expectedCount:    1,
			expectedPaths:    []string{"/us.rom"},
		},
		{
			name: "prefers untagged over wrong region",
			results: []database.SearchResultWithCursor{
				{Path: "/untagged.rom", Tags: []database.TagInfo{{Type: "other", Tag: "test"}}},
				{Path: "/jp.rom", Tags: []database.TagInfo{{Type: "region", Tag: "jp"}}},
				{Path: "/fr.rom", Tags: []database.TagInfo{{Type: "region", Tag: "fr"}}},
			},
			preferredRegions: []string{"us"},
			expectedCount:    1,
			expectedPaths:    []string{"/untagged.rom"},
		},
		{
			name: "returns all wrong regions if no matches",
			results: []database.SearchResultWithCursor{
				{Path: "/jp.rom", Tags: []database.TagInfo{{Type: "region", Tag: "jp"}}},
				{Path: "/fr.rom", Tags: []database.TagInfo{{Type: "region", Tag: "fr"}}},
			},
			preferredRegions: []string{"us"},
			expectedCount:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterByPreferredRegions(tt.results, tt.preferredRegions)
			assert.Len(t, filtered, tt.expectedCount)
			if len(tt.expectedPaths) > 0 {
				paths := make([]string, len(filtered))
				for i, r := range filtered {
					paths[i] = r.Path
				}
				assert.Equal(t, tt.expectedPaths, paths)
			}
		})
	}
}

func TestFilterByPreferredLanguages(t *testing.T) {
	tests := []struct {
		name           string
		results        []database.SearchResultWithCursor
		preferredLangs []string
		expectedPaths  []string
		expectedCount  int
	}{
		{
			name: "no preferred languages returns all",
			results: []database.SearchResultWithCursor{
				{Tags: []database.TagInfo{{Type: "lang", Tag: "en"}}},
				{Tags: []database.TagInfo{{Type: "lang", Tag: "ja"}}},
			},
			preferredLangs: []string{},
			expectedCount:  2,
		},
		{
			name: "single preferred language filters correctly",
			results: []database.SearchResultWithCursor{
				{Tags: []database.TagInfo{{Type: "lang", Tag: "en"}}},
				{Tags: []database.TagInfo{{Type: "lang", Tag: "ja"}}},
				{Tags: []database.TagInfo{{Type: "lang", Tag: "fr"}}},
			},
			preferredLangs: []string{"en"},
			expectedCount:  1,
		},
		{
			name: "multiple preferred languages",
			results: []database.SearchResultWithCursor{
				{Tags: []database.TagInfo{{Type: "lang", Tag: "en"}}},
				{Tags: []database.TagInfo{{Type: "lang", Tag: "ja"}}},
				{Tags: []database.TagInfo{{Type: "lang", Tag: "es"}}},
			},
			preferredLangs: []string{"en", "es"},
			expectedCount:  2,
		},
		{
			name: "prefers tagged over untagged",
			results: []database.SearchResultWithCursor{
				{Path: "/untagged.rom", Tags: []database.TagInfo{{Type: "other", Tag: "test"}}},
				{Path: "/en.rom", Tags: []database.TagInfo{{Type: "lang", Tag: "en"}}},
				{Path: "/de.rom", Tags: []database.TagInfo{{Type: "lang", Tag: "de"}}},
			},
			preferredLangs: []string{"en"},
			expectedCount:  1,
			expectedPaths:  []string{"/en.rom"},
		},
		{
			name: "prefers untagged over wrong language",
			results: []database.SearchResultWithCursor{
				{Path: "/untagged.rom", Tags: []database.TagInfo{{Type: "other", Tag: "test"}}},
				{Path: "/de.rom", Tags: []database.TagInfo{{Type: "lang", Tag: "de"}}},
				{Path: "/fr.rom", Tags: []database.TagInfo{{Type: "lang", Tag: "fr"}}},
			},
			preferredLangs: []string{"en"},
			expectedCount:  1,
			expectedPaths:  []string{"/untagged.rom"},
		},
		{
			name: "returns all wrong languages if no matches",
			results: []database.SearchResultWithCursor{
				{Path: "/de.rom", Tags: []database.TagInfo{{Type: "lang", Tag: "de"}}},
				{Path: "/fr.rom", Tags: []database.TagInfo{{Type: "lang", Tag: "fr"}}},
			},
			preferredLangs: []string{"en"},
			expectedCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterByPreferredLanguages(tt.results, tt.preferredLangs)
			assert.Len(t, filtered, tt.expectedCount)
			if len(tt.expectedPaths) > 0 {
				paths := make([]string, len(filtered))
				for i, r := range filtered {
					paths[i] = r.Path
				}
				assert.Equal(t, tt.expectedPaths, paths)
			}
		})
	}
}

func TestFilterOutVariants(t *testing.T) {
	tests := []struct {
		name          string
		description   string
		results       []database.SearchResultWithCursor
		expectedCount int
	}{
		{
			name: "filters out demos",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game Demo", Tags: []database.TagInfo{{Type: "unfinished", Tag: "demo"}}},
			},
			expectedCount: 1,
			description:   "should keep only main game",
		},
		{
			name: "filters out betas",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game Beta", Tags: []database.TagInfo{{Type: "unfinished", Tag: "beta"}}},
			},
			expectedCount: 1,
			description:   "should keep only main game",
		},
		{
			name: "filters out prototypes",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game Proto", Tags: []database.TagInfo{{Type: "unfinished", Tag: "proto"}}},
			},
			expectedCount: 1,
			description:   "should keep only main game",
		},
		{
			name: "filters out hacks",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game Hack", Tags: []database.TagInfo{{Type: "unlicensed", Tag: "hack"}}},
			},
			expectedCount: 1,
			description:   "should keep only main game",
		},
		{
			name: "filters out translations",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game (Translation)", Tags: []database.TagInfo{{Type: "unlicensed", Tag: "translation"}}},
			},
			expectedCount: 1,
			description:   "should keep only main game",
		},
		{
			name: "keeps all when no variants",
			results: []database.SearchResultWithCursor{
				{Name: "Game 1", Tags: []database.TagInfo{}},
				{Name: "Game 2", Tags: []database.TagInfo{}},
			},
			expectedCount: 2,
			description:   "should keep all games",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterOutVariants(tt.results)
			assert.Len(t, filtered, tt.expectedCount, tt.description)
		})
	}
}

func TestFilterOutRereleases(t *testing.T) {
	tests := []struct {
		name          string
		description   string
		results       []database.SearchResultWithCursor
		expectedCount int
	}{
		{
			name: "filters out rereleases",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game (Re-release)", Tags: []database.TagInfo{{Type: "rerelease", Tag: "true"}}},
			},
			expectedCount: 1,
			description:   "should keep only original",
		},
		{
			name: "filters out reboxed",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game (Reboxed)", Tags: []database.TagInfo{{Type: "reboxed", Tag: "true"}}},
			},
			expectedCount: 1,
			description:   "should keep only original",
		},
		{
			name: "keeps all originals",
			results: []database.SearchResultWithCursor{
				{Name: "Game 1", Tags: []database.TagInfo{}},
				{Name: "Game 2", Tags: []database.TagInfo{}},
			},
			expectedCount: 2,
			description:   "should keep all originals",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterOutRereleases(tt.results)
			assert.Len(t, filtered, tt.expectedCount, tt.description)
		})
	}
}

func TestSelectBestResult(t *testing.T) {
	tests := []struct {
		name         string
		expectedName string
		description  string
		results      []database.SearchResultWithCursor
		tagFilters   []database.TagFilter
	}{
		{
			name: "single result returns that result",
			results: []database.SearchResultWithCursor{
				{Name: "Game 1", Path: "/test/game1.rom"},
			},
			expectedName: "Game 1",
			description:  "should return single result",
		},
		{
			name: "prefers user-specified tag filters",
			results: []database.SearchResultWithCursor{
				{Name: "Game (USA)", Tags: []database.TagInfo{{Type: "region", Tag: "us"}}},
				{Name: "Game (Japan)", Tags: []database.TagInfo{{Type: "region", Tag: "jp"}}},
			},
			tagFilters:   []database.TagFilter{{Type: "region", Value: "jp"}},
			expectedName: "Game (Japan)",
			description:  "should prefer Japan region when specified",
		},
		{
			name: "prefers main game over demo",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game Demo", Tags: []database.TagInfo{{Type: "unfinished", Tag: "demo"}}},
			},
			expectedName: "Game",
			description:  "should filter out demo",
		},
		{
			name: "prefers original over rerelease",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Tags: []database.TagInfo{}},
				{Name: "Game (Re-release)", Tags: []database.TagInfo{{Type: "rerelease", Tag: "true"}}},
			},
			expectedName: "Game",
			description:  "should filter out rerelease",
		},
		{
			name: "alphabetical sorting as final tiebreaker",
			results: []database.SearchResultWithCursor{
				{Name: "Game", Path: "/test/z-game.rom"},
				{Name: "Game", Path: "/test/a-game.rom"},
			},
			expectedName: "Game",
			description:  "should pick alphabetically first filename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConfig := &config.Instance{}
			result := selectBestResult(tt.results, tt.tagFilters, mockConfig)
			assert.Equal(t, tt.expectedName, result.Name, tt.description)
		})
	}
}

func TestHasAllTags(t *testing.T) {
	tests := []struct {
		name       string
		tagFilters []database.TagFilter
		result     database.SearchResultWithCursor
		expected   bool
	}{
		{
			name: "has all required tags",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
					{Type: "lang", Tag: "en"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
			},
			expected: true,
		},
		{
			name: "missing one tag",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "us"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us"},
				{Type: "lang", Value: "en"},
			},
			expected: false,
		},
		{
			name: "wrong tag value",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{
					{Type: "region", Tag: "jp"},
				},
			},
			tagFilters: []database.TagFilter{
				{Type: "region", Value: "us"},
			},
			expected: false,
		},
		{
			name: "no filters means match all",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{},
			},
			tagFilters: []database.TagFilter{},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAllTags(&tt.result, tt.tagFilters)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsVariant(t *testing.T) {
	tests := []struct {
		name     string
		result   database.SearchResultWithCursor
		expected bool
	}{
		{
			name: "demo is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedDemo)}},
			},
			expected: true,
		},
		{
			name: "beta is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedBeta)}},
			},
			expected: true,
		},
		{
			name: "prototype is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedProto)}},
			},
			expected: true,
		},
		{
			name: "hack is variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedHack)}},
			},
			expected: true,
		},
		{
			name: "regular game is not variant",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVariant(&tt.result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRerelease(t *testing.T) {
	tests := []struct {
		name     string
		result   database.SearchResultWithCursor
		expected bool
	}{
		{
			name: "rerelease tag marks as rerelease",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeRerelease), Tag: "true"}},
			},
			expected: true,
		},
		{
			name: "reboxed tag marks as rerelease",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: string(tags.TagTypeReboxed), Tag: "true"}},
			},
			expected: true,
		},
		{
			name: "original game is not rerelease",
			result: database.SearchResultWithCursor{
				Tags: []database.TagInfo{{Type: "region", Tag: "us"}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRerelease(&tt.result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := &config.Instance{}

	regions := cfg.DefaultRegions()
	langs := cfg.DefaultLangs()

	assert.Equal(t, []string{"us", "world"}, regions)
	assert.Equal(t, []string{"en"}, langs)
}

// TestCmdTitleCacheBehavior tests cache hit/miss scenarios
func TestCmdTitleCacheBehavior(t *testing.T) {
	t.Run("Cache hit - should not search database", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		systemID := "SNES"
		slug := "supermarioworld"

		cmd := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/Super Mario World"},
			AdvArgs: map[string]string{},
		}

		env := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd,
		}

		// Mock cache hit - should return cached values
		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, systemID, slug, []database.TagFilter(nil)).
			Return(int64(123), "/cached/path.smc", true)

		// When cache hits, GetMediaByDBID is called to fetch full media details
		mockMediaDB.On("GetMediaByDBID", mock.Anything, int64(123)).
			Return(database.Media{Path: "/cached/path.smc"}, nil)

		// Platform launch should be called with cached path
		mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		// SearchMediaBySlug should NOT be called when cache hits
		result, err := cmdTitle(mockPlatform, env)

		require.NoError(t, err)
		assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
		mockMediaDB.AssertExpectations(t)
		mockPlatform.AssertExpectations(t)

		// Verify SearchMediaBySlug was NOT called
		mockMediaDB.AssertNotCalled(t, "SearchMediaBySlug")
	})

	t.Run("Cache miss - should search and update cache", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		systemID := "SNES"
		slug := "supermarioworld"

		cmd := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/Super Mario World"},
			AdvArgs: map[string]string{},
		}

		env := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd,
		}

		// Mock cache miss
		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, systemID, slug, []database.TagFilter(nil)).
			Return(int64(0), "", false)

		expectedResults := []database.SearchResultWithCursor{
			{
				MediaID:  123,
				SystemID: systemID,
				Name:     "Super Mario World",
				Path:     "/test/smw.smc",
			},
		}
		mockMediaDB.On("SearchMediaBySlug",
			context.Background(), systemID, slug, []database.TagFilter(nil)).
			Return(expectedResults, nil)

		// Should update cache after successful search
		mockMediaDB.On("SetCachedSlugResolution",
			mock.Anything, systemID, slug, []database.TagFilter(nil),
			int64(123), mock.AnythingOfType("string")). // strategy string
			Return(nil).Once()

		mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		result, err := cmdTitle(mockPlatform, env)

		require.NoError(t, err)
		assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
		mockMediaDB.AssertExpectations(t)
		mockPlatform.AssertExpectations(t)
	})

	t.Run("Cache with different tag filters", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		systemID := "SNES"
		slug := "supermarioworld"
		tags1 := []database.TagFilter{{Type: "region", Value: "usa", Operator: database.TagOperatorAND}}
		tags2 := []database.TagFilter{{Type: "region", Value: "jp", Operator: database.TagOperatorAND}}

		// First call with USA tag
		cmd1 := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/Super Mario World"},
			AdvArgs: map[string]string{"tags": "region:usa"},
		}

		env1 := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd1,
		}

		// Cache miss for USA version
		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, systemID, slug, tags1).
			Return(int64(0), "", false).Once()

		mockMediaDB.On("SearchMediaBySlug",
			context.Background(), systemID, slug, tags1).
			Return([]database.SearchResultWithCursor{
				{MediaID: 100, SystemID: systemID, Path: "/usa.smc"},
			}, nil).Once()

		mockMediaDB.On("SetCachedSlugResolution",
			mock.Anything, systemID, slug, tags1, int64(100), mock.AnythingOfType("string")).
			Return(nil).Once()

		mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		_, err := cmdTitle(mockPlatform, env1)
		require.NoError(t, err)

		// Second call with different tags should not use same cache
		cmd2 := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/Super Mario World"},
			AdvArgs: map[string]string{"tags": "region:jp"},
		}

		env2 := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd2,
		}

		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, systemID, slug, tags2).
			Return(int64(0), "", false).Once()

		mockMediaDB.On("SearchMediaBySlug",
			context.Background(), systemID, slug, tags2).
			Return([]database.SearchResultWithCursor{
				{MediaID: 200, SystemID: systemID, Path: "/jp.smc"},
			}, nil).Once()

		mockMediaDB.On("SetCachedSlugResolution",
			mock.Anything, systemID, slug, tags2, int64(200), mock.AnythingOfType("string")).
			Return(nil).Once()

		mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		_, err = cmdTitle(mockPlatform, env2)
		require.NoError(t, err)

		mockMediaDB.AssertExpectations(t)
		mockPlatform.AssertExpectations(t)
	})
}

// TestCmdTitleErrorHandling tests error scenarios
func TestCmdTitleErrorHandling(t *testing.T) {
	t.Run("Database search error", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		cmd := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/Super Mario World"},
			AdvArgs: map[string]string{},
		}

		env := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd,
		}

		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, "SNES", "supermarioworld", []database.TagFilter(nil)).
			Return(int64(0), "", false)

		mockMediaDB.On("SearchMediaBySlug",
			context.Background(), "SNES", "supermarioworld", []database.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{}, assert.AnError)

		_, err := cmdTitle(mockPlatform, env)

		require.Error(t, err)
		mockMediaDB.AssertExpectations(t)
	})

	t.Run("Platform launch error", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		cmd := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/Super Mario World"},
			AdvArgs: map[string]string{},
		}

		env := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd,
		}

		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, "SNES", "supermarioworld", []database.TagFilter(nil)).
			Return(int64(0), "", false)

		expectedResults := []database.SearchResultWithCursor{
			{MediaID: 123, SystemID: "SNES", Name: "Super Mario World", Path: "/test/smw.smc"},
		}
		mockMediaDB.On("SearchMediaBySlug",
			context.Background(), "SNES", "supermarioworld", []database.TagFilter(nil)).
			Return(expectedResults, nil)

		// Cache will be set before platform launch attempt
		mockMediaDB.On("SetCachedSlugResolution",
			mock.Anything, "SNES", "supermarioworld", []database.TagFilter(nil),
			int64(123), mock.AnythingOfType("string")).
			Return(nil).Maybe()

		// Platform launch fails
		mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).
			Return(assert.AnError)

		_, err := cmdTitle(mockPlatform, env)

		require.Error(t, err)
		mockMediaDB.AssertExpectations(t)
		mockPlatform.AssertExpectations(t)
	})

	t.Run("Invalid tag filter format", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		cmd := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/Super Mario World"},
			AdvArgs: map[string]string{"tags": "invalid_format"},
		}

		env := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd,
		}

		// Cache check happens before tag parsing
		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, "SNES", mock.AnythingOfType("string"), []database.TagFilter(nil)).
			Return(int64(0), "", false).Maybe()

		// Tag parsing fails but code continues with search anyway
		// Also subtitle fallback will kick in
		mockMediaDB.On("SearchMediaBySlug",
			mock.Anything, "SNES", mock.AnythingOfType("string"), []database.TagFilter(nil)).
			Return([]database.SearchResultWithCursor{}, nil).Maybe()
		mockMediaDB.On("SearchMediaBySlugPrefix",
			mock.Anything, "SNES", mock.AnythingOfType("string"), mock.Anything).
			Return([]database.SearchResultWithCursor{}, nil).Maybe()
		mockMediaDB.On("GetTitlesWithPreFilter",
			mock.Anything, "SNES", mock.AnythingOfType("int"), mock.AnythingOfType("int"),
			mock.AnythingOfType("int"), mock.AnythingOfType("int")).
			Return([]database.MediaTitle{}, nil).Maybe()

		_, err := cmdTitle(mockPlatform, env)

		// Tag parsing logs a warning but doesn't fail the command, so no error expected from invalid format
		// The command will fail because no results are found
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no results found")
	})

	t.Run("No results found", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		cmd := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/NonexistentGame12345"},
			AdvArgs: map[string]string{},
		}

		env := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd,
		}

		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, "SNES", "nonexistentgame12345", []database.TagFilter(nil)).
			Return(int64(0), "", false)

		// All search strategies return empty
		mockMediaDB.On("SearchMediaBySlug",
			mock.Anything, "SNES", mock.AnythingOfType("string"), mock.Anything).
			Return([]database.SearchResultWithCursor{}, nil).Maybe()

		mockMediaDB.On("SearchMediaBySlugPrefix",
			mock.Anything, "SNES", mock.AnythingOfType("string"), mock.Anything).
			Return([]database.SearchResultWithCursor{}, nil).Maybe()

		mockMediaDB.On("GetTitlesWithPreFilter",
			mock.Anything, "SNES", mock.AnythingOfType("int"), mock.AnythingOfType("int"),
			mock.AnythingOfType("int"), mock.AnythingOfType("int")).
			Return([]database.MediaTitle{}, nil).Maybe()

		_, err := cmdTitle(mockPlatform, env)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no results found")
		mockMediaDB.AssertExpectations(t)
	})
}

// TestCmdTitlePerformance tests performance-related scenarios
func TestCmdTitlePerformance(t *testing.T) {
	t.Run("Large result set filtering", func(t *testing.T) {
		mockMediaDB := helpers.NewMockMediaDBI()
		mockPlatform := mocks.NewMockPlatform()
		mockPlaylistController := playlists.PlaylistController{}
		mockConfig := &config.Instance{}

		db := &database.Database{
			MediaDB: mockMediaDB,
		}

		cmd := parser.Command{
			Name:    "launch.title",
			Args:    []string{"snes/mario"},
			AdvArgs: map[string]string{},
		}

		env := platforms.CmdEnv{
			Playlist: mockPlaylistController,
			Cfg:      mockConfig,
			Database: db,
			Cmd:      cmd,
		}

		// Create 100 results to test filtering performance
		results := make([]database.SearchResultWithCursor, 100)
		for i := range results {
			results[i] = database.SearchResultWithCursor{
				MediaID:  int64(i),
				SystemID: "SNES",
				Name:     "Mario Game",
				Path:     "/test/mario" + string(rune(i)) + ".smc",
				Tags: []database.TagInfo{
					{Type: "region", Tag: "usa"},
				},
			}
		}

		mockMediaDB.On("GetCachedSlugResolution",
			mock.Anything, "SNES", "mario", []database.TagFilter(nil)).
			Return(int64(0), "", false)

		mockMediaDB.On("SearchMediaBySlug",
			context.Background(), "SNES", "mario", []database.TagFilter(nil)).
			Return(results, nil)

		mockMediaDB.On("SetCachedSlugResolution",
			mock.Anything, "SNES", "mario", []database.TagFilter(nil),
			mock.AnythingOfType("int64"), mock.AnythingOfType("string")).
			Return(nil).Maybe()

		mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		result, err := cmdTitle(mockPlatform, env)

		require.NoError(t, err)
		assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
		mockMediaDB.AssertExpectations(t)
		mockPlatform.AssertExpectations(t)
	})
}
