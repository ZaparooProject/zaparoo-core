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

package mediascanner

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestAddMediaPath_NonUniqueError tests that non-UNIQUE errors fail immediately
// without attempting to find existing system
func TestAddMediaPath_NonUniqueError(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := &helpers.MockMediaDBI{}

	// Create fresh scan state
	scanState := &database.ScanState{
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagIDs:        make(map[string]int),
		TagTypeIDs:    make(map[string]int),
		SystemsIndex:  0,
		TitlesIndex:   0,
		MediaIndex:    0,
		TagsIndex:     0,
		TagTypesIndex: 0,
	}

	// Create a non-UNIQUE database error (like connection error)
	connectionError := assert.AnError // This will be different from UNIQUE constraint

	// Mock the system insertion to fail with non-UNIQUE error
	mockDB.On("InsertSystem", database.System{
		DBID:     int64(1),
		SystemID: "TV",
		Name:     "TV",
	}).Return(database.System{}, connectionError).Once()

	// FindSystem should NOT be called for non-UNIQUE errors
	// This is the key difference - we should fail fast, not try recovery

	// Mock additional methods that might be called
	mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()

	// Call AddMediaPath with a TV show path
	titleIndex, mediaIndex, err := AddMediaPath(mockDB, scanState, "TV", "kodi-show://1/Loki", false, false, nil)

	// Function should return error and (0, 0) for non-recoverable errors
	require.Error(t, err, "should return error for non-recoverable database errors")
	assert.Contains(t, err.Error(), "error inserting system", "error should indicate insert failure")
	assert.Equal(t, 0, titleIndex, "titleIndex should be 0 for non-recoverable database errors")
	assert.Equal(t, 0, mediaIndex, "mediaIndex should be 0 for non-recoverable database errors")

	// SystemIDs cache should still be empty
	assert.Empty(t, scanState.SystemIDs, "SystemIDs cache should be empty when non-recoverable error occurs")

	// Verify all mocks were called as expected (FindSystem should NOT have been called)
	mockDB.AssertExpectations(t)
}

func TestGetTitleFromFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "strips parentheses",
			filename: "Super Mario Bros (USA)",
			want:     "Super Mario Bros",
		},
		{
			name:     "strips square brackets",
			filename: "Legend of Zelda [!]",
			want:     "Legend of Zelda",
		},
		{
			name:     "strips braces",
			filename: "Game Title {Europe}",
			want:     "Game Title",
		},
		{
			name:     "strips angle brackets",
			filename: "Sonic <Beta>",
			want:     "Sonic",
		},
		{
			name:     "strips all bracket types",
			filename: "Final Fantasy (USA)[!]{En}<Proto>",
			want:     "Final Fantasy",
		},
		{
			name:     "handles mixed metadata",
			filename: "Metal Gear Solid (Disc 1 of 2) (USA) [!]",
			want:     "Metal Gear Solid",
		},
		{
			name:     "no brackets returns full name",
			filename: "Plain Game Name",
			want:     "Plain Game Name",
		},
		{
			name:     "trims whitespace",
			filename: "  Spaced Name  (USA)",
			want:     "Spaced Name",
		},
		{
			name:     "preserves dashes and colons in title",
			filename: "Legend of Zelda: Link's Awakening - DX (USA)",
			want:     "Legend of Zelda: Link's Awakening - DX",
		},
		{
			name:     "handles brace before other brackets",
			filename: "Game {Proto} (USA) [!]",
			want:     "Game",
		},
		{
			name:     "handles angle before other brackets",
			filename: "Game <Alpha> (USA) [!]",
			want:     "Game",
		},
		{
			name:     "preserves leading number with period (no stripping)",
			filename: "01. Super Mario Bros (USA)",
			want:     "01. Super Mario Bros",
		},
		{
			name:     "preserves leading number with dash (no stripping)",
			filename: "42 - Answer (USA)",
			want:     "42 - Answer",
		},
		{
			name:     "preserves leading number with space (no stripping)",
			filename: "1 Game Title (USA)",
			want:     "1 Game Title",
		},
		{
			name:     "underscores not converted when space present",
			filename: "Super_Mario_Bros (USA)",
			want:     "Super_Mario_Bros", // Has space before (USA), so underscores NOT converted
		},
		{
			name:     "underscores not converted when space present mixed",
			filename: "Mega_Man_X (USA)",
			want:     "Mega_Man_X", // Has space before (USA), so underscores NOT converted
		},
		{
			name:     "preserves ampersand",
			filename: "Sonic & Knuckles (USA)",
			want:     "Sonic & Knuckles",
		},
		{
			name:     "normalizes multiple spaces",
			filename: "Game   Title   Here (USA)",
			want:     "Game Title Here",
		},
		{
			name:     "handles all transformations combined (no number stripping)",
			filename: "01. Super_Mario_Bros & Luigi   (USA)",
			want:     "01. Super_Mario_Bros & Luigi", // Has spaces, so underscores NOT converted
		},
		{
			name:     "preserves dashes in title after cleanup",
			filename: "Zelda - Link's Awakening (USA)",
			want:     "Zelda - Link's Awakening",
		},
		{
			name:     "preserves colons in title after cleanup",
			filename: "Game: The Subtitle (USA)",
			want:     "Game: The Subtitle",
		},
		{
			name:     "converts underscores without brackets",
			filename: "Super_Mario_World",
			want:     "Super Mario World",
		},
		{
			name:     "preserves leading number without brackets (no stripping)",
			filename: "01. Game Title",
			want:     "01. Game Title",
		},
		{
			name:     "preserves ampersand without brackets",
			filename: "Rock & Roll Racing",
			want:     "Rock & Roll Racing",
		},
		// Separator normalization with minimum count heuristic
		{
			name:     "two_dashes_converts_to_spaces",
			filename: "super-mario-bros",
			want:     "super mario bros",
		},
		{
			name:     "two_underscores_converts_to_spaces",
			filename: "legend_of_zelda",
			want:     "legend of zelda",
		},
		{
			name:     "mixed_separators_two_total",
			filename: "mega-man_x",
			want:     "mega man x",
		},
		{
			name:     "one_dash_preserved",
			filename: "Spider-Man",
			want:     "Spider-Man", // Only 1 separator, not converted
		},
		{
			name:     "one_underscore_preserved",
			filename: "F_Zero",
			want:     "F_Zero", // Only 1 separator, not converted
		},
		{
			name:     "has_spaces_no_conversion",
			filename: "Super Mario-Bros",
			want:     "Super Mario-Bros", // Has spaces, separators ignored
		},
		{
			name:     "three_dashes_converts",
			filename: "mega-man-x-4",
			want:     "mega man x 4",
		},
		{
			name:     "many_underscores_converts",
			filename: "the_legend_of_zelda_a_link_to_the_past",
			want:     "the legend of zelda a link to the past",
		},
		{
			name:     "with_metadata_and_separators",
			filename: "super-mario-bros (USA)",
			want:     "super-mario-bros", // Has space before (USA), so dashes NOT converted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pass false for stripLeadingNumbers - we removed unconditional stripping
			got := tags.ParseTitleFromFilename(tt.filename, false)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestAddMediaPath_PopulatesSlugMetadata is a regression test for the critical bug where
// SlugLength and SlugWordCount were not being populated during media scanning.
// This caused fuzzy matching to fail because the prefilter would return 0 candidates.
//
// Bug context: Integration tests passed because they manually created MediaTitle records
// with correct metadata, but production scanning via AddMediaPath() was broken.
func TestAddMediaPath_PopulatesSlugMetadata(t *testing.T) {
	t.Parallel()

	// Create mock database
	mockDB := &helpers.MockMediaDBI{}

	// Create fresh scan state
	scanState := &database.ScanState{
		SystemIDs:     make(map[string]int),
		TitleIDs:      make(map[string]int),
		MediaIDs:      make(map[string]int),
		TagIDs:        make(map[string]int),
		TagTypeIDs:    make(map[string]int),
		SystemsIndex:  0,
		TitlesIndex:   0,
		MediaIndex:    0,
		TagsIndex:     0,
		TagTypesIndex: 0,
	}

	// Test cases covering various title formats
	tests := []struct {
		name              string
		path              string // Full file path (gameName will be extracted from this)
		expectedSlug      string
		expectedLength    int
		expectedWordCount int
	}{
		{
			name:              "single word",
			path:              "/roms/snes/Earthbound.sfc",
			expectedSlug:      "earthbound",
			expectedLength:    10,
			expectedWordCount: 1,
		},
		{
			name:              "two words",
			path:              "/roms/snes/Super Mario.sfc",
			expectedSlug:      "supermario",
			expectedLength:    10,
			expectedWordCount: 2,
		},
		{
			name:              "three words",
			path:              "/roms/snes/Legend of Zelda.sfc",
			expectedSlug:      "legendofzelda",
			expectedLength:    13,
			expectedWordCount: 3,
		},
		{
			name:              "with metadata in parens",
			path:              "/roms/snes/Final Fantasy (USA).sfc",
			expectedSlug:      "finalfantasy",
			expectedLength:    12,
			expectedWordCount: 2,
		},
		{
			name:              "with secondary title",
			path:              "/roms/snes/Zelda - Link's Awakening.sfc",
			expectedSlug:      "zeldalinksawakening",
			expectedLength:    19,
			expectedWordCount: 3, // "zelda" + "link's" + "awakening" (apostrophe preserved)
		},
		{
			name:              "with ampersand",
			path:              "/roms/snes/Sonic & Knuckles.sfc",
			expectedSlug:      "sonicandknuckles",
			expectedLength:    16,
			expectedWordCount: 3,
		},
		{
			name:              "very short",
			path:              "/roms/snes/Q.sfc",
			expectedSlug:      "q",
			expectedLength:    1,
			expectedWordCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset scan state for each test
			scanState.SystemsIndex = 0
			scanState.TitlesIndex = 0
			scanState.MediaIndex = 0
			scanState.SystemIDs = make(map[string]int)
			scanState.TitleIDs = make(map[string]int)
			scanState.MediaIDs = make(map[string]int)

			// Mock system insertion
			mockDB.On("InsertSystem", mock.AnythingOfType("database.System")).
				Return(database.System{DBID: int64(1), SystemID: "SNES", Name: "SNES"}, nil).Once()

			// Mock tag type and tag lookups (for filename tag extraction)
			mockDB.On("FindTagType", mock.AnythingOfType("database.TagType")).
				Return(database.TagType{DBID: 1, Type: "extension"}, nil).Maybe()
			mockDB.On("FindTag", mock.AnythingOfType("database.Tag")).
				Return(database.Tag{DBID: 1}, nil).Maybe()
			mockDB.On("InsertTag", mock.AnythingOfType("database.Tag")).
				Return(database.Tag{DBID: 1}, nil).Maybe()
			mockDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).
				Return(database.MediaTag{DBID: 1}, nil).Maybe()

			// CRITICAL: Mock title insertion and capture the actual title to verify metadata
			var capturedTitle database.MediaTitle
			mockDB.On("InsertMediaTitle", mock.AnythingOfType("*database.MediaTitle")).
				Run(func(args mock.Arguments) {
					// Capture the actual title being inserted
					title, ok := args.Get(0).(*database.MediaTitle)
					if ok {
						capturedTitle = *title
					}
				}).
				Return(database.MediaTitle{DBID: int64(1)}, nil).Once()

			// Mock media insertion
			mockDB.On("InsertMedia", mock.AnythingOfType("database.Media")).
				Return(database.Media{DBID: int64(1)}, nil).Once()

			// Mock tag parsing (no tags for this test)
			mockDB.On("GetTotalMediaCount").Return(0, nil).Maybe()

			// Call AddMediaPath
			titleIndex, mediaIndex, err := AddMediaPath(
				mockDB,
				scanState,
				"SNES",
				tt.path,
				false, // skipExisting
				false, // noExt
				nil,   // extra tags
			)

			// Verify no errors
			require.NoError(t, err)
			assert.Equal(t, 1, titleIndex)
			assert.Equal(t, 1, mediaIndex)

			// CRITICAL ASSERTIONS: Verify slug metadata was populated correctly
			assert.Equal(t, tt.expectedSlug, capturedTitle.Slug,
				"Slug should match expected value for %s", tt.path)
			assert.Equal(t, tt.expectedLength, capturedTitle.SlugLength,
				"SlugLength MUST be populated (not 0) for fuzzy matching prefilter to work")
			assert.Equal(t, tt.expectedWordCount, capturedTitle.SlugWordCount,
				"SlugWordCount MUST be populated (not 0) for fuzzy matching prefilter to work")
			assert.NotEqual(t, 0, capturedTitle.SlugLength,
				"CRITICAL BUG CHECK: SlugLength was 0 - fuzzy matching prefilter will fail!")
			assert.NotEqual(t, 0, capturedTitle.SlugWordCount,
				"CRITICAL BUG CHECK: SlugWordCount was 0 - fuzzy matching prefilter will fail!")

			// Verify mocks
			mockDB.AssertExpectations(t)
		})
	}
}

// TestGetPathFragments_VirtualPathsWithEncoding tests that GetPathFragments correctly
// processes virtual paths with URL encoding, ensuring:
// 1. Path is preserved as-is (with encoding)
// 2. FileName is decoded for display
// 3. Title is decoded and cleaned
// 4. Slug is generated from decoded name
// 5. Extension is empty for virtual paths
func TestGetPathFragments_VirtualPathsWithEncoding(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		path         string
		description  string
		expectedFrag MediaPathFragments
		noExt        bool
	}{
		{
			name:  "kodi_movie_with_spaces",
			path:  "kodi-movie://123/The%20Matrix",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "kodi-movie://123/The%20Matrix",
				FileName: "The Matrix", // Decoded
				Title:    "The Matrix", // Decoded and cleaned
				Slug:     "matrix",     // Slugified strips leading "The"
				Ext:      "",           // Virtual paths have no extension
			},
			description: "Movie with URL-encoded spaces",
		},
		{
			name:  "kodi_movie_with_parens",
			path:  "kodi-movie://456/The%20Matrix%20%28Reloaded%29",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "kodi-movie://456/The%20Matrix%20%28Reloaded%29",
				FileName: "The Matrix (Reloaded)",
				Title:    "The Matrix", // Parens stripped by title parser
				Slug:     "matrix",     // Slugified strips leading "The"
				Ext:      "",
			},
			description: "Movie with encoded spaces and parentheses",
		},
		{
			name:  "kodi_show_with_encoded_slash",
			path:  "kodi-show://789/Hot%2FCold",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "kodi-show://789/Hot%2FCold",
				FileName: "Hot/Cold", // %2F decoded to /
				Title:    "Hot/Cold",
				Slug:     "hotcold", // Slash removed in slugification
				Ext:      "",
			},
			description: "Show with encoded forward slash (critical test)",
		},
		{
			name:  "steam_with_brackets",
			path:  "steam://111/Game%20%5BDLC%5D%20Edition",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "steam://111/Game%20%5BDLC%5D%20Edition",
				FileName: "Game [DLC] Edition",
				Title:    "Game Edition", // Brackets stripped
				Slug:     "game",         // "Edition" suffix stripped by slugify
				Ext:      "",
			},
			description: "Steam game with encoded brackets",
		},
		{
			name:  "scummvm_with_colon",
			path:  "scummvm://monkey1/Monkey%20Island%3A%20Special%20Edition",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "scummvm://monkey1/Monkey%20Island%3A%20Special%20Edition",
				FileName: "Monkey Island: Special Edition",
				Title:    "Monkey Island: Special Edition", // Colon preserved in title
				Slug:     "monkeyislandspecial",            // "Edition" suffix stripped
				Ext:      "",
			},
			description: "ScummVM with encoded colon",
		},
		{
			name:  "flashpoint_with_ampersand",
			path:  "flashpoint://flash1/Tom%20%26%20Jerry",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "flashpoint://flash1/Tom%20%26%20Jerry",
				FileName: "Tom & Jerry",
				Title:    "Tom & Jerry",
				Slug:     "tomandjerry", // & becomes 'and' in slug
				Ext:      "",
			},
			description: "Flashpoint with encoded ampersand",
		},
		{
			name:  "http_url_with_spaces",
			path:  "http://server.com/My%20Game.zip",
			noExt: false,
			expectedFrag: MediaPathFragments{
				Path:     "http://server.com/My%20Game.zip",
				FileName: "My Game.zip", // Includes extension from HTTP URL
				Title:    "My Game",     // Extension stripped by ParseTitleFromFilename
				Slug:     "mygame",      // Extension not included in slug
				Ext:      ".zip",        // HTTP/HTTPS URLs have extension extracted for tags
			},
			description: "HTTP URL with encoded spaces",
		},
		{
			name:  "https_nested_path",
			path:  "https://cdn.example.com/games/Super%20Mario.sfc",
			noExt: false,
			expectedFrag: MediaPathFragments{
				Path:     "https://cdn.example.com/games/Super%20Mario.sfc",
				FileName: "Super Mario.sfc", // Includes extension from HTTP URL
				Title:    "Super Mario",     // Extension stripped by ParseTitleFromFilename
				Slug:     "supermario",      // Extension not included in slug
				Ext:      ".sfc",            // HTTP/HTTPS URLs have extension extracted for tags
			},
			description: "HTTPS URL with nested path",
		},
		{
			name:  "kodi_episode_complex",
			path:  "kodi-episode://222/S01E01%20-%20The%20Beginning%20%28Part%201%29",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "kodi-episode://222/S01E01%20-%20The%20Beginning%20%28Part%201%29",
				FileName: "S01E01 - The Beginning (Part 1)",
				Title:    "- The Beginning", // S01E01 stripped by ParseTitleFromFilename (scene release parsing)
				Slug:     "thebeginning",    // "The" article stripped, S01E01 removed
				Ext:      "",
			},
			description: "Episode with complex naming",
		},
		{
			name:  "launchbox_simple",
			path:  "launchbox://lb123/SimpleGame",
			noExt: true,
			expectedFrag: MediaPathFragments{
				Path:     "launchbox://lb123/SimpleGame",
				FileName: "SimpleGame",
				Title:    "SimpleGame",
				Slug:     "simplegame",
				Ext:      "",
			},
			description: "LaunchBox with no special characters",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := GetPathFragments(PathFragmentParams{
				Config:              nil,
				Path:                tc.path,
				NoExt:               tc.noExt,
				StripLeadingNumbers: false,
				SystemID:            "",
			})

			assert.Equal(t, tc.expectedFrag.Path, result.Path,
				"Path should be preserved as-is (with encoding): %s", tc.description)
			assert.Equal(t, tc.expectedFrag.FileName, result.FileName,
				"FileName should be decoded: %s", tc.description)
			assert.Equal(t, tc.expectedFrag.Title, result.Title,
				"Title should be decoded and cleaned: %s", tc.description)
			assert.Equal(t, tc.expectedFrag.Slug, result.Slug,
				"Slug should be generated from decoded name: %s", tc.description)
			assert.Equal(t, tc.expectedFrag.Ext, result.Ext,
				"Extension should be empty for virtual paths: %s", tc.description)

			// Verify no percent encoding in decoded fields
			assert.NotContains(t, result.FileName, "%",
				"FileName should not contain percent encoding")
			assert.NotContains(t, result.Title, "%",
				"Title should not contain percent encoding")
			assert.NotContains(t, result.Slug, "%",
				"Slug should not contain percent encoding")

			t.Logf("✓ %s: %s → title=%q, slug=%q",
				tc.name, tc.path, result.Title, result.Slug)
		})
	}
}

// TestGetPathFragments_MalformedVirtualPaths tests graceful handling of malformed virtual paths
func TestGetPathFragments_MalformedVirtualPaths(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		path        string
		description string
		noExt       bool
		shouldPanic bool
	}{
		{
			name:        "incomplete_percent_encoding",
			path:        "kodi-movie://123/Game%",
			noExt:       true,
			shouldPanic: false,
			description: "Should handle incomplete percent encoding gracefully",
		},
		{
			name:        "invalid_percent_hex",
			path:        "steam://456/Game%ZZ",
			noExt:       true,
			shouldPanic: false,
			description: "Should handle invalid hex in percent encoding",
		},
		{
			name:        "double_encoding",
			path:        "kodi-show://789/Name%2520Here", // %2520 = encoded %20
			noExt:       true,
			shouldPanic: false,
			description: "Should handle double encoding (decodes once)",
		},
		{
			name:        "mixed_case_scheme",
			path:        "Kodi-Movie://111/Title",
			noExt:       true,
			shouldPanic: false,
			description: "Should handle case-insensitive schemes",
		},
		{
			name:        "empty_name_section",
			path:        "kodi-movie://123/",
			noExt:       true,
			shouldPanic: false,
			description: "Should handle empty name section",
		},
		{
			name:        "no_name_section",
			path:        "kodi-episode://456",
			noExt:       true,
			shouldPanic: false,
			description: "Should handle missing name section",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.shouldPanic {
				assert.Panics(t, func() {
					GetPathFragments(PathFragmentParams{
						Config:              nil,
						Path:                tc.path,
						NoExt:               tc.noExt,
						StripLeadingNumbers: false,
						SystemID:            "",
					})
				}, tc.description)
			} else {
				assert.NotPanics(t, func() {
					result := GetPathFragments(PathFragmentParams{
						Config:              nil,
						Path:                tc.path,
						NoExt:               tc.noExt,
						StripLeadingNumbers: false,
						SystemID:            "",
					})
					t.Logf("Handled malformed path gracefully: %s → fileName=%q",
						tc.path, result.FileName)
				}, tc.description)
			}
		})
	}
}

// TestGetPathFragments_VirtualPathsVsRegularPaths compares behavior between
// virtual paths and regular file paths to ensure consistent handling
func TestGetPathFragments_VirtualPathsVsRegularPaths(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                string
		virtualPath         string
		regularPath         string
		description         string
		expectDifferentSlug bool
	}{
		{
			name:                "simple_name",
			virtualPath:         "kodi-movie://123/SimpleGame",
			regularPath:         "/roms/snes/SimpleGame.sfc",
			expectDifferentSlug: false,
			description:         "Simple names should produce same slug",
		},
		{
			name:                "spaces_in_name",
			virtualPath:         "steam://456/Super%20Mario",
			regularPath:         "/roms/snes/Super Mario.sfc",
			expectDifferentSlug: false,
			description:         "Spaces should produce same slug after decoding",
		},
		{
			name:                "metadata_in_name",
			virtualPath:         "kodi-movie://789/Game%20%28USA%29",
			regularPath:         "/roms/snes/Game (USA).sfc",
			expectDifferentSlug: false,
			description:         "Metadata cleanup should produce same slug",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			virtualResult := GetPathFragments(PathFragmentParams{
				Config:              nil,
				Path:                tc.virtualPath,
				NoExt:               true,
				StripLeadingNumbers: false,
				SystemID:            "",
			})
			regularResult := GetPathFragments(PathFragmentParams{
				Config:              nil,
				Path:                tc.regularPath,
				NoExt:               false,
				StripLeadingNumbers: false,
				SystemID:            "",
			})

			if tc.expectDifferentSlug {
				assert.NotEqual(t, virtualResult.Slug, regularResult.Slug,
					"%s: slugs should differ", tc.description)
			} else {
				assert.Equal(t, virtualResult.Slug, regularResult.Slug,
					"%s: slugs should match after decoding/normalization", tc.description)
			}

			// Virtual paths should never have extensions
			assert.Empty(t, virtualResult.Ext,
				"Virtual paths should have no extension")

			t.Logf("✓ %s: virtual_slug=%q, regular_slug=%q",
				tc.name, virtualResult.Slug, regularResult.Slug)
		})
	}
}
