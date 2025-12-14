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

package esde

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGamePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		gamePath     string
		romsBasePath string
		systemFolder string
		want         string
	}{
		{
			name:         "relative with dot prefix",
			gamePath:     "./game.rom",
			romsBasePath: "/home/user/roms",
			systemFolder: "nes",
			want:         "/home/user/roms/nes/game.rom",
		},
		{
			name:         "relative without prefix",
			gamePath:     "game.rom",
			romsBasePath: "/home/user/roms",
			systemFolder: "nes",
			want:         "/home/user/roms/nes/game.rom",
		},
		{
			name:         "absolute path",
			gamePath:     "/different/path/game.rom",
			romsBasePath: "/home/user/roms",
			systemFolder: "nes",
			want:         "/different/path/game.rom",
		},
		{
			name:         "nested relative path",
			gamePath:     "./subdir/game.rom",
			romsBasePath: "/home/user/roms",
			systemFolder: "snes",
			want:         "/home/user/roms/snes/subdir/game.rom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveGamePath(tt.gamePath, tt.romsBasePath, tt.systemFolder)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadGameList(t *testing.T) {
	t.Parallel()

	t.Run("reads valid gamelist.xml", func(t *testing.T) {
		t.Parallel()

		// Create a temporary gamelist.xml
		tmpDir := t.TempDir()
		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Super Mario Bros.</name>
    <path>./smb.nes</path>
  </game>
  <game>
    <name>Legend of Zelda</name>
    <path>./zelda.nes</path>
  </game>
</gameList>`

		gamelistPath := filepath.Join(tmpDir, "gamelist.xml")
		err := os.WriteFile(gamelistPath, []byte(gamelistContent), 0o600)
		require.NoError(t, err)

		gameList, err := ReadGameList(gamelistPath)
		require.NoError(t, err)

		assert.Len(t, gameList.Games, 2)
		assert.Equal(t, "Super Mario Bros.", gameList.Games[0].Name)
		assert.Equal(t, "./smb.nes", gameList.Games[0].Path)
		assert.Equal(t, "Legend of Zelda", gameList.Games[1].Name)
		assert.Equal(t, "./zelda.nes", gameList.Games[1].Path)
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		t.Parallel()

		_, err := ReadGameList("/nonexistent/path/gamelist.xml")
		require.Error(t, err)
	})

	t.Run("returns error for invalid XML", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		gamelistPath := filepath.Join(tmpDir, "gamelist.xml")
		err := os.WriteFile(gamelistPath, []byte("not valid xml"), 0o600)
		require.NoError(t, err)

		_, err = ReadGameList(gamelistPath)
		require.Error(t, err)
	})
}

func TestScanGamelist(t *testing.T) {
	t.Parallel()

	t.Run("scans gamelist and returns results", func(t *testing.T) {
		t.Parallel()

		// Create temporary directory structure
		tmpDir := t.TempDir()
		romsPath := filepath.Join(tmpDir, "roms")
		nesPath := filepath.Join(romsPath, "nes")
		err := os.MkdirAll(nesPath, 0o750)
		require.NoError(t, err)

		// Create gamelist.xml
		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Super Mario Bros.</name>
    <path>./smb.nes</path>
  </game>
  <game>
    <name>Metroid</name>
    <path>./metroid.nes</path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)

		cfg := ScannerConfig{
			RomsBasePath: romsPath,
			SystemFolder: "nes",
		}

		results, err := ScanGamelist(cfg)
		require.NoError(t, err)

		assert.Len(t, results, 2)
		assert.Equal(t, "Super Mario Bros.", results[0].Name)
		assert.Equal(t, filepath.Join(romsPath, "nes", "smb.nes"), results[0].Path)
		assert.Equal(t, "Metroid", results[1].Name)
		assert.Equal(t, filepath.Join(romsPath, "nes", "metroid.nes"), results[1].Path)
	})

	t.Run("returns nil for missing gamelist", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		cfg := ScannerConfig{
			RomsBasePath: tmpDir,
			SystemFolder: "nes",
		}

		results, err := ScanGamelist(cfg)
		require.NoError(t, err)
		assert.Nil(t, results)
	})

	t.Run("uses separate gamelist path", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		romsPath := filepath.Join(tmpDir, "roms")
		gamelistsPath := filepath.Join(tmpDir, "gamelists")
		nesGamelistPath := filepath.Join(gamelistsPath, "nes")
		err := os.MkdirAll(nesGamelistPath, 0o750)
		require.NoError(t, err)

		// Create gamelist.xml in separate location
		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Test Game</name>
    <path>./test.nes</path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesGamelistPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)

		cfg := ScannerConfig{
			RomsBasePath:     romsPath,
			GamelistBasePath: gamelistsPath,
			SystemFolder:     "nes",
		}

		results, err := ScanGamelist(cfg)
		require.NoError(t, err)

		assert.Len(t, results, 1)
		assert.Equal(t, "Test Game", results[0].Name)
		// Path should be resolved relative to RomsBasePath, not GamelistBasePath
		assert.Equal(t, filepath.Join(romsPath, "nes", "test.nes"), results[0].Path)
	})

	t.Run("skips entries with empty path", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		nesPath := filepath.Join(tmpDir, "nes")
		err := os.MkdirAll(nesPath, 0o750)
		require.NoError(t, err)

		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Has Path</name>
    <path>./game.nes</path>
  </game>
  <game>
    <name>No Path</name>
    <path></path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)

		cfg := ScannerConfig{
			RomsBasePath: tmpDir,
			SystemFolder: "nes",
		}

		results, err := ScanGamelist(cfg)
		require.NoError(t, err)

		assert.Len(t, results, 1)
		assert.Equal(t, "Has Path", results[0].Name)
	})
}
