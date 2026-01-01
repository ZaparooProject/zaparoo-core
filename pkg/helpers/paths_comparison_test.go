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
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizePathForComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		expectedWindows string
		expectedUnix    string
	}{
		{
			name:            "forward slashes",
			input:           "C:/RetroBat/roms/snes/game.sfc",
			expectedWindows: "c:/retrobat/roms/snes/game.sfc",
			expectedUnix:    "c:/retrobat/roms/snes/game.sfc",
		},
		{
			name:            "backslashes on windows",
			input:           `C:\RetroBat\roms\snes\game.sfc`,
			expectedWindows: "c:/retrobat/roms/snes/game.sfc",
			expectedUnix:    `c:\retrobat\roms\snes\game.sfc`, // Unix: backslashes are filename chars, preserved as-is
		},
		{
			name:            "mixed slashes",
			input:           `C:/RetroBat\roms/snes\game.sfc`,
			expectedWindows: "c:/retrobat/roms/snes/game.sfc",
			expectedUnix:    `c:/retrobat\roms/snes\game.sfc`, // Unix: backslashes preserved
		},
		{
			name:            "trailing slash",
			input:           "C:/RetroBat/roms/",
			expectedWindows: "c:/retrobat/roms",
			expectedUnix:    "c:/retrobat/roms",
		},
		{
			name:            "dot segments",
			input:           "C:/RetroBat/./roms/../roms/game.sfc",
			expectedWindows: "c:/retrobat/roms/game.sfc",
			expectedUnix:    "c:/retrobat/roms/game.sfc",
		},
		{
			name:            "uppercase normalized to lowercase",
			input:           "C:/RETROBAT/ROMS/SNES/GAME.SFC",
			expectedWindows: "c:/retrobat/roms/snes/game.sfc",
			expectedUnix:    "c:/retrobat/roms/snes/game.sfc",
		},
		{
			name:            "unix absolute path",
			input:           "/home/user/RetroBat/roms/snes/game.sfc",
			expectedWindows: "/home/user/retrobat/roms/snes/game.sfc",
			expectedUnix:    "/home/user/retrobat/roms/snes/game.sfc",
		},
		{
			name:            "relative path",
			input:           "roms/snes/game.sfc",
			expectedWindows: "roms/snes/game.sfc",
			expectedUnix:    "roms/snes/game.sfc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizePathForComparison(tt.input)

			if runtime.GOOS == "windows" {
				assert.Equal(t, tt.expectedWindows, result, "Windows normalization failed")
			} else {
				assert.Equal(t, tt.expectedUnix, result, "Unix normalization failed")
			}
		})
	}
}

func TestPathHasPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		root     string
		expected bool
	}{
		{
			name:     "exact match",
			path:     "C:/RetroBat/roms/snes",
			root:     "C:/RetroBat/roms/snes",
			expected: true,
		},
		{
			name:     "path inside root",
			path:     "C:/RetroBat/roms/snes/game.sfc",
			root:     "C:/RetroBat/roms/snes",
			expected: true,
		},
		{
			name:     "path NOT inside root (prefix bug case)",
			path:     "C:/RetroBat/roms/snes2/game.sfc",
			root:     "C:/RetroBat/roms/snes",
			expected: false,
		},
		{
			name:     "path NOT inside root (different branch)",
			path:     "C:/RetroBat/bios/system.bin",
			root:     "C:/RetroBat/roms/snes",
			expected: false,
		},
		{
			name:     "mixed slashes - path with backslashes",
			path:     `C:\RetroBat\roms\snes\game.sfc`,
			root:     "C:/RetroBat/roms/snes",
			expected: runtime.GOOS == "windows", // Only works on Windows (Linux preserves backslashes)
		},
		{
			name:     "mixed slashes - root with backslashes",
			path:     "C:/RetroBat/roms/snes/game.sfc",
			root:     `C:\RetroBat\roms\snes`,
			expected: runtime.GOOS == "windows", // Only works on Windows (Linux preserves backslashes)
		},
		{
			name:     "root with trailing slash",
			path:     "C:/RetroBat/roms/snes/game.sfc",
			root:     "C:/RetroBat/roms/snes/",
			expected: true,
		},
		{
			name:     "case difference should match (case-insensitive comparison)",
			path:     "C:/RETROBAT/ROMS/SNES/game.sfc",
			root:     "c:/retrobat/roms/snes",
			expected: true,
		},
		{
			name:     "case difference on unix paths should match (case-insensitive comparison)",
			path:     "/home/user/RETROBAT/ROMS/SNES/game.sfc",
			root:     "/home/user/retrobat/roms/snes",
			expected: true,
		},
		{
			name:     "deeply nested path",
			path:     "C:/RetroBat/roms/snes/subdir1/subdir2/game.sfc",
			root:     "C:/RetroBat/roms/snes",
			expected: true,
		},
		{
			name:     "similar prefix but different directory (megadrive vs megadrive2)",
			path:     "C:/RetroBat/roms/megadrive2/game.bin",
			root:     "C:/RetroBat/roms/megadrive",
			expected: false,
		},
		{
			name:     "empty root",
			path:     "C:/RetroBat/roms/snes/game.sfc",
			root:     "",
			expected: false, // Empty root doesn't match non-empty paths
		},
		{
			name:     "empty path",
			path:     "",
			root:     "C:/RetroBat/roms/snes",
			expected: false,
		},
		{
			name:     "both empty",
			path:     "",
			root:     "",
			expected: true, // Exact match
		},
		{
			name:     "dot segments in path",
			path:     "C:/RetroBat/roms/./snes/../snes/game.sfc",
			root:     "C:/RetroBat/roms/snes",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := PathHasPrefix(tt.path, tt.root)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPathHasPrefixPrefixBug(t *testing.T) {
	t.Parallel()

	// This is the critical bug we're fixing: "roms" should NOT match "roms2"
	testCases := []struct {
		root     string
		path     string
		expected bool
	}{
		{"C:/RetroBat/roms", "C:/RetroBat/roms/game.sfc", true},
		{"C:/RetroBat/roms", "C:/RetroBat/roms2/game.sfc", false},
		{"C:/RetroBat/roms", "C:/RetroBat/roms-backup/game.sfc", false},
		{"/vol", "/vol/data/file.txt", true},
		{"/vol", "/volcano/data/file.txt", false},
		{"C:/Program Files/App", "C:/Program Files/App/game.exe", true},
		{"C:/Program Files/App", "C:/Program Files/App2/game.exe", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			result := PathHasPrefix(tc.path, tc.root)
			assert.Equal(t, tc.expected, result,
				"PathHasPrefix(%q, %q) should be %v", tc.path, tc.root, tc.expected)
		})
	}
}

func TestPathHasPrefixWindowsSlashMismatch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	t.Parallel()

	// This tests the original bug: database paths have forward slashes,
	// but filepath.Join creates backslashes on Windows
	testCases := []struct {
		name     string
		path     string // From database (forward slashes)
		root     string // From filepath.Join (backslashes on Windows)
		expected bool
	}{
		{
			name:     "database path vs filepath.Join root",
			path:     "C:/RetroBat/roms/snes/game.sfc",
			root:     `C:\RetroBat\roms\snes`,
			expected: true,
		},
		{
			name:     "both database format",
			path:     "C:/RetroBat/roms/snes/game.sfc",
			root:     "C:/RetroBat/roms/snes",
			expected: true,
		},
		{
			name:     "both Windows format",
			path:     `C:\RetroBat\roms\snes\game.sfc`,
			root:     `C:\RetroBat\roms\snes`,
			expected: true,
		},
		{
			name:     "completely mixed slashes",
			path:     `C:\RetroBat/roms\snes/game.sfc`,
			root:     `C:/RetroBat\roms/snes`,
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := PathHasPrefix(tc.path, tc.root)
			assert.Equal(t, tc.expected, result)
		})
	}
}
