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

package shared

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCustomScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   string
		expected bool
	}{
		// Custom Zaparoo schemes
		{
			name:     "steam",
			scheme:   "steam",
			expected: true,
		},
		{
			name:     "flashpoint",
			scheme:   "flashpoint",
			expected: true,
		},
		{
			name:     "launchbox",
			scheme:   "launchbox",
			expected: true,
		},
		{
			name:     "scummvm",
			scheme:   "scummvm",
			expected: true,
		},
		{
			name:     "kodi_movie",
			scheme:   "kodi-movie",
			expected: true,
		},
		{
			name:     "kodi_episode",
			scheme:   "kodi-episode",
			expected: true,
		},
		{
			name:     "kodi_song",
			scheme:   "kodi-song",
			expected: true,
		},
		{
			name:     "kodi_album",
			scheme:   "kodi-album",
			expected: true,
		},
		{
			name:     "kodi_artist",
			scheme:   "kodi-artist",
			expected: true,
		},
		{
			name:     "kodi_show",
			scheme:   "kodi-show",
			expected: true,
		},

		// Case insensitive
		{
			name:     "steam_uppercase",
			scheme:   "STEAM",
			expected: true,
		},
		{
			name:     "steam_mixed_case",
			scheme:   "Steam",
			expected: true,
		},

		// Non-custom schemes
		{
			name:     "http",
			scheme:   "http",
			expected: false,
		},
		{
			name:     "https",
			scheme:   "https",
			expected: false,
		},
		{
			name:     "file",
			scheme:   "file",
			expected: false,
		},
		{
			name:     "ftp",
			scheme:   "ftp",
			expected: false,
		},
		{
			name:     "unknown",
			scheme:   "unknown",
			expected: false,
		},
		{
			name:     "empty",
			scheme:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsCustomScheme(tt.scheme)
			assert.Equal(t, tt.expected, result, "IsCustomScheme result mismatch")
		})
	}
}

func TestIsStandardSchemeForDecoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   string
		expected bool
	}{
		{
			name:     "http",
			scheme:   "http",
			expected: true,
		},
		{
			name:     "https",
			scheme:   "https",
			expected: true,
		},
		{
			name:     "http_uppercase",
			scheme:   "HTTP",
			expected: true,
		},
		{
			name:     "https_mixed_case",
			scheme:   "Https",
			expected: true,
		},
		{
			name:     "file",
			scheme:   "file",
			expected: false,
		},
		{
			name:     "ftp",
			scheme:   "ftp",
			expected: false,
		},
		{
			name:     "steam",
			scheme:   "steam",
			expected: false,
		},
		{
			name:     "empty",
			scheme:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsStandardSchemeForDecoding(tt.scheme)
			assert.Equal(t, tt.expected, result, "IsStandardSchemeForDecoding result mismatch")
		})
	}
}

func TestShouldDecodeURIScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   string
		expected bool
	}{
		// Custom schemes
		{
			name:     "steam",
			scheme:   "steam",
			expected: true,
		},
		{
			name:     "kodi_movie",
			scheme:   "kodi-movie",
			expected: true,
		},

		// Standard schemes
		{
			name:     "http",
			scheme:   "http",
			expected: true,
		},
		{
			name:     "https",
			scheme:   "https",
			expected: true,
		},

		// Non-decodable schemes
		{
			name:     "file",
			scheme:   "file",
			expected: false,
		},
		{
			name:     "ftp",
			scheme:   "ftp",
			expected: false,
		},
		{
			name:     "unknown",
			scheme:   "unknown",
			expected: false,
		},
		{
			name:     "empty",
			scheme:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ShouldDecodeURIScheme(tt.scheme)
			assert.Equal(t, tt.expected, result, "ShouldDecodeURIScheme result mismatch")
		})
	}
}

func TestValidCustomSchemes(t *testing.T) {
	t.Parallel()

	schemes := ValidCustomSchemes()

	// Should return a slice
	assert.NotNil(t, schemes, "ValidCustomSchemes should never return nil")

	// Should have at least 10 schemes (4 base + 6 kodi)
	assert.GreaterOrEqual(t, len(schemes), 10, "ValidCustomSchemes should return at least 10 schemes")

	// Should contain expected schemes
	expectedSchemes := []string{
		"steam",
		"flashpoint",
		"launchbox",
		"scummvm",
		"kodi-movie",
		"kodi-episode",
		"kodi-song",
		"kodi-album",
		"kodi-artist",
		"kodi-show",
	}

	for _, expected := range expectedSchemes {
		assert.Contains(t, schemes, expected, "ValidCustomSchemes should contain %s", expected)
	}
}

func TestStandardSchemesForDecoding(t *testing.T) {
	t.Parallel()

	schemes := StandardSchemesForDecoding()

	// Should return a slice
	assert.NotNil(t, schemes, "StandardSchemesForDecoding should never return nil")

	// Should have exactly 2 schemes
	assert.Len(t, schemes, 2, "StandardSchemesForDecoding should return 2 schemes")

	// Should contain http and https
	assert.Contains(t, schemes, "http", "StandardSchemesForDecoding should contain http")
	assert.Contains(t, schemes, "https", "StandardSchemesForDecoding should contain https")
}

func TestAllSchemesAreUnique(t *testing.T) {
	t.Parallel()

	// Collect all schemes (case-insensitive)
	seen := make(map[string]string) // lowercase -> original

	// Check custom schemes for duplicates
	for _, scheme := range customSchemes {
		lower := strings.ToLower(scheme)
		if original, exists := seen[lower]; exists {
			t.Errorf("duplicate scheme found (case-insensitive): %q and %q both resolve to %q",
				original, scheme, lower)
		}
		seen[lower] = scheme
	}

	// Check standard schemes for duplicates
	for _, scheme := range standardSchemesForDecoding {
		lower := strings.ToLower(scheme)
		if original, exists := seen[lower]; exists {
			t.Errorf("duplicate scheme found (case-insensitive): %q and %q both resolve to %q",
				original, scheme, lower)
		}
		seen[lower] = scheme
	}

	// Verify we have at least 12 unique schemes (6 custom + 6 kodi + 2 standard)
	assert.GreaterOrEqual(t, len(seen), 12,
		"should have at least 12 unique schemes across custom and standard")
}
