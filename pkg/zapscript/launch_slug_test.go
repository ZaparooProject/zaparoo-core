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

func TestCmdSlug(t *testing.T) {
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
			name:           "asterisk in middle - valid (Q*bert)",
			input:          "atari2600/Q*bert",
			expectedSystem: "Atari2600",
			expectedSlug:   "qbert",
			shouldError:    false,
		},
		{
			name:        "suffix wildcard - invalid",
			input:       "snes/game*",
			shouldError: true,
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
				Name:    "launch.slug",
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
				mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			}

			result, err := cmdSlug(mockPlatform, env)

			if tt.shouldError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid slug format")
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

func TestCmdSlugWithTags(t *testing.T) {
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
		{Type: "region", Value: "usa"},
		{Type: "type", Value: "game"},
	}

	cmd := parser.Command{
		Name:    "launch.slug",
		Args:    []string{input},
		AdvArgs: map[string]string{"tags": "region:usa,type:game"},
	}

	env := platforms.CmdEnv{
		Playlist: mockPlaylistController,
		Cfg:      mockConfig,
		Database: db,
		Cmd:      cmd,
	}

	expectedResults := []database.SearchResultWithCursor{
		{
			SystemID: expectedSystem,
			Name:     "Super Mario World (USA)",
			Path:     "/test/path/super-mario-world.smc",
		},
	}
	mockMediaDB.On("SearchMediaBySlug", context.Background(), expectedSystem, expectedSlug, expectedTags).
		Return(expectedResults, nil)
	mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := cmdSlug(mockPlatform, env)

	require.NoError(t, err)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
	assert.Equal(t, platforms.CmdResult{MediaChanged: true}, result)
}

func TestCmdSlugWithSubtitleFallback(t *testing.T) {
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
				Name:    "launch.slug",
				Args:    []string{tt.input},
				AdvArgs: map[string]string{},
			}

			env := platforms.CmdEnv{
				Playlist: mockPlaylistController,
				Cfg:      mockConfig,
				Database: db,
				Cmd:      cmd,
			}

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
					}
				}
			}

			if !tt.shouldError {
				mockPlatform.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			}

			result, err := cmdSlug(mockPlatform, env)

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

func TestCmdSlugEdgeCases(t *testing.T) {
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
			errorMsg:    "invalid slug format",
		},
		{
			name:        "only game no system",
			args:        []string{"/mario"},
			shouldError: true,
			errorMsg:    "invalid slug format",
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
				Name:    "launch.slug",
				Args:    tt.args,
				AdvArgs: map[string]string{},
			}

			env := platforms.CmdEnv{
				Playlist: mockPlaylistController,
				Cfg:      mockConfig,
				Database: db,
				Cmd:      cmd,
			}

			_, err := cmdSlug(mockPlatform, env)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

func TestMightBeSlug(t *testing.T) {
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
			name:     "asterisk in middle of title - IS a slug (Q*bert)",
			input:    "atari2600/Q*bert",
			expected: true,
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
			result := mightBeSlug(tt.input)
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
