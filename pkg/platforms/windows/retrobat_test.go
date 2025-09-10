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
	"testing"

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

	// Test some common systems
	assert.Equal(t, "snes", mapping["snes"])
	assert.Equal(t, "nes", mapping["nes"])
	assert.Equal(t, "genesis", mapping["genesis"])
	assert.Equal(t, "psx", mapping["psx"])

	// Ensure we have a reasonable number of systems
	assert.Greater(t, len(mapping), 20, "Should have more than 20 system mappings")
}

func TestCreateRetroBatLauncher(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	systemFolder := "snes"
	systemID := "snes"

	launcher := createRetroBatLauncher(systemFolder, systemID, tempDir)

	assert.Equal(t, "RetroBatSNES", launcher.ID)
	assert.Equal(t, "snes", launcher.SystemID)
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
	systemID := "snes"

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
	assert.Equal(t, "snes", launcher.SystemID)
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
