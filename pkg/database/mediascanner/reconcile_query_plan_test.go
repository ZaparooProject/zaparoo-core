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

package mediascanner

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/require"
)

// zaparooReconcileEQPEnv gates this investigation harness. It is not a
// regression test: it exists to capture EXPLAIN QUERY PLAN, at a synthetic
// scale close to the device system that surfaced the problem, for the three
// reconcile steps #1279 found scaling with total corpus size instead of with
// what actually changed (capture tag additions, capture stale tag titles,
// delete stale tag links), plus the chunked upsert media statement added in
// the same round (verifying its range predicate actually pins ScanStage as
// the driving table is that fix's pre-ship gate).
//
//	ZAPAROO_RECONCILE_EQP=1 go test -run TestReconcileQueryPlansAtScale -v ./pkg/database/mediascanner/
const zaparooReconcileEQPEnv = "ZAPAROO_RECONCILE_EQP"

// explainQueryPlan and dumpSQLiteStats are duplicated (not imported) from
// pkg/database/mediadb/sql_plan_test.go: they're unexported there, and this
// harness lives in mediascanner specifically to reuse the synthetic-library
// generator (buildSyntheticFilenames, StageMediaPath) without an import
// cycle. Keep in sync if the originals change.
func explainQueryPlan(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rows.Close())
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

func dumpSQLiteStats(ctx context.Context, t *testing.T, db *sql.DB) {
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
		WHERE tbl IN ('ScanStage', 'ScanStageTags', 'Media', 'MediaTitles', 'MediaTags', 'Tags', 'TagTypes')
		ORDER BY tbl, idx
	`)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, rows.Close())
	}()

	for rows.Next() {
		var tbl, stat string
		var idx sql.NullString
		require.NoError(t, rows.Scan(&tbl, &idx, &stat))
		t.Logf("sqlite_stat1: tbl=%s idx=%s stat=%s", tbl, idx.String, stat)
	}
	require.NoError(t, rows.Err())
}

// TestReconcileQueryPlansAtScale is an investigation harness, not a
// regression test — see zaparooReconcileEQPEnv.
func TestReconcileQueryPlansAtScale(t *testing.T) {
	if os.Getenv(zaparooReconcileEQPEnv) != "1" {
		t.Skipf("set %s=1 to capture reconcile query plans at scale", zaparooReconcileEQPEnv)
	}

	const n = largeReconcileBenchSize
	ctx := context.Background()
	filenames := buildSyntheticFilenames(n)

	db, cleanup := helpers.NewInMemoryMediaDB(t)
	defer cleanup()
	require.NoError(t, SeedCanonicalTags(ctx, db))

	// MigrateUp (NewInMemoryMediaDB only calls the lower-level Allocate)
	// seeds sqlite_stat1 the same way a fresh device install does — without
	// it this harness would plan against empty statistics, which produces a
	// different (likely better) plan than the device ever sees mid-index.
	// On device, stats are seeded once at migrate time, refreshed once early
	// via PRAGMA optimize, then not touched again until end-of-run — so
	// during a mega-system's reconcile the planner is working from stats
	// captured when the table was still small. See #1279 round 3 notes.
	require.NoError(t, db.MigrateUp())

	require.NoError(t, db.BeginTransaction(true))
	for _, fn := range filenames {
		require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: db, SystemID: "nes", Path: fn}))
	}
	stats, err := db.ReconcileStagedSystem(ctx, "nes", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, db.CommitTransaction())
	t.Logf("staged %d files -> %d titles, %d media, %d tag links",
		n, stats.TitlesInserted, stats.MediaUpserted, stats.TagLinksAdded)

	sqlDB := db.UnsafeGetSQLDb()
	dumpSQLiteStats(ctx, t, sqlDB)

	var cacheSize, pageCount int64
	require.NoError(t, sqlDB.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize))
	require.NoError(t, sqlDB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount))
	t.Logf("cache_size=%d page_count=%d", cacheSize, pageCount)

	systemDBID := stats.SystemDBID

	// sqlReconcileStagedSystem clears ScanStage at the end of every reconcile
	// (sqlClearScanStage), so by this point it's empty — capturing EQP against
	// an empty, statistics-less ScanStage would be unrepresentative of what
	// the planner sees while the chunked upsert media statement actually
	// runs. Re-stage the same files and commit (without reconciling this
	// second batch) so ScanStage is populated the same way mid-reconcile,
	// and visible to the pooled connection EXPLAIN QUERY PLAN below runs on
	// (staging alone, via db.tx, wouldn't be — WAL readers on a different
	// connection can't see an uncommitted transaction).
	require.NoError(t, db.BeginTransaction(true))
	for _, fn := range filenames {
		require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: db, SystemID: "nes", Path: fn}))
	}
	require.NoError(t, db.CommitTransaction())

	// Query text below is duplicated from unexported statements/consts in
	// pkg/database/mediadb/sql_scan_reconcile.go (scanStaleLinkFilter and the
	// capture/delete steps built from it, plus the chunked upsert media
	// statement) — kept in literal form here rather than exported, since
	// this harness is an investigation tool, not a shipped dependency. Keep
	// in sync with the source if those queries change shape.

	explainQueryPlan(ctx, t, sqlDB, `
		WITH multi_titles AS (
			SELECT MediaTitleDBID FROM Media
			WHERE SystemDBID = ? AND IsMissing = 0
			GROUP BY MediaTitleDBID HAVING COUNT(*) > 1
		)
		INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID)
		SELECT m.MediaTitleDBID
		FROM ScanStageTags st
		JOIN Media m ON m.SystemDBID = ? AND m.Path = st.Path
		JOIN multi_titles mtit ON mtit.MediaTitleDBID = m.MediaTitleDBID
		JOIN TagTypes tt ON tt.Type = st.TagType
		JOIN Tags t ON t.TypeDBID = tt.DBID AND t.Tag = st.Tag
		WHERE NOT EXISTS (
			SELECT 1 FROM MediaTags mt WHERE mt.MediaDBID = m.DBID AND mt.TagDBID = t.DBID
		)`, systemDBID, systemDBID)

	const staleLinkFilter = `
		FROM Media m
		JOIN ScanStage s ON s.Path = m.Path
		JOIN MediaTags mt ON mt.MediaDBID = m.DBID
		JOIN Tags t ON t.DBID = mt.TagDBID
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE m.SystemDBID = ?
		  AND tt.Type NOT IN (?, ?, ?, ?, ?)
		  AND tt.Type NOT LIKE ?
		  AND tt.Type NOT LIKE ?
		  AND NOT EXISTS (
			SELECT 1 FROM ScanStageTags st
			WHERE st.Path = m.Path AND st.TagType = tt.Type AND st.Tag = t.Tag
		  )`
	nonScannerTypeArgs := []any{
		systemDBID,
		string(tags.TagTypeUser),
		string(tags.TagTypeProperty),
		string(tags.TagTypeRating),
		string(tags.TagTypeGenre),
		string(tags.TagTypeGameFamily),
		string(tags.ScraperType("")) + "%",
		string(tags.ScraperRunType("")) + "%",
	}

	explainQueryPlan(ctx, t, sqlDB,
		"WITH multi_titles AS ("+
			"SELECT MediaTitleDBID FROM Media WHERE SystemDBID = ? AND IsMissing = 0 "+
			"GROUP BY MediaTitleDBID HAVING COUNT(*) > 1) "+
			"INSERT OR IGNORE INTO ScanTouchedTitles (TitleDBID) SELECT m.MediaTitleDBID"+staleLinkFilter+
			" AND EXISTS (SELECT 1 FROM multi_titles mtit WHERE mtit.MediaTitleDBID = m.MediaTitleDBID)",
		append([]any{systemDBID}, nonScannerTypeArgs...)...)

	explainQueryPlan(ctx, t, sqlDB,
		"DELETE FROM MediaTags WHERE (MediaDBID, TagDBID) IN (SELECT mt.MediaDBID, mt.TagDBID"+staleLinkFilter+")",
		nonScannerTypeArgs...)

	// The chunked upsert media statement from item 1, using an actual
	// scanUpsertMediaBatchSize-wide slice of ScanStage.Path (not a wide-open
	// sentinel range) — a real chunk's cardinality matters to the planner's
	// cost estimate, so a near-unbounded range would not be representative.
	var chunkLower, chunkUpper string
	require.NoError(t, sqlDB.QueryRowContext(ctx,
		"SELECT Path FROM ScanStage ORDER BY Path LIMIT 1 OFFSET ?", n/2).Scan(&chunkLower))
	require.NoError(t, sqlDB.QueryRowContext(ctx,
		"SELECT Path FROM ScanStage ORDER BY Path LIMIT 1 OFFSET ?", n/2+2000).Scan(&chunkUpper))
	explainQueryPlan(ctx, t, sqlDB, `
		INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing)
		SELECT t.DBID, ?, s.Path, s.ParentDir, s.SortName, 0
		FROM ScanStage s
		CROSS JOIN MediaTitles t ON t.SystemDBID = ? AND t.Slug = s.Slug
		WHERE s.Path > ? AND s.Path <= ?
		ON CONFLICT (SystemDBID, Path) DO UPDATE SET
			MediaTitleDBID = excluded.MediaTitleDBID,
			ParentDir      = excluded.ParentDir,
			SortName       = excluded.SortName,
			IsMissing      = 0
		WHERE MediaTitleDBID <> excluded.MediaTitleDBID
		   OR ParentDir <> excluded.ParentDir
		   OR SortName <> excluded.SortName
		   OR IsMissing <> 0`, systemDBID, systemDBID, chunkLower, chunkUpper)
}
