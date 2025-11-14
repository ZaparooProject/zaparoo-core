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

package helpers

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeURIIfNeeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Custom Zaparoo schemes
		{
			name:     "steam_with_url_encoding",
			input:    "steam://123/Super%20Hot%2FCold",
			expected: "steam://123/Super Hot/Cold",
		},
		{
			name:     "kodi_movie_with_url_encoding",
			input:    "kodi-movie://456/The%20Matrix%20%28Reloaded%29",
			expected: "kodi-movie://456/The Matrix (Reloaded)",
		},
		{
			name:     "flashpoint_with_url_encoding",
			input:    "flashpoint://789/Flash%20Game%20Title",
			expected: "flashpoint://789/Flash Game Title",
		},
		{
			name:     "launchbox_with_url_encoding",
			input:    "launchbox://abc/Game%20With%20Spaces",
			expected: "launchbox://abc/Game With Spaces",
		},
		{
			name:     "scummvm_with_url_encoding",
			input:    "scummvm://def/Monkey%20Island",
			expected: "scummvm://def/Monkey Island",
		},

		// HTTP/HTTPS schemes
		{
			name:     "http_with_url_encoding",
			input:    "http://example.com/path%20with%20spaces/file.zip",
			expected: "http://example.com/path with spaces/file.zip",
		},
		{
			name:     "https_with_url_encoding",
			input:    "https://example.com/games/My%20Game.iso",
			expected: "https://example.com/games/My Game.iso",
		},

		// No encoding (should return as-is)
		{
			name:     "steam_without_encoding",
			input:    "steam://123/SimpleName",
			expected: "steam://123/SimpleName",
		},
		{
			name:     "regular_file_path",
			input:    "/home/user/games/mario.nes",
			expected: "/home/user/games/mario.nes",
		},
		{
			name:     "http_without_encoding",
			input:    "http://example.com/simple/path",
			expected: "http://example.com/simple/path",
		},

		// Other schemes (should not decode)
		{
			name:     "file_scheme_with_encoding",
			input:    "file:///path/My%20Game.rom",
			expected: "file:///path/My%20Game.rom",
		},
		{
			name:     "ftp_scheme_with_encoding",
			input:    "ftp://server/My%20File.zip",
			expected: "ftp://server/My%20File.zip",
		},
		{
			name:     "custom_unknown_scheme",
			input:    "myscheme://data%20here",
			expected: "myscheme://data%20here",
		},

		// Edge cases
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "no_scheme",
			input:    "just/a/path",
			expected: "just/a/path",
		},
		{
			name:     "scheme_without_percent",
			input:    "steam://123/NoEncoding",
			expected: "steam://123/NoEncoding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := DecodeURIIfNeeded(tt.input)
			assert.Equal(t, tt.expected, result, "DecodeURIIfNeeded result mismatch")
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
			result, err := virtualpath.ExtractSchemeID(tt.virtualPath, tt.expectedScheme)

			if tt.wantErr {
				require.Error(t, err, "ExtractSchemeID should return error")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedID, result, "ExtractSchemeID result mismatch")
			}
		})
	}
}

func TestParseVirtualPathStr_EdgeCases(t *testing.T) {
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
			result, err := virtualpath.ParseVirtualPathStr(tt.virtualPath)

			if tt.wantErr {
				require.Error(t, err, "ParseVirtualPathStr should return error")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedID, result.ID, "ParseVirtualPathStr ID mismatch")
				assert.Equal(t, tt.expectedName, result.Name, "ParseVirtualPathStr Name mismatch")
			}
		})
	}
}

func TestDecodeURIIfNeeded_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Query parameters and fragments
		{
			name:     "steam_with_query_params",
			input:    "steam://123/Game%20Name?launch=true",
			expected: "steam://123/Game Name?launch=true",
		},
		{
			name:     "kodi_with_fragment",
			input:    "kodi-movie://456/The%20Matrix#play",
			expected: "kodi-movie://456/The Matrix#play", // Fragment kept as part of name
		},
		{
			name:     "http_with_query_and_fragment",
			input:    "http://example.com/path%20name/file.zip?download=1#section",
			expected: "http://example.com/path name/file.zip?download=1#section",
		},

		// Multiple slashes and trailing slashes
		{
			name:     "steam_trailing_slash",
			input:    "steam://123/Game%20Name/",
			expected: "steam://123/Game Name", // Trailing slash trimmed during normalization
		},
		{
			name:     "steam_multiple_path_segments",
			input:    "steam://123/Category/Sub/Game%20Name",
			expected: "steam://123/Category/Sub/Game Name",
		},

		// Mixed encoded and non-encoded
		{
			name:     "partial_encoding",
			input:    "steam://123/Game Name%20Title",
			expected: "steam://123/Game Name Title",
		},

		// Invalid percent encoding (should not crash, graceful handling)
		{
			name:     "invalid_percent_at_end",
			input:    "steam://123/Game%",
			expected: "steam://123/Game%", // ParseVirtualPathStr falls back to undecoded
		},
		{
			name:     "invalid_percent_encoding",
			input:    "steam://123/Game%ZZ",
			expected: "steam://123/Game%ZZ", // Falls back
		},

		// HTTP with userinfo and port
		{
			name:     "http_with_port",
			input:    "http://example.com:8080/my%20game.zip",
			expected: "http://example.com:8080/my game.zip",
		},
		{
			name:     "http_with_userinfo",
			input:    "http://user:pass@example.com/my%20file.iso",
			expected: "http://user:pass@example.com/my file.iso",
		},

		// Only query/fragment contains percent (no path encoding)
		{
			name:     "percent_only_in_query",
			input:    "steam://123/GameName?param=%20value",
			expected: "steam://123/GameName?param=%20value", // Query not decoded for custom schemes
		},

		// Empty name with query/fragment
		{
			name:     "empty_name_with_query",
			input:    "steam://123/?param=value",
			expected: "steam://123/?param=value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := DecodeURIIfNeeded(tt.input)
			assert.Equal(t, tt.expected, result, "DecodeURIIfNeeded edge case result mismatch")
		})
	}
}

func TestFilenameFromPath_URIEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Virtual paths with query/fragment
		{
			name:     "steam_with_query",
			input:    "steam://123/Game%20Title?param=value",
			expected: "Game Title",
		},
		{
			name:     "kodi_with_fragment",
			input:    "kodi-movie://456/The%20Movie#section",
			expected: "The Movie#section", // Fragment is part of the name
		},

		// HTTP URLs with query/fragment
		{
			name:     "http_with_query",
			input:    "http://example.com/My%20Game.zip?download=1",
			expected: "My Game.zip",
		},
		{
			name:     "https_with_fragment",
			input:    "https://server.com/File%20Name.iso#info",
			expected: "File Name.iso",
		},

		// Multiple path segments
		{
			name:     "steam_nested_paths",
			input:    "steam://123/Category/SubCategory/Game%20Name",
			expected: "Game Name",
		},
		{
			name:     "http_nested_paths",
			input:    "http://example.com/games/roms/My%20Game.zip",
			expected: "My Game.zip",
		},

		// Trailing slashes
		{
			name:     "steam_trailing_slash",
			input:    "steam://123/GameName/",
			expected: "GameName", // Trailing slash trimmed
		},
		{
			name:     "http_trailing_slash",
			input:    "http://example.com/path/",
			expected: "",
		},

		// Empty name component
		{
			name:     "steam_empty_name",
			input:    "steam://123/",
			expected: "",
		},
		{
			name:     "steam_no_name",
			input:    "steam://123",
			expected: "123", // Return ID for legacy card support
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FilenameFromPath(tt.input)
			assert.Equal(t, tt.expected, result, "FilenameFromPath URI edge case result mismatch")
		})
	}
}

// TestURIRFCCompliance tests RFC 3986 compliance for URI parsing
func TestURIRFCCompliance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expected    string
		description string
	}{
		// Control character tests
		{
			name:        "control_char_null_byte",
			input:       "http://example.com/file\x00name.zip",
			expected:    "http://example.com/file\x00name.zip", // Should return as-is (graceful fallback)
			description: "URL with null byte control character",
		},
		{
			name:        "control_char_tab",
			input:       "http://example.com/file\tname.zip",
			expected:    "http://example.com/file\tname.zip", // Should return as-is
			description: "URL with tab control character",
		},
		{
			name:        "control_char_newline",
			input:       "http://example.com/file\nname.zip",
			expected:    "http://example.com/file\nname.zip", // Should return as-is
			description: "URL with newline control character",
		},

		// Invalid scheme tests
		{
			name:        "scheme_starts_with_digit",
			input:       "123://example.com/path",
			expected:    "123://example.com/path", // Should return as-is
			description: "Scheme starting with digit (invalid per RFC 3986)",
		},
		{
			name:        "scheme_with_underscore",
			input:       "ht_tp://example.com/path",
			expected:    "ht_tp://example.com/path", // Should return as-is
			description: "Scheme with underscore (invalid per RFC 3986)",
		},
		{
			name:        "scheme_with_special_char",
			input:       "ht!tp://example.com/path",
			expected:    "ht!tp://example.com/path", // Should return as-is
			description: "Scheme with exclamation mark (invalid)",
		},
		{
			name:        "valid_scheme_with_plus",
			input:       "svn+ssh://example.com/path/file.txt",
			expected:    "svn+ssh://example.com/path/file.txt", // Valid scheme, should parse
			description: "Valid scheme with + character",
		},
		{
			name:        "valid_scheme_with_dash",
			input:       "my-scheme://123/name",
			expected:    "my-scheme://123/name", // Valid custom scheme
			description: "Valid scheme with - character",
		},

		// Userinfo with @ symbol tests
		{
			name:        "userinfo_password_with_at",
			input:       "http://user:p@ss@example.com/file.zip",
			expected:    "http://user:p@ss@example.com/file.zip", // Should handle @ in password
			description: "Password containing @ symbol (uses LastIndex)",
		},
		{
			name:        "userinfo_multiple_at",
			input:       "http://user@domain:p@ss@word@example.com/file.zip",
			expected:    "http://user@domain:p@ss@word@example.com/file.zip",
			description: "Multiple @ symbols in userinfo",
		},

		// IPv6 address tests
		{
			name:        "ipv6_simple",
			input:       "http://[2001:db8::1]/path/file.zip",
			expected:    "http://[2001:db8::1]/path/file.zip",
			description: "Simple IPv6 address",
		},
		{
			name:        "ipv6_with_port",
			input:       "http://[2001:db8::1]:8080/path/file.zip",
			expected:    "http://[2001:db8::1]:8080/path/file.zip",
			description: "IPv6 address with port",
		},
		{
			name:        "ipv6_with_userinfo",
			input:       "http://user:pass@[2001:db8::1]/path/file.zip",
			expected:    "http://user:pass@[2001:db8::1]/path/file.zip",
			description: "IPv6 address with userinfo",
		},
		{
			name:        "ipv6_localhost",
			input:       "http://[::1]/file.zip",
			expected:    "http://[::1]/file.zip",
			description: "IPv6 localhost",
		},
		{
			name:        "ipv6_malformed_no_closing_bracket",
			input:       "http://[2001:db8::1/file.zip",
			expected:    "http://[2001:db8::1/file.zip", // Malformed - should return as-is
			description: "Malformed IPv6 - missing closing bracket",
		},

		// Port validation tests
		{
			name:        "valid_port",
			input:       "http://example.com:8080/file.zip",
			expected:    "http://example.com:8080/file.zip",
			description: "Valid numeric port",
		},
		{
			name:        "invalid_port_alpha",
			input:       "http://example.com:abc/file.zip",
			expected:    "http://example.com:abc/file.zip", // Invalid port - should return as-is
			description: "Invalid alphabetic port",
		},
		{
			name:        "invalid_port_special",
			input:       "http://example.com:80-90/file.zip",
			expected:    "http://example.com:80-90/file.zip", // Invalid port - should return as-is
			description: "Invalid port with special character",
		},

		// Valid custom schemes (should still work)
		{
			name:        "steam_scheme",
			input:       "steam://123/Game%20Name",
			expected:    "steam://123/Game Name",
			description: "Steam custom scheme (valid)",
		},
		{
			name:        "kodi_movie_scheme",
			input:       "kodi-movie://456/The%20Matrix",
			expected:    "kodi-movie://456/The Matrix",
			description: "Kodi movie custom scheme (valid)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := DecodeURIIfNeeded(tt.input)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestFilenameFromPath_RFCCompliance tests RFC compliance for filename extraction
func TestFilenameFromPath_RFCCompliance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expected    string
		description string
	}{
		// IPv6 filename extraction
		{
			name:        "ipv6_filename",
			input:       "http://[2001:db8::1]/path/to/file.zip",
			expected:    "file.zip",
			description: "Extract filename from IPv6 URL",
		},
		{
			name:        "ipv6_with_port_filename",
			input:       "http://[2001:db8::1]:8080/path/file.tar.gz",
			expected:    "file.tar.gz",
			description: "Extract filename from IPv6 URL with port",
		},
		{
			name:        "ipv6_encoded_filename",
			input:       "http://[::1]/My%20File.zip",
			expected:    "My File.zip",
			description: "Extract decoded filename from IPv6 URL",
		},

		// Port validation in filename extraction
		{
			name:        "valid_port_filename",
			input:       "http://example.com:8080/file.zip",
			expected:    "file.zip",
			description: "Extract filename from URL with valid port",
		},
		{
			name:        "invalid_port_returns_empty",
			input:       "http://example.com:abc/file.zip",
			expected:    "file.zip",
			description: "Invalid port still extracts filename (graceful fallback)",
		},

		// Userinfo with @ in filename extraction
		{
			name:        "userinfo_at_symbol_filename",
			input:       "http://user:p@ss@example.com/file.zip",
			expected:    "file.zip",
			description: "Extract filename from URL with @ in password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FilenameFromPath(tt.input)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestIsValidScheme tests RFC 3986 scheme validation
func TestIsValidScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   string
		expected bool
	}{
		// Valid schemes
		{name: "http", scheme: "http", expected: true},
		{name: "https", scheme: "https", expected: true},
		{name: "steam", scheme: "steam", expected: true},
		{name: "kodi-movie", scheme: "kodi-movie", expected: true},
		{name: "svn+ssh", scheme: "svn+ssh", expected: true},
		{name: "my.scheme", scheme: "my.scheme", expected: true},
		{name: "A", scheme: "A", expected: true},

		// Invalid schemes
		{name: "empty", scheme: "", expected: false},
		{name: "starts_with_digit", scheme: "123abc", expected: false},
		{name: "starts_with_plus", scheme: "+scheme", expected: false},
		{name: "contains_underscore", scheme: "my_scheme", expected: false},
		{name: "contains_slash", scheme: "my/scheme", expected: false},
		{name: "contains_space", scheme: "my scheme", expected: false},
		{name: "contains_special", scheme: "my!scheme", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := virtualpath.IsValidScheme(tt.scheme)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsValidPort tests port validation
func TestIsValidPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		port     string
		expected bool
	}{
		// Valid ports
		{name: "empty", port: "", expected: true},
		{name: "standard_http", port: ":80", expected: true},
		{name: "standard_https", port: ":443", expected: true},
		{name: "high_port", port: ":8080", expected: true},
		{name: "very_high_port", port: ":65535", expected: true},

		// Invalid ports
		{name: "no_colon", port: "8080", expected: false},
		{name: "just_colon", port: ":", expected: false},
		{name: "alphabetic", port: ":abc", expected: false},
		{name: "alphanumeric", port: ":80a", expected: false},
		{name: "negative", port: ":-80", expected: false},
		{name: "with_slash", port: ":80/", expected: false},
		{name: "with_dash", port: ":80-90", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isValidPort(tt.port)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestContainsControlChar tests control character detection
func TestContainsControlChar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// No control characters
		{name: "normal_text", input: "hello world", expected: false},
		{name: "url", input: "http://example.com/path", expected: false},
		{name: "special_chars", input: "hello!@#$%^&*()", expected: false},
		//nolint:gosmopolitan // Testing unicode character handling - intentionally uses non-ASCII characters
		{name: "unicode", input: "hello 世界", expected: false},

		// With control characters
		{name: "null_byte", input: "hello\x00world", expected: true},
		{name: "tab", input: "hello\tworld", expected: true},
		{name: "newline", input: "hello\nworld", expected: true},
		{name: "carriage_return", input: "hello\rworld", expected: true},
		{name: "delete", input: "hello\x7Fworld", expected: true},
		{name: "bell", input: "hello\x07world", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := virtualpath.ContainsControlChar(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDecodeURIIfNeeded_MalformedGracefulFallback tests that malformed URIs
// are handled gracefully without panicking, falling back to undecoded strings
func TestDecodeURIIfNeeded_MalformedGracefulFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expected    string
		description string
	}{
		{
			name:        "incomplete_percent_encoding_at_end",
			input:       "kodi-movie://123/Game%",
			expected:    "kodi-movie://123/Game%",
			description: "Incomplete percent encoding (no hex digits) should fallback",
		},
		{
			name:        "incomplete_percent_encoding_one_digit",
			input:       "steam://456/Game%2",
			expected:    "steam://456/Game%2",
			description: "Incomplete percent encoding (one hex digit) should fallback",
		},
		{
			name:        "invalid_hex_characters",
			input:       "kodi-show://789/Game%ZZ",
			expected:    "kodi-show://789/Game%ZZ",
			description: "Invalid hex characters in encoding should fallback",
		},
		{
			name:        "invalid_hex_mixed",
			input:       "steam://111/Game%2G",
			expected:    "steam://111/Game%2G",
			description: "Mixed valid/invalid hex should fallback",
		},
		{
			name:        "multiple_incomplete_encodings",
			input:       "kodi-movie://222/Game%20Title%",
			expected:    "kodi-movie://222/Game%20Title%",
			description: "Multiple encodings with one incomplete should fallback",
		},
		{
			name:        "percent_with_space",
			input:       "steam://333/Game% 20",
			expected:    "steam://333/Game% 20",
			description: "Percent followed by space should fallback",
		},
		{
			name:        "control_character_in_path",
			input:       "kodi-movie://444/Name\x00Here",
			expected:    "kodi-movie://444/Name\x00Here",
			description: "Control characters should prevent parsing, return as-is",
		},
		{
			name:        "control_character_after_encoding",
			input:       "steam://555/Game%20\x00Name",
			expected:    "steam://555/Game%20\x00Name",
			description: "Control chars should prevent decoding",
		},
		{
			name:        "invalid_scheme_digits",
			input:       "123scheme://666/Name",
			expected:    "123scheme://666/Name",
			description: "RFC 3986: scheme must start with letter, no decoding",
		},
		{
			name:        "invalid_scheme_special_char",
			input:       "sch@eme://777/Name%20Here",
			expected:    "sch@eme://777/Name%20Here",
			description: "Invalid scheme characters should prevent decoding",
		},
		{
			name:        "malformed_authority_no_slashes",
			input:       "kodi-movie:888/Name%20Here",
			expected:    "kodi-movie:888/Name%20Here",
			description: "Missing // after scheme should still work (opaque URI)",
		},
		{
			name:        "empty_scheme",
			input:       "://123/Name%20Here",
			expected:    "://123/Name%20Here",
			description: "Empty scheme should not decode",
		},
		{
			name:        "just_scheme",
			input:       "steam://",
			expected:    "steam://",
			description: "Just scheme with no authority/path should not crash",
		},
		{
			name:        "malformed_ipv6_missing_bracket",
			input:       "http://[2001:db8::1/file%20name.zip",
			expected:    "http://[2001:db8::1/file%20name.zip",
			description: "Malformed IPv6 (missing ]) should fallback",
		},
		{
			name:        "malformed_ipv6_extra_bracket",
			input:       "http://[2001:db8::1]]/file%20name.zip",
			expected:    "http://[2001:db8::1]]/file%20name.zip",
			description: "Malformed IPv6 (extra ]) should fallback",
		},
		{
			name:        "invalid_port_letters",
			input:       "http://example.com:abc/file%20name.zip",
			expected:    "http://example.com:abc/file%20name.zip",
			description: "Non-numeric port should fallback",
		},
		{
			name:        "invalid_port_negative",
			input:       "http://example.com:-80/file%20name.zip",
			expected:    "http://example.com:-80/file%20name.zip",
			description: "Negative port should fallback",
		},
		{
			name:        "double_encoding",
			input:       "steam://999/Name%2520Here",
			expected:    "steam://999/Name%20Here",
			description: "Double encoding should decode once only",
		},
		{
			name:        "triple_encoding",
			input:       "kodi-movie://100/Name%252520Here",
			expected:    "kodi-movie://100/Name%2520Here",
			description: "Triple encoding should decode once only",
		},
		{
			name:        "mixed_encoding_quality",
			input:       "steam://200/Game%20Name%ZZTitle",
			expected:    "steam://200/Game%20Name%ZZTitle",
			description: "Mix of valid and invalid encoding should fallback all",
		},
		{
			name:        "unicode_in_scheme",
			input:       "ködi-movie://300/Name%20Here",
			expected:    "ködi-movie://300/Name%20Here",
			description: "Non-ASCII in scheme should not decode (invalid scheme)",
		},
		{
			name:        "empty_string",
			input:       "",
			expected:    "",
			description: "Empty string should return empty",
		},
		{
			name:        "just_percent",
			input:       "%",
			expected:    "%",
			description: "Just percent sign should not crash",
		},
		{
			name:        "percent_percent",
			input:       "%%",
			expected:    "%%",
			description: "Double percent should not crash",
		},
		{
			name:        "scheme_with_plus",
			input:       "sche+me://400/Name%20Here",
			expected:    "sche+me://400/Name%20Here",
			description: "Scheme with + is valid RFC 3986 but not custom, no decode",
		},
		{
			name:        "scheme_with_dash",
			input:       "sche-me://500/Name%20Here",
			expected:    "sche-me://500/Name%20Here",
			description: "Scheme with - is valid but not in custom list",
		},
		{
			name:        "scheme_with_dot",
			input:       "sche.me://600/Name%20Here",
			expected:    "sche.me://600/Name%20Here",
			description: "Scheme with . is valid RFC 3986 but not custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Should not panic
			assert.NotPanics(t, func() {
				DecodeURIIfNeeded(tt.input)
			}, "Should not panic on malformed URI: %s", tt.description)

			// Should return expected fallback
			result := DecodeURIIfNeeded(tt.input)
			assert.Equal(t, tt.expected, result,
				"Malformed URI should fallback gracefully: %s", tt.description)

			t.Logf("✓ Graceful fallback: %s → %q", tt.description, result)
		})
	}
}

// TestParseVirtualPathStr_MalformedGracefulFallback tests that ParseVirtualPathStr
// handles malformed paths gracefully without panicking
func TestParseVirtualPathStr_MalformedGracefulFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		description string
		shouldPanic bool
	}{
		{
			name:        "incomplete_encoding",
			input:       "kodi-movie://123/Game%",
			shouldPanic: false,
			description: "Should handle incomplete percent encoding",
		},
		{
			name:        "invalid_encoding",
			input:       "steam://456/Game%ZZ",
			shouldPanic: false,
			description: "Should handle invalid hex in encoding",
		},
		{
			name:        "control_characters",
			input:       "kodi-show://789/Name\x00Here",
			shouldPanic: false,
			description: "Should handle control characters",
		},
		{
			name:        "empty_path",
			input:       "",
			shouldPanic: false,
			description: "Should handle empty path",
		},
		{
			name:        "no_scheme",
			input:       "123/Name",
			shouldPanic: false,
			description: "Should handle path with no scheme",
		},
		{
			name:        "invalid_scheme",
			input:       "123invalid://456/Name",
			description: "Should handle invalid scheme (starts with digit)",
			shouldPanic: false,
		},
		{
			name:        "empty_scheme",
			input:       "://123/Name",
			description: "Should handle empty scheme",
			shouldPanic: false,
		},
		{
			name:        "just_scheme",
			input:       "steam://",
			description: "Should handle just scheme with no path",
			shouldPanic: false,
		},
		{
			name:        "malformed_ipv6",
			input:       "http://[invalid]/file",
			shouldPanic: false,
			description: "Should handle malformed IPv6",
		},
		{
			name:        "very_long_path",
			input:       "steam://123/" + string(make([]byte, 10000)),
			shouldPanic: false,
			description: "Should handle very long paths",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.shouldPanic {
				assert.Panics(t, func() {
					_, _ = virtualpath.ParseVirtualPathStr(tt.input)
				}, tt.description)
			} else {
				assert.NotPanics(t, func() {
					result, _ := virtualpath.ParseVirtualPathStr(tt.input)
					t.Logf("Handled malformed path: %q → scheme=%q, id=%q, name=%q",
						tt.input, result.Scheme, result.ID, result.Name)
				}, tt.description)
			}
		})
	}
}

// TestExtractSchemeID_MalformedHandling tests ExtractSchemeID with malformed inputs
func TestExtractSchemeID_MalformedHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		path           string
		expectedScheme string
		description    string
		shouldError    bool
	}{
		{
			name:           "wrong_scheme",
			path:           "steam://123/Name",
			expectedScheme: "kodi-movie",
			shouldError:    true,
			description:    "Should error when scheme doesn't match",
		},
		{
			name:           "case_mismatch",
			path:           "STEAM://123/Name",
			expectedScheme: "steam",
			description:    "Should handle case-insensitive scheme matching",
			shouldError:    false,
		},
		{
			name:           "empty_path",
			path:           "",
			expectedScheme: "steam",
			shouldError:    true,
			description:    "Should error on empty path",
		},
		{
			name:           "no_id_section",
			path:           "steam://",
			expectedScheme: "steam",
			shouldError:    true,
			description:    "Should error when no ID section",
		},
		{
			name:           "empty_id",
			path:           "steam:///Name",
			expectedScheme: "steam",
			description:    "Should support empty ID for legacy cards",
			shouldError:    false,
		},
		{
			name:           "id_with_encoding",
			path:           "steam://12%203/Name",
			expectedScheme: "steam",
			shouldError:    false,
			description:    "Should extract ID even with encoding in it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := virtualpath.ExtractSchemeID(tt.path, tt.expectedScheme)

			if tt.shouldError {
				assert.Error(t, err, tt.description)
			} else {
				require.NoError(t, err, tt.description)
				t.Logf("Extracted ID: %q from %q", result, tt.path)
			}
		})
	}
}
