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

package file

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)

	assert.NotNil(t, reader)
	assert.Equal(t, cfg, reader.cfg)
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	metadata := reader.Metadata()

	assert.Equal(t, "file", metadata.ID)
	assert.Equal(t, "File-based token reader", metadata.Description)
	assert.True(t, metadata.DefaultEnabled)
	assert.True(t, metadata.DefaultAutoDetect)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	ids := reader.IDs()

	require.Len(t, ids, 1)
	assert.Equal(t, "file", ids[0])
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	capabilities := reader.Capabilities()

	require.Len(t, capabilities, 1)
	assert.Contains(t, capabilities, readers.CapabilityRemovable)
}

func TestWrite_NotSupported(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	token, err := reader.Write("test-data")

	assert.Nil(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing not supported")
}

func TestCancelWrite(t *testing.T) {
	t.Parallel()

	reader := &Reader{}

	// Should not panic
	reader.CancelWrite()
}

func TestOnMediaChange(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	err := reader.OnMediaChange(&models.ActiveMedia{})

	assert.NoError(t, err, "OnMediaChange should return nil")
}

func TestConnected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		polling  bool
		expected bool
	}{
		{
			name:     "not polling",
			polling:  false,
			expected: false,
		},
		{
			name:     "polling",
			polling:  true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &Reader{
				polling: tt.polling,
			}

			assert.Equal(t, tt.expected, reader.Connected())
		})
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	reader := &Reader{}
	result := reader.Detect([]string{})

	assert.Empty(t, result, "file reader does not auto-detect")
}

func TestOpen_InvalidDriverID(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "invalid_driver",
		Path:   "/tmp/test.txt",
	}

	err := reader.Open(device, scanQueue)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid reader id")
}

func TestOpen_RelativePath(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   "relative/path/token.txt", // Not absolute
	}

	err := reader.Open(device, scanQueue)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be absolute")
}

func TestOpen_NonExistentParentDirectory(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create an absolute path with non-existent parent directory
	nonExistentPath := filepath.Join(os.TempDir(), "nonexistent-directory-12345", "subdir", "token.txt")

	device := config.ReadersConnect{
		Driver: "file",
		Path:   nonExistentPath,
	}

	err := reader.Open(device, scanQueue)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat parent directory")
}

func TestOpen_CreatesFileIfNotExists(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp directory
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")

	// Verify file doesn't exist
	_, err := os.Stat(tokenFile)
	require.Error(t, err, "file should not exist initially")

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// Verify file was created
	_, err = os.Stat(tokenFile)
	assert.NoError(t, err, "file should be created by Open()")
}

func TestOpen_InitialFileContent_TokenDetected(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file with initial content
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte("**LAUNCH.CMD:test_game"), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// Wait for initial token detection
	scan := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "**LAUNCH.CMD:test_game", scan.Token.Text)
	assert.Equal(t, TokenType, scan.Token.Type)
	assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
	assert.False(t, scan.ReaderError)
}

func TestOpen_FileContentChange_NewToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file with initial content
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte("token1"), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: initial token
	scan1 := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "token1", scan1.Token.Text)
	assert.NotEmpty(t, scan1.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")

	// Change file content
	err = os.WriteFile(tokenFile, []byte("token2"), 0o600)
	require.NoError(t, err)

	// Second scan: new token
	scan2 := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan2.Token)
	assert.Equal(t, "token2", scan2.Token.Text)
	assert.NotEmpty(t, scan2.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
	assert.False(t, scan2.ReaderError)
}

func TestOpen_FileBecomesEmpty_TokenRemoval(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file with initial content
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte("active_token"), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "active_token", scan1.Token.Text)
	assert.NotEmpty(t, scan1.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")

	// Empty the file
	err = os.WriteFile(tokenFile, []byte(""), 0o600)
	require.NoError(t, err)

	// Second scan: token removal
	scan2 := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.Nil(t, scan2.Token, "token should be nil when file is emptied")
	assert.False(t, scan2.ReaderError)
}

func TestOpen_DuplicateContent_Ignored(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file with content
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte("same_token"), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// First scan: token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "same_token", scan1.Token.Text)
	assert.NotEmpty(t, scan1.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")

	// Write the same content again
	err = os.WriteFile(tokenFile, []byte("same_token"), 0o600)
	require.NoError(t, err)

	// No additional scan should be sent
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)
}

func TestOpen_EmptyFileInitially_NoToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file with empty content
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte(""), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// No scan should be sent for empty file
	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)

	// Now add content
	err = os.WriteFile(tokenFile, []byte("new_token"), 0o600)
	require.NoError(t, err)

	// Token should be detected
	scan := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "new_token", scan.Token.Text)
	assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
}

func TestOpen_WhitespaceHandling(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file with whitespace
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte("  token_with_spaces  \n"), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()

	// Token should be trimmed
	scan := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan.Token)
	assert.Equal(t, "token_with_spaces", scan.Token.Text, "whitespace should be trimmed")
	assert.NotEmpty(t, scan.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
}

func TestClose_StopsPolling(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte(""), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)

	// Verify reader is connected
	assert.True(t, reader.Connected())

	// Close the reader
	err = reader.Close()
	require.NoError(t, err)

	// Verify reader is disconnected
	assert.False(t, reader.polling)
	assert.False(t, reader.Connected())

	// Write to file - no scan should be sent
	err = os.WriteFile(tokenFile, []byte("should_not_trigger"), 0o600)
	require.NoError(t, err)

	testutils.AssertNoScan(t, scanQueue, 500*time.Millisecond)
}

func TestInfo_ReturnsPath(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		path: "/test/path/token.txt",
	}

	assert.Equal(t, "/test/path/token.txt", reader.Info())
}

func TestPath_ReturnsPath(t *testing.T) {
	t.Parallel()

	reader := &Reader{
		path: "/test/path/token.txt",
		device: config.ReadersConnect{
			Driver: "file",
			Path:   "/test/path/token.txt",
		},
	}

	assert.Equal(t, "/test/path/token.txt", reader.Path())
}

func TestOpen_ConsecutiveReadErrors_SendsReaderError(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	reader := NewReader(cfg)
	scanQueue := testutils.CreateTestScanChannel(t)

	// Create temp file with initial content to establish a token
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	err := os.WriteFile(tokenFile, []byte("active_token"), 0o600)
	require.NoError(t, err)

	device := config.ReadersConnect{
		Driver: "file",
		Path:   tokenFile,
	}

	err = reader.Open(device, scanQueue)
	require.NoError(t, err)

	// First scan: initial token detected
	scan1 := testutils.AssertScanReceived(t, scanQueue, 1*time.Second)
	assert.NotNil(t, scan1.Token)
	assert.Equal(t, "active_token", scan1.Token.Text)
	assert.NotEmpty(t, scan1.Token.ReaderID, "ReaderID must be set on tokens from hardware readers")
	assert.False(t, scan1.ReaderError)

	// Delete the file to cause consecutive read errors
	err = os.Remove(tokenFile)
	require.NoError(t, err)

	// Wait for the reader to detect file read errors
	// The reader polls every 100ms, so after ~1.2 seconds it should have
	// failed 10+ times and triggered the consecutive error handler

	// Expect error scans for the first few failures
	// (these are normal error scans, not ReaderError yet)
	for range 5 {
		scan := testutils.AssertScanReceived(t, scanQueue, 500*time.Millisecond)
		if scan.ReaderError {
			// Found the ReaderError scan earlier than expected - that's OK
			break
		}
		require.Error(t, scan.Error, "should have an error for failed read")
		assert.Nil(t, scan.Token)
	}

	// Now wait for the ReaderError scan (sent after maxConsecutiveErrors)
	// This might already have been received in the loop above
	var readerErrorScan *config.ReadersConnect
	timeout := time.After(2 * time.Second)
	for {
		select {
		case scan := <-scanQueue:
			if scan.ReaderError {
				// This is the ReaderError scan we're looking for
				assert.True(t, scan.ReaderError, "ReaderError flag should be set")
				assert.Nil(t, scan.Token, "token should be nil in ReaderError scan")
				readerErrorScan = &config.ReadersConnect{} // marker that we found it
				goto foundReaderError
			}
		case <-timeout:
			require.Fail(t, "timeout waiting for ReaderError scan after consecutive failures")
			return
		}
	}

foundReaderError:
	assert.NotNil(t, readerErrorScan, "should have received ReaderError scan")

	// Give the reader time to close itself
	time.Sleep(200 * time.Millisecond)

	// Verify reader is now disconnected
	assert.False(t, reader.Connected(), "reader should be disconnected after consecutive errors")
}
