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

package hasher

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeFileHashes_RegularFile(t *testing.T) {
	t.Parallel()

	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"

	err := os.WriteFile(testFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	// Compute hashes
	hash, err := ComputeFileHashes(testFile)
	require.NoError(t, err)
	require.NotNil(t, hash)

	// Verify the hash structure is populated correctly
	assert.NotEmpty(t, hash.CRC32)
	assert.Len(t, hash.CRC32, 8) // CRC32 should be 8 hex characters
	assert.NotEmpty(t, hash.MD5)
	assert.Len(t, hash.MD5, 32) // MD5 should be 32 hex characters
	assert.NotEmpty(t, hash.SHA1)
	assert.Len(t, hash.SHA1, 40) // SHA1 should be 40 hex characters
	assert.Equal(t, int64(13), hash.FileSize)

	// Verify consistency - computing the same file twice should give same results
	hash2, err := ComputeFileHashes(testFile)
	require.NoError(t, err)
	assert.Equal(t, hash, hash2)
}

func TestComputeFileHashes_EmptyFile(t *testing.T) {
	t.Parallel()

	// Create empty test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.txt")

	err := os.WriteFile(testFile, []byte{}, 0o600)
	require.NoError(t, err)

	// Compute hashes
	hash, err := ComputeFileHashes(testFile)
	require.NoError(t, err)
	require.NotNil(t, hash)

	// Verify expected values for empty file
	assert.Equal(t, "00000000", hash.CRC32)
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", hash.MD5)
	assert.Equal(t, "da39a3ee5e6b4b0d3255bfef95601890afd80709", hash.SHA1)
	assert.Equal(t, int64(0), hash.FileSize)
}

func TestComputeFileHashes_NonexistentFile(t *testing.T) {
	t.Parallel()

	hash, err := ComputeFileHashes("/nonexistent/file.txt")
	require.Error(t, err)
	assert.Nil(t, hash)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestComputeFileHashes_ZipArchive(t *testing.T) {
	t.Parallel()

	// Create temporary directory and test content
	tmpDir := t.TempDir()
	zipFile := filepath.Join(tmpDir, "test.zip")
	testContent := "Game ROM content"

	// Create a ZIP file with test content
	f, err := os.Create(zipFile) // #nosec G304 - zipFile is constructed safely from t.TempDir()
	require.NoError(t, err)
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("Failed to close file: %v", closeErr)
		}
	}()

	w := zip.NewWriter(f)
	defer func() {
		if closeErr := w.Close(); closeErr != nil {
			t.Logf("Failed to close zip writer: %v", closeErr)
		}
	}()

	// Add a file to the ZIP
	fw, err := w.Create("game.rom")
	require.NoError(t, err)
	_, err = fw.Write([]byte(testContent))
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	// Test MiSTer-style path: /path/to/archive.zip/file_inside.rom
	misterPath := zipFile + "/game.rom"

	// Compute hashes
	hash, err := ComputeFileHashes(misterPath)
	require.NoError(t, err)
	require.NotNil(t, hash)

	// The hashes should match the content inside the ZIP, not the ZIP file itself
	assert.Equal(t, int64(len(testContent)), hash.FileSize)
	assert.NotEmpty(t, hash.CRC32)
	assert.NotEmpty(t, hash.MD5)
	assert.NotEmpty(t, hash.SHA1)

	// Verify by comparing with direct computation of the same content
	tmpFile := filepath.Join(tmpDir, "direct.txt")
	err = os.WriteFile(tmpFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	directHash, err := ComputeFileHashes(tmpFile)
	require.NoError(t, err)

	assert.Equal(t, directHash.CRC32, hash.CRC32)
	assert.Equal(t, directHash.MD5, hash.MD5)
	assert.Equal(t, directHash.SHA1, hash.SHA1)
	assert.Equal(t, directHash.FileSize, hash.FileSize)
}

func TestComputeFileHashes_ZipFileNotFound(t *testing.T) {
	t.Parallel()

	// Create temporary directory and ZIP file
	tmpDir := t.TempDir()
	zipFile := filepath.Join(tmpDir, "test.zip")

	// Create a ZIP file
	f, err := os.Create(zipFile) // #nosec G304 - zipFile is constructed safely from t.TempDir()
	require.NoError(t, err)
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("Failed to close file: %v", closeErr)
		}
	}()

	w := zip.NewWriter(f)
	fw, err := w.Create("existing.rom")
	require.NoError(t, err)
	_, err = fw.Write([]byte("content"))
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	// Test with non-existent file inside ZIP
	misterPath := zipFile + "/nonexistent.rom"

	hash, err := ComputeFileHashes(misterPath)
	require.Error(t, err)
	assert.Nil(t, hash)
	assert.Contains(t, err.Error(), "not found in archive")
}

func TestComputeFileHashes_InvalidZipPath(t *testing.T) {
	t.Parallel()

	// Test with .zip in path but file doesn't exist
	hash, err := ComputeFileHashes("/nonexistent/file.zip/game.rom")
	require.Error(t, err)
	assert.Nil(t, hash)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestComputeFileHashes_CorruptedZip(t *testing.T) {
	t.Parallel()

	// Create a file with .zip extension but invalid content
	tmpDir := t.TempDir()
	fakeZip := filepath.Join(tmpDir, "corrupted.zip")

	err := os.WriteFile(fakeZip, []byte("not a zip file"), 0o600)
	require.NoError(t, err)

	// Test with corrupted ZIP
	misterPath := fakeZip + "/game.rom"

	hash, err := ComputeFileHashes(misterPath)
	require.Error(t, err)
	assert.Nil(t, hash)
	assert.Contains(t, err.Error(), "failed to open zip")
}

func TestValidateHashes_Success(t *testing.T) {
	t.Parallel()

	// Create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"

	err := os.WriteFile(testFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	// Compute the actual hashes first
	actualHash, err := ComputeFileHashes(testFile)
	require.NoError(t, err)

	// Create expected hash using the computed values
	expectedHash := &FileHash{
		CRC32:    actualHash.CRC32,
		MD5:      actualHash.MD5,
		SHA1:     actualHash.SHA1,
		FileSize: actualHash.FileSize,
	}

	// Validate hashes
	valid, err := ValidateHashes(testFile, expectedHash)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestValidateHashes_PartialValidation(t *testing.T) {
	t.Parallel()

	// Create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"

	err := os.WriteFile(testFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	// Compute the actual hashes first
	actualHash, err := ComputeFileHashes(testFile)
	require.NoError(t, err)

	// Test with only MD5 hash provided
	expectedHash := &FileHash{
		MD5: actualHash.MD5,
	}

	valid, err := ValidateHashes(testFile, expectedHash)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestValidateHashes_Mismatch(t *testing.T) {
	t.Parallel()

	// Create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"

	err := os.WriteFile(testFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	// Validate hashes - should fail for each mismatch
	tests := []struct {
		hash *FileHash
		name string
	}{
		{
			name: "CRC32 mismatch",
			hash: &FileHash{CRC32: "wrongcrc"},
		},
		{
			name: "MD5 mismatch",
			hash: &FileHash{MD5: "wrongmd5hash"},
		},
		{
			name: "SHA1 mismatch",
			hash: &FileHash{SHA1: "wrongsha1hash"},
		},
		{
			name: "FileSize mismatch",
			hash: &FileHash{FileSize: 999},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			valid, err := ValidateHashes(testFile, tt.hash)
			require.NoError(t, err)
			assert.False(t, valid)
		})
	}
}

func TestValidateHashes_NonexistentFile(t *testing.T) {
	t.Parallel()

	expectedHash := &FileHash{
		MD5: "somehash",
	}

	valid, err := ValidateHashes("/nonexistent/file.txt", expectedHash)
	require.Error(t, err)
	assert.False(t, valid)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestValidateHashes_EmptyExpectedHash(t *testing.T) {
	t.Parallel()

	// Create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	err := os.WriteFile(testFile, []byte("content"), 0o600)
	require.NoError(t, err)

	// Validate with empty expected hash - should always return true
	expectedHash := &FileHash{}

	valid, err := ValidateHashes(testFile, expectedHash)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestHashReader_ErrorHandling(t *testing.T) {
	t.Parallel()

	// Test with failing reader
	failingReader := &failingReader{}
	hash, err := hashReader(failingReader, 100)
	require.Error(t, err)
	assert.Nil(t, hash)
	assert.Contains(t, err.Error(), "failed to read file for hashing")
}

// failingReader is a test helper that always returns an error when reading
type failingReader struct{}

func (*failingReader) Read(_ []byte) (n int, err error) {
	return 0, errors.New("test read error")
}

func TestFileHash_StructFields(t *testing.T) {
	t.Parallel()

	// Test FileHash struct field access
	hash := &FileHash{
		CRC32:    "12345678",
		MD5:      "abcdef1234567890abcdef1234567890",
		SHA1:     "abcdef1234567890abcdef1234567890abcdef12",
		FileSize: 1024,
	}

	assert.Equal(t, "12345678", hash.CRC32)
	assert.Equal(t, "abcdef1234567890abcdef1234567890", hash.MD5)
	assert.Equal(t, "abcdef1234567890abcdef1234567890abcdef12", hash.SHA1)
	assert.Equal(t, int64(1024), hash.FileSize)
}

func TestComputeFileHashes_MultipleZipExtensions(t *testing.T) {
	t.Parallel()

	// Test edge case: path contains .zip multiple times
	// Example: /path/game.zip.backup/archive.zip/file.rom
	tmpDir := t.TempDir()

	// Create directory with .zip in name
	zipBackupDir := filepath.Join(tmpDir, "game.zip.backup")
	err := os.MkdirAll(zipBackupDir, 0o750)
	require.NoError(t, err)

	// Create actual ZIP file in that directory
	zipFile := filepath.Join(zipBackupDir, "archive.zip")
	f, err := os.Create(zipFile) // #nosec G304 - zipFile is constructed safely from t.TempDir()
	require.NoError(t, err)
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Logf("Failed to close file: %v", closeErr)
		}
	}()

	w := zip.NewWriter(f)
	fw, err := w.Create("file.rom")
	require.NoError(t, err)
	_, err = fw.Write([]byte("test content"))
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	// Test path with multiple .zip occurrences
	complexPath := zipFile + "/file.rom"

	hash, err := ComputeFileHashes(complexPath)
	require.NoError(t, err)
	require.NotNil(t, hash)
	assert.Equal(t, int64(12), hash.FileSize) // "test content" is 12 bytes
}
