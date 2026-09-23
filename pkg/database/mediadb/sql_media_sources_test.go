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

func TestMediaSourceGroupsMigrationDown(t *testing.T) {
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	goose.SetBaseFS(migrationFiles)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.DownTo(db.sql.Load(), "migrations", 20260920120000))
	for _, table := range []string{"MediaSources", "ScanStageSources"} {
		_, err := db.sql.Load().ExecContext(ctx, "SELECT SourceGroup FROM "+table+" LIMIT 1")
		require.Error(t, err, table)
	}
	require.NoError(t, goose.Up(db.sql.Load(), "migrations"))
	for _, table := range []string{"MediaSources", "ScanStageSources"} {
		_, err := db.sql.Load().ExecContext(ctx, "SELECT SourceGroup FROM "+table+" LIMIT 1")
		require.NoError(t, err, table)
	}
}

// Rows sharing a directory are one game only when every row on it carries the
// same non-empty group, judged across the system before any scope applies.
func TestMediaSourcesForScrapeSharedGame(t *testing.T) {
	t.Parallel()
	db, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()
	root := filepath.ToSlash(filepath.Join(t.TempDir(), "games"))
	dir := func(name string) string { return filepath.ToSlash(filepath.Join(root, name)) }
	rows := []struct {
		media, source, group string
		shared               bool
	}{
		{media: "test://kyra3-en/K", source: dir("kyra3"), group: "kyra3", shared: true},
		{media: "test://kyra3-fr/K", source: dir("kyra3"), group: "kyra3", shared: true},
		{media: "test://comp-a/A", source: dir("compilation"), group: "a"},
		{media: "test://comp-b/B", source: dir("compilation"), group: "b"},
		{media: "test://nogroup-a/A", source: dir("nogroup"), group: ""},
		{media: "test://nogroup-b/B", source: dir("nogroup"), group: ""},
		{media: "test://partial-a/A", source: dir("partial"), group: "a"},
		{media: "test://partial-b/B", source: dir("partial"), group: ""},
		{media: "test://single/S", source: dir("single"), group: "single", shared: true},
	}
	_, err := db.sql.Load().ExecContext(ctx, "DELETE FROM Media")
	require.NoError(t, err)
	for i, row := range rows {
		id := i + 1
		_, err = db.sql.Load().ExecContext(ctx,
			"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (?, 1, 1, ?, 0)",
			id, row.media)
		require.NoError(t, err)
		_, err = db.sql.Load().ExecContext(ctx, `
			INSERT INTO MediaSources (MediaDBID, SourcePath, SourceKey, SourceRoot, SourceKind, SourceGroup)
			VALUES (?, ?, ?, ?, 'directory', ?)`, id, row.source, row.source, root, row.group)
		require.NoError(t, err)
	}

	all, err := db.GetMediaSourcesForScrape(ctx, "NES", nil)
	require.NoError(t, err)
	require.Len(t, all, len(rows))
	want := make(map[string]bool, len(rows))
	for _, row := range rows {
		want[row.media] = row.shared
	}
	for _, source := range all {
		require.Equal(t, want[source.MediaPath], source.SharedGame, source.MediaPath)
	}

	for i, media := range []string{"test://kyra3-en/K", "test://comp-a/A", "test://partial-a/A"} {
		scope := &database.ScrapeScope{SystemID: "NES", Path: media, MediaID: int64([]int{1, 3, 7}[i])}
		scoped, scopeErr := db.GetMediaSourcesForScrape(ctx, "NES", scope)
		require.NoError(t, scopeErr)
		require.Len(t, scoped, 1)
		require.Equal(t, want[media], scoped[0].SharedGame, "scope must not hide the rows %s shares with", media)
	}
}
