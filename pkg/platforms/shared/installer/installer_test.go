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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
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

func TestInstallRemoteFile_NilContext(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)

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
		nil,
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
		nil,
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
		nil,
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
		nil,
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
		nil,
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		nil,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.Error(t, err)
	assert.ErrorIs(t, errors.Unwrap(err), context.Canceled)
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
		nil,
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

	expectedErr := errors.New("network error")
	downloader := func(_ DownloaderArgs) error {
		return expectedErr
	}

	_, err := InstallRemoteFile(
		context.Background(),
		cfg,
		mockPlatform,
		nil,
		"http://example.com/game.rom",
		"nes",
		"",
		"",
		downloader,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestInstallRemoteFile_UIEventLoaderLifecycle(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	tempDir := t.TempDir()
	mockPlatform := setupMockPlatformWithTempDir(t, tempDir)
	published := make(chan models.UIStateResponse, 2)
	ui := uievents.New(clockwork.NewFakeClock(), nil, func(state models.UIStateResponse) {
		published <- state
	})

	downloader := func(args DownloaderArgs) error {
		state := ui.State()
		require.Len(t, state.Events, 1)
		assert.Equal(t, models.UIEventKindLoader, state.Events[0].Kind)
		assert.Contains(t, state.Events[0].Message, "Downloading game")
		require.NoError(t, os.WriteFile(args.tempPath, []byte("test"), 0o600))
		return os.Rename(args.tempPath, args.finalPath)
	}

	path, err := InstallRemoteFile(
		t.Context(), cfg, mockPlatform, ui,
		"http://example.com/game.rom", "nes", "", "", downloader,
	)
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	opened := <-published
	assert.Equal(t, uint64(1), opened.Revision)
	resolved := <-published
	assert.Equal(t, uint64(2), resolved.Revision)
	require.Len(t, resolved.Resolved, 1)
	assert.Equal(t, models.UIOutcomeCompleted, resolved.Resolved[0].Outcome)
	assert.Empty(t, ui.State().Events)
}

type delayedUIRenderer struct {
	delay time.Duration
}

func (*delayedUIRenderer) PresentUI(
	_ context.Context,
	_ *models.UIEvent,
) (func() error, error) {
	return func() error { return nil }, nil
}

func (r *delayedUIRenderer) MinimumUIDisplay(_ models.UIEventKind) time.Duration {
	return r.delay
}

func TestShowPreNotice_NoService(t *testing.T) {
	t.Parallel()

	require.NoError(t, showPreNotice(t.Context(), nil, "Test notice"))
}

func TestShowPreNotice_EmptyText(t *testing.T) {
	t.Parallel()

	ui := uievents.New(clockwork.NewFakeClock(), nil, nil)
	require.NoError(t, showPreNotice(t.Context(), ui, ""))
	assert.Empty(t, ui.State().Events)
}

func TestShowPreNotice_CompletesAfterRendererDelay(t *testing.T) {
	t.Parallel()

	const delay = 10 * time.Millisecond
	published := make(chan models.UIStateResponse, 2)
	ui := uievents.New(clockwork.NewRealClock(), &delayedUIRenderer{delay: delay},
		func(state models.UIStateResponse) {
			published <- state
		})

	start := time.Now()
	require.NoError(t, showPreNotice(t.Context(), ui, "Test notice"))
	assert.GreaterOrEqual(t, time.Since(start), delay)

	opened := <-published
	require.Len(t, opened.Events, 1)
	assert.Equal(t, models.UIEventKindNotice, opened.Events[0].Kind)
	resolved := <-published
	require.Len(t, resolved.Resolved, 1)
	assert.Equal(t, models.UIOutcomeCompleted, resolved.Resolved[0].Outcome)
}

func TestShowPreNotice_RendererDelayRespectsCancellation(t *testing.T) {
	t.Parallel()

	ui := uievents.New(clockwork.NewRealClock(), &delayedUIRenderer{delay: time.Minute}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	start := time.Now()
	require.NoError(t, showPreNotice(ctx, ui, "Test notice"))
	assert.Less(t, time.Since(start), time.Second)
	assert.Eventually(t, func() bool {
		return len(ui.State().Events) == 0
	}, time.Second, time.Millisecond)
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

// TestNamesFromURLDoesNotDoubleDecode pins that the last path segment is
// decoded exactly once. url.Parse already returns Path unescaped, so a second
// pass turned %252e%252e%252f back into "../" and let whoever served the URL
// choose which directory the download landed in.
func TestNamesFromURLDoesNotDoubleDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawURL   string
		wantFile string
	}{
		{
			// The escapes survive as literal characters in the name instead
			// of becoming separators. This is the case that traversed.
			name:     "double encoded traversal stays one segment",
			rawURL:   "http://example.com/roms/%252e%252e%252f%252e%252e%252fScripts%252fpwn.sh",
			wantFile: "%2e%2e%2f%2e%2e%2fScripts%2fpwn.sh",
		},
		{
			// Singly encoded separators are decoded by url.Parse itself, so
			// path.Base resolves them away before anything joins the name.
			name:     "single encoded traversal collapses to the base name",
			rawURL:   "http://example.com/roms/%2e%2e%2f%2e%2e%2fpwn.sh",
			wantFile: "pwn.sh",
		},
		{
			// path.Base does not treat a backslash as a separator, so this
			// one reaches the caller intact and only findInstallDir stops it.
			name:     "encoded backslash traversal stays one segment",
			rawURL:   `http://example.com/roms/..%5C..%5Cpwn.exe`,
			wantFile: `..\..\pwn.exe`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantFile, namesFromURL(tt.rawURL, "").filename)
		})
	}
}

// TestFindInstallDirNeverEscapesSystemDir is the containment this all exists
// for. A remote server picks the last path segment of the download URL, and
// from the install directory a few levels of "../" reach the MiSTer scripts
// folder and its boot script, which run as root. Nothing downstream re-checks
// the path, so whatever the name is, the result has to be refused or stay put.
func TestFindInstallDirNeverEscapesSystemDir(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"http://example.com/roms/%252e%252e%252f%252e%252e%252f%252e%252e%252fScripts%252fpwn.sh",
		"http://example.com/roms/%252e%252e%252f%252e%252e%252f%252e%252e%252flinux%252fuser-startup.sh",
		"http://example.com/roms/%2e%2e%2f%2e%2e%2fpwn.sh",
		`http://example.com/roms/..%5C..%5Cpwn.exe`,
		`http://example.com/roms/%252e%252e%255C%252e%252e%255Cpwn.exe`,
		"http://example.com/roms/..",
		"http://example.com/roms/.",
		"http://example.com/roms/",
		"http://example.com/",
	} {
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			pl := setupMockPlatformWithTempDir(t, tempDir)
			systemDir := filepath.Join(tempDir, "media", "NES")

			got, err := findInstallDir(&config.Instance{}, pl, "nes", namesFromURL(rawURL, ""))
			if err != nil {
				return
			}
			assert.True(t, isWithinDir(systemDir, got),
				"install path escaped the system directory: %s", got)
		})
	}
}

// TestFindInstallDirRefusesDirectoryNames pins the names that resolve to a
// directory rather than a file. Joining one moves the download up a level or
// onto the system folder itself, so they are refused outright rather than
// cleaned into something that looks fine.
func TestFindInstallDirRefusesDirectoryNames(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	pl := setupMockPlatformWithTempDir(t, tempDir)

	for _, filename := range []string{"..", ".", "", "a/b", `a\b`} {
		_, err := findInstallDir(&config.Instance{}, pl, "nes", mediaNames{filename: filename})
		require.ErrorIs(t, err, ErrUnsafeMediaName, "filename %q", filename)
	}
}

// TestFindInstallDirKeepsOrdinaryNames is the other half: the names real
// delivery URLs carry, including spaces and brackets, still resolve inside the
// system's own directory.
func TestFindInstallDirKeepsOrdinaryNames(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	pl := setupMockPlatformWithTempDir(t, tempDir)

	for _, tt := range []struct {
		rawURL   string
		wantBase string
	}{
		{
			rawURL:   "https://cdn.example.com/media/games/jaguar/Downfall.rom",
			wantBase: "Downfall.rom",
		},
		{
			rawURL:   "https://cdn.example.com/media/games/jaguar/Full%20Circle%20-%20Rocketeer%20(Promo).rom",
			wantBase: "Full Circle - Rocketeer (Promo).rom",
		},
	} {
		got, err := findInstallDir(&config.Instance{}, pl, "nes", namesFromURL(tt.rawURL, ""))
		require.NoError(t, err)
		assert.Equal(t, tt.wantBase, filepath.Base(got))
		assert.Equal(t, filepath.Join(tempDir, "media", "NES"), filepath.Dir(got))
	}
}
