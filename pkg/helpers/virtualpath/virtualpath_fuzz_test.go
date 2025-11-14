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

package virtualpath

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzParseVirtualPathStr tests ParseVirtualPathStr with random inputs
// to discover edge cases in virtual path parsing (scheme://id/name format)
func FuzzParseVirtualPathStr(f *testing.F) {
	// Seed corpus with known good and edge case inputs from existing tests
	f.Add("steam://123/GameName")
	f.Add("steam://456/Game%20With%20Spaces")
	f.Add("kodi-movie://789/The%20Matrix")
	f.Add("flashpoint://abc/Flash%20Game")
	f.Add("launchbox://def/Game%20Title")
	f.Add("scummvm://ghi/Monkey%20Island")
	f.Add("")                               // Empty string
	f.Add("://")                            // Malformed
	f.Add("steam://")                       // Missing ID
	f.Add("steam://123")                    // Missing name
	f.Add("steam:///Name")                  // Empty ID (legacy support)
	f.Add("http://example.com")             // Wrong scheme type
	f.Add("steam://123/Name\x00Control")    // Control char
	f.Add("steam://123/Game%")              // Incomplete encoding
	f.Add("steam://123/Game%ZZ")            // Invalid encoding
	f.Add("steam://123/Super%20Hot%2FCold") // Encoded slash
	f.Add("invalid")                        // No scheme
	f.Add("nocolon")                        // No colon
	f.Add("1scheme://test")                 // Invalid scheme (starts with digit)
	f.Add("sch eme://test")                 // Space in scheme

	f.Fuzz(func(t *testing.T, virtualPath string) {
		// Call the function - should never panic
		result, err := ParseVirtualPathStr(virtualPath)

		// Property 1: If successful, scheme must be valid
		if err == nil {
			if result.Scheme != "" {
				// Scheme should follow RFC 3986 rules
				if !IsValidScheme(result.Scheme) {
					t.Errorf("Invalid scheme accepted: %q", result.Scheme)
				}
			}
		}

		// Property 2: ID and Name should be valid UTF-8 if parsed
		if err == nil {
			if !utf8.ValidString(result.ID) {
				t.Errorf("Invalid UTF-8 in ID: %q from input: %q", result.ID, virtualPath)
			}
			if !utf8.ValidString(result.Name) {
				t.Errorf("Invalid UTF-8 in Name: %q from input: %q", result.Name, virtualPath)
			}
		}

		// Property 3: Should return error for paths without scheme separator
		if !strings.Contains(virtualPath, "://") && err == nil {
			t.Errorf("Should reject path without scheme: %q", virtualPath)
		}

		// Property 4: If contains control chars, should reject
		if ContainsControlChar(virtualPath) {
			if err == nil && result.Scheme != "" {
				t.Errorf("Should reject control characters: %q", virtualPath)
			}
		}

		// Property 5: Result components should not be longer than input
		if err == nil {
			totalLen := len(result.Scheme) + len(result.ID) + len(result.Name)
			if totalLen > len(virtualPath)*2 {
				t.Errorf("Result components unexpectedly long: input=%d, total=%d",
					len(virtualPath), totalLen)
			}
		}
	})
}
