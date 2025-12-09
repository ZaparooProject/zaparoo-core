//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package launchers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSteamLauncher(t *testing.T) {
	t.Parallel()

	t.Run("returns_launcher_with_correct_id", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(SteamOptions{})

		assert.Equal(t, "Steam", launcher.ID)
		assert.Equal(t, systemdefs.SystemPC, launcher.SystemID)
		assert.Contains(t, launcher.Schemes, shared.SchemeSteam)
	})

	t.Run("has_scanner_and_launch_functions", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(SteamOptions{})

		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})
}

func TestSteamLauncherLaunch(t *testing.T) {
	t.Parallel()

	t.Run("rejects_invalid_steam_id", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(SteamOptions{})

		// Non-numeric Steam ID should fail
		_, err := launcher.Launch(nil, "steam://not-a-number/game")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Steam game ID")
	})

	t.Run("rejects_empty_steam_id", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(SteamOptions{})

		_, err := launcher.Launch(nil, "steam://")

		assert.Error(t, err)
	})

	t.Run("rejects_malformed_path", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(SteamOptions{})

		_, err := launcher.Launch(nil, "not-a-steam-path")

		assert.Error(t, err)
	})

	// Note: We don't test actual execution because it would require
	// mocking exec.Command or having Steam installed. The validation
	// tests above cover the important error paths.
}

func TestFindSteamDir(t *testing.T) {
	// Cannot run in parallel due to HOME env modification

	t.Run("finds_default_steam_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create default Steam directory
		steamPath := filepath.Join(home, ".steam", "steam")
		require.NoError(t, os.MkdirAll(steamPath, 0o750))

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			FallbackPath: "/fallback/steam",
		})

		assert.Equal(t, steamPath, result)
	})

	t.Run("finds_local_share_steam_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create .local/share/Steam directory
		steamPath := filepath.Join(home, ".local", "share", "Steam")
		require.NoError(t, os.MkdirAll(steamPath, 0o750))

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			FallbackPath: "/fallback/steam",
		})

		assert.Equal(t, steamPath, result)
	})

	t.Run("prefers_first_found_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create both default paths
		defaultPath := filepath.Join(home, ".steam", "steam")
		require.NoError(t, os.MkdirAll(defaultPath, 0o750))

		localPath := filepath.Join(home, ".local", "share", "Steam")
		require.NoError(t, os.MkdirAll(localPath, 0o750))

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			FallbackPath: "/fallback/steam",
		})

		// Should prefer .steam/steam (listed first)
		assert.Equal(t, defaultPath, result)
	})

	t.Run("uses_extra_paths", func(t *testing.T) {
		home := withTempHome(t)

		// Create custom extra path
		customPath := filepath.Join(home, "custom", "steam")
		require.NoError(t, os.MkdirAll(customPath, 0o750))

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			ExtraPaths:   []string{customPath},
			FallbackPath: "/fallback/steam",
		})

		// Should find the extra path
		assert.Equal(t, customPath, result)
	})

	t.Run("checks_flatpak_path_when_enabled", func(t *testing.T) {
		home := withTempHome(t)

		// Create Flatpak Steam path
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakSteamID, ".steam", "steam")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			CheckFlatpak: true,
			FallbackPath: "/fallback/steam",
		})

		assert.Equal(t, flatpakPath, result)
	})

	t.Run("skips_flatpak_path_when_disabled", func(t *testing.T) {
		home := withTempHome(t)

		// Create only Flatpak Steam path
		flatpakPath := filepath.Join(home, ".var", "app", FlatpakSteamID, ".steam", "steam")
		require.NoError(t, os.MkdirAll(flatpakPath, 0o750))

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			CheckFlatpak: false, // Disabled
			FallbackPath: "/fallback/steam",
		})

		// Should use fallback since Flatpak check is disabled
		assert.Equal(t, "/fallback/steam", result)
	})

	t.Run("uses_fallback_when_no_path_found", func(t *testing.T) {
		_ = withTempHome(t)

		// Don't create any Steam directories

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			FallbackPath: "/custom/fallback/path",
		})

		assert.Equal(t, "/custom/fallback/path", result)
	})

	t.Run("finds_snap_steam_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create Snap Steam path
		snapPath := filepath.Join(home, "snap", "steam", "common", ".steam", "steam")
		require.NoError(t, os.MkdirAll(snapPath, 0o750))

		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result := findSteamDir(cfg, SteamOptions{
			FallbackPath: "/fallback/steam",
		})

		assert.Equal(t, snapPath, result)
	})
}

func TestSteamIDNormalization(t *testing.T) {
	t.Parallel()

	// Test the path normalization behavior by checking what the launcher
	// expects. The Launch function normalizes steam://rungameid/X to steam://X

	t.Run("handles_rungameid_format", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(SteamOptions{})

		// This should extract "123" from "steam://rungameid/123"
		// Note: We can't fully test execution, but we can verify
		// the validation doesn't reject it
		_, err := launcher.Launch(nil, "steam://rungameid/123")

		// Should fail at command execution (no steam installed), not validation
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to launch Steam")
	})

	t.Run("handles_standard_format", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(SteamOptions{})

		// Standard format: steam://123
		_, err := launcher.Launch(nil, "steam://123/SomeGame")

		// Should fail at command execution, not validation
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to launch Steam")
	})
}
