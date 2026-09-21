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

package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLogEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "error level",
			input:    `{"level":"error","time":"2025-11-20T13:04:23Z","message":"service failed to start"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:23 service failed to start",
		},
		{
			name:     "warn level",
			input:    `{"level":"warn","time":"2025-11-20T13:04:22Z","message":"config file not found"}`,
			expected: "[yellow::b] WARN[-:-:-] 13:04:22 config file not found",
		},
		{
			name:     "info level",
			input:    `{"level":"info","time":"2025-11-20T13:04:21Z","message":"service starting"}`,
			expected: "[green::b] INFO[-:-:-] 13:04:21 service starting",
		},
		{
			name:     "debug level",
			input:    `{"level":"debug","time":"2025-11-20T13:04:20Z","message":"loading config"}`,
			expected: "[gray::b]DEBUG[-:-:-] 13:04:20 loading config",
		},
		{
			name:     "invalid JSON",
			input:    "this is not json",
			expected: "this is not json",
		},
		{
			name:     "stack trace line",
			input:    "    at some.function (file.go:123)",
			expected: "    at some.function (file.go:123)",
		},
		{
			name:     "unknown level",
			input:    `{"level":"trace","time":"2025-11-20T13:04:20Z","message":"test"}`,
			expected: "[white::b]TRACE[-:-:-] 13:04:20 test",
		},
		{
			name:     "missing message field",
			input:    `{"level":"info","time":"2025-11-20T13:04:20Z"}`,
			expected: "[green::b] INFO[-:-:-] 13:04:20 ",
		},
		{
			name:     "malformed timestamp",
			input:    `{"level":"info","time":"invalid","message":"test"}`,
			expected: "[green::b] INFO[-:-:-] invalid test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatLogEntry(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatLogContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "multiple lines newest first",
			input: `{"level":"info","time":"2025-11-20T13:04:20Z","message":"first"}
{"level":"warn","time":"2025-11-20T13:04:21Z","message":"second"}
{"level":"error","time":"2025-11-20T13:04:22Z","message":"third"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:22 third\n" +
				"[yellow::b] WARN[-:-:-] 13:04:21 second\n" +
				"[green::b] INFO[-:-:-] 13:04:20 first",
		},
		{
			name:     "empty content",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "\n\n  \n\n",
			expected: "",
		},
		{
			name: "mixed valid and invalid JSON",
			input: `{"level":"info","time":"2025-11-20T13:04:20Z","message":"valid"}
not json line
{"level":"error","time":"2025-11-20T13:04:22Z","message":"valid2"}`,
			expected: "[red::b]ERROR[-:-:-] 13:04:22 valid2\nnot json line\n[green::b] INFO[-:-:-] 13:04:20 valid",
		},
		{
			name:     "single line",
			input:    `{"level":"debug","time":"2025-11-20T13:04:20Z","message":"single"}`,
			expected: "[gray::b]DEBUG[-:-:-] 13:04:20 single",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := formatLogContent(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadLastLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		expected    string
		numLines    int
		expectError bool
	}{
		{
			name:     "read last 3 lines from 5",
			content:  "line1\nline2\nline3\nline4\nline5\n",
			numLines: 3,
			expected: "line3\nline4\nline5",
		},
		{
			name:     "read more lines than exist",
			content:  "line1\nline2\n",
			numLines: 10,
			expected: "line1\nline2",
		},
		{
			name:     "read all lines",
			content:  "line1\nline2\nline3\n",
			numLines: 3,
			expected: "line1\nline2\nline3",
		},
		{
			name:     "empty file",
			content:  "",
			numLines: 10,
			expected: "",
		},
		{
			name:     "single line no newline",
			content:  "single",
			numLines: 10,
			expected: "single",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test.log")
			err := os.WriteFile(tmpFile, []byte(tt.content), 0o600)
			require.NoError(t, err)

			// Test readLastLines
			result, err := readLastLines(tmpFile, tt.numLines)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestReadLastLinesNonexistentFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "doesnotexist.log")

	result, err := readLastLines(nonexistent, 10)

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "failed to read log file")
}

func TestCopyLogToSd_Success(t *testing.T) {
	t.Parallel()

	// Setup: create temp directories and log file
	logDir := t.TempDir()
	destDir := t.TempDir()

	logContent := []byte("test log content\nline 2\nline 3")
	logPath := filepath.Join(logDir, config.LogFile)
	err := os.WriteFile(logPath, logContent, 0o600)
	require.NoError(t, err)

	destPath := filepath.Join(destDir, "exported.log")

	// Create mock platform with custom LogDir
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		LogDir: logDir,
	})

	// Execute
	result := copyLogToSd(mockPlatform, destPath, "SD Card")

	// Verify the result message
	assert.Contains(t, result, "Copied")
	assert.Contains(t, result, config.LogFile)
	assert.Contains(t, result, "SD Card")

	// Verify file was actually copied with correct content
	//nolint:gosec // destPath is from t.TempDir(), safe in tests
	copiedContent, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, logContent, copiedContent)

	mockPlatform.AssertExpectations(t)
}

func TestCopyLogToSd_Error(t *testing.T) {
	t.Parallel()

	// Setup: create temp directory but no log file (simulates missing log)
	logDir := t.TempDir()
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "exported.log")

	// Create mock platform with LogDir pointing to empty directory
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		LogDir: logDir,
	})

	// Execute - should fail because source log file doesn't exist
	result := copyLogToSd(mockPlatform, destPath, "SD Card")

	// Verify the error message
	assert.Contains(t, result, "Unable to copy")
	assert.Contains(t, result, "SD Card")

	// Verify destination file was not created
	_, err := os.Stat(destPath)
	assert.True(t, os.IsNotExist(err), "Destination file should not exist after failed copy")

	mockPlatform.AssertExpectations(t)
}

// The log page is now reachable from two places, and its return target used to
// be a hardcoded switch to the settings page. Opened from the main menu, which
// is the case that matters when the service is down, that page has never been
// registered — and switching to an unknown page is a no-op, so the user would
// have been left on the log page with no way out.
func TestBuildExportLogModal_ReturnsToItsCaller(t *testing.T) {
	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), DataDir: t.TempDir()})

	wentBack := make(chan struct{}, 1)
	pages := tview.NewPages()
	runner.Start(pages)
	runner.QueueUpdateDraw(func() {
		BuildExportLogModal(pages, runner.App(), pl, "", "", func() {
			wentBack <- struct{}{}
		})
	})

	require.True(t, runner.WaitForText("Export Logs", uiSettleTimeout))

	runner.SimulateEscape()
	select {
	case <-wentBack:
	case <-time.After(uiSettleTimeout):
		t.Fatal("escape did not return to the caller")
	}
}

// The TUI's upload used to read the bundle itself and say so when it could not.
// Routing the read through helpers.UploadLog lost that until the read failure
// got its own sentinel, and this is the surface where a user reads the result:
// telling somebody with no log file that the upload failed sends them looking
// at their network.
//
// The loading dialog matters as much as the wording. It is put up before the
// upload and taken down after, so a failure that left it up would trap the user
// behind "Uploading log file..." for good.
func TestUploadLog_SaysTheLogCouldNotBeRead(t *testing.T) {
	runner := NewTestAppRunner(t, 75, 15)
	defer runner.Stop()

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{LogDir: t.TempDir(), DataDir: t.TempDir()})

	pages := tview.NewPages()
	runner.Start(pages)

	// Run on the application's own loop, which is where the Upload button's
	// handler runs it. uploadLog forces a draw, and tview forbids that from any
	// other goroutine — doing it from the test goroutine is a data race the
	// race detector catches.
	var outcome string
	var dialogLeftUp bool
	runner.QueueUpdateDraw(func() {
		outcome = uploadLog(pl, pages, runner.App())
		dialogLeftUp = pages.HasPage("temp_upload")
	})

	assert.Equal(t, "Unable to read log file.", outcome)
	assert.False(t, dialogLeftUp,
		"the loading dialog has to come down whatever the upload did")
}
