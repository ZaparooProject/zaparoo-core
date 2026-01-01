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

package virtualpath

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateVirtualPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   string
		id       string
		pathName string
		expected string
	}{
		{
			name:     "simple_name",
			scheme:   "kodi-movie",
			id:       "123",
			pathName: "The Matrix",
			expected: "kodi-movie://123/The%20Matrix",
		},
		{
			name:     "name_with_slash",
			scheme:   "kodi-show",
			id:       "456",
			pathName: "Some Hot/Cold",
			expected: "kodi-show://456/Some%20Hot%2FCold",
		},
		{
			name:     "alphanumeric_id",
			scheme:   "scummvm",
			id:       "monkey1",
			pathName: "Monkey Island",
			expected: "scummvm://monkey1/Monkey%20Island",
		},
		{
			name:     "id_with_special_chars",
			scheme:   "launchbox",
			id:       "game-id_123",
			pathName: "Game Title",
			expected: "launchbox://game-id_123/Game%20Title",
		},
		{
			name:     "id_with_space",
			scheme:   "steam",
			id:       "space id",
			pathName: "Game Name",
			expected: "steam://space%20id/Game%20Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := CreateVirtualPath(tt.scheme, tt.id, tt.pathName)
			assert.Equal(t, tt.expected, result)

			// Verify round-trip: create path, parse it back
			parsed, err := ParseVirtualPathStr(result)
			require.NoError(t, err, "Should parse created path without error")
			assert.Equal(t, tt.scheme, parsed.Scheme, "Scheme should match")
			assert.Equal(t, tt.id, parsed.ID, "ID should match after round-trip")
			assert.Equal(t, tt.pathName, parsed.Name, "Name should match after round-trip")
		})
	}
}

func TestExtractSchemeID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		virtualPath    string
		expectedScheme string
		expectedID     string
		wantErr        bool
	}{
		{
			name:           "steam_simple",
			virtualPath:    "steam://123/GameName",
			expectedScheme: "steam",
			expectedID:     "123",
			wantErr:        false,
		},
		{
			name:           "steam_with_url_encoding",
			virtualPath:    "steam://456/Super%20Game",
			expectedScheme: "steam",
			expectedID:     "456",
			wantErr:        false,
		},
		{
			name:           "kodi_movie",
			virtualPath:    "kodi-movie://789/The%20Matrix",
			expectedScheme: "kodi-movie",
			expectedID:     "789",
			wantErr:        false,
		},
		{
			name:           "scheme_mismatch",
			virtualPath:    "steam://123/Game",
			expectedScheme: "kodi-movie",
			expectedID:     "",
			wantErr:        true,
		},
		{
			name:           "not_virtual_path",
			virtualPath:    "/regular/file/path.rom",
			expectedScheme: "steam",
			expectedID:     "",
			wantErr:        true,
		},
		{
			name:           "empty_path",
			virtualPath:    "",
			expectedScheme: "steam",
			expectedID:     "",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ExtractSchemeID(tt.virtualPath, tt.expectedScheme)

			if tt.wantErr {
				require.Error(t, err, "ExtractSchemeID should return error")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedID, result, "ExtractSchemeID result mismatch")
			}
		})
	}
}

func TestParseVirtualPathStr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		virtualPath  string
		expectedID   string
		expectedName string
		wantErr      bool
	}{
		// Query parameters
		{
			name:         "with_query_params",
			virtualPath:  "steam://123/Game%20Name?param=value",
			expectedID:   "123",
			expectedName: "Game Name",
			wantErr:      false,
		},
		{
			name:         "query_params_with_percent",
			virtualPath:  "kodi-movie://456/Movie?filter=%20test",
			expectedID:   "456",
			expectedName: "Movie",
			wantErr:      false,
		},

		// Fragments
		{
			name:         "with_fragment",
			virtualPath:  "steam://789/My%20Game#section",
			expectedID:   "789",
			expectedName: "My Game#section", // Fragment kept as part of name
			wantErr:      false,
		},

		// Multiple/trailing slashes
		{
			name:         "trailing_slash",
			virtualPath:  "steam://123/GameName/",
			expectedID:   "123",
			expectedName: "GameName", // Trailing slash trimmed
			wantErr:      false,
		},
		{
			name:         "multiple_slashes_in_name",
			virtualPath:  "steam://123/Path/To/Game",
			expectedID:   "123",
			expectedName: "Path/To/Game",
			wantErr:      false,
		},

		// Empty components
		{
			name:         "empty_name",
			virtualPath:  "steam://123/",
			expectedID:   "123",
			expectedName: "",
			wantErr:      false,
		},
		{
			name:         "no_name_component",
			virtualPath:  "steam://123",
			expectedID:   "123",
			expectedName: "",
			wantErr:      false,
		},

		// Special characters
		{
			name:         "name_with_parentheses",
			virtualPath:  "launchbox://abc/Game%20%28USA%29",
			expectedID:   "abc",
			expectedName: "Game (USA)",
			wantErr:      false,
		},
		{
			name:         "name_with_brackets",
			virtualPath:  "scummvm://def/Game%20%5BRev%201%5D",
			expectedID:   "def",
			expectedName: "Game [Rev 1]",
			wantErr:      false,
		},

		// Invalid URL encoding (graceful fallback)
		{
			name:         "invalid_percent_encoding",
			virtualPath:  "steam://123/Game%",
			expectedID:   "123",
			expectedName: "Game%", // Falls back to undecoded
			wantErr:      false,
		},
		{
			name:         "percent_without_hex",
			virtualPath:  "steam://123/Game%ZZ",
			expectedID:   "123",
			expectedName: "Game%ZZ", // Falls back to undecoded
			wantErr:      false,
		},

		// Error cases
		{
			name:         "no_scheme",
			virtualPath:  "just/a/path",
			expectedID:   "",
			expectedName: "",
			wantErr:      true,
		},
		{
			name:         "no_id",
			virtualPath:  "steam:///GameName",
			expectedID:   "",
			expectedName: "GameName",
			wantErr:      false, // Support empty ID for legacy cards
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ParseVirtualPathStr(tt.virtualPath)

			if tt.wantErr {
				require.Error(t, err, "ParseVirtualPathStr should return error")
			} else {
				assert.Equal(t, tt.expectedID, result.ID, "ParseVirtualPathStr ID mismatch")
				assert.Equal(t, tt.expectedName, result.Name, "ParseVirtualPathStr Name mismatch")
			}
		})
	}
}

func TestIsValidScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scheme string
		valid  bool
	}{
		// Valid schemes
		{"steam", true},
		{"kodi-movie", true},
		{"kodi-show", true},
		{"HTTPS", true},         // Case insensitive
		{"custom-scheme", true}, // Hyphens allowed
		{"scheme123", true},     // Numbers in middle
		{"a", true},             // Single char
		{"ab", true},            // Two chars
		{"custom.scheme", true}, // Dots allowed
		{"custom+scheme", true}, // Plus allowed
		{"h", true},             // Single letter is valid

		// Invalid schemes
		{"1scheme", false},  // Starts with number
		{"-scheme", false},  // Starts with hyphen
		{".scheme", false},  // Starts with dot
		{"+scheme", false},  // Starts with plus
		{"sch eme", false},  // Contains space
		{"sch\teme", false}, // Contains tab
		{"sch\neme", false}, // Contains newline
		{"scheme!", false},  // Invalid char
		{"", false},         // Empty
		{"123", false},      // All numbers
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			t.Parallel()
			result := IsValidScheme(tt.scheme)
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestContainsControlChar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected bool
	}{
		// No control chars
		{"normal text", false},
		{"123", false},
		{"special!@#$%", false},
		{"", false},

		// Control chars
		{"text\x00here", true}, // NULL
		{"text\x01here", true}, // SOH
		{"text\x1Fhere", true}, // US (last ASCII control)
		{"text\x7Fhere", true}, // DEL
		{"text\nhere", true},   // Newline
		{"text\there", true},   // Tab
		{"text\rhere", true},   // Carriage return
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result := ContainsControlChar(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
