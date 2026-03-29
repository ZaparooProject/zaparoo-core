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

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker/activegame"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFullPath(t *testing.T) {
	cleanup := func() {
		_ = os.Remove(misterconfig.FullPathFile)
		_ = os.Remove(misterconfig.ActiveGameFile)
	}
	cleanup()
	t.Cleanup(cleanup)

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
			cleanup()

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
	cleanup := func() {
		_ = os.Remove(misterconfig.FullPathFile)
		_ = os.Remove(misterconfig.ActiveGameFile)
	}
	cleanup()
	t.Cleanup(cleanup)

	tr := &Tracker{}

	// Should not panic when file doesn't exist
	tr.loadFullPath()

	_, err := os.Stat(misterconfig.ActiveGameFile)
	assert.True(t, os.IsNotExist(err), "active game file should not exist")
}
