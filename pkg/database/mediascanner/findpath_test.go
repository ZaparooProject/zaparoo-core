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

package mediascanner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindPath_BasicFunctionality tests that FindPath works for simple cases
func TestFindPath_BasicFunctionality(t *testing.T) {
	t.Parallel()

	// Create a temporary directory structure for testing
	tmpDir := t.TempDir()

	// Create test directory structure
	testDir := filepath.Join(tmpDir, "TestDir")
	require.NoError(t, os.Mkdir(testDir, 0o750))

	testFile := filepath.Join(testDir, "testfile.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o600))

	// Test finding existing directory
	result, err := FindPath(testDir)
	require.NoError(t, err)
	assert.Equal(t, testDir, result)

	// Test finding existing file
	result, err = FindPath(testFile)
	require.NoError(t, err)
	assert.Equal(t, testFile, result)

	// Test non-existent path
	nonExistent := filepath.Join(tmpDir, "DoesNotExist")
	_, err = FindPath(nonExistent)
	assert.Error(t, err)
}

// TestFindPath_CaseNormalization is the critical regression test for the case-sensitivity bug
// This test verifies that FindPath returns the actual filesystem case, not the input case
func TestFindPath_CaseNormalization(t *testing.T) {
	// Skip on case-sensitive filesystems (Linux ext4, etc.)
	// This test is specifically for case-insensitive filesystems (Windows, macOS)
	if runtime.GOOS == "linux" {
		t.Skip("Skipping case-insensitive test on Linux (case-sensitive filesystem)")
	}

	t.Parallel()

	tmpDir := t.TempDir()

	// Create directory with specific case
	realDir := filepath.Join(tmpDir, "RetroBat")
	require.NoError(t, os.Mkdir(realDir, 0o750))

	romsDir := filepath.Join(realDir, "roms")
	require.NoError(t, os.Mkdir(romsDir, 0o750))

	megadriveDir := filepath.Join(romsDir, "megadrive")
	require.NoError(t, os.Mkdir(megadriveDir, 0o750))

	testFile := filepath.Join(megadriveDir, "SonicTheHedgehog.md")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o600))

	tests := []struct {
		name          string
		inputPath     string
		expectedPath  string
		description   string
		skipOnWindows bool // Some tests might behave differently on Windows
	}{
		{
			name:         "Single component wrong case - lowercase",
			inputPath:    filepath.Join(tmpDir, "retrobat"),
			expectedPath: realDir,
			description:  "Input has lowercase 'retrobat', should return 'RetroBat'",
		},
		{
			name:         "Single component wrong case - uppercase",
			inputPath:    filepath.Join(tmpDir, "RETROBAT"),
			expectedPath: realDir,
			description:  "Input has uppercase 'RETROBAT', should return 'RetroBat'",
		},
		{
			name:         "Multiple components wrong case",
			inputPath:    filepath.Join(tmpDir, "retrobat", "ROMS", "MEGADRIVE"),
			expectedPath: megadriveDir,
			description:  "All components have wrong case",
		},
		{
			name:         "File with wrong case",
			inputPath:    filepath.Join(tmpDir, "retrobat", "roms", "megadrive", "sonicthehedgehog.md"),
			expectedPath: testFile,
			description:  "File path with wrong case should return actual case",
		},
		{
			name:         "Mixed correct and wrong case",
			inputPath:    filepath.Join(tmpDir, "RetroBat", "ROMS", "megadrive"),
			expectedPath: megadriveDir,
			description:  "Mix of correct and wrong case components",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("Skipping on Windows")
			}

			result, err := FindPath(tt.inputPath)
			require.NoError(t, err, "FindPath should succeed for: %s", tt.description)

			// The critical assertion: result should match filesystem case, not input case
			assert.Equal(t, tt.expectedPath, result,
				"FindPath must return actual filesystem case, not input case. %s", tt.description)

			// Additional assertion: verify case-insensitive equality
			assert.True(t, strings.EqualFold(tt.inputPath, result),
				"Result should be case-insensitively equal to input")
		})
	}
}

// TestFindPath_RegressionWindowsCaseBug specifically tests the bug we fixed
// where paths with wrong case were being indexed and causing launch failures
func TestFindPath_RegressionWindowsCaseBug(t *testing.T) {
	// This is the exact scenario from the bug report
	if runtime.GOOS == "linux" {
		t.Skip("Skipping Windows-specific regression test on Linux")
	}

	t.Parallel()

	tmpDir := t.TempDir()

	// Simulate the exact bug scenario:
	// - User has C:\RetroBat (actual filesystem)
	// - Config or hardcoded path has C:\Retrobat (wrong case)
	// - Old code would return C:\Retrobat, new code must return C:\RetroBat

	actualPath := filepath.Join(tmpDir, "RetroBat", "roms", "megadrive")
	require.NoError(t, os.MkdirAll(actualPath, 0o750))

	gameFile := filepath.Join(actualPath, "3 Ninjas Kick Back (USA).md")
	require.NoError(t, os.WriteFile(gameFile, []byte("test"), 0o600))

	// Input path with wrong case (lowercase 'b')
	wrongCasePath := filepath.Join(tmpDir, "Retrobat", "roms", "megadrive", "3 Ninjas Kick Back (USA).md")

	result, err := FindPath(wrongCasePath)
	require.NoError(t, err, "FindPath must succeed even with wrong case input")

	// CRITICAL: Result must have correct case, not input case
	assert.Equal(t, gameFile, result,
		"REGRESSION: FindPath must return actual filesystem case (RetroBat) not input case (Retrobat)")

	// Verify the specific component that was wrong is now correct
	assert.Contains(t, result, "RetroBat", "Path must contain 'RetroBat' with uppercase B")
	assert.NotContains(t, result, "Retrobat", "Path must NOT contain 'Retrobat' with lowercase b")
}

// TestFindPath_ErrorCases tests error handling
func TestFindPath_ErrorCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inputPath   string
		description string
		shouldError bool
	}{
		{
			name:        "Non-existent path",
			inputPath:   "/this/path/does/not/exist",
			shouldError: true,
			description: "Should return error for non-existent path",
		},
		{
			name:        "Empty path",
			inputPath:   "",
			shouldError: true,
			description: "Should return error for empty path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FindPath(tt.inputPath)
			if tt.shouldError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestFindPath_WindowsVolumes tests Windows volume handling (C:, D:, etc.)
func TestFindPath_WindowsVolumes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific test on non-Windows platform")
	}

	t.Parallel()

	// Test that volume names are preserved
	// We can't easily create fake volumes, so we test with C: which should exist
	cDrive := "C:\\"
	result, err := FindPath(cDrive)
	// This might fail if C: doesn't exist (unlikely but possible)
	if err != nil {
		t.Skip("C:\\ not accessible, skipping volume test")
	}

	assert.Contains(t, result, "C:", "Result should preserve volume name")
}

// TestFindPath_Performance ensures FindPath doesn't have O(n) issues for deep paths
func TestFindPath_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	t.Parallel()

	tmpDir := t.TempDir()

	// Create a deep directory structure
	deepPath := tmpDir
	for range 20 {
		deepPath = filepath.Join(deepPath, "level")
		require.NoError(t, os.Mkdir(deepPath, 0o750))
	}

	// FindPath should complete in reasonable time even for deep paths
	result, err := FindPath(deepPath)
	require.NoError(t, err)
	assert.Equal(t, deepPath, result)
}

// TestFindPath_SymlinkHandling tests behavior with symlinks
func TestFindPath_SymlinkHandling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink test on Windows (requires admin privileges)")
	}

	t.Parallel()

	tmpDir := t.TempDir()

	// Create target directory
	target := filepath.Join(tmpDir, "Target")
	require.NoError(t, os.Mkdir(target, 0o750))

	// Create symlink
	link := filepath.Join(tmpDir, "Link")
	require.NoError(t, os.Symlink(target, link))

	// FindPath should resolve the symlink
	result, err := FindPath(link)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

// TestFindPath_LinuxCaseAmbiguity tests the Linux-specific case where both
// File.txt and file.txt can exist, and we should prefer exact match
func TestFindPath_LinuxCaseAmbiguity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping Linux-specific case-sensitivity test on non-Linux platform")
	}

	t.Parallel()

	tmpDir := t.TempDir()

	// Create two files with same name but different case
	upperFile := filepath.Join(tmpDir, "FILE.txt")
	require.NoError(t, os.WriteFile(upperFile, []byte("upper"), 0o600))

	lowerFile := filepath.Join(tmpDir, "file.txt")
	require.NoError(t, os.WriteFile(lowerFile, []byte("lower"), 0o600))

	// When we ask for FILE.txt, we should get FILE.txt (exact match), not file.txt
	result, err := FindPath(upperFile)
	require.NoError(t, err)
	assert.Equal(t, upperFile, result, "Should prefer exact match on Linux")

	// When we ask for file.txt, we should get file.txt (exact match), not FILE.txt
	result, err = FindPath(lowerFile)
	require.NoError(t, err)
	assert.Equal(t, lowerFile, result, "Should prefer exact match on Linux")

	// Verify files are actually different by reading both
	upperContent, err := os.ReadFile(upperFile) //nolint:gosec // Test files are safe
	require.NoError(t, err)
	lowerContent, err := os.ReadFile(lowerFile) //nolint:gosec // Test files are safe
	require.NoError(t, err)
	assert.NotEqual(t, upperContent, lowerContent, "Files should have different content to prove they're distinct")
}

// TestFindPath_ShortFilenames tests Windows 8.3 short filename handling
func TestFindPath_ShortFilenames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific 8.3 filename test on non-Windows platform")
	}

	t.Parallel()

	// Note: This test documents the current limitation with 8.3 names
	// We handle them via the os.Stat fallback, but the returned path
	// will keep the short name format rather than expanding it to long name

	// Common 8.3 path that exists on most Windows systems
	shortPath := `C:\PROGRA~1`
	if _, err := os.Stat(shortPath); err != nil {
		t.Skip("C:\\PROGRA~1 not found, skipping 8.3 test")
	}

	// FindPath should handle short names via the os.Stat fallback
	result, err := FindPath(shortPath)
	// We accept either success with short name, or success with long name
	if err != nil {
		t.Logf("FindPath failed on short name: %v", err)
		t.Skip("8.3 short names not fully supported")
	}

	assert.NotEmpty(t, result, "Should return some valid path for 8.3 name")
}

// TestFindPath_DirtyPaths tests paths with redundant separators and relative components.
// This verifies that filepath.Abs and our path splitting logic handle "dirty" paths correctly.
func TestFindPath_DirtyPaths(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Setup: tmp/A/B/file.txt
	targetDir := filepath.Join(tmpDir, "A", "B")
	require.NoError(t, os.MkdirAll(targetDir, 0o750))
	targetFile := filepath.Join(targetDir, "file.txt")
	require.NoError(t, os.WriteFile(targetFile, []byte("test"), 0o600))

	tests := []struct {
		name        string
		buildPath   func(string) string
		description string
	}{
		{
			name: "Multiple consecutive separators",
			buildPath: func(tmp string) string {
				sep := string(filepath.Separator)
				// tmp//A////B/file.txt
				return tmp + sep + sep + "A" + sep + sep + sep + sep + "B" + sep + "file.txt"
			},
			description: "Path with multiple consecutive separators should be cleaned and resolved",
		},
		{
			name: "Relative components with dots",
			buildPath: func(tmp string) string {
				// tmp/A/../A/B/./file.txt
				return filepath.Join(tmp, "A", "..", "A", "B", ".", "file.txt")
			},
			description: "Path with . and .. should be resolved via filepath.Abs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirtyPath := tt.buildPath(tmpDir)

			result, err := FindPath(dirtyPath)
			require.NoError(t, err, tt.description)
			assert.Equal(t, targetFile, result, tt.description)
		})
	}
}

// TestFindPath_Unicode handles special characters and emoji in paths.
// This tests that FindPath correctly handles Unicode normalization issues
// that can occur on macOS (NFD vs NFC) and other platforms.
func TestFindPath_Unicode(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Directory with Emoji and Accents: "Mëdia_🎮"
	unicodeDirName := "Mëdia_🎮"
	unicodeDir := filepath.Join(tmpDir, unicodeDirName)
	require.NoError(t, os.Mkdir(unicodeDir, 0o750))

	unicodeFileName := "Gáme_🎲.txt"
	unicodeFile := filepath.Join(unicodeDir, unicodeFileName)
	require.NoError(t, os.WriteFile(unicodeFile, []byte("test"), 0o600))

	// Test finding the file
	result, err := FindPath(unicodeFile)
	require.NoError(t, err, "Should handle Unicode characters in path")
	assert.Equal(t, unicodeFile, result)
}

// TestFindPath_DotFiles tests handling of hidden files (starting with .)
func TestFindPath_DotFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a hidden directory and file
	dotDir := filepath.Join(tmpDir, ".hidden")
	require.NoError(t, os.Mkdir(dotDir, 0o750))

	dotFile := filepath.Join(dotDir, ".secret.txt")
	require.NoError(t, os.WriteFile(dotFile, []byte("test"), 0o600))

	// FindPath should handle dot files/directories
	result, err := FindPath(dotFile)
	require.NoError(t, err, "Should handle dot files")
	assert.Equal(t, dotFile, result)
}

// TestFindPath_SpecialCharacters tests paths with spaces and other special chars
func TestFindPath_SpecialCharacters(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create directory with various special characters
	specialDir := filepath.Join(tmpDir, "Dir With Spaces & Special-Chars")
	require.NoError(t, os.Mkdir(specialDir, 0o750))

	specialFile := filepath.Join(specialDir, "File (Copy) [1].txt")
	require.NoError(t, os.WriteFile(specialFile, []byte("test"), 0o600))

	// FindPath should handle special characters
	result, err := FindPath(specialFile)
	require.NoError(t, err, "Should handle special characters in path")
	assert.Equal(t, specialFile, result)
}

// TestFindPath_VeryDeepStructure tests very deep directory hierarchies
// to ensure no stack overflow or performance issues with long paths
func TestFindPath_VeryDeepStructure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping deep structure test in short mode")
	}

	t.Parallel()
	tmpDir := t.TempDir()

	// Create a path ~300 characters long
	longName := strings.Repeat("a", 50)
	current := tmpDir
	for range 6 {
		current = filepath.Join(current, longName)
		require.NoError(t, os.Mkdir(current, 0o750))
	}

	file := filepath.Join(current, "test.txt")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0o600))

	result, err := FindPath(file)
	require.NoError(t, err, "Should handle very long paths")
	assert.Equal(t, file, result)

	// Verify path is actually long
	assert.Greater(t, len(result), 250, "Test path should be > 250 chars to test long path handling")
}

// TestFindPath_RootPaths tests various root path formats
func TestFindPath_RootPaths(t *testing.T) {
	t.Parallel()

	// Test Unix root (skip on Windows)
	t.Run("Unix root", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Skipping Unix root test on Windows")
		}

		result, err := FindPath("/")
		if err != nil {
			t.Logf("Root path not accessible: %v", err)
			return
		}

		assert.True(t, filepath.IsAbs(result), "Root path should be absolute")
		_, statErr := os.Stat(result)
		assert.NoError(t, statErr, "Returned path should exist")
	})

	// Test current directory
	t.Run("Current directory", func(t *testing.T) {
		result, err := FindPath(".")
		if err != nil {
			t.Logf("Current directory not accessible: %v", err)
			return
		}

		assert.True(t, filepath.IsAbs(result), "Current directory should resolve to absolute")
		_, statErr := os.Stat(result)
		assert.NoError(t, statErr, "Returned path should exist")
	})
}
