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

package mediadb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestMediaSourcesMigrationUpgrade(t *testing.T) {
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	var before int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&before))
	goose.SetBaseFS(migrationFiles)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.DownTo(db.sql.Load(), "migrations", 20260901120000))
	_, err := db.sql.Load().ExecContext(ctx, "SELECT 1 FROM MediaSources LIMIT 1")
	require.Error(t, err)
	require.NoError(t, goose.Up(db.sql.Load(), "migrations"))

	var after int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&after))
	require.Equal(t, before, after)
	_, err = db.sql.Load().ExecContext(ctx, "SELECT 1 FROM MediaSources LIMIT 1")
	require.NoError(t, err)
}

func TestMediaSourcesForScrape(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	rootA := filepath.ToSlash(filepath.Join(t.TempDir(), "drive-a"))
	rootB := filepath.ToSlash(filepath.Join(t.TempDir(), "drive-b"))
	mediaPaths := []string{"test://one/One", "test://two/Two", "test://three/Three", "test://missing/Missing"}
	sourcePaths := []string{
		filepath.ToSlash(filepath.Join(rootA, "Shared")),
		filepath.ToSlash(filepath.Join(rootA, "Shared")),
		filepath.ToSlash(filepath.Join(rootB, "Shared")),
		filepath.ToSlash(filepath.Join(rootA, "Missing")),
	}
	_, err := db.sql.Load().ExecContext(ctx, "DELETE FROM Media")
	require.NoError(t, err)
	for i := range mediaPaths {
		id := i + 1
		_, err = db.sql.Load().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (?, 1, 1, ?, ?)",
			id, mediaPaths[i], i == 3)
		require.NoError(t, err)
		roots := []string{rootA, rootA, rootB, rootA}
		_, err = db.sql.Load().ExecContext(ctx, `
			INSERT INTO MediaSources (MediaDBID, SourcePath, SourceKey, SourceRoot, SourceKind)
			VALUES (?, ?, ?, ?, 'directory')`, id, sourcePaths[i], sourcePaths[i], roots[i])
		require.NoError(t, err)
	}

	roots, err := db.GetMediaSourceRoots(ctx, "NES")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{rootA, rootB}, roots)

	all, err := db.GetMediaSourcesForScrape(ctx, "NES", nil)
	require.NoError(t, err)
	require.Len(t, all, 3)
	for _, source := range all {
		if source.MediaPath == mediaPaths[2] {
			require.True(t, source.Unique)
		} else {
			require.False(t, source.Unique)
		}
	}

	scope := &database.ScrapeScope{SystemID: "NES", Path: mediaPaths[0], MediaID: 1}
	scoped, err := db.GetMediaSourcesForScrape(ctx, "NES", scope)
	require.NoError(t, err)
	require.Len(t, scoped, 1)
	require.False(t, scoped[0].Unique, "scope must not hide system-wide source ambiguity")

	_, err = db.GetMediaSourcesForScrape(ctx, "SNES", scope)
	require.Error(t, err)
}
