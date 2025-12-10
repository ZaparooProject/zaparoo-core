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

package steam

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// withTempHome sets HOME to a temporary directory for the duration of the test.
// Returns the temp directory path.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	return tmpDir
}

func TestClientFindSteamDir(t *testing.T) {
	// Cannot run in parallel due to HOME env modification

	t.Run("finds_default_steam_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create default Steam directory
		steamPath := filepath.Join(home, ".steam", "steam")
		require.NoError(t, os.MkdirAll(steamPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, steamPath, result)
	})

	t.Run("finds_local_share_steam_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create .local/share/Steam directory
		steamPath := filepath.Join(home, ".local", "share", "Steam")
		require.NoError(t, os.MkdirAll(steamPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, steamPath, result)
	})

	t.Run("prefers_first_found_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create both default paths
		defaultPath := filepath.Join(home, ".steam", "steam")
		require.NoError(t, os.MkdirAll(defaultPath, 0o750))

		localPath := filepath.Join(home, ".local", "share", "Steam")
		require.NoError(t, os.MkdirAll(localPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		// Should prefer .steam/steam (listed first)
		assert.Equal(t, defaultPath, result)
	})

	t.Run("uses_extra_paths", func(t *testing.T) {
		home := withTempHome(t)

		// Create custom extra path
		customPath := filepath.Join(home, "custom", "steam")
		require.NoError(t, os.MkdirAll(customPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			ExtraPaths:   []string{customPath},
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		// Should find the extra path
		assert.Equal(t, customPath, result)
	})

	t.Run("checks_flatpak_path_when_enabled", func(t *testing.T) {
		home := withTempHome(t)

		// Create Flatpak Steam path
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakSteamID, ".steam", "steam")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			CheckFlatpak: true,
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, flatpakPath, result)
	})

	t.Run("skips_flatpak_path_when_disabled", func(t *testing.T) {
		home := withTempHome(t)

		// Create only Flatpak Steam path
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakSteamID, ".steam", "steam")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			CheckFlatpak: false, // Disabled
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		// Should use fallback since Flatpak check is disabled
		assert.Equal(t, "/fallback/steam", result)
	})

	t.Run("uses_fallback_when_no_path_found", func(t *testing.T) {
		_ = withTempHome(t)

		// Don't create any Steam directories

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			FallbackPath: "/custom/fallback/path",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, "/custom/fallback/path", result)
	})

	t.Run("uses_user_configured_install_dir", func(t *testing.T) {
		home := withTempHome(t)

		// Create user-configured directory
		customPath := filepath.Join(home, "custom", "Steam")
		require.NoError(t, os.MkdirAll(customPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		configDir := t.TempDir()
		cfg, err := testhelpers.NewTestConfig(fs, configDir)
		require.NoError(t, err)

		// Configure custom install dir
		cfg.SetLauncherDefaultsForTesting([]config.LaunchersDefault{
			{Launcher: "Steam", InstallDir: customPath},
		})

		client := NewClient(Options{
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, customPath, result)
	})

	t.Run("finds_snap_steam_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create Snap Steam path
		snapPath := filepath.Join(home, "snap", "steam", "common", ".steam", "steam")
		require.NoError(t, os.MkdirAll(snapPath, 0o750))

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, snapPath, result)
	})
}

func TestClientLaunch(t *testing.T) {
	t.Parallel()

	t.Run("rejects_invalid_steam_id", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{}, mockCmd)

		// Non-numeric Steam ID should fail before even trying to run command
		_, err := client.Launch(nil, "steam://not-a-number/game", nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Steam game ID")
		mockCmd.AssertNotCalled(t, "Start", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("rejects_empty_steam_id", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://", nil)

		require.Error(t, err)
		mockCmd.AssertNotCalled(t, "Start", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("rejects_malformed_path", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "not-a-steam-path", nil)

		require.Error(t, err)
		mockCmd.AssertNotCalled(t, "Start", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("launches_with_xdg_open_when_enabled", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		_, err := client.Launch(nil, "steam://123/GameName", nil)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://rungameid/123"})
	})

	t.Run("launches_with_steam_command_when_xdg_disabled", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: false}, mockCmd)

		_, err := client.Launch(nil, "steam://456/AnotherGame", nil)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "steam", []string{"steam://rungameid/456"})
	})

	t.Run("handles_rungameid_format", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		_, err := client.Launch(nil, "steam://rungameid/789", nil)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://rungameid/789"})
	})

	t.Run("handles_large_bpid", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		// Big Picture ID format for non-Steam games
		_, err := client.Launch(nil, "steam://2305843009213693952/NonSteamGame", nil)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://rungameid/2305843009213693952"})
	})

	t.Run("returns_error_when_command_fails", func(t *testing.T) {
		t.Parallel()

		mockCmd := &mocks.MockCommandExecutor{}
		mockCmd.On("Start", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("command failed"))
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		_, err := client.Launch(nil, "steam://123/Game", nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to launch Steam")
	})

	t.Run("launches_details_page_when_action_is_details", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		opts := &platforms.LaunchOptions{Action: "details"}
		_, err := client.Launch(nil, "steam://123/GameName", opts)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://nav/games/details/123"})
	})

	t.Run("launches_details_page_case_insensitive", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		opts := &platforms.LaunchOptions{Action: "DETAILS"}
		_, err := client.Launch(nil, "steam://456/Game", opts)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://nav/games/details/456"})
	})

	t.Run("launches_normal_url_when_action_is_run", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		opts := &platforms.LaunchOptions{Action: "run"}
		_, err := client.Launch(nil, "steam://123/GameName", opts)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://rungameid/123"})
	})

	t.Run("uses_config_action_when_opts_is_nil", func(t *testing.T) {
		// Cannot use t.Parallel() due to config modification

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure Steam launcher with action=details
		cfg.SetLauncherDefaultsForTesting([]config.LaunchersDefault{
			{Launcher: "Steam", Action: "details"},
		})

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		_, err = client.Launch(cfg, "steam://123/Game", nil)

		require.NoError(t, err)
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://nav/games/details/123"})
	})

	t.Run("opts_action_overrides_config_action", func(t *testing.T) {
		// Cannot use t.Parallel() due to config modification

		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		// Configure Steam launcher with action=details
		cfg.SetLauncherDefaultsForTesting([]config.LaunchersDefault{
			{Launcher: "Steam", Action: "details"},
		})

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{UseXdgOpen: true}, mockCmd)

		// Override with action=run from opts
		opts := &platforms.LaunchOptions{Action: "run"}
		_, err = client.Launch(cfg, "steam://123/Game", opts)

		require.NoError(t, err)
		// Should use run URL because opts override config
		mockCmd.AssertCalled(t, "Start", mock.Anything, "xdg-open", []string{"steam://rungameid/123"})
	})
}
