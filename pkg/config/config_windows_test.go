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

//go:build windows

package config

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckAllow_WindowsPathNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		allow    []string
		allowRe  []*regexp.Regexp
		expected bool
	}{
		{
			name:     "windows_forward_slash_normalized_to_backslash",
			allow:    []string{`C:\\root\\.*\.lnk`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\root\\.*\.lnk`)},
			input:    "C:/root/notepad.lnk",
			expected: true, // Should match after normalization on Windows
		},
		{
			name:     "windows_mixed_separators_normalized",
			allow:    []string{`C:\\Users\\.*\\Desktop\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Users\\.*\\Desktop\\.*`)},
			input:    "C:/Users/test/Desktop/file.exe",
			expected: true, // Should match after normalization
		},
		{
			name:     "windows_complex_path_with_spaces",
			allow:    []string{`C:\\Program Files\\.*\\.*\.exe`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Program Files\\.*\\.*\.exe`)},
			input:    "C:/Program Files/My App/app.exe",
			expected: true, // Should match after normalization
		},
		{
			name:     "windows_unc_path_normalization",
			allow:    []string{`\\\\server\\share\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`\\\\server\\share\\.*`)},
			input:    "//server/share/file.txt",
			expected: true, // UNC paths should be normalized
		},
		{
			name:     "windows_drive_letter_case_insensitive",
			allow:    []string{`C:\\Games\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Games\\.*`)},
			input:    "c:/Games/mario.smc",
			expected: true, // Should match regardless of drive letter case
		},
		{
			name:     "windows_long_path_prefix",
			allow:    []string{`C:\\Games\\.*\.rom`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Games\\.*\.rom`)},
			input:    `\\?\C:/Games/zelda.rom`,
			expected: true, // Long path prefix should be handled
		},
		{
			name:     "windows_path_no_normalization_needed_backslash",
			allow:    []string{`C:\\root\\.*\.lnk`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\root\\.*\.lnk`)},
			input:    `C:\root\notepad.lnk`,
			expected: true, // Direct match, no normalization needed
		},
		{
			name:     "windows_path_mismatch_after_normalization",
			allow:    []string{`C:\\Games\\.*\.exe`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Games\\.*\.exe`)},
			input:    "D:/Games/app.exe", // Different drive
			expected: false, // Should not match even after normalization
		},
		{
			name:     "windows_relative_path_no_normalization",
			allow:    []string{`games\\.*\.rom`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`games\\.*\.rom`)},
			input:    "games/mario.rom", // Relative path, not Windows-style
			expected: false, // No normalization for non-Windows-style paths
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := checkAllow(tt.allow, tt.allowRe, tt.input)
			assert.Equal(t, tt.expected, result,
				"Windows path normalization failed for input: %s, pattern: %s",
				tt.input, tt.allow[0])
		})
	}
}

func TestIsWindowsStylePath_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "windows_drive_with_colon_only",
			path:     "C:",
			expected: true,
		},
		{
			name:     "windows_drive_with_dot",
			path:     "C:.",
			expected: true,
		},
		{
			name:     "windows_drive_with_dotdot",
			path:     "C:..",
			expected: true,
		},
		{
			name:     "windows_lowercase_drive_with_path",
			path:     "z:\\temp\\file.txt",
			expected: true,
		},
		{
			name:     "windows_unc_minimal",
			path:     "\\\\a",
			expected: true, // UNC paths can be minimal
		},
		{
			name:     "windows_forward_slash_unc",
			path:     "//server",
			expected: true,
		},
		{
			name:     "windows_long_path_prefix_minimal",
			path:     "\\\\?\\C:",
			expected: true,
		},
		{
			name:     "windows_long_unc_path_prefix",
			path:     "\\\\?\\UNC\\server\\share",
			expected: true,
		},
		{
			name:     "not_windows_colon_in_middle",
			path:     "/home/user:group/file",
			expected: false,
		},
		{
			name:     "not_windows_single_backslash",
			path:     "\\file",
			expected: false, // Single backslash is not UNC
		},
		{
			name:     "not_windows_forward_slash_only",
			path:     "/server/share",
			expected: false, // Unix-style path
		},
		{
			name:     "windows_drive_number_invalid",
			path:     "9:\\path",
			expected: false, // Numbers are not valid drive letters
		},
		{
			name:     "windows_drive_symbol_invalid",
			path:     "@:\\path",
			expected: false, // Symbols are not valid drive letters
		},
		{
			name:     "edge_case_almost_unc",
			path:     "\\server\\share", // Only two backslashes at start, not four
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isWindowsStylePath(tt.path)
			assert.Equal(t, tt.expected, result,
				"isWindowsStylePath edge case failed for: %s", tt.path)
		})
	}
}

func TestCheckAllow_WindowsSpecialPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		allow    []string
		allowRe  []*regexp.Regexp
		expected bool
	}{
		{
			name:     "windows_system32_path",
			allow:    []string{`C:\\Windows\\System32\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Windows\\System32\\.*`)},
			input:    "C:/Windows/System32/notepad.exe",
			expected: true,
		},
		{
			name:     "windows_program_files_x86",
			allow:    []string{`C:\\Program Files \(x86\)\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Program Files \(x86\)\\.*`)},
			input:    "C:/Program Files (x86)/Steam/steam.exe",
			expected: true,
		},
		{
			name:     "windows_users_appdata",
			allow:    []string{`C:\\Users\\.*\\AppData\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Users\\.*\\AppData\\.*`)},
			input:    "C:/Users/John/AppData/Local/app.exe",
			expected: true,
		},
		{
			name:     "windows_temp_directory",
			allow:    []string{`C:\\Windows\\Temp\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`C:\\Windows\\Temp\\.*`)},
			input:    "C:/Windows/Temp/tempfile.tmp",
			expected: true,
		},
		{
			name:     "windows_network_drive",
			allow:    []string{`Z:\\.*`},
			allowRe:  []*regexp.Regexp{regexp.MustCompile(`Z:\\.*`)},
			input:    "Z:/shared/documents/file.doc",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := checkAllow(tt.allow, tt.allowRe, tt.input)
			assert.Equal(t, tt.expected, result,
				"Windows special path test failed for input: %s", tt.input)
		})
	}
}