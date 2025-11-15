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
	"context"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTempMediaDB creates a temporary MediaDB for testing
func setupTempMediaDB(t *testing.T) (db *mediadb.MediaDB, cleanup func()) {
	t.Helper()

	// Create temp directory for the test database
	tempDir, err := os.MkdirTemp("", "zaparoo-test-virtual-path-*")
	require.NoError(t, err)

	// Create a mock platform that returns our temp directory
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: tempDir,
	})

	// Open the database
	ctx := context.Background()
	db, err = mediadb.OpenMediaDB(ctx, mockPlatform)
	require.NoError(t, err)

	cleanup = func() {
		if db != nil {
			_ = db.Close()
		}
		_ = os.RemoveAll(tempDir)
	}

	return db, cleanup
}

// TestVirtualPath_EndToEndFlow tests the complete flow of virtual paths through the system:
// Scanner creates virtual path → Indexing pipeline processes → Database stores → Retrieval → Launcher parses
//
// This is a critical integration test that verifies URL encoding/decoding works correctly
// across the entire system, preventing launch failures due to encoding mismatches.
func TestVirtualPath_EndToEndFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	testCases := []struct {
		name             string
		scheme           string
		id               string
		displayName      string
		systemID         string
		expectedEncoded  string // What should be stored in DB (encoded)
		expectedDecoded  string // What should be displayed (decoded)
		expectedSlugPart string // Expected substring in slug
		hasSpecialChars  bool   // Whether path contains characters that need encoding
	}{
		{
			name:             "kodi_movie_with_spaces_and_parens",
			scheme:           shared.SchemeKodiMovie,
			id:               "123",
			displayName:      "The Matrix (Reloaded)",
			systemID:         "movie",
			expectedEncoded:  "%20", // Should contain encoded space
			expectedDecoded:  "The Matrix (Reloaded)",
			expectedSlugPart: "matrix",
			hasSpecialChars:  true,
		},
		{
			name:             "kodi_show_with_encoded_slash",
			scheme:           shared.SchemeKodiShow,
			id:               "456",
			displayName:      "Hot/Cold",
			systemID:         "tvshow",
			expectedEncoded:  "%2F", // Should contain encoded forward slash
			expectedDecoded:  "Hot/Cold",
			expectedSlugPart: "hotcold",
			hasSpecialChars:  true,
		},
		{
			name:             "steam_with_brackets",
			scheme:           shared.SchemeSteam,
			id:               "789",
			displayName:      "Game [DLC] Edition",
			systemID:         "pc",
			expectedEncoded:  "%5B", // Should contain encoded [
			expectedDecoded:  "Game [DLC] Edition",
			expectedSlugPart: "game",
			hasSpecialChars:  true,
		},
		{
			name:             "scummvm_with_colon",
			scheme:           shared.SchemeScummVM,
			id:               "monkey1",
			displayName:      "Monkey Island: Special Edition",
			systemID:         "scummvm",
			expectedEncoded:  "Island", // Colon may not be encoded by CreateVirtualPath
			expectedDecoded:  "Monkey Island: Special Edition",
			expectedSlugPart: "monkey",
			hasSpecialChars:  false, // Colon doesn't require encoding in path component
		},
		{
			name:             "flashpoint_with_ampersand",
			scheme:           shared.SchemeFlashpoint,
			id:               "flash123",
			displayName:      "Tom & Jerry",
			systemID:         "flashpoint",
			expectedEncoded:  "&", // Ampersand may not be encoded in path component
			expectedDecoded:  "Tom & Jerry",
			expectedSlugPart: "tom",
			hasSpecialChars:  false, // Ampersand is safe in path component
		},
		{
			name:             "launchbox_simple_name",
			scheme:           shared.SchemeLaunchBox,
			id:               "lb456",
			displayName:      "SimpleGame",
			systemID:         "arcade",
			expectedEncoded:  "SimpleGame", // No encoding needed
			expectedDecoded:  "SimpleGame",
			expectedSlugPart: "simplegame",
			hasSpecialChars:  false,
		},
		{
			name:             "kodi_episode_complex",
			scheme:           shared.SchemeKodiEpisode,
			id:               "999",
			displayName:      "S01E01 - The Beginning (Part 1)",
			systemID:         "tvshow",
			expectedEncoded:  "%20", // Should contain encoded space
			expectedDecoded:  "S01E01 - The Beginning (Part 1)",
			expectedSlugPart: "beginning",
			hasSpecialChars:  true,
		},
		{
			name:             "kodi_song_with_dash",
			scheme:           shared.SchemeKodiSong,
			id:               "111",
			displayName:      "Artist - Song Name",
			systemID:         "music",
			expectedEncoded:  "%20", // Should contain encoded space
			expectedDecoded:  "Artist - Song Name",
			expectedSlugPart: "artist",
			hasSpecialChars:  true,
		},
		{
			name:             "kodi_album_with_quotes",
			scheme:           shared.SchemeKodiAlbum,
			id:               "222",
			displayName:      `Album "Title" Here`,
			systemID:         "music",
			expectedEncoded:  "%22", // Should contain encoded quote
			expectedDecoded:  `Album "Title" Here`,
			expectedSlugPart: "album",
			hasSpecialChars:  true,
		},
		{
			name:             "kodi_artist_with_apostrophe",
			scheme:           shared.SchemeKodiArtist,
			id:               "333",
			displayName:      "Bob's Band",
			systemID:         "music",
			expectedEncoded:  "%27", // Should contain encoded apostrophe
			expectedDecoded:  "Bob's Band",
			expectedSlugPart: "bob",
			hasSpecialChars:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Setup separate database for each subtest
			db, cleanup := setupTempMediaDB(t)
			defer cleanup()

			// Step 1: Create virtual path (simulating scanner)
			virtualPath := virtualpath.CreateVirtualPath(tc.scheme, tc.id, tc.displayName)
			t.Logf("Created virtual path: %s", virtualPath)

			// Verify encoding happened if special chars present
			if tc.hasSpecialChars {
				assert.Contains(t, virtualPath, tc.expectedEncoded,
					"Virtual path should contain URL encoding for special characters")
			}

			// Step 2: Index path (simulating media scanner pipeline)
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

			// Begin transaction
			err := db.BeginTransaction(false)
			require.NoError(t, err)

			// Add media path
			titleIndex, mediaIndex, err := AddMediaPath(
				db,
				scanState,
				tc.systemID,
				virtualPath,
				false, // noExt
				false, // stripLeadingNumbers
				nil,   // cfg
			)
			require.NoError(t, err, "AddMediaPath should succeed")
			assert.Positive(t, titleIndex, "Title index should be assigned")
			assert.Positive(t, mediaIndex, "Media index should be assigned")

			// Commit transaction
			err = db.CommitTransaction()
			require.NoError(t, err)

			t.Logf("Indexed: titleIndex=%d, mediaIndex=%d", titleIndex, mediaIndex)

			// Step 3: Retrieve from database (simulating launcher retrieval)
			media, err := db.FindMedia(database.Media{DBID: int64(mediaIndex)})
			require.NoError(t, err, "Should be able to retrieve media by ID")

			// Verify path is stored correctly (still encoded)
			assert.Equal(t, virtualPath, media.Path,
				"Path in database should match original virtual path (with encoding)")

			if tc.hasSpecialChars {
				assert.Contains(t, media.Path, tc.expectedEncoded,
					"Database path should preserve URL encoding")
			}

			t.Logf("Retrieved path from DB: %s", media.Path)

			// Step 4: Extract ID (simulating launcher)
			extractedID, err := virtualpath.ExtractSchemeID(media.Path, tc.scheme)
			require.NoError(t, err, "ExtractSchemeID should succeed")
			assert.Equal(t, tc.id, extractedID,
				"Extracted ID should match original ID regardless of encoding in name")

			t.Logf("Extracted ID: %s", extractedID)

			// Step 5: Verify PathInfo decoding (for display purposes)
			pathInfo := helpers.GetPathInfo(media.Path)
			assert.Equal(t, tc.displayName, pathInfo.Name,
				"PathInfo.Name should be decoded for display")
			assert.Empty(t, pathInfo.Extension,
				"Virtual paths should have no extension")

			t.Logf("PathInfo.Name (decoded): %s", pathInfo.Name)

			// Step 6: Verify slugification uses decoded name
			slug := helpers.SlugifyPath(media.Path)
			assert.NotContains(t, slug, "%",
				"Slug should not contain percent encoding")
			assert.Contains(t, slug, tc.expectedSlugPart,
				"Slug should contain expected part from decoded name")

			t.Logf("Slug: %s", slug)

			// Step 7: Verify title metadata was populated correctly
			title, err := db.FindMediaTitle(&database.MediaTitle{DBID: int64(titleIndex)})
			require.NoError(t, err, "Should be able to retrieve title")

			assert.NotEmpty(t, title.Slug, "Title slug should be populated")
			assert.Positive(t, title.SlugLength,
				"SlugLength should be populated for fuzzy matching")
			assert.Positive(t, title.SlugWordCount,
				"SlugWordCount should be populated for fuzzy matching")

			t.Logf("Title slug: %s (length=%d, words=%d)",
				title.Slug, title.SlugLength, title.SlugWordCount)

			// Step 8: Round-trip verification - create path again and compare
			recreatedPath := virtualpath.CreateVirtualPath(tc.scheme, tc.id, tc.displayName)
			assert.Equal(t, virtualPath, recreatedPath,
				"Re-creating the same virtual path should produce identical encoding")
		})
	}
}

// TestVirtualPath_MalformedGracefulHandling tests that malformed virtual paths
// don't crash the system and are handled gracefully during indexing and retrieval
func TestVirtualPath_MalformedGracefulHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	testCases := []struct {
		name             string
		virtualPath      string
		systemID         string
		expectedBehavior string
		shouldIndex      bool
	}{
		{
			name:             "incomplete_percent_encoding",
			virtualPath:      "kodi-movie://123/Game%",
			systemID:         "movie",
			shouldIndex:      true, // Should succeed, fallback to undecoded
			expectedBehavior: "Fallback to undecoded path",
		},
		{
			name:             "missing_id_section",
			virtualPath:      "kodi-movie:///NameOnly",
			systemID:         "movie",
			shouldIndex:      true, // Should succeed with empty ID
			expectedBehavior: "Accept empty ID section",
		},
		{
			name:             "double_percent_encoding",
			virtualPath:      "steam://456/Game%2520Name", // %2520 = encoded %20
			systemID:         "pc",
			shouldIndex:      true,
			expectedBehavior: "Store as-is, decode once on retrieval",
		},
		{
			name:             "mixed_case_scheme",
			virtualPath:      "Kodi-Movie://789/Title",
			systemID:         "movie",
			shouldIndex:      true,
			expectedBehavior: "Case-insensitive scheme handling",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Setup separate database for each subtest
			db, cleanup := setupTempMediaDB(t)
			defer cleanup()

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

			err := db.BeginTransaction(false)
			require.NoError(t, err)

			_, mediaIndex, err := AddMediaPath(
				db,
				scanState,
				tc.systemID,
				tc.virtualPath,
				false,
				false,
				nil,
			)

			if tc.shouldIndex {
				require.NoError(t, err, "Should handle malformed path gracefully: %s", tc.expectedBehavior)
				assert.Positive(t, mediaIndex, "Should assign media index")

				err = db.CommitTransaction()
				require.NoError(t, err)

				// Verify we can retrieve it
				retrievedMedia, errFind := db.FindMedia(database.Media{DBID: int64(mediaIndex)})
				require.NoError(t, errFind)
				assert.Equal(t, tc.virtualPath, retrievedMedia.Path,
					"Path should be stored as-is even if malformed")

				t.Logf("✓ Gracefully handled malformed path: %s → %s",
					tc.virtualPath, tc.expectedBehavior)
			} else {
				require.Error(t, err, "Should reject invalid path")
				_ = db.RollbackTransaction()
			}
		})
	}
}

// TestVirtualPath_HTTPURLHandling tests that HTTP/HTTPS URLs are handled correctly
// with partial decoding (path component only)
func TestVirtualPath_HTTPURLHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	testCases := []struct {
		name         string
		url          string
		systemID     string
		expectedName string // Expected decoded name (without extension)
	}{
		{
			name:         "http_with_spaces",
			url:          "http://server.com/My%20Game.zip",
			systemID:     "pc",
			expectedName: "My Game", // Name excludes extension
		},
		{
			name:         "https_with_special_chars",
			url:          "https://cdn.example.com/games/Cool%20%26%20Fun%20Game.iso",
			systemID:     "ps1",
			expectedName: "Cool & Fun Game", // Name excludes extension
		},
		{
			name:         "http_nested_path",
			url:          "http://server.com/roms/snes/Super%20Mario.sfc",
			systemID:     "snes",
			expectedName: "Super Mario", // Name excludes extension
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Setup separate database for each subtest
			db, cleanup := setupTempMediaDB(t)
			defer cleanup()

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

			// Seed canonical tags before testing (required for extension tag creation)
			err := SeedCanonicalTags(db, scanState)
			require.NoError(t, err)

			err = db.BeginTransaction(false)
			require.NoError(t, err)

			titleIndex, mediaIndex, err := AddMediaPath(
				db,
				scanState,
				tc.systemID,
				tc.url,
				false,
				false,
				nil,
			)
			require.NoError(t, err)

			err = db.CommitTransaction()
			require.NoError(t, err)

			// Retrieve and verify
			media, err := db.FindMedia(database.Media{DBID: int64(mediaIndex)})
			require.NoError(t, err)
			assert.Equal(t, tc.url, media.Path, "URL should be stored as-is")

			// Verify PathInfo decodes correctly
			pathInfo := helpers.GetPathInfo(media.Path)
			assert.Equal(t, tc.expectedName, pathInfo.Name,
				"PathInfo should decode HTTP URL path component")

			t.Logf("✓ HTTP URL handled: %s → name=%s", tc.url, pathInfo.Name)

			// Verify title was created
			_, err = db.FindMediaTitle(&database.MediaTitle{DBID: int64(titleIndex)})
			require.NoError(t, err)
		})
	}
}
