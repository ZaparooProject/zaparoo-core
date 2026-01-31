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

func TestLoadStartPath(t *testing.T) {
	// Clean up any existing files before and after test
	cleanup := func() {
		_ = os.Remove(misterconfig.StartPathFile)
		_ = os.Remove(misterconfig.ActiveGameFile)
	}
	cleanup()
	t.Cleanup(cleanup)

	tests := []struct {
		name             string
		startPathContent string
		wantActiveGame   string
	}{
		{
			name:             "reads path and sets active game",
			startPathContent: "/media/fat/games/SNES/SuperMarioWorld.sfc",
			wantActiveGame:   "/media/fat/games/SNES/SuperMarioWorld.sfc",
		},
		{
			name:             "trims whitespace from path",
			startPathContent: "  /media/fat/games/Genesis/Sonic.md  \n",
			wantActiveGame:   "/media/fat/games/Genesis/Sonic.md",
		},
		{
			name:             "empty file does not set active game",
			startPathContent: "",
			wantActiveGame:   "",
		},
		{
			name:             "whitespace-only file does not set active game",
			startPathContent: "   \n\t  ",
			wantActiveGame:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up before each subtest
			cleanup()

			// Create STARTPATH file with test content
			err := os.WriteFile(misterconfig.StartPathFile, []byte(tt.startPathContent), 0o600)
			require.NoError(t, err)

			// Create a minimal tracker (we only need it to call the method)
			tr := &Tracker{}

			// Call loadStartPath
			tr.loadStartPath()

			// Verify active game was set correctly
			if tt.wantActiveGame == "" {
				// If we expect no active game, check that file wasn't created or is empty
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

func TestLoadStartPath_FileNotFound(t *testing.T) {
	// Clean up any existing files
	cleanup := func() {
		_ = os.Remove(misterconfig.StartPathFile)
		_ = os.Remove(misterconfig.ActiveGameFile)
	}
	cleanup()
	t.Cleanup(cleanup)

	// Create a minimal tracker
	tr := &Tracker{}

	// Should not panic when file doesn't exist
	tr.loadStartPath()

	// Verify no active game was set (file should not exist)
	_, err := os.Stat(misterconfig.ActiveGameFile)
	assert.True(t, os.IsNotExist(err), "active game file should not exist")
}
