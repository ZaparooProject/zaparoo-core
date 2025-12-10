//go:build darwin

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

	t.Run("finds_macos_steam_path", func(t *testing.T) {
		home := withTempHome(t)

		// Create macOS Steam directory
		steamPath := filepath.Join(home, "Library", "Application Support", "Steam")
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
		cfg.SetLauncherDefaults("Steam", customPath, "")

		client := NewClient(Options{
			FallbackPath: "/fallback/steam",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, customPath, result)
	})
}

func TestClientLaunch(t *testing.T) {
	t.Parallel()

	t.Run("launches_steam_game_with_valid_id", func(t *testing.T) {
		t.Parallel()

		mockCmd := mocks.NewMockCommandExecutor()
		mockCmd.On("Start", mock.Anything, "open", "steam://rungameid/730").Return(nil)

		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://730/Counter-Strike")

		require.NoError(t, err)
		mockCmd.AssertExpectations(t)
	})

	t.Run("handles_rungameid_format", func(t *testing.T) {
		t.Parallel()

		mockCmd := mocks.NewMockCommandExecutor()
		mockCmd.On("Start", mock.Anything, "open", "steam://rungameid/730").Return(nil)

		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://rungameid/730")

		require.NoError(t, err)
		mockCmd.AssertExpectations(t)
	})

	t.Run("returns_error_for_invalid_steam_id", func(t *testing.T) {
		t.Parallel()

		mockCmd := mocks.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://not-a-number/game")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Steam game ID")
	})

	t.Run("returns_error_when_open_fails", func(t *testing.T) {
		t.Parallel()

		mockCmd := mocks.NewMockCommandExecutor()
		mockCmd.On("Start", mock.Anything, "open", mock.Anything).Return(errors.New("open failed"))

		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://730/Counter-Strike")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to launch Steam")
	})
}
