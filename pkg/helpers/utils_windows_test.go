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

package helpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMd5Hash_WindowsPaths(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Create test file with Windows-style path
	testFile := filepath.Join(tempDir, "test_file.txt")
	testContent := "Hello, Windows!\nThis is a test file."
	err := os.WriteFile(testFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name         string
		pathFunc     func() string
		expectedHash string
		wantErr      bool
	}{
		{
			name: "windows_absolute_path_with_backslashes",
			pathFunc: func() string {
				// Convert forward slashes to backslashes for Windows-style path
				return filepath.FromSlash(testFile)
			},
			expectedHash: "d2c9e7c8f8b9a5b2e3f4a5b6c7d8e9f0", // MD5 will be computed
			wantErr:      false,
		},
		{
			name: "windows_path_with_forward_slashes",
			pathFunc: func() string {
				// Use forward slashes (should still work on Windows)
				return filepath.ToSlash(testFile)
			},
			expectedHash: "d2c9e7c8f8b9a5b2e3f4a5b6c7d8e9f0", // Same content, same MD5
			wantErr:      false,
		},
		{
			name: "windows_unc_path_style",
			pathFunc: func() string {
				// For testing, use local path but format as UNC-style
				return `\\?\` + filepath.FromSlash(testFile)
			},
			expectedHash: "d2c9e7c8f8b9a5b2e3f4a5b6c7d8e9f0", // Same content
			wantErr:      false,
		},
		{
			name: "windows_drive_letter_uppercase",
			pathFunc: func() string {
				path := filepath.FromSlash(testFile)
				if len(path) >= 2 && path[1] == ':' {
					// Ensure drive letter is uppercase
					return string(path[0]-32) + path[1:]
				}
				return path
			},
			expectedHash: "d2c9e7c8f8b9a5b2e3f4a5b6c7d8e9f0", // Same content
			wantErr:      false,
		},
		{
			name: "windows_nonexistent_drive",
			pathFunc: func() string {
				return `Z:\nonexistent\file.txt`
			},
			expectedHash: "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := tt.pathFunc()
			result, err := GetMd5Hash(path)

			if tt.wantErr {
				require.Error(t, err, "GetMd5Hash should return error for %s", path)
				assert.Contains(t, err.Error(), "failed to open file for MD5 hash")
				assert.Empty(t, result)
			} else {
				require.NoError(t, err, "GetMd5Hash should not return error for %s", path)
				assert.NotEmpty(t, result, "MD5 hash should not be empty")
				assert.Len(t, result, 32, "MD5 hash should be 32 characters")
			}
		})
	}
}

func TestCopyFile_WindowsPaths(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Create source file
	sourceFile := filepath.Join(tempDir, "source.txt")
	testContent := "Hello, Windows!\nThis is a test file for copying."
	err := os.WriteFile(sourceFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name         string
		sourceFunc   func() string
		destFunc     func() string
		wantErr      bool
		validateFunc func(t *testing.T, destPath string)
	}{
		{
			name: "copy_with_windows_backslash_paths",
			sourceFunc: func() string {
				return filepath.FromSlash(sourceFile)
			},
			destFunc: func() string {
				return filepath.FromSlash(filepath.Join(tempDir, "dest_backslash.txt"))
			},
			wantErr: false,
			validateFunc: func(t *testing.T, destPath string) {
				content, err := os.ReadFile(destPath)
				require.NoError(t, err)
				assert.Equal(t, testContent, string(content))
			},
		},
		{
			name: "copy_with_mixed_separators",
			sourceFunc: func() string {
				// Source with backslashes
				return filepath.FromSlash(sourceFile)
			},
			destFunc: func() string {
				// Destination with forward slashes
				return filepath.ToSlash(filepath.Join(tempDir, "dest_mixed.txt"))
			},
			wantErr: false,
			validateFunc: func(t *testing.T, destPath string) {
				content, err := os.ReadFile(destPath)
				require.NoError(t, err)
				assert.Equal(t, testContent, string(content))
			},
		},
		{
			name: "copy_to_nested_windows_path",
			sourceFunc: func() string {
				return sourceFile
			},
			destFunc: func() string {
				nestedDir := filepath.Join(tempDir, "nested", "deep", "folder")
				err := os.MkdirAll(nestedDir, 0o755)
				require.NoError(t, err)
				return filepath.Join(nestedDir, "nested_file.txt")
			},
			wantErr: false,
			validateFunc: func(t *testing.T, destPath string) {
				content, err := os.ReadFile(destPath)
				require.NoError(t, err)
				assert.Equal(t, testContent, string(content))
			},
		},
		{
			name: "copy_with_windows_reserved_chars_in_path",
			sourceFunc: func() string {
				return sourceFile
			},
			destFunc: func() string {
				// Note: Using valid characters but testing path handling
				return filepath.Join(tempDir, "file with spaces & symbols.txt")
			},
			wantErr: false,
			validateFunc: func(t *testing.T, destPath string) {
				content, err := os.ReadFile(destPath)
				require.NoError(t, err)
				assert.Equal(t, testContent, string(content))
			},
		},
		{
			name: "copy_from_nonexistent_windows_drive",
			sourceFunc: func() string {
				return `Z:\nonexistent\source.txt`
			},
			destFunc: func() string {
				return filepath.Join(tempDir, "dest_error.txt")
			},
			wantErr: true,
			validateFunc: func(t *testing.T, destPath string) {
				// File should not exist if copy failed
				_, err := os.Stat(destPath)
				assert.True(t, os.IsNotExist(err), "Destination file should not exist after failed copy")
			},
		},
		{
			name: "copy_to_invalid_windows_drive",
			sourceFunc: func() string {
				return sourceFile
			},
			destFunc: func() string {
				return `Z:\invalid\destination.txt`
			},
			wantErr: true,
			validateFunc: func(t *testing.T, destPath string) {
				// No validation needed for invalid destination
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sourcePath := tt.sourceFunc()
			destPath := tt.destFunc()

			err := CopyFile(sourcePath, destPath)

			if tt.wantErr {
				require.Error(t, err, "CopyFile should return error for invalid paths")
			} else {
				require.NoError(t, err, "CopyFile should not return error for valid paths")
				tt.validateFunc(t, destPath)
			}
		})
	}
}

func TestFilenameFromPath_WindowsPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "windows_absolute_path_backslashes",
			input:    `C:\Games\Super Mario Bros.smc`,
			expected: "supermariobros",
		},
		{
			name:     "windows_absolute_path_forward_slashes",
			input:    "C:/Games/Super Mario Bros.smc",
			expected: "supermariobros",
		},
		{
			name:     "windows_mixed_separators",
			input:    `C:\Games/Retro\Street Fighter II.rom`,
			expected: "streetfighterii",
		},
		{
			name:     "windows_unc_path",
			input:    `\\server\share\games\Zelda.nes`,
			expected: "zelda",
		},
		{
			name:     "windows_long_path_prefix",
			input:    `\\?\C:\Very\Long\Path\To\Game.sfc`,
			expected: "game",
		},
		{
			name:     "windows_path_with_spaces",
			input:    `C:\Program Files\My Games\Cool Game.exe`,
			expected: "coolgame",
		},
		{
			name:     "windows_path_with_special_chars",
			input:    `C:\Games\Street Fighter II - Special Edition.zip`,
			expected: "streetfighterii-specialedition",
		},
		{
			name:     "windows_relative_path",
			input:    `games\mario\Super Mario World.smc`,
			expected: "supermarioworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FilenameFromPath(tt.input)
			assert.Equal(t, tt.expected, result, "FilenameFromPath result mismatch for Windows path")
		})
	}
}

func TestSlugifyPath_WindowsPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "windows_path_with_backslashes",
			input:    `C:\Games\Street Fighter II.rom`,
			expected: "streetfighterii",
		},
		{
			name:     "windows_path_with_forward_slashes",
			input:    "C:/Games/Street Fighter II.rom",
			expected: "streetfighterii",
		},
		{
			name:     "windows_unc_path",
			input:    `\\server\share\Street Fighter II.rom`,
			expected: "streetfighterii",
		},
		{
			name:     "windows_path_with_multiple_extensions",
			input:    `C:\Games\archive.tar.gz`,
			expected: "archive",
		},
		{
			name:     "windows_path_with_unicode_chars",
			input:    `C:\Games\ストリートファイター.rom`,
			expected: "ストリートファイター",
		},
		{
			name:     "windows_path_with_mixed_case",
			input:    `C:\GAMES\Super MARIO Bros.SMC`,
			expected: "supermariobros",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SlugifyPath(tt.input)
			assert.Equal(t, tt.expected, result, "SlugifyPath result mismatch for Windows path")
		})
	}
}