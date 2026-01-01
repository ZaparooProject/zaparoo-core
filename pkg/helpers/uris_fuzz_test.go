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

package helpers

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
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
		result, err := virtualpath.ParseVirtualPathStr(virtualPath)

		// Property 1: If successful, scheme must be valid
		if err == nil {
			if result.Scheme != "" {
				// Scheme should follow RFC 3986 rules
				if !virtualpath.IsValidScheme(result.Scheme) {
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
		if virtualpath.ContainsControlChar(virtualPath) {
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

// FuzzDecodeURIIfNeeded tests URI decoding with random inputs
// to discover edge cases in scheme-based decoding logic
func FuzzDecodeURIIfNeeded(f *testing.F) {
	// Seed corpus with various schemes and edge cases
	f.Add("steam://123/Game%20Name")
	f.Add("http://example.com/file%20name.zip")
	f.Add("https://example.com/path%20with%20spaces/file.txt")
	f.Add("kodi-movie://456/Movie")
	f.Add("file:///path/file.txt")
	f.Add("")                                        // Empty
	f.Add("just/a/path")                             // No scheme
	f.Add("http://example.com/file%")                // Incomplete encoding
	f.Add("steam://123/Name\x00")                    // Control char
	f.Add("http://[2001:db8::1]/file.zip")           // IPv6
	f.Add("http://example.com:8080/file.zip")        // Port
	f.Add("http://user:pass@host/file.zip")          // Userinfo
	f.Add("steam://123/Super%20Hot%2FCold")          // Encoded slash
	f.Add("https://example.com/games/My%20Game.iso") // HTTPS
	f.Add("ftp://server/My%20File.zip")              // FTP (should not decode)
	f.Add("myscheme://data%20here")                  // Unknown scheme

	f.Fuzz(func(t *testing.T, uri string) {
		// Call function - should never panic
		result := DecodeURIIfNeeded(uri)

		// Property 1: Result must be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("Invalid UTF-8 in result: %q from input: %q", result, uri)
		}

		// Property 2: Result should not be unreasonably longer than input
		// URL decoding can only shrink or stay same, but parsing may rearrange
		if len(result) > len(uri)*2 {
			t.Errorf("Result unexpectedly long: input=%d, output=%d", len(uri), len(result))
		}

		// Property 3: If input has no encoding and no scheme, result should equal input
		if !strings.Contains(uri, "%") && !strings.Contains(uri, "://") {
			if result != uri {
				t.Errorf("No encoding/scheme but result changed: %q -> %q", uri, result)
			}
		}

		// Property 4: Idempotence - decoding twice should equal decoding once
		result2 := DecodeURIIfNeeded(result)
		if result2 != result {
			t.Errorf("Not idempotent: decode(%q)=%q, decode(decode(%q))=%q",
				uri, result, uri, result2)
		}

		// Property 5: Should preserve scheme if present and valid
		if strings.Contains(uri, "://") && !virtualpath.ContainsControlChar(uri) {
			schemeEnd := strings.Index(uri, "://")
			schemeEnd2 := strings.Index(result, "://")
			if schemeEnd >= 0 && schemeEnd2 >= 0 {
				origScheme := strings.ToLower(uri[:schemeEnd])
				resultScheme := strings.ToLower(result[:schemeEnd2])
				// Only check if original scheme is valid
				if virtualpath.IsValidScheme(origScheme) {
					if origScheme != resultScheme {
						t.Errorf("Scheme changed: %q -> %q", origScheme, resultScheme)
					}
				}
			}
		}
	})
}

// FuzzIsValidExtension tests extension validation with random inputs
// to discover edge cases in alphanumeric-only validation
func FuzzIsValidExtension(f *testing.F) {
	// Seed corpus with valid, invalid, and edge case extensions
	f.Add(".txt")
	f.Add(".zip")
	f.Add(".mp3")
	f.Add(".tar")
	f.Add(".rom")
	f.Add(".nes")
	f.Add(".iso")
	f.Add("")
	f.Add(".")
	f.Add("..")
	f.Add(".txt~")      // Special char
	f.Add(".file-name") // Dash
	f.Add(".file name") // Space
	f.Add(".123")       // Numbers only
	f.Add("txt")        // No leading dot
	f.Add(".a")         // Single char
	f.Add(".UPPERCASE")
	f.Add(".MiXeD")

	f.Fuzz(func(t *testing.T, ext string) {
		// Call function - should never panic
		result := IsValidExtension(ext)

		// Property 1: Empty or just "." should be invalid
		if ext == "" || ext == "." {
			if result {
				t.Errorf("Should reject empty/dot-only: %q", ext)
			}
		}

		// Property 2: Extensions with non-alphanumeric should be invalid
		if len(ext) > 1 {
			hasNonAlnum := false
			checkExt := ext
			if ext[0] == '.' {
				checkExt = ext[1:]
			}
			for _, ch := range checkExt {
				if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
					hasNonAlnum = true
					break
				}
			}
			if hasNonAlnum && result {
				t.Errorf("Should reject non-alphanumeric extension: %q", ext)
			}
		}

		// Property 3: Valid extensions should be alphanumeric with optional leading dot
		if result {
			checkExt := ext
			if ext[0] == '.' {
				checkExt = ext[1:]
			}
			if checkExt == "" {
				t.Errorf("Valid extension has no characters after dot: %q", ext)
			}
			for _, ch := range checkExt {
				if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
					t.Errorf("Valid extension has non-alphanumeric char: %q (char: %c)", ext, ch)
					break
				}
			}
		}

		// Property 4: Result should be deterministic
		result2 := IsValidExtension(ext)
		if result != result2 {
			t.Errorf("Non-deterministic result for: %q", ext)
		}
	})
}

// FuzzFilenameFromPath tests filename extraction from paths/URIs with random inputs
// to discover edge cases in path/URI parsing and decoding
func FuzzFilenameFromPath(f *testing.F) {
	// Seed corpus with various path formats
	f.Add("/path/to/file.txt")
	f.Add("C:\\Windows\\file.exe")
	f.Add("steam://123/Game%20Name")
	f.Add("http://example.com/file%20name.zip")
	f.Add("https://example.com/path/file.iso")
	f.Add("kodi-movie://456/The%20Matrix")
	f.Add("")
	f.Add(".")
	f.Add("/")
	f.Add("\\")
	f.Add("file.txt")
	f.Add("file")
	f.Add("/path/")
	f.Add("steam://123/")
	f.Add("http://example.com/")
	f.Add("steam://123") // No slash, no name

	f.Fuzz(func(t *testing.T, path string) {
		// Call function - should never panic
		result := FilenameFromPath(path)

		// Property 1: Result should be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("Invalid UTF-8 in filename: %q from path: %q", result, path)
		}

		// Property 2: Empty path should return empty result
		if path == "" && result != "" {
			t.Errorf("Empty path should return empty filename, got: %q", result)
		}

		// Property 3: Result should not be longer than input
		if len(result) > len(path) {
			t.Errorf("Filename longer than path: input=%d, output=%d", len(path), len(result))
		}

		// Property 4: Result should not contain unencoded path separators
		// (unless the input was a URI with encoded slashes that got decoded)
		// Exception: root paths like "/" or "\\" are valid edge cases
		if !strings.Contains(path, "://") && path != "/" && path != "\\" {
			// Regular path - result should not have separators
			if strings.Contains(result, "/") || strings.Contains(result, "\\") {
				t.Errorf("Filename contains separator: %q from path: %q", result, path)
			}
		}

		// Property 5: Deterministic - calling twice should give same result
		result2 := FilenameFromPath(path)
		if result != result2 {
			t.Errorf("Non-deterministic result for path: %q", path)
		}
	})
}

// FuzzContainsControlChar tests control character detection
func FuzzContainsControlChar(f *testing.F) {
	// Seed corpus
	f.Add("normal string")
	f.Add("string\nwith\nnewlines")
	f.Add("string\twith\ttabs")
	f.Add("string\x00with\x00nulls")
	f.Add("\x1F") // Unit separator
	f.Add("\x7F") // DEL
	f.Add("")
	f.Add("unicode: 你好") //nolint:gosmopolitan // Intentional use of Han script to test unicode handling

	f.Fuzz(func(t *testing.T, s string) {
		// Call function - should never panic
		result := virtualpath.ContainsControlChar(s)

		// Property: Manually verify the result
		hasControl := false
		for i := range len(s) {
			c := s[i]
			if c < 0x20 || c == 0x7F {
				hasControl = true
				break
			}
		}

		if result != hasControl {
			t.Errorf("containsControlChar mismatch for %q: got %v, expected %v", s, result, hasControl)
		}
	})
}

// FuzzIsValidScheme tests RFC 3986 scheme validation
func FuzzIsValidScheme(f *testing.F) {
	// Seed corpus with valid and invalid schemes
	f.Add("http")
	f.Add("https")
	f.Add("steam")
	f.Add("kodi-movie")
	f.Add("kodi+movie")
	f.Add("h2")
	f.Add("file")
	f.Add("")
	f.Add("123")     // Starts with digit
	f.Add("-scheme") // Starts with dash
	f.Add("sch eme") // Contains space
	f.Add("sch@eme") // Contains @

	f.Fuzz(func(t *testing.T, scheme string) {
		// Call function - should never panic
		result := virtualpath.IsValidScheme(scheme)

		// Property 1: Empty should be invalid
		if scheme == "" && result {
			t.Error("Empty scheme should be invalid")
		}

		// Property 2: Must start with letter
		if scheme != "" {
			firstChar := scheme[0]
			isLetter := (firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z')
			if !isLetter && result {
				t.Errorf("Scheme starting with non-letter accepted: %q", scheme)
			}
		}

		// Property 3: Valid schemes should only contain alphanumeric, +, -, .
		if result {
			for i, ch := range scheme {
				if i == 0 {
					// First char must be letter
					if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
						t.Errorf("Valid scheme doesn't start with letter: %q", scheme)
						break
					}
				} else {
					// Remaining chars must be alphanumeric or +-.
					if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') &&
						(ch < '0' || ch > '9') && ch != '+' && ch != '-' && ch != '.' {
						t.Errorf("Valid scheme has invalid char: %q (char: %c)", scheme, ch)
						break
					}
				}
			}
		}
	})
}

// FuzzIsValidPort tests port validation
func FuzzIsValidPort(f *testing.F) {
	// Seed corpus
	f.Add(":80")
	f.Add(":443")
	f.Add(":8080")
	f.Add(":9999")
	f.Add("")
	f.Add(":")
	f.Add(":abc")
	f.Add("80")  // Missing colon
	f.Add(":1a") // Mixed

	f.Fuzz(func(t *testing.T, port string) {
		// Call function - should never panic
		result := isValidPort(port)

		// Property 1: Empty should be valid
		if port == "" && !result {
			t.Error("Empty port should be valid")
		}

		// Property 2: If not empty, must start with colon
		if port != "" && !strings.HasPrefix(port, ":") && result {
			t.Errorf("Valid port without colon: %q", port)
		}

		// Property 3: After colon, must be all digits
		if result && len(port) > 1 {
			for i := 1; i < len(port); i++ {
				if port[i] < '0' || port[i] > '9' {
					t.Errorf("Valid port has non-digit: %q", port)
					break
				}
			}
		}
	})
}
