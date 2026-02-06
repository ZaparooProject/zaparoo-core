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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setupMockPlatformWithTempDir creates a mock platform with DataDir set to a temp directory.
func setupMockPlatformWithTempDir(t *testing.T, tempDir string) *mocks.MockPlatform {
	t.Helper()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: tempDir})
	return mockPlatform
}

// setupShowLoader sets up the ShowLoader mock to return a noop function.
func setupShowLoader(mockPlatform *mocks.MockPlatform) {
	noopFunc := func() error { return nil }
	mockPlatform.On("ShowLoader",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("models.NoticeArgs"),
	).Return(noopFunc, nil)
}

// setupShowLoaderNil sets up the ShowLoader mock to return nil function.
func setupShowLoaderNil(mockPlatform *mocks.MockPlatform) {
	mockPlatform.On("ShowLoader",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("models.NoticeArgs"),
	).Return(nil, nil)
}

func TestInstallRemoteFile_NilContext(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)
	setupShowLoader(mockPlatform)

	var receivedCtx context.Context
	downloader := func(args DownloaderArgs) error {
		receivedCtx = args.ctx
		if err := os.WriteFile(args.tempPath, []byte("test"), 0o600); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}
		return os.Rename(args.tempPath, args.finalPath)
	}

	// Pass nil context - should not panic, should create a valid context
	//nolint:staticcheck // SA1012: intentionally testing nil context handling
	path, err := InstallRemoteFile(
		nil,
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.NotNil(t, receivedCtx, "downloader should receive a non-nil context")

	// Verify it has the emergency timeout
	deadline, hasDeadline := receivedCtx.Deadline()
	assert.True(t, hasDeadline, "context should have a deadline from emergency timeout")
	assert.WithinDuration(t, time.Now().Add(maxDownloadTimeout), deadline, 5*time.Second)
}

func TestInstallRemoteFile_NilDownloader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()

	_, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		nil, // nil downloader
	)

	require.Error(t, err)
	assert.Equal(t, "downloader function is nil", err.Error())
}

func TestInstallRemoteFile_EmptyURL(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()

	downloader := func(_ DownloaderArgs) error {
		return nil
	}

	_, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		"", // empty URL
		"nes",
		"",
		"",
		downloader,
	)

	require.Error(t, err)
	assert.Equal(t, "media download url is empty", err.Error())
}

func TestInstallRemoteFile_EmptySystemID(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()

	downloader := func(_ DownloaderArgs) error {
		return nil
	}

	_, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"", // empty system ID
		"",
		"",
		downloader,
	)

	require.Error(t, err)
	assert.Equal(t, "media system id is empty", err.Error())
}

func TestInstallRemoteFile_ContextPassedToDownloader(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)
	setupShowLoader(mockPlatform)

	var receivedCtx context.Context
	downloader := func(args DownloaderArgs) error {
		receivedCtx = args.ctx
		// Create the file to simulate successful download
		if err := os.WriteFile(args.tempPath, []byte("test"), 0o600); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}
		return os.Rename(args.tempPath, args.finalPath)
	}

	// Use a context with a value to verify it's preserved through the wrapper
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "test-value")
	_, err := InstallRemoteFile(
		ctx,
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.NoError(t, err)
	// Verify the context is a child of our original (value should be accessible)
	assert.Equal(t, "test-value", receivedCtx.Value(ctxKey{}), "context should preserve parent values")
	// Verify it has the emergency timeout
	_, hasDeadline := receivedCtx.Deadline()
	assert.True(t, hasDeadline, "context should have emergency timeout deadline")
}

func TestInstallRemoteFile_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)
	setupShowLoader(mockPlatform)

	ctx, cancel := context.WithCancel(context.Background())

	downloader := func(args DownloaderArgs) error {
		// Simulate that the downloader checks context and returns error when cancelled
		select {
		case <-args.ctx.Done():
			return args.ctx.Err()
		default:
		}
		// Cancel context during "download"
		cancel()
		// Check again after cancellation
		select {
		case <-args.ctx.Done():
			return args.ctx.Err()
		default:
			return nil
		}
	}

	_, err := InstallRemoteFile(
		ctx,
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.Error(t, err)
	assert.ErrorIs(t, errors.Unwrap(err), context.Canceled)
}

func TestInstallRemoteFile_NilShowLoaderFunction(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)
	setupShowLoaderNil(mockPlatform)

	downloader := func(args DownloaderArgs) error {
		if err := os.WriteFile(args.tempPath, []byte("test"), 0o600); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}
		return os.Rename(args.tempPath, args.finalPath)
	}

	// This should not panic even though ShowLoader returns nil
	path, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.NoError(t, err)
	assert.NotEmpty(t, path)
}

func TestInstallRemoteFile_NilShowNoticeFunction(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)
	setupShowLoader(mockPlatform)

	// Setup ShowNotice to return nil hide function
	mockPlatform.On("ShowNotice",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("models.NoticeArgs"),
	).Return(nil, time.Duration(0), nil)

	downloader := func(args DownloaderArgs) error {
		if err := os.WriteFile(args.tempPath, []byte("test"), 0o600); err != nil {
			return fmt.Errorf("write temp file: %w", err)
		}
		return os.Rename(args.tempPath, args.finalPath)
	}

	// This should not panic even though ShowNotice returns nil hide function
	path, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"Pre-download notice", // trigger showPreNotice
		"",
		downloader,
	)

	require.NoError(t, err)
	assert.NotEmpty(t, path)
}

func TestInstallRemoteFile_FileAlreadyExists(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)

	// Pre-create the file in the media directory structure
	// Note: findInstallDir uses DataDir/media/systemID as the path
	// System ID "nes" maps to "NES" in systemdefs
	nesDir := filepath.Join(tempDir, "media", "NES")
	require.NoError(t, os.MkdirAll(nesDir, 0o750))
	existingFile := filepath.Join(nesDir, "game.rom")
	require.NoError(t, os.WriteFile(existingFile, []byte("existing"), 0o600))

	downloaderCalled := false
	downloader := func(_ DownloaderArgs) error {
		downloaderCalled = true
		return nil
	}

	path, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.NoError(t, err)
	assert.Equal(t, existingFile, path)
	assert.False(t, downloaderCalled, "downloader should not be called when file exists")
}

func TestInstallRemoteFile_DownloaderError(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)
	setupShowLoader(mockPlatform)

	expectedErr := errors.New("network error")
	downloader := func(_ DownloaderArgs) error {
		return expectedErr
	}

	_, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestShowPreNotice_NilHideFunction(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()

	// Return nil hide function - this should not cause a panic
	mockPlatform.On("ShowNotice",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("models.NoticeArgs"),
	).Return(nil, time.Duration(0), nil)

	// This should not panic
	err := showPreNotice(cfg, mockPlatform, "Test notice")
	require.NoError(t, err)
}

func TestShowPreNotice_EmptyText(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()

	// ShowNotice should not be called with empty text
	err := showPreNotice(cfg, mockPlatform, "")
	require.NoError(t, err)

	// Verify ShowNotice was never called
	mockPlatform.AssertNotCalled(t, "ShowNotice",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("models.NoticeArgs"),
	)
}

func TestShowPreNotice_WithDelay(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()

	hideCalled := false
	hideFunc := func() error {
		hideCalled = true
		return nil
	}

	// Return a delay to test the delay path
	mockPlatform.On("ShowNotice",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("models.NoticeArgs"),
	).Return(hideFunc, 10*time.Millisecond, nil)

	start := time.Now()
	err := showPreNotice(cfg, mockPlatform, "Test notice")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, hideCalled, "hide function should be called")
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond, "should have delayed")
}

func TestShowPreNotice_HideError(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()

	hideErr := errors.New("hide error")
	hideFunc := func() error {
		return hideErr
	}

	mockPlatform.On("ShowNotice",
		mock.AnythingOfType("*config.Instance"),
		mock.AnythingOfType("models.NoticeArgs"),
	).Return(hideFunc, time.Duration(0), nil)

	err := showPreNotice(cfg, mockPlatform, "Test notice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error hiding pre-notice")
}

func TestNamesFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rawURL      string
		defaultName string
		wantDisplay string
		wantFile    string
		wantExt     string
	}{
		{
			name:        "simple URL",
			rawURL:      "http://example.com/game.rom",
			defaultName: "",
			wantDisplay: "game",
			wantFile:    "game.rom",
			wantExt:     ".rom",
		},
		{
			name:        "URL with encoded characters",
			rawURL:      "http://example.com/Super%20Mario.rom",
			defaultName: "",
			wantDisplay: "Super Mario",
			wantFile:    "Super Mario.rom",
			wantExt:     ".rom",
		},
		{
			name:        "with default name",
			rawURL:      "http://example.com/game.rom",
			defaultName: "Custom Name",
			wantDisplay: "Custom Name",
			wantFile:    "game.rom",
			wantExt:     ".rom",
		},
		{
			name:        "invalid URL falls back to filepath",
			rawURL:      "not-a-url/game.rom",
			defaultName: "",
			wantDisplay: "game",
			wantFile:    "game.rom",
			wantExt:     ".rom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			names := namesFromURL(tt.rawURL, tt.defaultName)
			assert.Equal(t, tt.wantDisplay, names.display)
			assert.Equal(t, tt.wantFile, names.filename)
			assert.Equal(t, tt.wantExt, names.ext)
		})
	}
}
