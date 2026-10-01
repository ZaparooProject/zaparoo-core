//go:build linux && !android

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

package mister

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngIEND is the fixed tail that marks a PNG as fully written.
var pngIEND = []byte{0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}

const testCaptureTimeout = 500 * time.Millisecond

// TestScreenshotTimeoutCoversMainWriteTime pins the wait to Main's real write
// time. On a MiSTer with a full-HD output Main creates the PNG at once but
// takes 3.3 to 3.7 s to finish writing it; a 3 s wait failed about half of all
// captures for a screenshot that did succeed. The wait must stay inside the
// API request timeout, or the request is cancelled before Core can report.
func TestScreenshotTimeoutCoversMainWriteTime(t *testing.T) {
	t.Parallel()

	assert.GreaterOrEqual(t, screenshotTimeout, 8*time.Second)
	assert.Less(t, screenshotTimeout, config.APIRequestTimeout)
}

func TestScreenshotWatchDirs(t *testing.T) {
	t.Parallel()

	dir := func(name string) string {
		return filepath.Join(misterconfig.ScreenshotsDir, name)
	}

	tests := []struct {
		name     string
		coreName string
		rbfName  string
		want     []string
	}{
		{
			name:     "setname override watches both",
			coreName: "RA_SNES",
			rbfName:  "SNES",
			want:     []string{dir("RA_SNES"), dir("SNES")},
		},
		{
			name:     "same names watch one",
			coreName: "SNES",
			rbfName:  "SNES",
			want:     []string{dir("SNES")},
		},
		{
			name:     "missing rbf name watches core name only",
			coreName: "MENU",
			rbfName:  "",
			want:     []string{dir("MENU")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, screenshotWatchDirs(tt.coreName, tt.rbfName))
		})
	}
}

func TestCaptureScreenshot_FindsFileInSecondDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	coreDir := filepath.Join(root, "RA_SNES")
	rbfDir := filepath.Join(root, "SNES")
	shot := filepath.Join(rbfDir, "shot.png")

	result, err := captureScreenshot([]string{coreDir, rbfDir}, testCaptureTimeout, func() error {
		return os.WriteFile(shot, pngIEND, 0o600)
	})

	require.NoError(t, err)
	assert.Equal(t, shot, result.Path)
	assert.Equal(t, pngIEND, result.Data)
}

func TestCaptureScreenshot_RemovesOnlyUnusedCreatedDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	coreDir := filepath.Join(root, "RA_SNES")
	rbfDir := filepath.Join(root, "SNES")

	_, err := captureScreenshot([]string{coreDir, rbfDir}, testCaptureTimeout, func() error {
		return os.WriteFile(filepath.Join(rbfDir, "shot.png"), pngIEND, 0o600)
	})

	require.NoError(t, err)
	assert.NoDirExists(t, coreDir, "empty directory created by the call is removed")
	assert.DirExists(t, rbfDir, "directory holding the screenshot is kept")
}

func TestCaptureScreenshot_KeepsPreexistingEmptyDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	existing := filepath.Join(root, "SNES")
	require.NoError(t, os.Mkdir(existing, 0o750))

	_, err := captureScreenshot([]string{existing}, testCaptureTimeout, func() error {
		return errors.New("trigger failed")
	})

	require.Error(t, err)
	assert.DirExists(t, existing)
}

func TestCaptureScreenshot_IgnoresOtherExtensions(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "SNES")

	_, err := captureScreenshot([]string{dir}, 100*time.Millisecond, func() error {
		return os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600)
	})

	require.ErrorContains(t, err, "screenshot timed out")
}

func TestCaptureScreenshot_TriggerError(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "SNES")
	triggerErr := errors.New("no command interface")

	result, err := captureScreenshot([]string{dir}, testCaptureTimeout, func() error {
		return triggerErr
	})

	require.ErrorIs(t, err, triggerErr)
	assert.Nil(t, result)
	assert.NoDirExists(t, dir)
}

func TestCaptureScreenshot_IncompleteFileTimesOut(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "SNES")

	_, err := captureScreenshot([]string{dir}, 300*time.Millisecond, func() error {
		return os.WriteFile(filepath.Join(dir, "shot.png"), pngIEND[:4], 0o600)
	})

	require.ErrorContains(t, err, "screenshot file incomplete")
}

func TestCaptureScreenshot_CreateDirError(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	_, err := captureScreenshot([]string{filepath.Join(blocker, "SNES")}, testCaptureTimeout, func() error {
		return nil
	})

	require.ErrorContains(t, err, "create screenshots dir")
}
