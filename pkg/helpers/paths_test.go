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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPathInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected PathInfo
	}{
		{
			name: "unix_path_with_extension",
			path: "/games/snes/mario.sfc",
			expected: PathInfo{
				Path:      "/games/snes/mario.sfc",
				Filename:  "mario.sfc",
				Extension: ".sfc",
				Name:      "mario",
			},
		},
		{
			name: "windows_style_path",
			path: `C:/Games/Mario Bros.smc`,
			expected: PathInfo{
				Path:      `C:/Games/Mario Bros.smc`,
				Filename:  "Mario Bros.smc",
				Extension: ".smc",
				Name:      "Mario Bros",
			},
		},
		{
			name: "no_extension",
			path: "/roms/mario",
			expected: PathInfo{
				Path:      "/roms/mario",
				Filename:  "mario",
				Extension: "",
				Name:      "mario",
			},
		},
		{
			name: "current_directory_file",
			path: "game.rom",
			expected: PathInfo{
				Path:      "game.rom",
				Filename:  "game.rom",
				Extension: ".rom",
				Name:      "game",
			},
		},
		{
			name: "empty_path",
			path: "",
			expected: PathInfo{
				Path:      "",
				Filename:  ".",
				Extension: "",
				Name:      ".",
			},
		},
		{
			name: "path_with_spaces",
			path: "/games/arcade/Street Fighter II.zip",
			expected: PathInfo{
				Path:      "/games/arcade/Street Fighter II.zip",
				Filename:  "Street Fighter II.zip",
				Extension: ".zip",
				Name:      "Street Fighter II",
			},
		},
		{
			name: "nested_path",
			path: "/home/user/roms/nes/classics/super_mario_bros.nes",
			expected: PathInfo{
				Path:      "/home/user/roms/nes/classics/super_mario_bros.nes",
				Filename:  "super_mario_bros.nes",
				Extension: ".nes",
				Name:      "super_mario_bros",
			},
		},
		{
			name: "multiple_dots_in_name",
			path: "/games/v1.2.final.rom",
			expected: PathInfo{
				Path:      "/games/v1.2.final.rom",
				Filename:  "v1.2.final.rom",
				Extension: ".rom",
				Name:      "v1.2.final",
			},
		},
		// Virtual paths with URL decoding
		{
			name: "steam_virtual_path_with_url_encoding",
			path: "steam://123/Super%20Hot%2FCold",
			expected: PathInfo{
				Path:      "steam://123/Super%20Hot%2FCold",
				Filename:  "Super Hot/Cold",
				Extension: "",
				Name:      "Super Hot/Cold",
			},
		},
		{
			name: "kodi_movie_with_url_encoding",
			path: "kodi-movie://456/The%20Matrix",
			expected: PathInfo{
				Path:      "kodi-movie://456/The%20Matrix",
				Filename:  "The Matrix",
				Extension: "",
				Name:      "The Matrix",
			},
		},
		{
			name: "flashpoint_with_special_chars",
			path: "flashpoint://789/Game%20%28USA%29",
			expected: PathInfo{
				Path:      "flashpoint://789/Game%20%28USA%29",
				Filename:  "Game (USA)",
				Extension: "",
				Name:      "Game (USA)",
			},
		},
		{
			name: "http_with_url_encoding",
			path: "http://example.com/games/My%20Game.zip",
			expected: PathInfo{
				Path:      "http://example.com/games/My%20Game.zip",
				Filename:  "My Game.zip",
				Extension: ".zip",
				Name:      "My Game",
			},
		},
		{
			name: "https_with_url_encoding",
			path: "https://server.com/path/File%20Name.iso",
			expected: PathInfo{
				Path:      "https://server.com/path/File%20Name.iso",
				Filename:  "File Name.iso",
				Extension: ".iso",
				Name:      "File Name",
			},
		},
		{
			name: "steam_without_url_encoding",
			path: "steam://123/SimpleGameName",
			expected: PathInfo{
				Path:      "steam://123/SimpleGameName",
				Filename:  "SimpleGameName",
				Extension: "",
				Name:      "SimpleGameName",
			},
		},
		{
			name: "file_scheme_no_decoding",
			path: "file:///path/My%20Game.rom",
			expected: PathInfo{
				Path:      "file:///path/My%20Game.rom",
				Filename:  "My%20Game.rom",
				Extension: ".rom",
				Name:      "My%20Game",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetPathInfo(tt.path)
			assert.Equal(t, tt.expected, result, "GetPathInfo result mismatch")
		})
	}
}

func TestGetPathDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty path",
			path:     "",
			expected: ".",
		},
		{
			name:     "unix absolute path",
			path:     "/home/user/file.txt",
			expected: "/home/user",
		},
		{
			name:     "unix relative path",
			path:     "dir/file.txt",
			expected: "dir",
		},
		{
			name:     "windows absolute path with backslashes",
			path:     "C:\\Users\\user\\file.txt",
			expected: "C:\\Users\\user",
		},
		{
			name:     "windows absolute path with forward slashes",
			path:     "C:/Users/user/file.txt",
			expected: "C:/Users/user",
		},
		{
			name:     "mixed separators",
			path:     "C:\\Users/user\\file.txt",
			expected: "C:\\Users/user",
		},
		{
			name:     "root directory unix",
			path:     "/file.txt",
			expected: "/",
		},
		{
			name:     "root directory windows backslash",
			path:     "\\file.txt",
			expected: "\\",
		},
		{
			name:     "current directory file",
			path:     "file.txt",
			expected: ".",
		},
		{
			name:     "no extension",
			path:     "/home/user/filename",
			expected: "/home/user",
		},
		{
			name:     "nested path",
			path:     "/very/deep/nested/directory/structure/file.ext",
			expected: "/very/deep/nested/directory/structure",
		},
		{
			name:     "path with spaces",
			path:     "/home/user/My Documents/file.txt",
			expected: "/home/user/My Documents",
		},
		{
			name:     "UNC path",
			path:     "\\\\server\\share\\file.txt",
			expected: "\\\\server\\share",
		},
		{
			name:     "UNC path with forward slashes",
			path:     "//server/share/file.txt",
			expected: "//server/share",
		},
		{
			name:     "trailing separator preserved",
			path:     "/home/user/dir/",
			expected: "/home/user",
		},
		{
			name:     "multiple consecutive separators",
			path:     "/home//user///file.txt",
			expected: "/home//user//",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := getPathDir(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPathBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty path",
			path:     "",
			expected: ".",
		},
		{
			name:     "unix absolute path",
			path:     "/home/user/file.txt",
			expected: "file.txt",
		},
		{
			name:     "unix relative path",
			path:     "dir/file.txt",
			expected: "file.txt",
		},
		{
			name:     "windows absolute path with backslashes",
			path:     "C:\\Users\\user\\file.txt",
			expected: "file.txt",
		},
		{
			name:     "windows absolute path with forward slashes",
			path:     "C:/Users/user/file.txt",
			expected: "file.txt",
		},
		{
			name:     "mixed separators",
			path:     "C:\\Users/user\\file.txt",
			expected: "file.txt",
		},
		{
			name:     "root directory unix",
			path:     "/file.txt",
			expected: "file.txt",
		},
		{
			name:     "root directory windows backslash",
			path:     "\\file.txt",
			expected: "file.txt",
		},
		{
			name:     "current directory file",
			path:     "file.txt",
			expected: "file.txt",
		},
		{
			name:     "no extension",
			path:     "/home/user/filename",
			expected: "filename",
		},
		{
			name:     "filename with spaces",
			path:     "/home/user/My File.txt",
			expected: "My File.txt",
		},
		{
			name:     "filename with special characters",
			path:     "/home/user/file[1].txt",
			expected: "file[1].txt",
		},
		{
			name:     "multiple dots in filename",
			path:     "/home/user/file.v1.2.final.txt",
			expected: "file.v1.2.final.txt",
		},
		{
			name:     "hidden file unix",
			path:     "/home/user/.hidden",
			expected: ".hidden",
		},
		{
			name:     "directory name only",
			path:     "directory",
			expected: "directory",
		},
		{
			name:     "trailing separator",
			path:     "/home/user/dir/",
			expected: "",
		},
		{
			name:     "only separators",
			path:     "///",
			expected: "",
		},
		{
			name:     "UNC path",
			path:     "\\\\server\\share\\file.txt",
			expected: "file.txt",
		},
		{
			name:     "path with unicode",
			path:     "/home/user/файл.txt",
			expected: "файл.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := getPathBase(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPathExt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "simple extension",
			path:     "/home/user/file.txt",
			expected: ".txt",
		},
		{
			name:     "no extension",
			path:     "/home/user/filename",
			expected: "",
		},
		{
			name:     "multiple dots",
			path:     "/home/user/file.v1.2.final.txt",
			expected: ".txt",
		},
		{
			name:     "hidden file with extension",
			path:     "/home/user/.hidden.txt",
			expected: ".txt",
		},
		{
			name:     "hidden file without extension",
			path:     "/home/user/.hidden",
			expected: "",
		},
		{
			name:     "extension only",
			path:     ".txt",
			expected: "",
		},
		{
			name:     "dot at start of filename",
			path:     "/home/user/.config",
			expected: "",
		},
		{
			name:     "dot at start with extension",
			path:     "/home/user/.bashrc.backup",
			expected: ".backup",
		},
		{
			name:     "uppercase extension",
			path:     "/home/user/file.TXT",
			expected: ".TXT",
		},
		{
			name:     "mixed case extension",
			path:     "/home/user/file.HtMl",
			expected: ".HtMl",
		},
		{
			name:     "long extension",
			path:     "/home/user/file.extension",
			expected: ".extension",
		},
		{
			name:     "numeric extension",
			path:     "/home/user/file.123",
			expected: ".123",
		},
		{
			name:     "special chars in extension",
			path:     "/home/user/file.txt~",
			expected: ".txt~",
		},
		{
			name:     "windows path",
			path:     "C:\\Users\\user\\file.doc",
			expected: ".doc",
		},
		{
			name:     "current directory file",
			path:     "file.txt",
			expected: ".txt",
		},
		{
			name:     "file ending with dot",
			path:     "/home/user/file.",
			expected: ".",
		},
		{
			name:     "multiple consecutive dots",
			path:     "/home/user/file..txt",
			expected: ".txt",
		},
		{
			name:     "special case: dot file (should return empty)",
			path:     ".",
			expected: "",
		},
		{
			name:     "special case: double dot",
			path:     "..",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := getPathExt(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Note: PathIsLauncher tests are in the integration tests (pkg/database/mediascanner)
// to avoid import cycles with the platforms package. The function is thoroughly tested
// through those integration tests which exercise all code paths.

// TestGetPathInfo_VirtualPathEdgeCases tests edge cases in GetPathInfo with virtual paths
// that could cause issues with encoding/decoding, parsing, or display
func TestGetPathInfo_VirtualPathEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected PathInfo
		notes    string
	}{
		{
			name: "query_parameters_in_virtual_path",
			path: "kodi-movie://123/Movie?param=value",
			expected: PathInfo{
				Path:      "kodi-movie://123/Movie?param=value",
				Filename:  "Movie",
				Extension: "",
				Name:      "Movie",
			},
			notes: "Query params are stripped by FilenameFromPath",
		},
		{
			name: "fragment_in_virtual_path",
			path: "steam://456/Game#section",
			expected: PathInfo{
				Path:      "steam://456/Game#section",
				Filename:  "Game#section",
				Extension: "",
				Name:      "Game#section",
			},
			notes: "Fragments should be included in filename",
		},
		{
			name: "nested_virtual_path",
			path: "steam://789/Category/Sub/Game%20Name",
			expected: PathInfo{
				Path:      "steam://789/Category/Sub/Game%20Name",
				Filename:  "Game Name",
				Extension: "",
				Name:      "Game Name",
			},
			notes: "Nested paths should decode last component",
		},
		{
			name: "encoded_slash_in_name",
			path: "kodi-show://111/Show%2FSeason%201",
			expected: PathInfo{
				Path:      "kodi-show://111/Show%2FSeason%201",
				Filename:  "Show/Season 1",
				Extension: "",
				Name:      "Show/Season 1",
			},
			notes: "Encoded slashes (%2F) should decode to / in filename",
		},
		{
			name: "incomplete_percent_encoding",
			path: "kodi-movie://222/Game%",
			expected: PathInfo{
				Path:      "kodi-movie://222/Game%",
				Filename:  "Game%",
				Extension: "",
				Name:      "Game%",
			},
			notes: "Incomplete encoding should fallback to undecoded",
		},
		{
			name: "invalid_percent_hex",
			path: "steam://333/Game%ZZ",
			expected: PathInfo{
				Path:      "steam://333/Game%ZZ",
				Filename:  "Game%ZZ",
				Extension: "",
				Name:      "Game%ZZ",
			},
			notes: "Invalid hex in encoding should fallback gracefully",
		},
		{
			name: "double_encoding",
			path: "kodi-episode://444/Name%2520Here",
			expected: PathInfo{
				Path:      "kodi-episode://444/Name%2520Here",
				Filename:  "Name%20Here",
				Extension: "",
				Name:      "Name%20Here",
			},
			notes: "Double encoding (%2520 = encoded %20) should decode once",
		},
		{
			name: "http_ipv6_address",
			path: "http://[2001:db8::1]/file.zip",
			expected: PathInfo{
				Path:      "http://[2001:db8::1]/file.zip",
				Filename:  "file.zip",
				Extension: ".zip",
				Name:      "file",
			},
			notes: "IPv6 URLs with brackets should parse correctly",
		},
		{
			name: "http_with_port",
			path: "http://server.com:8080/path/File%20Name.zip",
			expected: PathInfo{
				Path:      "http://server.com:8080/path/File%20Name.zip",
				Filename:  "File Name.zip",
				Extension: ".zip",
				Name:      "File Name",
			},
			notes: "HTTP URLs with ports should decode path component",
		},
		{
			name: "http_with_userinfo",
			path: "http://user:pass@server.com/File%20Name.iso",
			expected: PathInfo{
				Path:      "http://user:pass@server.com/File%20Name.iso",
				Filename:  "File Name.iso",
				Extension: ".iso",
				Name:      "File Name",
			},
			notes: "HTTP URLs with userinfo should parse correctly",
		},
		{
			name: "empty_name_section",
			path: "kodi-movie://555/",
			expected: PathInfo{
				Path:      "kodi-movie://555/",
				Filename:  "",
				Extension: "",
				Name:      "",
			},
			notes: "Empty name section with trailing slash should not crash",
		},
		{
			name: "no_name_section",
			path: "kodi-episode://666",
			expected: PathInfo{
				Path:      "kodi-episode://666",
				Filename:  "666",
				Extension: "",
				Name:      "666",
			},
			notes: "No name section should parse for legacy card support",
		},
		{
			name: "mixed_case_scheme",
			path: "Kodi-Movie://777/Title",
			expected: PathInfo{
				Path:      "Kodi-Movie://777/Title",
				Filename:  "Title",
				Extension: "",
				Name:      "Title",
			},
			notes: "Mixed case schemes should still decode (case-insensitive)",
		},
		{
			name: "all_special_chars_encoded",
			path: "steam://888/Game%20%21%40%23%24%25%5E%26%2A%28%29",
			expected: PathInfo{
				Path:      "steam://888/Game%20%21%40%23%24%25%5E%26%2A%28%29",
				Filename:  "Game !@#$%^&*()",
				Extension: "",
				Name:      "Game !@#$%^&*()",
			},
			notes: "All special characters should decode correctly",
		},
		{
			name: "unicode_in_encoding",
			path: "kodi-song://999/Caf%C3%A9",
			expected: PathInfo{
				Path:      "kodi-song://999/Caf%C3%A9",
				Filename:  "Café",
				Extension: "",
				Name:      "Café",
			},
			notes: "UTF-8 encoded characters should decode correctly",
		},
		{
			name: "plus_sign_in_virtual_path",
			path: "steam://100/C%2B%2B%20Programming",
			expected: PathInfo{
				Path:      "steam://100/C%2B%2B%20Programming",
				Filename:  "C++ Programming",
				Extension: "",
				Name:      "C++ Programming",
			},
			notes: "Encoded plus signs should decode to +",
		},
		{
			name: "http_query_and_fragment",
			path: "http://example.com/File%20Name.zip?download=true#start",
			expected: PathInfo{
				Path:      "http://example.com/File%20Name.zip?download=true#start",
				Filename:  "File Name.zip",
				Extension: ".zip",
				Name:      "File Name",
			},
			notes: "HTTP URLs query and fragment are stripped by FilenameFromPath",
		},
		{
			name: "https_no_path",
			path: "https://example.com",
			expected: PathInfo{
				Path:      "https://example.com",
				Filename:  "example.com",
				Extension: "", // URIs don't have extensions
				Name:      "example.com",
			},
			notes: "HTTPS URL with no path should return domain as filename",
		},
		{
			name: "http_with_port_no_path",
			path: "http://server.com:8080",
			expected: PathInfo{
				Path:      "http://server.com:8080",
				Filename:  "server.com:8080",
				Extension: "", // No path = no extension
				Name:      "server.com:8080",
			},
			notes: "HTTP URL with port but no path should not parse extension",
		},
		{
			name: "https_subdomain_no_path",
			path: "https://cdn.example.com",
			expected: PathInfo{
				Path:      "https://cdn.example.com",
				Filename:  "cdn.example.com",
				Extension: "", // No path = no extension
				Name:      "cdn.example.com",
			},
			notes: "HTTPS URL with subdomain but no path should not parse extension",
		},
		{
			name: "http_trailing_slash_no_file",
			path: "http://example.com/",
			expected: PathInfo{
				Path:      "http://example.com/",
				Filename:  "",
				Extension: "", // Trailing slash = directory
				Name:      "",
			},
			notes: "HTTP URL with trailing slash should return empty filename",
		},
		{
			name: "kodi_all_schemes_album",
			path: "kodi-album://200/Album%20Name",
			expected: PathInfo{
				Path:      "kodi-album://200/Album%20Name",
				Filename:  "Album Name",
				Extension: "",
				Name:      "Album Name",
			},
			notes: "kodi-album scheme should decode",
		},
		{
			name: "kodi_all_schemes_artist",
			path: "kodi-artist://201/Artist%20Name",
			expected: PathInfo{
				Path:      "kodi-artist://201/Artist%20Name",
				Filename:  "Artist Name",
				Extension: "",
				Name:      "Artist Name",
			},
			notes: "kodi-artist scheme should decode",
		},
		{
			name: "launchbox_scheme",
			path: "launchbox://lb456/Game%20Title",
			expected: PathInfo{
				Path:      "launchbox://lb456/Game%20Title",
				Filename:  "Game Title",
				Extension: "",
				Name:      "Game Title",
			},
			notes: "launchbox scheme should decode",
		},
		{
			name: "scummvm_scheme_alphanumeric_id",
			path: "scummvm://monkey1/Monkey%20Island",
			expected: PathInfo{
				Path:      "scummvm://monkey1/Monkey%20Island",
				Filename:  "Monkey Island",
				Extension: "",
				Name:      "Monkey Island",
			},
			notes: "scummvm with alphanumeric ID should decode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetPathInfo(tt.path)
			assert.Equal(t, tt.expected.Path, result.Path, "Path mismatch: %s", tt.notes)
			assert.Equal(t, tt.expected.Filename, result.Filename, "Filename mismatch: %s", tt.notes)
			assert.Equal(t, tt.expected.Extension, result.Extension, "Extension mismatch: %s", tt.notes)
			assert.Equal(t, tt.expected.Name, result.Name, "Name mismatch: %s", tt.notes)

			t.Logf("✓ Edge case handled: %s", tt.notes)
		})
	}
}

// TestGetPathInfo_VirtualPathNoPanic tests that malformed virtual paths don't panic
func TestGetPathInfo_VirtualPathNoPanic(t *testing.T) {
	t.Parallel()

	malformedPaths := []string{
		"kodi-movie://",                    // No ID or name
		"steam://123",                      // No name section
		"://123/Name",                      // No scheme
		"invalid scheme://123/Name",        // Invalid scheme (space)
		"kodi-movie://abc/Name",            // Non-numeric ID (but still valid URI)
		"http://",                          // Incomplete HTTP URL
		"https://[invalid]/file",           // Malformed IPv6
		"kodi-movie://123/Name\x00Control", // Control character
	}

	for _, path := range malformedPaths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			assert.NotPanics(t, func() {
				result := GetPathInfo(path)
				t.Logf("Malformed path handled: %q → Filename=%q", path, result.Filename)
			}, "Should not panic on malformed path: %s", path)
		})
	}
}
