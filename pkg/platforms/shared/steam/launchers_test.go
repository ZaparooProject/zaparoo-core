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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSteamLauncher(t *testing.T) {
	t.Parallel()

	t.Run("returns_launcher_with_correct_id", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(Options{})

		assert.Equal(t, "Steam", launcher.ID)
		assert.Equal(t, systemdefs.SystemPC, launcher.SystemID)
		assert.Contains(t, launcher.Schemes, shared.SchemeSteam)
	})

	t.Run("has_scanner_and_launch_functions", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(Options{})

		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})

	t.Run("launcher_rejects_invalid_steam_id", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(Options{})

		// Non-numeric Steam ID should fail
		_, err := launcher.Launch(nil, "steam://not-a-number/game")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Steam game ID")
	})

	t.Run("launcher_rejects_empty_steam_id", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(Options{})

		_, err := launcher.Launch(nil, "steam://")

		assert.Error(t, err)
	})

	t.Run("launcher_rejects_malformed_path", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(Options{})

		_, err := launcher.Launch(nil, "not-a-steam-path")

		assert.Error(t, err)
	})
}

func TestNewSteamLauncherWithDefaultOptions(t *testing.T) {
	t.Parallel()

	t.Run("works_with_linux_defaults", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(DefaultLinuxOptions())

		assert.Equal(t, "Steam", launcher.ID)
		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})

	t.Run("works_with_steamos_defaults", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(DefaultSteamOSOptions())

		assert.Equal(t, "Steam", launcher.ID)
		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})

	t.Run("works_with_windows_defaults", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(DefaultWindowsOptions())

		assert.Equal(t, "Steam", launcher.ID)
		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})

	t.Run("works_with_darwin_defaults", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(DefaultDarwinOptions())

		assert.Equal(t, "Steam", launcher.ID)
		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})

	t.Run("works_with_bazzite_defaults", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(DefaultBazziteOptions())

		assert.Equal(t, "Steam", launcher.ID)
		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})

	t.Run("works_with_chimeraos_defaults", func(t *testing.T) {
		t.Parallel()

		launcher := NewSteamLauncher(DefaultChimeraOSOptions())

		assert.Equal(t, "Steam", launcher.ID)
		assert.NotNil(t, launcher.Scanner)
		assert.NotNil(t, launcher.Launch)
	})
}
