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
				Base:      "/games/snes",
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
				Base:      `C:/Games`,
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
				Base:      "/roms",
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
				Base:      ".",
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
				Base:      ".",
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
				Base:      "/games/arcade",
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
				Base:      "/home/user/roms/nes/classics",
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
				Base:      "/games",
				Filename:  "v1.2.final.rom",
				Extension: ".rom",
				Name:      "v1.2.final",
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
