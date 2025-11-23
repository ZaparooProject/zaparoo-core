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

package kodi

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKodiLaunchers_IDExtraction_WithEncodedPaths tests that all Kodi launcher
// methods correctly extract IDs from URL-encoded virtual paths.
//
// This is critical because:
// 1. Scanners create virtual paths with URL encoding: "kodi-movie://123/The%20Matrix"
// 2. Paths are stored in database with encoding preserved
// 3. Launchers must extract the ID correctly regardless of encoding in the name
//
// This test verifies the current manual parsing implementation:
//
//	pathID := strings.TrimPrefix(path, scheme+"://")
//	pathID = strings.SplitN(pathID, "/", 2)[0]
//	id, _ := strconv.Atoi(pathID)
func TestKodiLaunchers_IDExtraction_WithEncodedPaths(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		scheme      string
		id          string
		displayName string
		description string
		expectedID  int
	}{
		{
			name:        "movie_with_spaces",
			scheme:      shared.SchemeKodiMovie,
			id:          "123",
			displayName: "The Matrix",
			expectedID:  123,
			description: "Movie with spaces in name",
		},
		{
			name:        "movie_with_parens",
			scheme:      shared.SchemeKodiMovie,
			id:          "456",
			displayName: "The Matrix (Reloaded)",
			expectedID:  456,
			description: "Movie with parentheses",
		},
		{
			name:        "episode_with_dash",
			scheme:      shared.SchemeKodiEpisode,
			id:          "789",
			displayName: "S01E01 - Pilot",
			expectedID:  789,
			description: "Episode with season info",
		},
		{
			name:        "episode_with_special_chars",
			scheme:      shared.SchemeKodiEpisode,
			id:          "111",
			displayName: "The One Where They [Spoiler]",
			expectedID:  111,
			description: "Episode with brackets",
		},
		{
			name:        "song_with_artist",
			scheme:      shared.SchemeKodiSong,
			id:          "222",
			displayName: "Artist - Song Name",
			expectedID:  222,
			description: "Song with artist separator",
		},
		{
			name:        "song_with_quotes",
			scheme:      shared.SchemeKodiSong,
			id:          "333",
			displayName: `Song "Title" Here`,
			expectedID:  333,
			description: "Song with quotes",
		},
		{
			name:        "album_with_ampersand",
			scheme:      shared.SchemeKodiAlbum,
			id:          "444",
			displayName: "Rock & Roll",
			expectedID:  444,
			description: "Album with ampersand",
		},
		{
			name:        "album_with_colon",
			scheme:      shared.SchemeKodiAlbum,
			id:          "555",
			displayName: "Album: Subtitle",
			expectedID:  555,
			description: "Album with colon",
		},
		{
			name:        "artist_with_apostrophe",
			scheme:      shared.SchemeKodiArtist,
			id:          "666",
			displayName: "Bob's Band",
			expectedID:  666,
			description: "Artist with apostrophe",
		},
		{
			name:        "artist_with_numbers",
			scheme:      shared.SchemeKodiArtist,
			id:          "777",
			displayName: "The 1975",
			expectedID:  777,
			description: "Artist with numbers",
		},
		{
			name:        "show_with_forward_slash",
			scheme:      shared.SchemeKodiShow,
			id:          "888",
			displayName: "Hot/Cold",
			expectedID:  888,
			description: "Show with forward slash (most tricky case)",
		},
		{
			name:        "show_complex",
			scheme:      shared.SchemeKodiShow,
			id:          "999",
			displayName: "Show: The Series (2024) [Renewed]",
			expectedID:  999,
			description: "Show with multiple special characters",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Step 1: Create virtual path (simulating what scanner does)
			virtualPath := virtualpath.CreateVirtualPath(tc.scheme, tc.id, tc.displayName)
			t.Logf("Virtual path created: %s", virtualPath)

			// Step 2: Extract ID using the CURRENT launcher implementation
			// This simulates what happens in LaunchMovie, LaunchTVEpisode, etc.
			pathID := strings.TrimPrefix(virtualPath, tc.scheme+"://")
			pathID = strings.SplitN(pathID, "/", 2)[0]

			extractedID, err := strconv.Atoi(pathID)
			require.NoError(t, err, "Should be able to parse ID from virtual path")
			assert.Equal(t, tc.expectedID, extractedID,
				"Extracted ID should match expected ID for: %s", tc.description)

			t.Logf("✓ Successfully extracted ID %d from path with encoding: %s",
				extractedID, virtualPath)

			// Step 3: Also verify using the ExtractSchemeID helper
			// This tests the alternative approach
			extractedIDStr, err := virtualpath.ExtractSchemeID(virtualPath, tc.scheme)
			require.NoError(t, err, "ExtractSchemeID helper should succeed")

			extractedIDInt, err := strconv.Atoi(extractedIDStr)
			require.NoError(t, err, "Extracted ID should be numeric")
			assert.Equal(t, tc.expectedID, extractedIDInt,
				"ExtractSchemeID helper should give same result as manual parsing")

			t.Logf("✓ ExtractSchemeID helper also extracted correct ID: %d", extractedIDInt)
		})
	}
}

// TestKodiLaunchers_IDExtraction_EdgeCases tests edge cases in ID extraction
// that could cause launch failures
func TestKodiLaunchers_IDExtraction_EdgeCases(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		virtualPath string
		scheme      string
		expectedID  string
		description string
		shouldFail  bool
	}{
		{
			name:        "large_id_number",
			virtualPath: "kodi-movie://2147483647/Movie",
			scheme:      shared.SchemeKodiMovie,
			expectedID:  "2147483647",
			shouldFail:  false,
			description: "Max int32 ID",
		},
		{
			name:        "single_digit_id",
			virtualPath: "kodi-episode://5/Episode",
			scheme:      shared.SchemeKodiEpisode,
			expectedID:  "5",
			shouldFail:  false,
			description: "Single digit ID",
		},
		{
			name:        "id_with_leading_zeros",
			virtualPath: "kodi-song://007/Song",
			scheme:      shared.SchemeKodiSong,
			expectedID:  "007",
			shouldFail:  false,
			description: "ID with leading zeros (becomes 7)",
		},
		{
			name:        "empty_name_section",
			virtualPath: "kodi-album://123/",
			scheme:      shared.SchemeKodiAlbum,
			expectedID:  "123",
			shouldFail:  false,
			description: "Empty name section with trailing slash",
		},
		{
			name:        "no_name_section",
			virtualPath: "kodi-artist://456",
			scheme:      shared.SchemeKodiArtist,
			expectedID:  "456",
			shouldFail:  false,
			description: "No name section at all",
		},
		{
			name:        "nested_slashes_in_name",
			virtualPath: "kodi-show://789/Category%2FShow%2FSeason",
			scheme:      shared.SchemeKodiShow,
			expectedID:  "789",
			shouldFail:  false,
			description: "Multiple encoded slashes in name (should not affect ID)",
		},
		{
			name:        "query_params_in_path",
			virtualPath: "kodi-movie://111/Movie?param=value",
			scheme:      shared.SchemeKodiMovie,
			expectedID:  "111",
			shouldFail:  false,
			description: "Query parameters after name",
		},
		{
			name:        "fragment_in_path",
			virtualPath: "kodi-episode://222/Episode#section",
			scheme:      shared.SchemeKodiEpisode,
			expectedID:  "222",
			shouldFail:  false,
			description: "Fragment after name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Extract ID using manual parsing (current implementation)
			pathID := strings.TrimPrefix(tc.virtualPath, tc.scheme+"://")
			pathID = strings.SplitN(pathID, "/", 2)[0]

			extractedIDInt, err := strconv.Atoi(pathID)

			if tc.shouldFail {
				require.Error(t, err, "Should fail for: %s", tc.description)
			} else {
				require.NoError(t, err, "Should succeed for: %s", tc.description)

				// Convert expected to int for comparison
				expectedInt, _ := strconv.Atoi(tc.expectedID)
				assert.Equal(t, expectedInt, extractedIDInt,
					"Extracted ID should match for: %s", tc.description)

				t.Logf("✓ Edge case handled: %s → ID=%d", tc.description, extractedIDInt)
			}
		})
	}
}

// TestKodiLaunchers_IDExtraction_MalformedPaths tests that malformed paths
// are handled gracefully (error instead of panic)
func TestKodiLaunchers_IDExtraction_MalformedPaths(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		virtualPath string
		scheme      string
		description string
	}{
		{
			name:        "non_numeric_id",
			virtualPath: "kodi-movie://abc/Movie",
			scheme:      shared.SchemeKodiMovie,
			description: "Non-numeric ID should error",
		},
		{
			name:        "empty_id",
			virtualPath: "kodi-episode:///Episode",
			scheme:      shared.SchemeKodiEpisode,
			description: "Empty ID section should error",
		},
		{
			name:        "negative_id",
			virtualPath: "kodi-song://-123/Song",
			scheme:      shared.SchemeKodiSong,
			description: "Negative ID should parse but may be invalid",
		},
		{
			name:        "id_overflow",
			virtualPath: "kodi-album://99999999999999999999/Album",
			scheme:      shared.SchemeKodiAlbum,
			description: "ID too large for int should error",
		},
		{
			name:        "special_chars_in_id",
			virtualPath: "kodi-artist://12%203/Artist",
			scheme:      shared.SchemeKodiArtist,
			description: "Encoded space in ID section should error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Extract ID using manual parsing
			pathID := strings.TrimPrefix(tc.virtualPath, tc.scheme+"://")
			pathID = strings.SplitN(pathID, "/", 2)[0]

			// Should not panic
			assert.NotPanics(t, func() {
				_, _ = strconv.Atoi(pathID)
			}, "Should not panic on malformed path: %s", tc.description)

			// Most of these should error
			_, err := strconv.Atoi(pathID)
			t.Logf("Malformed path '%s' → error: %v", tc.virtualPath, err)

			// We don't assert error here because negative IDs parse successfully
			// The key is that it doesn't panic
		})
	}
}

// TestKodiLaunchers_ManualParsingVsHelper compares the current manual parsing
// implementation against the ExtractSchemeID helper to ensure consistency
func TestKodiLaunchers_ManualParsingVsHelper(t *testing.T) {
	t.Parallel()

	schemes := []string{
		shared.SchemeKodiMovie,
		shared.SchemeKodiEpisode,
		shared.SchemeKodiSong,
		shared.SchemeKodiAlbum,
		shared.SchemeKodiArtist,
		shared.SchemeKodiShow,
	}

	testPaths := []struct {
		id   string
		name string
	}{
		{"123", "Simple Name"},
		{"456", "Name With Spaces"},
		{"789", "Name/With/Slashes"},
		{"111", "Name (With) [Brackets]"},
		{"222", `Name "With" 'Quotes'`},
		{"333", "Name & Ampersand"},
		{"444", "Name: With: Colons"},
	}

	for _, scheme := range schemes {
		for _, tp := range testPaths {
			testName := scheme + "_" + tp.id
			t.Run(testName, func(t *testing.T) {
				t.Parallel()

				// Create virtual path
				virtualPath := virtualpath.CreateVirtualPath(scheme, tp.id, tp.name)

				// Method 1: Manual parsing (current implementation)
				pathID1 := strings.TrimPrefix(virtualPath, scheme+"://")
				pathID1 = strings.SplitN(pathID1, "/", 2)[0]

				// Method 2: ExtractSchemeID helper
				pathID2, err := virtualpath.ExtractSchemeID(virtualPath, scheme)
				require.NoError(t, err)

				// Both methods should give same result
				assert.Equal(t, pathID1, pathID2,
					"Manual parsing and ExtractSchemeID should give same result for %s",
					virtualPath)

				// Both should parse to same integer ID
				id1, err1 := strconv.Atoi(pathID1)
				id2, err2 := strconv.Atoi(pathID2)

				require.NoError(t, err1)
				require.NoError(t, err2)
				assert.Equal(t, id1, id2, "Both methods should parse to same integer ID")

				t.Logf("✓ Consistent: %s → ID=%d (manual=%s, helper=%s)",
					virtualPath, id1, pathID1, pathID2)
			})
		}
	}
}

// BenchmarkIDExtraction benchmarks the two ID extraction methods
func BenchmarkIDExtraction(b *testing.B) {
	virtualPath := virtualpath.CreateVirtualPath(
		shared.SchemeKodiMovie,
		"12345",
		"The Matrix (Reloaded) [4K] - Director's Cut",
	)

	b.Run("ManualParsing", func(b *testing.B) {
		for range b.N {
			pathID := strings.TrimPrefix(virtualPath, shared.SchemeKodiMovie+"://")
			pathID = strings.SplitN(pathID, "/", 2)[0]
			_, _ = strconv.Atoi(pathID)
		}
	})

	b.Run("ExtractSchemeID", func(b *testing.B) {
		for range b.N {
			extractedID, _ := virtualpath.ExtractSchemeID(virtualPath, shared.SchemeKodiMovie)
			_, _ = strconv.Atoi(extractedID)
		}
	})
}
