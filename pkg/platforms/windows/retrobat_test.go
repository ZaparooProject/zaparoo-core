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

package windows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetRetroBatKillHooks(t *testing.T) {
	t.Helper()

	origRunningGame := retroBatAPIRunningGame
	origEmuKill := retroBatAPIEmuKill
	origFindDir := retroBatFindDir
	origListProcesses := retroBatListProcesses
	origKillPIDTree := retroBatKillPIDTree
	origProcessPath := retroBatProcessPath
	origRunTaskKill := retroBatRunTaskKill
	origSleep := retroBatSleep

	t.Cleanup(func() {
		retroBatAPIRunningGame = origRunningGame
		retroBatAPIEmuKill = origEmuKill
		retroBatFindDir = origFindDir
		retroBatListProcesses = origListProcesses
		retroBatKillPIDTree = origKillPIDTree
		retroBatProcessPath = origProcessPath
		retroBatRunTaskKill = origRunTaskKill
		retroBatSleep = origSleep
	})
}

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

func TestCreateRetroBatLauncher(t *testing.T) {
	t.Parallel()
	systemFolder := "snes"
	info, exists := esde.LookupByFolderName(systemFolder)
	require.True(t, exists, "snes should exist in esde.SystemMap")

	launcher := createRetroBatLauncher(systemFolder, info)

	assert.Equal(t, "RetroBatSNES", launcher.ID)
	assert.Equal(t, "SNES", launcher.SystemID)
	assert.Empty(t, launcher.Folders) // RetroBat launchers use custom Test function, not Folders
	assert.True(t, launcher.SkipFilesystemScan)

	// Test functions are not nil
	assert.NotNil(t, launcher.Test)
	assert.NotNil(t, launcher.Launch)
	assert.NotNil(t, launcher.Kill)
	assert.NotNil(t, launcher.Scanner)
}

func TestRetroBatLauncherHasRequiredFunctions(t *testing.T) {
	t.Parallel()
	systemFolder := "snes"
	info, exists := esde.LookupByFolderName(systemFolder)
	require.True(t, exists, "snes should exist in esde.SystemMap")

	launcher := createRetroBatLauncher(systemFolder, info)

	// Verify launcher has all required functions set
	assert.Equal(t, "RetroBatSNES", launcher.ID)
	assert.Equal(t, "SNES", launcher.SystemID)
	assert.NotNil(t, launcher.Test, "Test function should be set")
	assert.NotNil(t, launcher.Launch, "Launch function should be set")
	assert.NotNil(t, launcher.Kill, "Kill function should be set")
	assert.NotNil(t, launcher.Scanner, "Scanner function should be set")
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

// TestRetroBatCommonSystemsExist tests that commonly expected systems exist in esde.SystemMap.
func TestRetroBatCommonSystemsExist(t *testing.T) {
	t.Parallel()

	// Test that common gaming systems are mapped (using actual RetroBat folder names)
	commonSystems := []string{
		"nes",
		"snes",
		"megadrive", // RetroBat uses "megadrive" not "genesis"
		"n64",
		"gamecube", // RetroBat uses "gamecube" not "gc"
		"wii",
		"psx",
		"ps2",
		"gba",
		"gb",
		"mame",
		"atari2600",
		"dreamcast",
	}

	for _, system := range commonSystems {
		info, exists := esde.LookupByFolderName(system)
		assert.True(t, exists, "Common system %s should exist in esde.SystemMap", system)
		if exists {
			assert.NotEmpty(t, info.SystemID, "Common system %s should have non-empty SystemID", system)
			assert.NotEmpty(t, info.GetLauncherID(), "Common system %s should have non-empty LauncherID", system)
		}
	}
}

// TestRetroBatLauncherCreation tests that launchers can be created for common systems.
func TestRetroBatLauncherCreation(t *testing.T) {
	t.Parallel()

	// Test creating launchers for some common systems
	testCases := []struct {
		folder      string
		expectID    string
		expectSysID string
	}{
		{"snes", "RetroBatSNES", "SNES"},
		{"nes", "RetroBatNES", "NES"},
		{"psx", "RetroBatPSX", "PSX"},
		{"n64", "RetroBatN64", "Nintendo64"},
		{"megadrive", "RetroBatMegaDrive", "Genesis"},
	}

	for _, tc := range testCases {
		t.Run(tc.folder, func(t *testing.T) {
			t.Parallel()
			info, exists := esde.LookupByFolderName(tc.folder)
			require.True(t, exists, "System %s should exist in esde.SystemMap", tc.folder)

			launcher := createRetroBatLauncher(tc.folder, info)

			assert.Equal(t, tc.expectID, launcher.ID, "Launcher ID mismatch for %s", tc.folder)
			assert.Equal(t, tc.expectSysID, launcher.SystemID, "SystemID mismatch for %s", tc.folder)
			assert.True(t, launcher.SkipFilesystemScan)
			assert.NotNil(t, launcher.Test)
			assert.NotNil(t, launcher.Launch)
			assert.NotNil(t, launcher.Kill)
			assert.NotNil(t, launcher.Scanner)
		})
	}
}

func TestKillRetroBatGame_NoRunningGameSkipsKill(t *testing.T) {
	resetRetroBatKillHooks(t)

	apiKillCalls := 0
	retroBatAPIRunningGame = func() (esapi.RunningGameResponse, bool, error) {
		return esapi.RunningGameResponse{}, false, nil
	}
	retroBatAPIEmuKill = func() error {
		apiKillCalls++
		return nil
	}
	retroBatSleep = func(_ time.Duration) {}

	err := killRetroBatGame(&config.Instance{})

	require.NoError(t, err)
	assert.Zero(t, apiKillCalls)
}

func TestKillRetroBatGame_ESAPISucceedsSkipsFallback(t *testing.T) {
	resetRetroBatKillHooks(t)

	stopped := false
	apiKillCalls := 0
	listCalls := 0
	retroBatAPIRunningGame = func() (esapi.RunningGameResponse, bool, error) {
		if stopped {
			return esapi.RunningGameResponse{}, false, nil
		}
		return esapi.RunningGameResponse{Name: "Game", SystemName: "snes"}, true, nil
	}
	retroBatAPIEmuKill = func() error {
		apiKillCalls++
		stopped = true
		return nil
	}
	retroBatListProcesses = func() ([]windowsProcessInfo, error) {
		listCalls++
		return nil, nil
	}
	retroBatSleep = func(_ time.Duration) {}

	err := killRetroBatGame(&config.Instance{})

	require.NoError(t, err)
	assert.Equal(t, 1, apiKillCalls)
	assert.Zero(t, listCalls)
}

func TestKillRetroBatGame_ESAPINoopKillsRetroBatEmulatorPID(t *testing.T) {
	resetRetroBatKillHooks(t)

	const retroBatDir = `C:\RetroBat`
	killed := false
	var killedPIDs []uint32
	retroBatAPIRunningGame = func() (esapi.RunningGameResponse, bool, error) {
		if killed {
			return esapi.RunningGameResponse{}, false, nil
		}
		return esapi.RunningGameResponse{Name: "Game", SystemName: "snes"}, true, nil
	}
	retroBatAPIEmuKill = func() error { return nil }
	retroBatFindDir = func(_ *config.Instance) (string, error) { return retroBatDir, nil }
	retroBatListProcesses = func() ([]windowsProcessInfo, error) {
		return []windowsProcessInfo{
			{PID: 100, ExePath: `C:\RetroBat\emulators\retroarch\retroarch.exe`},
			{PID: 200, ExePath: `D:\Tools\RetroArch\retroarch.exe`},
		}, nil
	}
	retroBatKillPIDTree = func(_ context.Context, pid uint32, _ string) error {
		killedPIDs = append(killedPIDs, pid)
		killed = true
		return nil
	}
	retroBatSleep = func(_ time.Duration) {}

	err := killRetroBatGame(&config.Instance{})

	require.NoError(t, err)
	assert.Equal(t, []uint32{100}, killedPIDs)
}

func TestKillRetroBatGame_ESAPINoopWithoutSafeCandidateReturnsError(t *testing.T) {
	resetRetroBatKillHooks(t)

	const retroBatDir = `C:\RetroBat`
	retroBatAPIRunningGame = func() (esapi.RunningGameResponse, bool, error) {
		return esapi.RunningGameResponse{Name: "Game", SystemName: "snes"}, true, nil
	}
	retroBatAPIEmuKill = func() error { return nil }
	retroBatFindDir = func(_ *config.Instance) (string, error) { return retroBatDir, nil }
	retroBatListProcesses = func() ([]windowsProcessInfo, error) {
		return []windowsProcessInfo{{PID: 200, ExePath: `D:\Tools\RetroArch\retroarch.exe`}}, nil
	}
	retroBatKillPIDTree = func(_ context.Context, pid uint32, _ string) error {
		return fmt.Errorf("unexpected kill for pid %d", pid)
	}
	retroBatSleep = func(_ time.Duration) {}

	err := killRetroBatGame(&config.Instance{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no RetroBat emulator process was found")
}

func TestKillRetroBatGame_ESAPINoopPropagatesFallbackError(t *testing.T) {
	resetRetroBatKillHooks(t)

	const retroBatDir = `C:\RetroBat`
	retroBatAPIRunningGame = func() (esapi.RunningGameResponse, bool, error) {
		return esapi.RunningGameResponse{Name: "Game", SystemName: "snes"}, true, nil
	}
	retroBatAPIEmuKill = func() error { return nil }
	retroBatFindDir = func(_ *config.Instance) (string, error) { return retroBatDir, nil }
	retroBatListProcesses = func() ([]windowsProcessInfo, error) {
		return []windowsProcessInfo{{PID: 100, ExePath: `C:\RetroBat\emulators\retroarch\retroarch.exe`}}, nil
	}
	retroBatKillPIDTree = func(_ context.Context, _ uint32, _ string) error {
		return errors.New("access denied")
	}
	retroBatSleep = func(_ time.Duration) {}

	err := killRetroBatGame(&config.Instance{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "process fallback failed")
	assert.Contains(t, err.Error(), "access denied")
}

func TestKillWindowsProcessTree_RevalidatesPath(t *testing.T) {
	resetRetroBatKillHooks(t)

	taskKillCalls := 0
	retroBatProcessPath = func(_ uint32) (string, error) {
		return `D:\Tools\RetroArch\retroarch.exe`, nil
	}
	retroBatRunTaskKill = func(_ context.Context, _ uint32) error {
		taskKillCalls++
		return nil
	}

	err := killWindowsProcessTree(context.Background(), 100, `C:\RetroBat\emulators`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer matches RetroBat emulator path")
	assert.Zero(t, taskKillCalls)
}

func TestIsRetroBatEmulatorProcess(t *testing.T) {
	testCases := []struct {
		name         string
		process      windowsProcessInfo
		shouldTarget bool
	}{
		{
			name:         "emulator under RetroBat emulators",
			process:      windowsProcessInfo{PID: 100, ExePath: `C:\RetroBat\emulators\retroarch\retroarch.exe`},
			shouldTarget: true,
		},
		{
			name:         "same emulator outside RetroBat",
			process:      windowsProcessInfo{PID: 200, ExePath: `D:\Tools\RetroArch\retroarch.exe`},
			shouldTarget: false,
		},
		{
			name:         "frontend process excluded",
			process:      windowsProcessInfo{PID: 300, ExePath: `C:\RetroBat\emulators\tools\emulationstation.exe`},
			shouldTarget: false,
		},
		{
			name:         "emulatorlauncher excluded",
			process:      windowsProcessInfo{PID: 400, ExePath: `C:\RetroBat\emulators\tools\emulatorlauncher.exe`},
			shouldTarget: false,
		},
		{
			name:         "prefix sibling excluded",
			process:      windowsProcessInfo{PID: 500, ExePath: `C:\RetroBat\emulators2\retroarch.exe`},
			shouldTarget: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isRetroBatEmulatorProcess(`C:\RetroBat\emulators`, tc.process)

			assert.Equal(t, tc.shouldTarget, result)
		})
	}
}
