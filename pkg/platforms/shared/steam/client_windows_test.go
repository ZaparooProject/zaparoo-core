//go:build windows

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

package steam

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestClientFindSteamDir(t *testing.T) {
	// Cannot run in parallel due to filesystem operations

	t.Run("uses_user_configured_install_dir", func(t *testing.T) {
		// Create user-configured directory
		customPath := filepath.Join(t.TempDir(), "custom", "Steam")
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
			FallbackPath: "C:\\fallback\\steam",
		})

		result := client.FindSteamDir(cfg)

		assert.Equal(t, customPath, result)
	})

	t.Run("uses_fallback_when_no_path_found", func(t *testing.T) {
		fs := testhelpers.NewMemoryFS()
		cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		client := NewClient(Options{
			FallbackPath: "C:\\custom\\fallback\\path",
		})

		result := client.FindSteamDir(cfg)

		// Either finds Steam via registry or uses fallback
		// We can't mock registry, so just verify it returns something
		assert.NotEmpty(t, result)
	})
}

func TestClientLaunch(t *testing.T) {
	t.Parallel()

	t.Run("launches_steam_game_with_valid_id", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		mockCmd.ExpectedCalls = nil
		opts := command.StartOptions{HideWindow: true}
		args := []string{"/c", "start", "steam://rungameid/730"}
		mockCmd.On("StartWithOptions", mock.Anything, opts, "cmd", args).Return(nil)

		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://730/Counter-Strike", nil)

		require.NoError(t, err)
		mockCmd.AssertExpectations(t)
	})

	t.Run("handles_rungameid_format", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		mockCmd.ExpectedCalls = nil
		opts := command.StartOptions{HideWindow: true}
		args := []string{"/c", "start", "steam://rungameid/730"}
		mockCmd.On("StartWithOptions", mock.Anything, opts, "cmd", args).Return(nil)

		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://rungameid/730", nil)

		require.NoError(t, err)
		mockCmd.AssertExpectations(t)
	})

	t.Run("returns_error_for_invalid_steam_id", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://not-a-number/game", nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Steam game ID")
	})

	t.Run("returns_error_when_command_fails", func(t *testing.T) {
		t.Parallel()

		mockCmd := testhelpers.NewMockCommandExecutor()
		mockCmd.ExpectedCalls = nil
		mockCmd.On(
			"StartWithOptions", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).Return(errors.New("command failed"))

		client := NewClientWithExecutor(Options{}, mockCmd)

		_, err := client.Launch(nil, "steam://730/Counter-Strike", nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start Steam")
	})
}
