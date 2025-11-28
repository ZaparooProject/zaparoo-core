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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
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

func TestLaunchVideo_EmptyPath(t *testing.T) {
	// Cannot use t.Parallel() - uses Platform instance

	pl := &Platform{
		consoleManager:  newConsoleManager(&Platform{}),
		launcherManager: state.NewLauncherManager(),
	}

	launcherFunc := launchVideo(pl)

	// Call with empty path - should fail validation before hitting hardware
	process, err := launcherFunc(nil, "")

	require.Error(t, err)
	assert.Nil(t, process)
	assert.Contains(t, err.Error(), "no path specified")
}

func TestLaunchScummVM_EmptyPath(t *testing.T) {
	// Cannot use t.Parallel() - uses Platform instance

	pl := &Platform{
		consoleManager:  newConsoleManager(&Platform{}),
		launcherManager: state.NewLauncherManager(),
	}

	launcherFunc := launchScummVM(pl)

	// Call with empty path - should fail validation before hitting hardware
	process, err := launcherFunc(nil, "")

	require.Error(t, err)
	assert.Nil(t, process)
	assert.Contains(t, err.Error(), "no path specified")
}

func TestLaunchScummVM_InvalidPath_NoTargetID(t *testing.T) {
	// Cannot use t.Parallel() - uses Platform instance

	pl := &Platform{
		consoleManager:  newConsoleManager(&Platform{}),
		launcherManager: state.NewLauncherManager(),
	}

	launcherFunc := launchScummVM(pl)

	// Path without target ID - should fail before hardware access
	testPaths := []string{
		"scummvm://",
		"scummvm:///",
		"scummvm:///GameName",
	}

	for _, path := range testPaths {
		process, err := launcherFunc(nil, path)
		require.Error(t, err, "path %s should fail", path)
		assert.Nil(t, process)
		// Error message can be either from ExtractSchemeID (malformed path) or from empty ID check
		assert.True(t,
			strings.Contains(err.Error(), "no ScummVM target ID specified") ||
				strings.Contains(err.Error(), "failed to extract ScummVM target ID"),
			"unexpected error message: %s", err.Error())
	}
}

func TestBuildFvpCommand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := "/media/fat/Videos/test.mp4"

	cmd := buildFvpCommand(ctx, path)

	require.NotNil(t, cmd)

	// Verify args contain the flags and path
	assert.Contains(t, cmd.Args, "-f", "should have fullscreen flag")
	assert.Contains(t, cmd.Args, "-u", "should have A/V diff recording flag")
	assert.Contains(t, cmd.Args, "-s", "should have sync flag")
	assert.Contains(t, cmd.Args, path, "should have video path")

	// Verify binary path is set
	expectedBinary := filepath.Join(misterconfig.LinuxDir, "fvp")
	assert.Contains(t, cmd.Args[0], "fvp", "first arg should be fvp binary")
	assert.Equal(t, expectedBinary, cmd.Args[0])

	// Verify SysProcAttr
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setsid, "should set Setsid for new session")

	// Verify environment
	hasLDPath := false
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "LD_LIBRARY_PATH=") {
			hasLDPath = true
			assert.Contains(t, env, misterconfig.LinuxDir)
			break
		}
	}
	assert.True(t, hasLDPath, "should set LD_LIBRARY_PATH")
}

func TestBuildFvpCommand_DifferentPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{"absolute path", "/media/fat/Videos/movie.mp4"},
		{"with spaces", "/media/fat/My Videos/file.mkv"},
		{"special chars", "/media/fat/Videos/test-file_v2.avi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := buildFvpCommand(context.Background(), tt.path)

			require.NotNil(t, cmd)
			assert.Contains(t, cmd.Args, tt.path, "path should be in args")
		})
	}
}

func TestBuildScummVMCommand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scummvmBinary := "/media/fat/ScummVM/scummvm"
	targetID := "monkey2"

	cmd := buildScummVMCommand(ctx, scummvmBinary, targetID)

	require.NotNil(t, cmd)

	// Verify taskset wrapper is used
	assert.Contains(t, cmd.Args[0], "taskset", "should use taskset")
	assert.Contains(t, cmd.Args, "03", "should have CPU affinity")
	assert.Contains(t, cmd.Args, scummvmBinary, "should have ScummVM binary")
	assert.Contains(t, cmd.Args, targetID, "should have target ID")

	// Verify ScummVM flags
	assert.Contains(t, cmd.Args, "--opl-driver=db")
	assert.Contains(t, cmd.Args, "--output-rate=48000")

	// Verify working directory
	assert.Equal(t, scummvmBaseDir, cmd.Dir)

	// Verify environment variables - check that our custom ones are set
	hasCustomHome := false
	hasCustomLDPath := false
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "HOME=") && strings.Contains(env, scummvmBaseDir) {
			hasCustomHome = true
		}
		if strings.HasPrefix(env, "LD_LIBRARY_PATH=") {
			if strings.Contains(env, "arm-linux-gnueabihf") && strings.Contains(env, "pulseaudio") {
				hasCustomLDPath = true
			}
		}
	}
	assert.True(t, hasCustomHome, "should set custom HOME for ScummVM")
	assert.True(t, hasCustomLDPath, "should set custom LD_LIBRARY_PATH")
}

func TestBuildScummVMCommand_DifferentTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		binary   string
		targetID string
	}{
		{"standard target", "/media/fat/ScummVM/scummvm", "monkey"},
		{"target with numbers", "/media/fat/ScummVM/scummvm", "loom-ega"},
		{"custom binary path", "/tmp/scummvm", "testgame"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := buildScummVMCommand(context.Background(), tt.binary, tt.targetID)

			require.NotNil(t, cmd)
			assert.Contains(t, cmd.Args, tt.binary)
			assert.Contains(t, cmd.Args, tt.targetID)
			assert.Equal(t, scummvmBaseDir, cmd.Dir)
		})
	}
}

// Regression test: N64 launcher should support .v64 extension (byte-swapped ROM format)
func TestN64LauncherExtensions(t *testing.T) {
	t.Parallel()

	pl := NewPlatform()
	launchers := CreateLaunchers(pl)

	// Find the N64 launcher by SystemID
	var n64Launcher *platforms.Launcher
	for i := range launchers {
		if launchers[i].SystemID == "Nintendo64" && len(launchers[i].Extensions) > 0 {
			n64Launcher = &launchers[i]
			break
		}
	}

	require.NotNil(t, n64Launcher, "N64 launcher should exist")

	// Verify all three N64 ROM formats are supported
	expectedExts := []string{".n64", ".z64", ".v64"}
	for _, ext := range expectedExts {
		assert.Contains(t, n64Launcher.Extensions, ext,
			"N64 launcher should support %s extension", ext)
	}
}
