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

package esde

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGamePath(t *testing.T) {
	t.Parallel()

	// Use a temp directory to get a platform-appropriate base path
	tmpDir := t.TempDir()
	romsBase := filepath.Join(tmpDir, "roms")

	t.Run("relative with dot prefix", func(t *testing.T) {
		t.Parallel()

		got := ResolveGamePath("./game.rom", romsBase, "nes")
		want := filepath.Join(romsBase, "nes", "game.rom")
		assert.Equal(t, want, got)
	})

	t.Run("relative without prefix", func(t *testing.T) {
		t.Parallel()

		got := ResolveGamePath("game.rom", romsBase, "nes")
		want := filepath.Join(romsBase, "nes", "game.rom")
		assert.Equal(t, want, got)
	})

	t.Run("absolute path outside system root", func(t *testing.T) {
		t.Parallel()

		absolutePath := filepath.Join(tmpDir, "different", "path", "game.rom")
		assert.Empty(t, ResolveGamePath(absolutePath, romsBase, "nes"))
	})

	t.Run("absolute path inside system root", func(t *testing.T) {
		t.Parallel()

		absolutePath := filepath.Join(romsBase, "nes", "game.rom")
		assert.Equal(t, absolutePath, ResolveGamePath(absolutePath, romsBase, "nes"))
	})

	t.Run("parent traversal", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, ResolveGamePath("../../outside.rom", romsBase, "nes"))
	})

	t.Run("nested relative path", func(t *testing.T) {
		t.Parallel()

		got := ResolveGamePath("./subdir/game.rom", romsBase, "snes")
		want := filepath.Join(romsBase, "snes", "subdir", "game.rom")
		assert.Equal(t, want, got)
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
		writeTestFiles(t, filepath.Join(nesPath, "smb.nes"), filepath.Join(nesPath, "metroid.nes"))

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
		writeTestFiles(t, filepath.Join(romsPath, "nes", "test.nes"))

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

	t.Run("rejects paths outside system root", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		romsPath := filepath.Join(tmpDir, "roms")
		nesPath := filepath.Join(romsPath, "nes")
		require.NoError(t, os.MkdirAll(nesPath, 0o750))
		outsidePath := filepath.Join(tmpDir, "outside.nes")
		require.NoError(t, os.WriteFile(outsidePath, []byte("rom"), 0o600))
		symlinkPath := filepath.Join(nesPath, "linked.nes")
		require.NoError(t, os.Symlink(outsidePath, symlinkPath))
		linkedDir := filepath.Join(nesPath, "linked-dir")
		require.NoError(t, os.Symlink(t.TempDir(), linkedDir))

		gamelistContent := `<gameList>
  <game><name>Traversal</name><path>../../outside.nes</path></game>
  <game><name>Absolute</name><path>` + outsidePath + `</path></game>
  <game><name>Symlink</name><path>./linked.nes</path></game>
  <game><name>Missing</name><path>./missing.nes</path></game>
  <game><name>Missing Through Symlink</name><path>./linked-dir/missing.nes</path></game>
</gameList>`
		require.NoError(t, os.WriteFile(filepath.Join(nesPath, "gamelist.xml"), []byte(gamelistContent), 0o600))

		results, err := ScanGamelist(ScannerConfig{RomsBasePath: romsPath, SystemFolder: "nes"})
		require.NoError(t, err)
		assert.Empty(t, results)
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
		writeTestFiles(t, filepath.Join(nesPath, "game.nes"))

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

func TestEnhanceResultsFromGamelist(t *testing.T) {
	t.Parallel()

	t.Run("enhances results with gamelist names", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		nesPath := filepath.Join(tmpDir, "nes")
		err := os.MkdirAll(nesPath, 0o750)
		require.NoError(t, err)

		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Super Mario Bros.</name>
    <path>./smb.nes</path>
  </game>
  <game>
    <name>The Legend of Zelda</name>
    <path>./zelda.nes</path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)
		writeTestFiles(t, filepath.Join(nesPath, "smb.nes"), filepath.Join(nesPath, "zelda.nes"))

		// Create results with file-based names (before enhancement)
		results := map[string]platforms.ScanResult{
			filepath.Join(tmpDir, "nes", "smb.nes"):   {Name: "smb", Path: filepath.Join(tmpDir, "nes", "smb.nes")},
			filepath.Join(tmpDir, "nes", "zelda.nes"): {Name: "zelda", Path: filepath.Join(tmpDir, "nes", "zelda.nes")},
		}

		cfg := ScannerConfig{
			RomsBasePath: tmpDir,
			SystemFolder: "nes",
		}

		err = EnhanceResultsFromGamelist(results, cfg)
		require.NoError(t, err)

		assert.Equal(t, "Super Mario Bros.", results[filepath.Join(tmpDir, "nes", "smb.nes")].Name)
		assert.Equal(t, "The Legend of Zelda", results[filepath.Join(tmpDir, "nes", "zelda.nes")].Name)
	})

	t.Run("leaves results unchanged when no gamelist", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()

		results := map[string]platforms.ScanResult{
			filepath.Join(tmpDir, "nes", "game.nes"): {Name: "game", Path: filepath.Join(tmpDir, "nes", "game.nes")},
		}

		cfg := ScannerConfig{
			RomsBasePath: tmpDir,
			SystemFolder: "nes",
		}

		err := EnhanceResultsFromGamelist(results, cfg)
		require.NoError(t, err)

		// Name should remain unchanged
		assert.Equal(t, "game", results[filepath.Join(tmpDir, "nes", "game.nes")].Name)
	})

	t.Run("uses separate gamelist path", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		romsPath := filepath.Join(tmpDir, "roms")
		gamelistsPath := filepath.Join(tmpDir, "gamelists")
		nesGamelistPath := filepath.Join(gamelistsPath, "nes")
		err := os.MkdirAll(nesGamelistPath, 0o750)
		require.NoError(t, err)

		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Enhanced Name</name>
    <path>./test.nes</path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesGamelistPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)

		testPath := filepath.Join(romsPath, "nes", "test.nes")
		writeTestFiles(t, testPath)
		results := map[string]platforms.ScanResult{
			testPath: {Name: "test", Path: testPath},
		}

		cfg := ScannerConfig{
			RomsBasePath:     romsPath,
			GamelistBasePath: gamelistsPath,
			SystemFolder:     "nes",
		}

		err = EnhanceResultsFromGamelist(results, cfg)
		require.NoError(t, err)

		assert.Equal(t, "Enhanced Name", results[testPath].Name)
	})

	t.Run("skips entries with empty name or path", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		nesPath := filepath.Join(tmpDir, "nes")
		err := os.MkdirAll(nesPath, 0o750)
		require.NoError(t, err)

		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name></name>
    <path>./noname.nes</path>
  </game>
  <game>
    <name>No Path Game</name>
    <path></path>
  </game>
  <game>
    <name>Valid Game</name>
    <path>./valid.nes</path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)

		nonamePath := filepath.Join(tmpDir, "nes", "noname.nes")
		validPath := filepath.Join(tmpDir, "nes", "valid.nes")
		writeTestFiles(t, nonamePath, validPath)
		results := map[string]platforms.ScanResult{
			nonamePath: {Name: "noname", Path: nonamePath},
			validPath:  {Name: "valid", Path: validPath},
		}

		cfg := ScannerConfig{
			RomsBasePath: tmpDir,
			SystemFolder: "nes",
		}

		err = EnhanceResultsFromGamelist(results, cfg)
		require.NoError(t, err)

		// noname.nes should not be enhanced (empty name in gamelist)
		assert.Equal(t, "noname", results[nonamePath].Name)
		// valid.nes should be enhanced
		assert.Equal(t, "Valid Game", results[validPath].Name)
	})
}

func writeTestFiles(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
	}
}

func TestCreateSystemScanner(t *testing.T) {
	t.Parallel()

	t.Run("creates scanner function that works", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		nesPath := filepath.Join(tmpDir, "nes")
		err := os.MkdirAll(nesPath, 0o750)
		require.NoError(t, err)

		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Test Game</name>
    <path>./test.nes</path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)
		writeTestFiles(t, filepath.Join(nesPath, "test.nes"))

		scanner := CreateSystemScanner(tmpDir, "", "nes")
		results, err := scanner()
		require.NoError(t, err)

		assert.Len(t, results, 1)
		assert.Equal(t, "Test Game", results[0].Name)
		assert.Equal(t, filepath.Join(tmpDir, "nes", "test.nes"), results[0].Path)
	})

	t.Run("creates scanner with separate gamelist path", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		romsPath := filepath.Join(tmpDir, "roms")
		gamelistsPath := filepath.Join(tmpDir, "gamelists")
		nesGamelistPath := filepath.Join(gamelistsPath, "nes")
		err := os.MkdirAll(nesGamelistPath, 0o750)
		require.NoError(t, err)

		gamelistContent := `<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <name>Separate Path Game</name>
    <path>./game.nes</path>
  </game>
</gameList>`
		err = os.WriteFile(filepath.Join(nesGamelistPath, "gamelist.xml"), []byte(gamelistContent), 0o600)
		require.NoError(t, err)
		writeTestFiles(t, filepath.Join(romsPath, "nes", "game.nes"))

		scanner := CreateSystemScanner(romsPath, gamelistsPath, "nes")
		results, err := scanner()
		require.NoError(t, err)

		assert.Len(t, results, 1)
		assert.Equal(t, "Separate Path Game", results[0].Name)
		// Path should be relative to romsPath
		assert.Equal(t, filepath.Join(romsPath, "nes", "game.nes"), results[0].Path)
	})

	t.Run("returns nil for missing gamelist", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		scanner := CreateSystemScanner(tmpDir, "", "nes")
		results, err := scanner()
		require.NoError(t, err)
		assert.Nil(t, results)
	})
}
