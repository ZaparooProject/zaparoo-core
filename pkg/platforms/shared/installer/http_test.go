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

package installer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadHTTPFile_Success(t *testing.T) {
	t.Parallel()

	content := []byte("test file content for download")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	finalPath := filepath.Join(tempDir, "game.rom")

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       context.Background(),
		url:       server.URL + "/game.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.NoError(t, err)

	// Verify final file exists with correct content
	data, err := os.ReadFile(finalPath) //nolint:gosec // Test file path from t.TempDir()
	require.NoError(t, err)
	assert.Equal(t, content, data)

	// Verify temp file was removed (renamed to final)
	_, err = os.Stat(tempPath)
	assert.True(t, os.IsNotExist(err), "temp file should not exist after successful download")
}

func TestDownloadHTTPFile_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Server that delays response to allow cancellation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if context is already cancelled
		select {
		case <-r.Context().Done():
			return
		default:
		}

		// Start writing response slowly
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(http.StatusOK)

		// Write slowly to allow cancellation
		for i := range 100 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
				_, _ = fmt.Fprintf(w, "chunk%d", i)
			}
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	finalPath := filepath.Join(tempDir, "game.rom")

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       ctx,
		url:       server.URL + "/game.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestDownloadHTTPFile_ContextTimeout(t *testing.T) {
	t.Parallel()

	// Server that never responds
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Block until request context is done
		<-r.Context().Done()
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	finalPath := filepath.Join(tempDir, "game.rom")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       ctx,
		url:       server.URL + "/game.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestDownloadHTTPFile_HTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"NotFound", http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError},
		{"Forbidden", http.StatusForbidden},
		{"Unauthorized", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			tempDir := t.TempDir()
			tempPath := filepath.Join(tempDir, "game.rom.part")
			finalPath := filepath.Join(tempDir, "game.rom")

			err := DownloadHTTPFile(DownloaderArgs{
				ctx:       context.Background(),
				url:       server.URL + "/game.rom",
				tempPath:  tempPath,
				finalPath: finalPath,
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("invalid status code: %d", tt.statusCode))
		})
	}
}

func TestDownloadHTTPFile_IncompleteDownload(t *testing.T) {
	t.Parallel()

	// Server that claims larger content than it sends
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000") // Claim 1000 bytes
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short")) // Only send 5 bytes
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	finalPath := filepath.Join(tempDir, "game.rom")

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       context.Background(),
		url:       server.URL + "/game.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	// When Content-Length exceeds actual data, io.Copy returns "unexpected EOF"
	// This is detected as a download error and temp file is cleaned up
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error downloading file")

	// Verify temp file was cleaned up
	_, statErr := os.Stat(tempPath)
	assert.True(t, os.IsNotExist(statErr), "temp file should be removed after incomplete download")
}

func TestDownloadHTTPFile_InvalidURL(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	finalPath := filepath.Join(tempDir, "game.rom")

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       context.Background(),
		url:       "http://localhost:99999/nonexistent", // Invalid port
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error getting url")
}

func TestDownloadHTTPFile_InvalidTempPath(t *testing.T) {
	t.Parallel()

	content := []byte("test content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       context.Background(),
		url:       server.URL + "/game.rom",
		tempPath:  "/nonexistent/directory/game.rom.part",
		finalPath: "/nonexistent/directory/game.rom",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error creating file")
}

func TestDownloadHTTPFile_RenameFailure(t *testing.T) {
	t.Parallel()

	content := []byte("test content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	// Final path in a different (non-existent) directory - rename will fail
	finalPath := filepath.Join(tempDir, "subdir", "game.rom")

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       context.Background(),
		url:       server.URL + "/game.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error renaming temp file")

	// Verify temp file was cleaned up after rename failure
	_, statErr := os.Stat(tempPath)
	assert.True(t, os.IsNotExist(statErr), "temp file should be removed after rename failure")
}

func TestDownloadHTTPFile_NoContentLength(t *testing.T) {
	t.Parallel()

	content := []byte("test file content without content-length header")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Don't set Content-Length - chunked transfer encoding
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	finalPath := filepath.Join(tempDir, "game.rom")

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       context.Background(),
		url:       server.URL + "/game.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.NoError(t, err, "download should succeed without Content-Length header")

	// Verify final file exists with correct content
	data, err := os.ReadFile(finalPath) //nolint:gosec // Test file path from t.TempDir()
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestDownloadHTTPFile_LargeFile(t *testing.T) {
	t.Parallel()

	// Generate 1MB of test data
	size := 1024 * 1024
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "large.rom.part")
	finalPath := filepath.Join(tempDir, "large.rom")

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       context.Background(),
		url:       server.URL + "/large.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.NoError(t, err)

	// Verify file size
	info, err := os.Stat(finalPath)
	require.NoError(t, err)
	assert.Equal(t, int64(size), info.Size())
}

func TestDownloadHTTPFile_ContextPreCancelled(t *testing.T) {
	t.Parallel()

	var requestReceived atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestReceived.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempPath := filepath.Join(tempDir, "game.rom.part")
	finalPath := filepath.Join(tempDir, "game.rom")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before making request

	err := DownloadHTTPFile(DownloaderArgs{
		ctx:       ctx,
		url:       server.URL + "/game.rom",
		tempPath:  tempPath,
		finalPath: finalPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
	// Request should not have been made (or cancelled immediately)
}
