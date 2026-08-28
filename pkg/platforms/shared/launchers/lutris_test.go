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

package launchers

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLutrisBuildLaunchCommandFlatpak(t *testing.T) {
	t.Parallel()

	opts := LutrisOptions{
		CheckFlatpak: true,
		lookPath: func(name string) (string, error) {
			if name == "flatpak" {
				return "/usr/bin/flatpak", nil
			}
			return "", os.ErrNotExist
		},
		isFlatpakInstalled: func(id string) bool { return id == FlatpakLutrisID },
		launchEnv:          func() []string { return []string{"DISPLAY=:1"} },
	}
	launcher := NewLutrisLauncher(opts)
	command, err := launcher.BuildLaunchCommand(nil, "lutris://game-slug/Game", nil)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/flatpak", command.Executable)
	assert.Equal(t, []string{
		"run", "--die-with-parent", FlatpakLutrisID, "lutris:rungame/game-slug",
	}, command.Args)
	assert.Equal(t, []string{"DISPLAY=:1"}, command.Env)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
}

func TestLutrisBuildLaunchCommandNative(t *testing.T) {
	t.Parallel()

	opts := LutrisOptions{
		CheckFlatpak: true,
		lookPath: func(name string) (string, error) {
			if name == "lutris" {
				return "/usr/bin/lutris", nil
			}
			return "", os.ErrNotExist
		},
		isFlatpakInstalled: func(string) bool { return true },
		launchEnv:          func() []string { return nil },
	}
	launcher := NewLutrisLauncher(opts)
	command, err := launcher.BuildLaunchCommand(nil, "lutris://game-slug/Game", nil)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/lutris", command.Executable)
	assert.Equal(t, []string{"lutris:rungame/game-slug"}, command.Args)
}

func TestScanLutrisGames(t *testing.T) {
	t.Parallel()

	t.Run("database_not_found", func(t *testing.T) {
		t.Parallel()

		results, err := ScanLutrisGames("/nonexistent/path/pga.db")
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("database_with_games", func(t *testing.T) {
		t.Parallel()

		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "pga.db")

		// Create and populate test database
		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, db.Close())
		}()

		// Create games table
		ctx := context.Background()
		_, err = db.ExecContext(ctx, `
			CREATE TABLE games (
				id INTEGER PRIMARY KEY,
				name TEXT,
				slug TEXT,
				installed INTEGER
			)
		`)
		require.NoError(t, err)

		// Insert test data: mix of installed and not installed games
		testGames := []struct {
			name      string
			slug      string
			installed int
		}{
			{"The Witcher 3", "the-witcher-3", 1},
			{"Cyberpunk 2077", "cyberpunk-2077", 1},
			{"Portal 2", "portal-2", 0}, // Not installed - should be skipped
			{"Half-Life 2", "half-life-2", 1},
			{"Uninstalled Game", "", 1},         // No slug - should be skipped
			{"No Install Flag", "some-game", 0}, // Not installed - should be skipped
		}

		for _, game := range testGames {
			_, err = db.ExecContext(
				ctx,
				"INSERT INTO games (name, slug, installed) VALUES (?, ?, ?)",
				game.name, game.slug, game.installed,
			)
			require.NoError(t, err)
		}

		// Close database before scanning
		require.NoError(t, db.Close())

		// Scan the database
		results, err := ScanLutrisGames(dbPath)
		require.NoError(t, err)

		// Should only find 3 installed games with slugs
		assert.Len(t, results, 3)

		// Verify results
		expectedGames := map[string]string{
			"The Witcher 3":  "the-witcher-3",
			"Cyberpunk 2077": "cyberpunk-2077",
			"Half-Life 2":    "half-life-2",
		}

		foundGames := make(map[string]bool)
		for _, result := range results {
			expectedSlug, exists := expectedGames[result.Name]
			assert.True(t, exists, "unexpected game found: %s", result.Name)
			assert.False(t, foundGames[result.Name], "duplicate game found: %s", result.Name)
			foundGames[result.Name] = true

			// Verify path format: lutris://slug/name (with URL encoding)
			assert.True(t, strings.HasPrefix(result.Path, "lutris://"), "path should start with lutris://")
			assert.Contains(t, result.Path, expectedSlug, "path should contain slug")
			assert.True(t, result.NoExt, "NoExt should be true for virtual paths")
		}

		// Verify all expected games were found
		for name := range expectedGames {
			assert.True(t, foundGames[name], "expected game not found: %s", name)
		}
	})

	t.Run("wal_database", func(t *testing.T) {
		t.Parallel()

		dbPath := filepath.Join(t.TempDir(), "pga.db")
		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, db.Close())
		}()

		ctx := context.Background()
		var journalMode string
		require.NoError(t, db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journalMode))
		require.Equal(t, "wal", strings.ToLower(journalMode))
		_, err = db.ExecContext(ctx, `
			CREATE TABLE games (
				id INTEGER PRIMARY KEY,
				name TEXT,
				slug TEXT,
				installed INTEGER
			);
			INSERT INTO games (name, slug, installed) VALUES ('WAL Game', 'wal-game', 1);
		`)
		require.NoError(t, err)
		require.FileExists(t, dbPath+"-wal")
		require.FileExists(t, dbPath+"-shm")

		results, err := ScanLutrisGames(dbPath)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "WAL Game", results[0].Name)
		assert.Equal(t, "lutris://wal-game/WAL%20Game", results[0].Path)
	})

	t.Run("empty_database", func(t *testing.T) {
		t.Parallel()

		// Create temporary database
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "pga.db")

		// Create empty database with games table
		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, db.Close())
		}()

		ctx := context.Background()
		_, err = db.ExecContext(ctx, `
			CREATE TABLE games (
				id INTEGER PRIMARY KEY,
				name TEXT,
				slug TEXT,
				installed INTEGER
			)
		`)
		require.NoError(t, err)
		require.NoError(t, db.Close())

		// Scan should return empty results
		results, err := ScanLutrisGames(dbPath)
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestLutrisScannerReturnsDatabaseErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	lutrisDir := filepath.Join(home, ".local", "share", "lutris")
	require.NoError(t, os.MkdirAll(lutrisDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(lutrisDir, "pga.db"), []byte("not sqlite"), 0o600))

	launcher := NewLutrisLauncher(LutrisOptions{
		lookPath: func(name string) (string, error) {
			if name == "lutris" {
				return "/usr/bin/lutris", nil
			}
			return "", os.ErrNotExist
		},
	})
	initial := []platforms.ScanResult{{Name: "Existing", Path: "/existing"}}
	results, err := launcher.Scanner(t.Context(), nil, "", initial)
	require.ErrorContains(t, err, "scan Lutris games")
	assert.Equal(t, initial, results)
}
