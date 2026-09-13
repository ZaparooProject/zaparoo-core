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

//go:build linux

package launchers

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/require"
)

func TestLutrisDirectoryMetadataSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gameDir := filepath.Join(root, "game")
	require.NoError(t, os.MkdirAll(gameDir, 0o750))
	dbPath := filepath.Join(root, "pga.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(),
		`CREATE TABLE games (name TEXT, slug TEXT, installed INTEGER, directory TEXT);
		INSERT INTO games VALUES ('Game', 'game', 1, ?);`, gameDir)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	results, err := ScanLutrisGames(dbPath)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, &platforms.MediaSource{
		Path: gameDir, Root: root, Kind: platforms.MediaSourceDirectory,
	}, results[0].Source)
}

func TestFaugusGamePathMetadataSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	executable := filepath.Join(root, "game.exe")
	require.NoError(t, os.WriteFile(executable, []byte("game"), 0o600))
	library := filepath.Join(root, "games.json")
	data, err := json.Marshal([]faugusGame{{GameID: "game", Title: "Game", GamePath: executable}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(library, data, 0o600))
	results, err := scanFaugusGames(library)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, &platforms.MediaSource{
		Path: executable, Root: root, Kind: platforms.MediaSourceFile,
	}, results[0].Source)
}
