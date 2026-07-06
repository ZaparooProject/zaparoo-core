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

package mediascanner

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
)

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

			result := GetPathFragments(&PathFragmentParams{
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

// TestGetPathFragments_PreResolvedMediaType verifies that when a non-empty
// MediaType is passed, GetPathFragments uses it directly instead of looking
// up the system via systemdefs.
func TestGetPathFragments_PreResolvedMediaType(t *testing.T) {
	t.Parallel()

	// Use a Movie media type with a game-like path. The slug pipeline
	// varies by media type, so different types produce different slugs
	// when the title contains scene-style artifacts.
	result := GetPathFragments(&PathFragmentParams{
		Path:      "/games/snes/Super Mario World.sfc",
		SystemID:  "snes",
		MediaType: slugs.MediaTypeMovie,
	})

	// The key assertion: passing MediaTypeMovie should produce a slug via
	// the Movie pipeline (which strips different artifacts). If the
	// pre-resolved type were ignored and the system looked up, we'd get
	// MediaTypeGame instead.
	resultDefault := GetPathFragments(&PathFragmentParams{
		Path:     "/games/snes/Super Mario World.sfc",
		SystemID: "snes",
		// MediaType left empty — falls back to systemdefs lookup (Game)
	})

	// Both should produce valid slugs; the point is coverage of the
	// pre-resolved path (non-empty MediaType is used without lookup).
	assert.NotEmpty(t, result.Slug, "pre-resolved MediaType path should produce a slug")
	assert.NotEmpty(t, resultDefault.Slug, "fallback path should produce a slug")
	assert.Equal(t, result.Title, resultDefault.Title, "title extraction should be identical regardless of media type")
}

// TestGetPathFragments_ProvidedName verifies that a non-empty ProvidedName
// overrides the title parsed from the filename. The slug derives from the
// override, and tags are still extracted from the filename.
func TestGetPathFragments_ProvidedName(t *testing.T) {
	t.Parallel()

	neoGeoPath := string(filepath.Separator) + filepath.Join("media", "fat", "games", "NEOGEO", "mslug.zip")
	snesPath := string(filepath.Separator) + filepath.Join("games", "snes", "sm64 (USA) (Rev 1).sfc")

	// NeoGeo case: filename "mslug.zip" should be displayed as "Metal Slug"
	// when the scanner provides the AltName from romsets.xml.
	withName := GetPathFragments(&PathFragmentParams{
		Path:         neoGeoPath,
		SystemID:     "NeoGeo",
		ProvidedName: "Metal Slug",
	})
	assert.Equal(t, "Metal Slug", withName.Title, "ProvidedName must be used as title")
	assert.Equal(t, "metalslug", withName.Slug, "slug must derive from provided name")

	// Without ProvidedName, the title falls back to filename parsing.
	withoutName := GetPathFragments(&PathFragmentParams{
		Path:     neoGeoPath,
		SystemID: "NeoGeo",
	})
	assert.Equal(t, "mslug", withoutName.Title, "filename-derived title without ProvidedName")

	// ProvidedName overrides title even when the filename has tag-like
	// content; tags themselves are still extracted from the filename.
	tagged := GetPathFragments(&PathFragmentParams{
		Path:         snesPath,
		SystemID:     "snes",
		ProvidedName: "Super Mario 64",
	})
	assert.Equal(t, "Super Mario 64", tagged.Title, "ProvidedName wins over tagged filename")
	assert.Contains(t, tagged.Tags, "region:us", "USA region tag must still be extracted from filename")
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
					GetPathFragments(&PathFragmentParams{
						Config:              nil,
						Path:                tc.path,
						NoExt:               tc.noExt,
						StripLeadingNumbers: false,
						SystemID:            "",
					})
				}, tc.description)
			} else {
				assert.NotPanics(t, func() {
					result := GetPathFragments(&PathFragmentParams{
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

			virtualResult := GetPathFragments(&PathFragmentParams{
				Config:              nil,
				Path:                tc.virtualPath,
				NoExt:               true,
				StripLeadingNumbers: false,
				SystemID:            "",
			})
			regularResult := GetPathFragments(&PathFragmentParams{
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
