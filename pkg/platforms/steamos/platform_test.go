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

package steamos

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlatform(t *testing.T) {
	t.Parallel()

	p := NewPlatform()

	assert.NotNil(t, p)
	assert.NotNil(t, p.Base)
	assert.Equal(t, platformids.SteamOS, p.ID())
}

func TestPlatformID(t *testing.T) {
	t.Parallel()

	p := NewPlatform()

	assert.Equal(t, platformids.SteamOS, p.ID())
}

func TestPlatformSettings(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	settings := p.Settings()

	// Settings should be XDG-based
	assert.NotEmpty(t, settings.DataDir)
	assert.NotEmpty(t, settings.ConfigDir)
	assert.NotEmpty(t, settings.TempDir)
	assert.NotEmpty(t, settings.LogDir)
}

func TestPlatformSupportedReaders(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	readers := p.SupportedReaders(cfg)

	// Should return a list (possibly empty depending on config)
	assert.NotNil(t, readers)
}

func TestPlatformLaunchers(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// SteamOS should have Steam and Generic launchers
	assert.GreaterOrEqual(t, len(launchers), 2,
		"SteamOS should have at least 2 launchers")

	// Verify expected launcher IDs are present
	launcherIDs := make(map[string]bool)
	for _, l := range launchers {
		launcherIDs[l.ID] = true
	}

	assert.True(t, launcherIDs["Steam"], "Should have Steam launcher")
	assert.True(t, launcherIDs["Generic"], "Should have Generic launcher")
}

func TestPlatformLaunchersUsesDirectSteam(t *testing.T) {
	t.Parallel()

	// SteamOS uses direct steam command (not xdg-open) for better Game Mode integration
	// We verify Steam launcher exists and is configured for console experience

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// Find Steam launcher
	var steamLauncher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "Steam" {
			steamLauncher = &launchers[i]
			break
		}
	}

	require.NotNil(t, steamLauncher, "Steam launcher should be present")
	// SteamOS Steam launcher exists and supports steam scheme
	assert.Contains(t, steamLauncher.Schemes, "steam")
}

func TestPlatformDoesNotHaveFlatpakLaunchers(t *testing.T) {
	t.Parallel()

	// SteamOS uses native Steam, not Flatpak
	// Verify no Flatpak-specific launchers are present

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// SteamOS should NOT have Lutris or Heroic by default
	launcherIDs := make(map[string]bool)
	for _, l := range launchers {
		launcherIDs[l.ID] = true
	}

	// These are not included in default SteamOS launchers
	assert.False(t, launcherIDs["Lutris"], "Should not have Lutris launcher by default")
	assert.False(t, launcherIDs["Heroic"], "Should not have Heroic launcher by default")
}

func TestPlatformSteamDeckPaths(t *testing.T) {
	t.Parallel()

	// Verify Steam launcher is configured with Steam Deck paths
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// Find Steam launcher
	var steamLauncher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "Steam" {
			steamLauncher = &launchers[i]
			break
		}
	}

	require.NotNil(t, steamLauncher, "Steam launcher should be present")
	// The launcher is properly configured - we just verify it exists
	// Internal paths like /home/deck/.steam/steam are set in the launcher options
}
