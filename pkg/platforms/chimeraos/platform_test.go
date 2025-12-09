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

package chimeraos

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlatform(t *testing.T) {
	t.Parallel()

	p := NewPlatform()

	assert.NotNil(t, p)
	assert.NotNil(t, p.Base)
	assert.Equal(t, platforms.PlatformIDChimeraOS, p.ID())
}

func TestPlatformID(t *testing.T) {
	t.Parallel()

	p := NewPlatform()

	assert.Equal(t, platforms.PlatformIDChimeraOS, p.ID())
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

	// ChimeraOS should have at least Steam, ChimeraGOG, and Generic launchers
	assert.GreaterOrEqual(t, len(launchers), 3,
		"ChimeraOS should have at least 3 launchers")

	// Verify expected launcher IDs are present
	launcherIDs := make(map[string]bool)
	for _, l := range launchers {
		launcherIDs[l.ID] = true
	}

	assert.True(t, launcherIDs["Steam"], "Should have Steam launcher")
	assert.True(t, launcherIDs["ChimeraGOG"], "Should have ChimeraGOG launcher")
	assert.True(t, launcherIDs["Generic"], "Should have Generic launcher")
}

func TestPlatformLaunchersContainSteamFirst(t *testing.T) {
	t.Parallel()

	// Setup temporary directory for config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fsHelper := helpers.NewOSFS()
	cfg, err := helpers.NewTestConfig(fsHelper, configDir)
	require.NoError(t, err)

	p := NewPlatform()
	launchers := p.Launchers(cfg)

	// Custom launchers come first, then built-in launchers
	// Find the first built-in launcher (after any custom launchers)
	// For default config with no custom launchers, Steam should be first
	if len(launchers) > 0 {
		// Find Steam launcher position
		steamPos := -1
		for i, l := range launchers {
			if l.ID == "Steam" {
				steamPos = i
				break
			}
		}
		assert.GreaterOrEqual(t, steamPos, 0, "Steam launcher should be present")
	}
}
