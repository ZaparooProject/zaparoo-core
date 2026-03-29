//go:build linux

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

package tracker

import (
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker/activegame"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanupTmpFiles(t *testing.T) {
	t.Helper()
	files := []string{
		misterconfig.FullPathFile,
		misterconfig.ActiveGameFile,
	}
	for _, f := range files {
		_ = os.Remove(f)
	}
}

func TestLoadFullPath(t *testing.T) {
	cleanupTmpFiles(t)
	t.Cleanup(func() { cleanupTmpFiles(t) })

	tests := []struct {
		name            string
		fullPathContent string
		wantActiveGame  string
	}{
		{
			name:            "reads path and sets active game",
			fullPathContent: "/media/fat/games/SNES/SuperMarioWorld.sfc",
			wantActiveGame:  "/media/fat/games/SNES/SuperMarioWorld.sfc",
		},
		{
			name:            "trims whitespace from path",
			fullPathContent: "  /media/fat/games/Genesis/Sonic.md  \n",
			wantActiveGame:  "/media/fat/games/Genesis/Sonic.md",
		},
		{
			name:            "empty file does not set active game",
			fullPathContent: "",
			wantActiveGame:  "",
		},
		{
			name:            "whitespace-only file does not set active game",
			fullPathContent: "   \n\t  ",
			wantActiveGame:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanupTmpFiles(t)

			err := os.WriteFile(misterconfig.FullPathFile, []byte(tt.fullPathContent), 0o600)
			require.NoError(t, err)

			tr := &Tracker{}
			tr.loadFullPath()

			if tt.wantActiveGame == "" {
				if _, statErr := os.Stat(misterconfig.ActiveGameFile); statErr == nil {
					content, readErr := os.ReadFile(misterconfig.ActiveGameFile)
					if readErr == nil {
						assert.Empty(t, string(content), "expected empty active game")
					}
				}
			} else {
				activeGame, getErr := activegame.GetActiveGame()
				require.NoError(t, getErr)
				assert.Equal(t, tt.wantActiveGame, activeGame)
			}
		})
	}
}

func TestLoadFullPath_FileNotFound(t *testing.T) {
	cleanupTmpFiles(t)
	t.Cleanup(func() { cleanupTmpFiles(t) })

	tr := &Tracker{}
	tr.loadFullPath()

	_, err := os.Stat(misterconfig.ActiveGameFile)
	assert.True(t, os.IsNotExist(err), "active game file should not exist")
}

func TestLoadFullPath_OverwritesPreviousActiveGame(t *testing.T) {
	cleanupTmpFiles(t)
	t.Cleanup(func() { cleanupTmpFiles(t) })

	tr := &Tracker{}

	// Set an initial game via FULLPATH
	err := os.WriteFile(misterconfig.FullPathFile, []byte("/media/fat/games/SNES/GameA.sfc"), 0o600)
	require.NoError(t, err)
	tr.loadFullPath()

	activeGame, err := activegame.GetActiveGame()
	require.NoError(t, err)
	assert.Equal(t, "/media/fat/games/SNES/GameA.sfc", activeGame)

	// Switch to a different game
	err = os.WriteFile(misterconfig.FullPathFile, []byte("/media/fat/games/Genesis/GameB.md"), 0o600)
	require.NoError(t, err)
	tr.loadFullPath()

	activeGame, err = activegame.GetActiveGame()
	require.NoError(t, err)
	assert.Equal(t, "/media/fat/games/Genesis/GameB.md", activeGame)
}

func TestLookupCoreName(t *testing.T) {
	t.Parallel()

	tr := &Tracker{
		NameMap: []NameMapping{
			{CoreName: "SNES", System: "SNES", Name: "SNES"},
			{CoreName: "sf2", System: "Arcade", Name: "Arcade", ArcadeName: "Street Fighter II"},
			{CoreName: "Genesis", System: "Genesis", Name: "Genesis"},
		},
	}

	tests := []struct {
		name       string
		coreName   string
		wantArcade string
		wantNil    bool
	}{
		{
			name:     "empty string returns nil",
			coreName: "",
			wantNil:  true,
		},
		{
			name:     "unknown core returns nil",
			coreName: "UnknownCore",
			wantNil:  true,
		},
		{
			name:       "arcade core returns mapping with arcade name",
			coreName:   "sf2",
			wantArcade: "Street Fighter II",
		},
		{
			name:       "case insensitive arcade lookup",
			coreName:   "SF2",
			wantArcade: "Street Fighter II",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tr.LookupCoreName(tt.coreName)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.wantArcade, result.ArcadeName)
			}
		})
	}
}

func TestStopGame(t *testing.T) {
	t.Parallel()

	var lastMedia *models.ActiveMedia
	tr := &Tracker{
		ActiveGameID:     "SNES/game.sfc",
		ActiveGameName:   "Super Mario World",
		ActiveGamePath:   "/media/fat/games/SNES/game.sfc",
		ActiveSystem:     "SNES",
		ActiveSystemName: "Super Nintendo",
		setActiveMedia: func(m *models.ActiveMedia) {
			lastMedia = m
		},
	}

	tr.stopGame()

	assert.Empty(t, tr.ActiveGameID)
	assert.Empty(t, tr.ActiveGameName)
	assert.Empty(t, tr.ActiveGamePath)
	assert.Empty(t, tr.ActiveSystem)
	assert.Empty(t, tr.ActiveSystemName)
	assert.Nil(t, lastMedia)
}

func TestStopCore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		activeCore string
		wantReturn bool
	}{
		{
			name:       "returns true when core was active",
			activeCore: "SNES",
			wantReturn: true,
		},
		{
			name:       "returns false when no core active",
			activeCore: "",
			wantReturn: false,
		},
		{
			name:       "clears arcade state",
			activeCore: ArcadeSystem,
			wantReturn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := &Tracker{
				ActiveCore:       tt.activeCore,
				ActiveGameID:     "test",
				ActiveGamePath:   "/test",
				ActiveGameName:   "Test",
				ActiveSystem:     "TestSys",
				ActiveSystemName: "TestSysName",
			}

			result := tr.stopCore()
			assert.Equal(t, tt.wantReturn, result)
			assert.Empty(t, tr.ActiveCore)

			if tt.activeCore == ArcadeSystem {
				assert.Empty(t, tr.ActiveGameID)
				assert.Empty(t, tr.ActiveGamePath)
				assert.Empty(t, tr.ActiveGameName)
				assert.Empty(t, tr.ActiveSystem)
				assert.Empty(t, tr.ActiveSystemName)
			}
		})
	}
}
