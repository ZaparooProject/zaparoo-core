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
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExplainHotScraperQueries(t *testing.T) {
	t.Skip("diagnostic helper; run manually with -run TestExplainHotScraperQueries -v")
	mediaDB, cleanup := setupScraperTestDB(t)
	defer cleanup()
	ctx := context.Background()

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT COUNT(*)
		FROM MediaTags
		WHERE TagDBID = ?
	`, int64(1))

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT m.DBID
		FROM Media m INDEXED BY media_system_path_idx
		CROSS JOIN MediaTags mt
		WHERE m.SystemDBID = ?
		  AND mt.MediaDBID = m.DBID
		  AND mt.TagDBID IN (?)
	`, int64(1), int64(1))

	// Previous scraped-ID shape. Kept to compare tag-driven scan against system-driven lookup.
	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT mt.MediaDBID
		FROM MediaTags mt
		JOIN Media m ON mt.MediaDBID = m.DBID
		WHERE mt.TagDBID = ? AND m.SystemDBID = ?
	`, int64(1), int64(1))

	// Previous scraped-ID shape. Kept to compare the removed join/distinct overhead.
	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT DISTINCT mt.MediaDBID
		FROM MediaTags mt
		JOIN Media m ON mt.MediaDBID = m.DBID
		JOIN Tags t ON mt.TagDBID = t.DBID
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE m.SystemDBID = ? AND tt.Type = ? AND t.Tag = ?
	`, int64(1), "scraper.test", "scraped")

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT mt.DBID, mt.SystemDBID, mt.Slug, mt.Name
		FROM MediaTitles mt
		WHERE mt.SystemDBID = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM Media m
			JOIN MediaTags mtag ON m.DBID = mtag.MediaDBID
			WHERE m.MediaTitleDBID = mt.DBID
			  AND mtag.TagDBID IN (?)
		  )
	`, int64(1), int64(1))

	// Previous missing-title shape. Kept to compare removed Tags/TagTypes correlated join overhead.
	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT mt.DBID, mt.SystemDBID, mt.Slug, mt.Name
		FROM MediaTitles mt
		WHERE mt.SystemDBID = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM Media m
			JOIN MediaTags mtag ON m.DBID = mtag.MediaDBID
			JOIN Tags t         ON mtag.TagDBID = t.DBID
			JOIN TagTypes tt    ON t.TypeDBID = tt.DBID
			WHERE m.MediaTitleDBID = mt.DBID
			  AND tt.Type = ?
			  AND (t.Tag = ? OR t.Tag = ?)
		  )
	`, int64(1), "scraper.test", "scraped", "scraped")

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT m.DBID, m.Path, m.ParentDir, m.MediaTitleDBID, m.SystemDBID, t.Slug, s.SystemID
		FROM Media m
		JOIN MediaTitles t ON m.MediaTitleDBID = t.DBID
		JOIN Systems s ON t.SystemDBID = s.DBID
		WHERE s.SystemID = ?
		ORDER BY m.DBID
	`, "NES")

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		DELETE FROM MediaTitleTags
		WHERE MediaTitleDBID IN (?, ?)
		  AND EXISTS (
			SELECT 1
			FROM Tags
			WHERE Tags.DBID = MediaTitleTags.TagDBID
			  AND Tags.TypeDBID = ?
		  )
	`, int64(1), int64(2), int64(2))

	// Previous production shape. Target-copy benchmark showed it scales with large tag-type cardinality.
	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		DELETE FROM MediaTitleTags
		WHERE MediaTitleDBID IN (?, ?)
		  AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID = ?)
	`, int64(1), int64(2), int64(2))

	// Rejected on target: fewer statements locally, but slower batch duration on target storage.
	// Kept here only to compare plan shape when investigating future delete alternatives.
	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		WITH delete_pairs(MediaTitleDBID, TypeDBID) AS (
			VALUES (?, ?), (?, ?)
		)
		DELETE FROM MediaTitleTags
		WHERE EXISTS (
			SELECT 1
			FROM delete_pairs p
			JOIN Tags t ON t.DBID = MediaTitleTags.TagDBID
			WHERE p.MediaTitleDBID = MediaTitleTags.MediaTitleDBID
			  AND p.TypeDBID = t.TypeDBID
		)
	`, int64(1), int64(2), int64(2), int64(2))

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT DBID, Tag
		FROM Tags
		WHERE TypeDBID = ? AND Tag IN (?, ?, ?)
	`, int64(2), "nintendo", "capcom", "konami")

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT DBID, Type, IsExclusive
		FROM TagTypes
		WHERE Type IN (?, ?, ?)
	`, "developer", "property", "scraper.test")

	explainQueryPlan(ctx, t, mediaDB.sql.Load(), `
		SELECT t.DBID, t.Tag
		FROM Tags t
		JOIN TagTypes tt ON t.TypeDBID = tt.DBID
		WHERE tt.Type = ? AND t.Tag IN (?, ?)
	`, "property", "description", "00000000000description")

	dumpScraperPragmas(ctx, t, mediaDB.sql.Load())
	dumpSQLiteStats(ctx, t, mediaDB.sql.Load())
}

func explainQueryPlan(ctx context.Context, t testing.TB, db *sql.DB, query string, args ...any) {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("failed to close rows: %v", closeErr)
		}
	}()

	t.Logf("EXPLAIN QUERY PLAN for:%s", query)
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		t.Logf("  id=%d parent=%d detail=%s", id, parent, detail)
	}
	require.NoError(t, rows.Err())
}

func dumpSQLiteStats(ctx context.Context, t testing.TB, db *sql.DB) {
	t.Helper()
	var statTableCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_schema
		WHERE type = 'table' AND name = 'sqlite_stat1'
	`).Scan(&statTableCount))
	if statTableCount == 0 {
		t.Log("sqlite_stat1 not present; ANALYZE has not populated planner stats")
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT tbl, idx, stat
		FROM sqlite_stat1
		WHERE tbl IN ('MediaTitleTags', 'MediaTags', 'Tags', 'TagTypes', 'Media', 'MediaTitles')
		ORDER BY tbl, idx
	`)
	require.NoError(t, err)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("failed to close sqlite_stat1 rows: %v", closeErr)
		}
	}()

	logged := 0
	for rows.Next() {
		var table, index, stat string
		require.NoError(t, rows.Scan(&table, &index, &stat))
		t.Logf("sqlite_stat1 tbl=%s idx=%s stat=%s", table, index, stat)
		logged++
	}
	require.NoError(t, rows.Err())
	if logged == 0 {
		t.Log("sqlite_stat1 present but no rows for hot scraper tables")
	}
}

func dumpScraperPragmas(ctx context.Context, t testing.TB, db *sql.DB) {
	t.Helper()
	for _, pragma := range []string{
		"journal_mode",
		"synchronous",
		"page_size",
		"cache_size",
		"temp_store",
		"foreign_keys",
		"busy_timeout",
	} {
		var value string
		require.NoError(t, db.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&value))
		t.Logf("PRAGMA %s = %s", pragma, value)
	}

	rows, err := db.QueryContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)")
	require.NoError(t, err)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("failed to close rows: %v", closeErr)
		}
	}()
	cols, err := rows.Columns()
	require.NoError(t, err)
	values := make([]string, len(cols))
	scan := make([]any, len(cols))
	for i := range values {
		scan[i] = &values[i]
	}
	for rows.Next() {
		require.NoError(t, rows.Scan(scan...))
		t.Logf("PRAGMA wal_checkpoint(PASSIVE) %v = %v", cols, values)
	}
	require.NoError(t, rows.Err())
}
