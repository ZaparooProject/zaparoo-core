//go:build windows

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

package windows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindRetroBatDir(t *testing.T) {
	t.Parallel()
	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		fs := helpers.NewMemoryFS()
		cfg, err := helpers.NewTestConfig(fs, t.TempDir())
		require.NoError(t, err)

		result, err := findRetroBatDir(cfg)
		require.Error(t, err)
		assert.Empty(t, result)
		assert.Contains(t, err.Error(), "RetroBat installation directory not found")
	})
}

func TestGetRetroBatSystemMapping(t *testing.T) {
	t.Parallel()
	mapping := getRetroBatSystemMapping()

	// Test some common systems with correct uppercase values
	assert.Equal(t, "SNES", mapping["snes"])
	assert.Equal(t, "NES", mapping["nes"])
	assert.Equal(t, "Genesis", mapping["genesis"])
	assert.Equal(t, "PSX", mapping["psx"])

	// Ensure we have a reasonable number of systems
	assert.Greater(t, len(mapping), 20, "Should have more than 20 system mappings")
}

func TestCreateRetroBatLauncher(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	systemFolder := "snes"
	mapping := getRetroBatSystemMapping()
	systemID := mapping[systemFolder] // Get the correct system ID from the mapping

	launcher := createRetroBatLauncher(systemFolder, systemID, tempDir)

	assert.Equal(t, "RetroBatSNES", launcher.ID)
	assert.Equal(t, "SNES", launcher.SystemID)
	assert.Equal(t, []string{filepath.Join("roms", "snes")}, launcher.Folders)
	assert.True(t, launcher.SkipFilesystemScan)

	// Test functions are not nil
	assert.NotNil(t, launcher.Test)
	assert.NotNil(t, launcher.Launch)
	assert.NotNil(t, launcher.Kill)
	assert.NotNil(t, launcher.Scanner)
}

func TestRetroBatLauncherTest(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	systemFolder := "snes"
	mapping := getRetroBatSystemMapping()
	systemID := mapping[systemFolder] // Get the correct system ID from the mapping

	// Create mock RetroBat directory structure
	romsDir := filepath.Join(tempDir, "roms", "snes")
	err := os.MkdirAll(romsDir, 0o750)
	require.NoError(t, err)

	// Create mock retrobat.exe
	retroBatExe := filepath.Join(tempDir, "retrobat.exe")
	err = os.WriteFile(retroBatExe, []byte("mock"), 0o600)
	require.NoError(t, err)

	launcher := createRetroBatLauncher(systemFolder, systemID, tempDir)

	// Test the launcher creation itself
	assert.Equal(t, "RetroBatSNES", launcher.ID)
	assert.Equal(t, "SNES", launcher.SystemID)
}

func TestRetroBatLauncherScanner(t *testing.T) {
	t.Parallel()
	// Test the XML parsing directly instead of through the launcher
	tempDir := t.TempDir()

	// Create mock gamelist.xml
	gamelistXML := `<?xml version="1.0"?>
<gameList>
	<game>
		<name>Super Mario World</name>
		<path>./mario.sfc</path>
	</game>
	<game>
		<name>The Legend of Zelda</name>
		<path>./zelda.sfc</path>
	</game>
</gameList>`

	gamelistPath := filepath.Join(tempDir, "gamelist.xml")
	err := os.WriteFile(gamelistPath, []byte(gamelistXML), 0o600)
	require.NoError(t, err)

	// Test that we can parse the gamelist XML
	gameList, err := esapi.ReadGameListXML(gamelistPath)
	require.NoError(t, err)

	assert.Len(t, gameList.Games, 2)
	assert.Equal(t, "Super Mario World", gameList.Games[0].Name)
	assert.Equal(t, "./mario.sfc", gameList.Games[0].Path)
	assert.Equal(t, "The Legend of Zelda", gameList.Games[1].Name)
	assert.Equal(t, "./zelda.sfc", gameList.Games[1].Path)
}

// TestRetroBatSystemMappingIntegrity tests that all system mappings are valid
func TestRetroBatSystemMappingIntegrity(t *testing.T) {
	t.Parallel()
	mapping := getRetroBatSystemMapping()

	// Test that we have a reasonable number of systems
	assert.GreaterOrEqual(t, len(mapping), 20, "Should have at least 20 systems in RetroBat mapping")

	// Test that system folder names are valid (lowercase, no spaces for RetroBat compatibility)
	for systemFolder := range mapping {
		assert.Regexp(t, `^[a-z0-9+._-]+$`, systemFolder,
			"System folder name %s should be lowercase alphanumeric with allowed special chars", systemFolder)
		assert.NotEmpty(t, systemFolder, "System folder name should not be empty")
		assert.Less(t, len(systemFolder), 30, "System folder name %s should be reasonable length", systemFolder)
	}

	// Test that all SystemIDs are valid and exist in systemdefs
	for systemFolder, systemID := range mapping {
		assert.NotEmpty(t, systemID, "System folder %s should have non-empty SystemID", systemFolder)
		assert.NotRegexp(t, `\s`, systemID,
			"SystemID %s should not contain whitespace for system folder %s", systemID, systemFolder)
		assert.Greater(t, len(systemID), 1,
			"SystemID should be more than 1 character for system folder %s", systemFolder)
		assert.Less(t, len(systemID), 50,
			"SystemID should be less than 50 characters for system folder %s", systemFolder)

		// Verify the SystemID exists in systemdefs by checking if it's a known system
		// This ensures we're not using invalid or typo'd system IDs
		validSystemID := isValidSystemID(systemID)
		assert.True(t, validSystemID,
			"SystemID %s for folder %s should be a valid system defined in systemdefs", systemID, systemFolder)
	}
}

// TestRetroBatCommonSystemsExist tests that commonly expected systems exist in the mapping
func TestRetroBatCommonSystemsExist(t *testing.T) {
	t.Parallel()
	mapping := getRetroBatSystemMapping()

	// Test that common gaming systems are mapped
	commonSystems := []string{
		"nes",
		"snes",
		"genesis",
		"n64",
		"psx",
		"gba",
		"gb",
		"arcade",
		"atari2600",
	}

	for _, system := range commonSystems {
		systemID, exists := mapping[system]
		assert.True(t, exists, "Common system %s should exist in RetroBat mapping", system)
		if exists {
			assert.NotEmpty(t, systemID, "Common system %s should have non-empty SystemID", system)
		}
	}
}

// TestAllRetroBatSystemsHaveValidStructure tests each system in the mapping
func TestAllRetroBatSystemsHaveValidStructure(t *testing.T) {
	t.Parallel()
	mapping := getRetroBatSystemMapping()

	for systemFolder, systemID := range mapping {
		t.Run(systemFolder, func(t *testing.T) {
			t.Parallel()

			// Test that systemFolder is a valid RetroBat system folder name
			assert.NotEmpty(t, systemFolder, "System folder name must not be empty")
			assert.Equal(t, strings.ToLower(systemFolder), systemFolder,
				"System folder %s should be lowercase for RetroBat compatibility", systemFolder)

			// Test that systemID is a valid Zaparoo system ID
			assert.NotEmpty(t, systemID, "SystemID must not be empty for folder %s", systemFolder)
			assert.True(t, isValidSystemID(systemID),
				"SystemID %s for folder %s should be a valid system defined in systemdefs", systemID, systemFolder)

			// Test that the launcher can be created without errors
			launcher := createRetroBatLauncher(systemFolder, systemID, "/tmp")
			assert.NotEmpty(t, launcher.ID, "Launcher ID should not be empty for system %s", systemFolder)
			assert.Equal(t, systemID, launcher.SystemID,
				"Launcher SystemID should match mapping for folder %s", systemFolder)
			assert.Contains(t, launcher.ID, systemID,
				"Launcher ID should contain SystemID for folder %s", systemFolder)
			assert.True(t, launcher.SkipFilesystemScan,
				"RetroBat launchers should skip filesystem scan for folder %s", systemFolder)
			assert.NotNil(t, launcher.Test,
				"Launcher Test function should not be nil for folder %s", systemFolder)
			assert.NotNil(t, launcher.Launch,
				"Launcher Launch function should not be nil for folder %s", systemFolder)
			assert.NotNil(t, launcher.Kill,
				"Launcher Kill function should not be nil for folder %s", systemFolder)
			assert.NotNil(t, launcher.Scanner,
				"Launcher Scanner function should not be nil for folder %s", systemFolder)
		})
	}
}

// isValidSystemID checks if a system ID exists in the systemdefs package
func isValidSystemID(systemID string) bool {
	// Check against known systemdefs constants
	// This is a simple validation that covers the main systems
	switch systemID {
	case systemdefs.System3DO,
		systemdefs.SystemAmiga,
		systemdefs.SystemAmstrad,
		systemdefs.SystemArcade,
		systemdefs.SystemAtari2600,
		systemdefs.SystemAtari5200,
		systemdefs.SystemAtari7800,
		systemdefs.SystemAtariLynx,
		systemdefs.SystemAtariST,
		systemdefs.SystemC64,
		systemdefs.SystemDreamcast,
		systemdefs.SystemFDS,
		systemdefs.SystemGameGear,
		systemdefs.SystemGameboy,
		systemdefs.SystemGBA,
		systemdefs.SystemGameboyColor,
		systemdefs.SystemGameCube,
		systemdefs.SystemGenesis,
		systemdefs.SystemMasterSystem,
		systemdefs.SystemMSX,
		systemdefs.SystemNintendo64,
		systemdefs.SystemNDS,
		systemdefs.SystemNeoGeo,
		systemdefs.SystemNeoGeoCD,
		systemdefs.SystemNES,
		systemdefs.SystemNeoGeoPocket,
		systemdefs.SystemNeoGeoPocketColor,
		systemdefs.SystemPC,
		systemdefs.SystemTurboGrafx16,
		systemdefs.SystemTurboGrafx16CD,
		systemdefs.SystemPokemonMini,
		systemdefs.SystemPSX,
		systemdefs.SystemPS2,
		systemdefs.SystemSaturn,
		systemdefs.SystemSNES,
		systemdefs.SystemVirtualBoy,
		systemdefs.SystemWonderSwan,
		systemdefs.SystemWonderSwanColor:
		return true
	default:
		return false
	}
}
