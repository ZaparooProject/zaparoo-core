//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInstallAddsSteamShortcut(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := InstallPaths{
		Binary:   filepath.Join(dir, "bin", "zaparoo"),
		Runtime:  filepath.Join(dir, "bin", runtimeExecutableName),
		Desktop:  filepath.Join(dir, "applications", runtimeExecutableName+".desktop"),
		SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(paths.Binary), 0o750))
	require.NoError(t, os.WriteFile(paths.Binary, []byte("binary"), 0o600))

	executor := &mocks.MockCommandExecutor{}
	executor.On("Run", mock.Anything, "steamos-add-to-steam", []string{paths.Desktop}).Return(nil).Once()
	result, err := installWithExecutor(t.Context(), paths, executor)
	require.NoError(t, err)
	require.True(t, result.ShortcutAdded)
	require.True(t, result.SteamRestartNeeded)

	target, err := os.Readlink(paths.Runtime)
	require.NoError(t, err)
	require.Equal(t, paths.Binary, target)
	desktop, err := os.ReadFile(paths.Desktop) //nolint:gosec // Test-controlled path.
	require.NoError(t, err)
	require.Contains(t, string(desktop), "Name=Zaparoo")
	require.Contains(t, string(desktop), "Exec="+paths.Runtime)
	executor.AssertExpectations(t)
}

func TestInstallDefaultArtwork(t *testing.T) {
	t.Parallel()

	configDir := filepath.Join(t.TempDir(), "userdata", "123", "config")
	locations := []shortcutLocation{{configDir: configDir, appID: 42}}
	require.NoError(t, installDefaultArtwork(locations))

	gridDir := filepath.Join(configDir, "grid")
	for _, name := range []string{"42.png", "42p.png", "42_hero.png", "42_logo.png"} {
		info, err := os.Stat(filepath.Join(gridDir, name))
		require.NoError(t, err)
		require.Positive(t, info.Size())
	}

	custom := []byte("custom artwork")
	coverPath := filepath.Join(gridDir, "42p.png")
	require.NoError(t, os.WriteFile(coverPath, custom, 0o600))
	require.NoError(t, installDefaultArtwork(locations))
	actual, err := os.ReadFile(coverPath) //nolint:gosec // Test-controlled path.
	require.NoError(t, err)
	require.Equal(t, custom, actual)
}

func TestStatusReady(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := InstallPaths{
		Binary: filepath.Join(dir, "zaparoo"), Runtime: filepath.Join(dir, runtimeExecutableName),
		Desktop: filepath.Join(dir, "runtime.desktop"), SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, os.WriteFile(paths.Binary, []byte("binary"), 0o600))
	require.NoError(t, os.Symlink(paths.Binary, paths.Runtime))
	require.NoError(t, os.WriteFile(paths.Desktop, desktopEntry(paths.Runtime), 0o600))
	shortcutPath := filepath.Join(paths.SteamDir, "userdata", "123", "config", "shortcuts.vdf")
	require.NoError(t, os.MkdirAll(filepath.Dir(shortcutPath), 0o750))
	require.NoError(t, os.WriteFile(shortcutPath, shortcutFixture(42, paths.Runtime), 0o600))

	status, err := statusWithPaths(paths)
	require.NoError(t, err)
	require.Equal(t, "ready", status.State)
	require.Len(t, status.ShortcutIDs, 1)
}

func TestStatusDetectsDuplicateShortcuts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := InstallPaths{
		Binary: filepath.Join(dir, "zaparoo"), Runtime: filepath.Join(dir, runtimeExecutableName),
		Desktop: filepath.Join(dir, "runtime.desktop"), SteamDir: filepath.Join(dir, "steam"),
	}
	for _, user := range []string{"123", "456"} {
		shortcutPath := filepath.Join(paths.SteamDir, "userdata", user, "config", "shortcuts.vdf")
		require.NoError(t, os.MkdirAll(filepath.Dir(shortcutPath), 0o750))
		require.NoError(t, os.WriteFile(shortcutPath, shortcutFixture(42, paths.Runtime), 0o600))
	}

	status, err := statusWithPaths(paths)
	require.NoError(t, err)
	require.Equal(t, "duplicate", status.State)
	require.Len(t, status.ShortcutIDs, 2)
}

func TestInstallRefusesRuntimeRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := InstallPaths{
		Binary:   filepath.Join(dir, "zaparoo"),
		Runtime:  filepath.Join(dir, runtimeExecutableName),
		Desktop:  filepath.Join(dir, "runtime.desktop"),
		SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, os.WriteFile(paths.Binary, []byte("binary"), 0o600))
	require.NoError(t, os.WriteFile(paths.Runtime, []byte("do not replace"), 0o600))

	_, err := installWithExecutor(context.Background(), paths, &mocks.MockCommandExecutor{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to replace non-symlink")
}

func TestUninstallRemovesRuntimeFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := InstallPaths{
		Binary:  filepath.Join(dir, "zaparoo"),
		Runtime: filepath.Join(dir, runtimeExecutableName),
		Desktop: filepath.Join(dir, "runtime.desktop"),
	}
	require.NoError(t, os.WriteFile(paths.Binary, []byte("binary"), 0o600))
	require.NoError(t, os.Symlink(paths.Binary, paths.Runtime))
	require.NoError(t, os.WriteFile(paths.Desktop, []byte("desktop"), 0o600))

	require.NoError(t, uninstallWithPaths(paths))
	_, runtimeErr := os.Lstat(paths.Runtime)
	require.ErrorIs(t, runtimeErr, os.ErrNotExist)
	_, desktopErr := os.Stat(paths.Desktop)
	require.ErrorIs(t, desktopErr, os.ErrNotExist)
}

func TestUninstallRefusesRuntimeRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := InstallPaths{
		Runtime: filepath.Join(dir, runtimeExecutableName),
		Desktop: filepath.Join(dir, "runtime.desktop"),
	}
	require.NoError(t, os.WriteFile(paths.Runtime, []byte("preserve"), 0o600))
	require.NoError(t, os.WriteFile(paths.Desktop, []byte("preserve"), 0o600))

	err := uninstallWithPaths(paths)
	require.ErrorContains(t, err, "refusing to remove non-symlink")
	actual, readErr := os.ReadFile(paths.Runtime) //nolint:gosec // Test-controlled path.
	require.NoError(t, readErr)
	require.Equal(t, []byte("preserve"), actual)
}
