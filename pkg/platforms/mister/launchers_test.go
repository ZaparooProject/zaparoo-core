//go:build linux

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

package mister

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckInZip_NonZipPath(t *testing.T) {
	t.Parallel()

	// Non-zip paths should be returned unchanged
	tests := []string{
		"/path/to/game.rom",
		"/path/to/game.bin",
		"/path/to/game.ZIP.backup",
		"",
	}

	for _, path := range tests {
		result := checkInZip(path)
		assert.Equal(t, path, result, "non-zip path should be unchanged")
	}
}

func TestCheckInZip_NonExistentFile(t *testing.T) {
	t.Parallel()

	// Non-existent zip file should return original path
	path := "/nonexistent/path/game.zip"
	result := checkInZip(path)
	assert.Equal(t, path, result, "non-existent file should return original path")
}

func TestCheckInZip_SingleFileZip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "game.zip")

	// Create a zip with a single file
	zipFile, err := os.Create(zipPath) //nolint:gosec // G304 - test file in temp dir
	require.NoError(t, err)
	defer func() {
		_ = zipFile.Close()
	}()

	zipWriter := zip.NewWriter(zipFile)
	fileWriter, err := zipWriter.Create("somefile.rom")
	require.NoError(t, err)
	_, err = fileWriter.Write([]byte("test content"))
	require.NoError(t, err)
	require.NoError(t, zipWriter.Close())

	// Should return path to the single file inside zip
	result := checkInZip(zipPath)
	expected := filepath.Join(zipPath, "somefile.rom")
	assert.Equal(t, expected, result)
}

func TestCheckInZip_MatchingFilename(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "SuperGame.zip")

	// Create a zip with multiple files, one matching the zip name
	zipFile, err := os.Create(zipPath) //nolint:gosec // G304 - test file in temp dir
	require.NoError(t, err)
	defer func() {
		_ = zipFile.Close()
	}()

	zipWriter := zip.NewWriter(zipFile)

	// Add non-matching file
	fw1, err := zipWriter.Create("readme.txt")
	require.NoError(t, err)
	_, err = fw1.Write([]byte("readme"))
	require.NoError(t, err)

	// Add matching file (case-insensitive match)
	fw2, err := zipWriter.Create("supergame.rom")
	require.NoError(t, err)
	_, err = fw2.Write([]byte("game data"))
	require.NoError(t, err)

	// Add another file
	fw3, err := zipWriter.Create("other.bin")
	require.NoError(t, err)
	_, err = fw3.Write([]byte("other"))
	require.NoError(t, err)

	require.NoError(t, zipWriter.Close())

	// Should return the matching file
	result := checkInZip(zipPath)
	expected := filepath.Join(zipPath, "supergame.rom")
	assert.Equal(t, expected, result)
}

func TestCheckInZip_MatchingFilenameCaseInsensitive(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "MyGame.zip")

	// Create a zip with file that matches in different case
	zipFile, err := os.Create(zipPath) //nolint:gosec // G304 - test file in temp dir
	require.NoError(t, err)
	defer func() {
		_ = zipFile.Close()
	}()

	zipWriter := zip.NewWriter(zipFile)
	fw, err := zipWriter.Create("MYGAME.ROM")
	require.NoError(t, err)
	_, err = fw.Write([]byte("game"))
	require.NoError(t, err)
	require.NoError(t, zipWriter.Close())

	// Should match case-insensitively
	result := checkInZip(zipPath)
	expected := filepath.Join(zipPath, "MYGAME.ROM")
	assert.Equal(t, expected, result)
}

func TestCheckInZip_MultipleFilesNoMatch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "game.zip")

	// Create a zip with multiple files, none matching
	zipFile, err := os.Create(zipPath) //nolint:gosec // G304 - test file in temp dir
	require.NoError(t, err)
	defer func() {
		_ = zipFile.Close()
	}()

	zipWriter := zip.NewWriter(zipFile)
	fw1, err := zipWriter.Create("file1.rom")
	require.NoError(t, err)
	_, err = fw1.Write([]byte("data1"))
	require.NoError(t, err)

	fw2, err := zipWriter.Create("file2.rom")
	require.NoError(t, err)
	_, err = fw2.Write([]byte("data2"))
	require.NoError(t, err)

	require.NoError(t, zipWriter.Close())

	// Should return original path (no match, multiple files)
	result := checkInZip(zipPath)
	assert.Equal(t, zipPath, result)
}

func TestCheckInZip_EmptyZip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "empty.zip")

	// Create an empty zip
	zipFile, err := os.Create(zipPath) //nolint:gosec // G304 - test file in temp dir
	require.NoError(t, err)
	defer func() {
		_ = zipFile.Close()
	}()

	zipWriter := zip.NewWriter(zipFile)
	require.NoError(t, zipWriter.Close())

	// Should return original path (no files in zip)
	result := checkInZip(zipPath)
	assert.Equal(t, zipPath, result)
}

func TestCheckInZip_SkipsDirectories(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "game.zip")

	// Create a zip with directories and one file
	zipFile, err := os.Create(zipPath) //nolint:gosec // G304 - test file in temp dir
	require.NoError(t, err)
	defer func() {
		_ = zipFile.Close()
	}()

	zipWriter := zip.NewWriter(zipFile)

	// Add a directory entry
	_, err = zipWriter.Create("folder/")
	require.NoError(t, err)

	// Add a file
	fw, err := zipWriter.Create("folder/game.rom")
	require.NoError(t, err)
	_, err = fw.Write([]byte("game"))
	require.NoError(t, err)

	require.NoError(t, zipWriter.Close())

	// Should find the file (not the directory)
	result := checkInZip(zipPath)
	expected := filepath.Join(zipPath, "folder", "game.rom")
	assert.Equal(t, expected, result)
}
