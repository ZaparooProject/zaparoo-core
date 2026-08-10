//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInstallAddsSteamShortcut(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fs := afero.NewOsFs()
	paths := &InstallPaths{
		FS:       fs,
		Binary:   filepath.Join(dir, "bin", "zaparoo"),
		Runtime:  filepath.Join(dir, "bin", runtimeExecutableName),
		Desktop:  filepath.Join(dir, "applications", runtimeExecutableName+".desktop"),
		SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, fs.MkdirAll(filepath.Dir(paths.Binary), 0o750))
	require.NoError(t, afero.WriteFile(fs, paths.Binary, []byte("binary"), 0o600))

	executor := &mocks.MockCommandExecutor{}
	executor.On("Run", mock.Anything, "steamos-add-to-steam", []string{paths.Desktop}).Return(nil).Once()
	result, err := installWithExecutor(t.Context(), paths, executor)
	require.NoError(t, err)
	require.True(t, result.ShortcutAdded)
	require.True(t, result.SteamRestartNeeded)

	target, err := readlinkFS(fs, paths.Runtime)
	require.NoError(t, err)
	require.Equal(t, paths.Binary, target)
	desktop, err := afero.ReadFile(fs, paths.Desktop)
	require.NoError(t, err)
	require.Contains(t, string(desktop), "Name=Zaparoo")
	require.Contains(t, string(desktop), "Exec=\""+paths.Runtime+"\"")
	executor.AssertExpectations(t)
}

func TestDesktopEntryQuotesRuntimePath(t *testing.T) {
	t.Parallel()

	runtimePath := filepath.Join(string(filepath.Separator), "home", "deck", "Zaparoo Runtime", `runtime$1`)
	entry := string(desktopEntry(runtimePath))
	escapedPath := filepath.Join(string(filepath.Separator), "home", "deck", "Zaparoo Runtime", "runtime") + `\$1`
	expected := `Exec="` + escapedPath + `"`

	require.Contains(t, entry, expected)
}

func TestInstallDefaultArtwork(t *testing.T) {
	t.Parallel()

	fs := afero.NewOsFs()
	configDir := filepath.Join(t.TempDir(), "userdata", "123", "config")
	locations := []shortcutLocation{{configDir: configDir, appID: 42}}
	require.NoError(t, installDefaultArtwork(fs, locations))

	gridDir := filepath.Join(configDir, "grid")
	for _, name := range []string{"42.png", "42p.png", "42_hero.png", "42_logo.png"} {
		info, err := fs.Stat(filepath.Join(gridDir, name))
		require.NoError(t, err)
		require.Positive(t, info.Size())
	}

	custom := []byte("custom artwork")
	coverPath := filepath.Join(gridDir, "42p.png")
	require.NoError(t, afero.WriteFile(fs, coverPath, custom, 0o600))
	require.NoError(t, installDefaultArtwork(fs, locations))
	actual, err := afero.ReadFile(fs, coverPath)
	require.NoError(t, err)
	require.Equal(t, custom, actual)
}

func TestStatusReady(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fs := afero.NewOsFs()
	paths := &InstallPaths{
		FS: fs, Binary: filepath.Join(dir, "zaparoo"), Runtime: filepath.Join(dir, runtimeExecutableName),
		Desktop: filepath.Join(dir, "runtime.desktop"), SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, afero.WriteFile(fs, paths.Binary, []byte("binary"), 0o600))
	require.NoError(t, symlinkFS(fs, paths.Binary, paths.Runtime))
	require.NoError(t, afero.WriteFile(fs, paths.Desktop, desktopEntry(paths.Runtime), 0o600))
	shortcutPath := filepath.Join(paths.SteamDir, "userdata", "123", "config", "shortcuts.vdf")
	require.NoError(t, fs.MkdirAll(filepath.Dir(shortcutPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, shortcutPath, runtimeShortcutFixture(42, paths.Runtime), 0o600))

	status, err := statusWithPaths(paths)
	require.NoError(t, err)
	require.Equal(t, "ready", status.State)
	require.Len(t, status.ShortcutIDs, 1)
}

func TestStatusDetectsDuplicateShortcuts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fs := afero.NewOsFs()
	paths := &InstallPaths{
		FS: fs, Binary: filepath.Join(dir, "zaparoo"), Runtime: filepath.Join(dir, runtimeExecutableName),
		Desktop: filepath.Join(dir, "runtime.desktop"), SteamDir: filepath.Join(dir, "steam"),
	}
	for _, user := range []string{"123", "456"} {
		shortcutPath := filepath.Join(paths.SteamDir, "userdata", user, "config", "shortcuts.vdf")
		require.NoError(t, fs.MkdirAll(filepath.Dir(shortcutPath), 0o750))
		require.NoError(t, afero.WriteFile(fs, shortcutPath, runtimeShortcutFixture(42, paths.Runtime), 0o600))
	}

	status, err := statusWithPaths(paths)
	require.NoError(t, err)
	require.Equal(t, "duplicate", status.State)
	require.Len(t, status.ShortcutIDs, 2)
}

func TestInstallRefusesRuntimeRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fs := afero.NewOsFs()
	paths := &InstallPaths{
		FS:       fs,
		Binary:   filepath.Join(dir, "zaparoo"),
		Runtime:  filepath.Join(dir, runtimeExecutableName),
		Desktop:  filepath.Join(dir, "runtime.desktop"),
		SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, afero.WriteFile(fs, paths.Binary, []byte("binary"), 0o600))
	require.NoError(t, afero.WriteFile(fs, paths.Runtime, []byte("do not replace"), 0o600))

	_, err := installWithExecutor(t.Context(), paths, &mocks.MockCommandExecutor{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to replace non-symlink")
}

func TestUninstallRemovesRuntimeFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fs := afero.NewOsFs()
	paths := &InstallPaths{
		FS: fs, Binary: filepath.Join(dir, "zaparoo"),
		Runtime: filepath.Join(dir, runtimeExecutableName), Desktop: filepath.Join(dir, "runtime.desktop"),
	}
	require.NoError(t, afero.WriteFile(fs, paths.Binary, []byte("binary"), 0o600))
	require.NoError(t, symlinkFS(fs, paths.Binary, paths.Runtime))
	require.NoError(t, afero.WriteFile(fs, paths.Desktop, []byte("desktop"), 0o600))

	require.NoError(t, uninstallWithPaths(paths))
	_, runtimeErr := lstatFS(fs, paths.Runtime)
	require.ErrorIs(t, runtimeErr, os.ErrNotExist)
	_, desktopErr := fs.Stat(paths.Desktop)
	require.ErrorIs(t, desktopErr, os.ErrNotExist)
}

func TestUninstallRefusesRuntimeRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fs := afero.NewOsFs()
	paths := &InstallPaths{
		FS: fs, Runtime: filepath.Join(dir, runtimeExecutableName),
		Desktop: filepath.Join(dir, "runtime.desktop"),
	}
	require.NoError(t, afero.WriteFile(fs, paths.Runtime, []byte("preserve"), 0o600))
	require.NoError(t, afero.WriteFile(fs, paths.Desktop, []byte("preserve"), 0o600))

	err := uninstallWithPaths(paths)
	require.ErrorContains(t, err, "refusing to remove non-symlink")
	actual, readErr := afero.ReadFile(fs, paths.Runtime)
	require.NoError(t, readErr)
	require.Equal(t, []byte("preserve"), actual)
}
