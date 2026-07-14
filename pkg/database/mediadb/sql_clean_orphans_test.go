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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupCleanOrphansDB creates a MediaDB pre-populated with:
//
//   - Two systems: "NES" (DBID=1) and "SNES" (DBID=2)
//   - Two TagTypes: "genre" (additive, DBID=1), "property" (additive, DBID=2)
//   - Four tags: "Action" (genre, DBID=1), "RPG" (genre, DBID=2),
//     "Cover" (property, DBID=3), "Soundtrack" (property, DBID=4)
//   - NES: one MediaTitle "mario" (DBID=1) with one Media row "mario.nes"
//     (DBID=1, IsMissing=1). The title has a MediaTitleTag (RPG) and a
//     MediaTitleProperty (Cover). The media row has a MediaTag (Action) and
//     a MediaProperty (Cover).
//   - SNES: one MediaTitle "zelda" (DBID=2) with two Media rows:
//     "zelda.sfc" (DBID=2, IsMissing=1) and "zelda_alt.sfc" (DBID=3,
//     IsMissing=0). The title has a MediaTitleTag (RPG). Both media rows
//     have a MediaTag (Action).
//   - A standalone MediaTitle "orphan" (DBID=3) with no Media rows at all
//     (already orphaned before any cleanup), with a MediaTitleTag (RPG).
//
// Tag "Soundtrack" (DBID=4) is never referenced by any join table and
// represents a pre-existing orphan that must be preserved (we only clean up
// tags that were referenced by now-deleted rows).
func setupCleanOrphansDB(t *testing.T) (db *MediaDB, cleanup func()) {
	t.Helper()
	db, cleanup = setupTempMediaDB(t)

	ctx := context.Background()
	conn := db.sql.Load()

	_, err := conn.ExecContext(ctx, `
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
		    (1, 'genre',    0),
		    (2, 'property', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
		    (1, 1, 'Action'),
		    (2, 1, 'RPG'),
		    (3, 2, 'Cover'),
		    (4, 2, 'Soundtrack');
		INSERT INTO Systems (DBID, SystemID, Name) VALUES
		    (1, 'NES',  'Nintendo Entertainment System'),
		    (2, 'SNES', 'Super Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
		    (1, 1, 'mario',  'Super Mario Bros'),
		    (2, 2, 'zelda',  'The Legend of Zelda'),
		    (3, 2, 'orphan', 'Orphan Title');
		INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID) VALUES
		    (1, 2),
		    (2, 2),
		    (3, 2);
	`)
	require.NoError(t, err)

	nesCoverPath := filepath.Join("covers", "mario.png")
	_, err = conn.ExecContext(ctx,
		"INSERT INTO MediaTitleProperties (DBID, MediaTitleDBID, TypeTagDBID, Text)"+
			" VALUES (1, 1, 3, ?)",
		nesCoverPath)
	require.NoError(t, err)

	nesPath := filepath.Join("roms", "nes", "mario.nes")
	snesPath := filepath.Join("roms", "snes", "zelda.sfc")
	snesAltPath := filepath.Join("roms", "snes", "zelda_alt.sfc")
	_, err = conn.ExecContext(ctx,
		"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES"+
			" (1, 1, 1, ?, 1),"+ // NES mario — missing
			" (2, 2, 2, ?, 1),"+ // SNES zelda — missing
			" (3, 2, 2, ?, 0)", // SNES zelda alt — present
		nesPath, snesPath, snesAltPath)
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, `
		INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES
		    (1, 1),
		    (2, 1),
		    (3, 1);
	`)
	require.NoError(t, err)

	nesPropPath := filepath.Join("roms", "nes", "mario.mp4")
	_, err = conn.ExecContext(ctx,
		"INSERT INTO MediaProperties (DBID, MediaDBID, TypeTagDBID, Text)"+
			" VALUES (1, 1, 3, ?)",
		nesPropPath)
	require.NoError(t, err)

	return db, cleanup
}

// countRows returns the number of rows in the given table.
func countRows(t *testing.T, db *MediaDB, table string) int {
	t.Helper()
	var n int
	//nolint:gosec // table name is test-internal, never from user input
	err := db.sql.Load().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+table).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestSqlCleanMediaOrphans_NoMissing verifies that the function is a no-op
// when no Media rows have IsMissing=1.
func TestSqlCleanMediaOrphans_NoMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	deleted, err := sqlCleanMediaOrphans(context.Background(), db.sql.Load())
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

// TestSqlCleanMediaOrphans_DeletesMissingMediaAndChildren verifies that
// missing Media rows and their direct children (MediaTags, MediaProperties)
// are removed.
func TestSqlCleanMediaOrphans_DeletesMissingMediaAndChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	deleted, err := sqlCleanMediaOrphans(context.Background(), db.sql.Load())
	require.NoError(t, err)
	// Media DBID 1 (NES mario) and DBID 2 (SNES zelda) are missing.
	assert.Equal(t, int64(2), deleted)

	// Media DBID 3 (SNES zelda alt) must survive.
	var mediaCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(), "SELECT COUNT(*) FROM Media").Scan(&mediaCount))
	assert.Equal(t, 1, mediaCount, "only the non-missing SNES media row should remain")

	var mediaTagCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(), "SELECT COUNT(*) FROM MediaTags WHERE MediaDBID IN (1,2)").Scan(&mediaTagCount))
	assert.Equal(t, 0, mediaTagCount, "MediaTags for deleted media must be gone")

	var mediaPropCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(), "SELECT COUNT(*) FROM MediaProperties WHERE MediaDBID = 1").Scan(&mediaPropCount))
	assert.Equal(t, 0, mediaPropCount, "MediaProperties for deleted media must be gone")
}

func TestCleanMediaOrphans_RefreshesCachedMediaCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	ctx := context.Background()
	_, err := db.sql.Load().ExecContext(ctx,
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, '3'), (?, '2')",
		DBConfigMediaTotalCount, DBConfigMediaMissingCount)
	require.NoError(t, err)

	deleted, err := db.CleanMediaOrphans(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	total, err := db.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	missing, err := db.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 0, missing)
}

// TestSqlCleanMediaOrphans_KeepsNonMissingMedia verifies that Media rows with
// IsMissing=0 are untouched.
func TestSqlCleanMediaOrphans_KeepsNonMissingMedia(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	_, err := sqlCleanMediaOrphans(context.Background(), db.sql.Load())
	require.NoError(t, err)

	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM Media WHERE IsMissing = 0").Scan(&count))
	assert.Equal(t, 1, count, "the surviving SNES zelda_alt row must remain")

	// Its MediaTag should also survive.
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM MediaTags WHERE MediaDBID = 3").Scan(&count))
	assert.Equal(t, 1, count, "the surviving media's tag must remain")
}

// TestSqlCleanMediaOrphans_RemovesFullyOrphanedTitle verifies that a
// MediaTitle is removed when ALL of its Media rows were missing and deleted.
func TestSqlCleanMediaOrphans_RemovesFullyOrphanedTitle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	_, err := sqlCleanMediaOrphans(context.Background(), db.sql.Load())
	require.NoError(t, err)

	// NES "mario" title had only one Media row (IsMissing=1) → must be gone.
	var titleCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM MediaTitles WHERE DBID = 1").Scan(&titleCount))
	assert.Equal(t, 0, titleCount, "fully-orphaned NES title must be deleted")

	// Its MediaTitleTags and MediaTitleProperties must also be gone.
	var titleTagCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM MediaTitleTags WHERE MediaTitleDBID = 1").Scan(&titleTagCount))
	assert.Equal(t, 0, titleTagCount, "MediaTitleTags for deleted title must be gone")

	var titlePropCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM MediaTitleProperties WHERE MediaTitleDBID = 1").Scan(&titlePropCount))
	assert.Equal(t, 0, titlePropCount, "MediaTitleProperties for deleted title must be gone")
}

// TestSqlCleanMediaOrphans_KeepsPartiallyMissingTitle verifies that a
// MediaTitle is retained when at least one of its Media rows survives.
func TestSqlCleanMediaOrphans_KeepsPartiallyMissingTitle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	_, err := sqlCleanMediaOrphans(context.Background(), db.sql.Load())
	require.NoError(t, err)

	// SNES "zelda" title had two rows: one missing, one present → must survive.
	var titleCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM MediaTitles WHERE DBID = 2").Scan(&titleCount))
	assert.Equal(t, 1, titleCount, "partially-missing SNES title must survive")

	// Its MediaTitleTag must also survive.
	var titleTagCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM MediaTitleTags WHERE MediaTitleDBID = 2").Scan(&titleTagCount))
	assert.Equal(t, 1, titleTagCount, "MediaTitleTag for surviving title must remain")
}

// TestSqlCleanMediaOrphans_RemovesOrphanedTags verifies that Tags become
// unreferenced after the cleanup are deleted, but Tags still in use survive.
func TestSqlCleanMediaOrphans_RemovesOrphanedTags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	_, err := sqlCleanMediaOrphans(context.Background(), db.sql.Load())
	require.NoError(t, err)

	// Tag "Action" (DBID=1) was referenced by MediaTags for all three media
	// rows.  After cleanup, it is still referenced by the surviving SNES row
	// (Media DBID=3) → must survive.
	var actionCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM Tags WHERE DBID = 1").Scan(&actionCount))
	assert.Equal(t, 1, actionCount, "Action tag still referenced by surviving media must survive")

	// Tag "RPG" (DBID=2) was referenced by MediaTitleTags for the NES title
	// (now gone) and the SNES "zelda" title (still present) and the orphan
	// title (also gone).  It remains through the surviving SNES title.
	var rpgCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM Tags WHERE DBID = 2").Scan(&rpgCount))
	assert.Equal(t, 1, rpgCount, "RPG tag still referenced by surviving title must survive")

	// Tag "Cover" (DBID=3) was referenced by MediaTitleProperties for the NES
	// title (now gone) and MediaProperties for the NES media (now gone).
	// It has no surviving references → must be deleted.
	var coverCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM Tags WHERE DBID = 3").Scan(&coverCount))
	assert.Equal(t, 0, coverCount, "Cover tag with no surviving references must be deleted")

	// Tag "Soundtrack" (DBID=4) was never referenced by any join table and is
	// NOT in our candidate set — the function must not touch it.
	var soundtrackCount int
	require.NoError(t, db.sql.Load().QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM Tags WHERE DBID = 4").Scan(&soundtrackCount))
	assert.Equal(t, 1, soundtrackCount, "unreferenced-but-not-candidate tag must not be touched")
}

// TestSqlCleanMediaOrphans_ReturnCount verifies the returned count equals the
// number of Media rows actually deleted.
func TestSqlCleanMediaOrphans_ReturnCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	deleted, err := sqlCleanMediaOrphans(context.Background(), db.sql.Load())
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted, "expected 2 missing media rows to be deleted")
}

// TestCleanMediaOrphans_GuardIndexingRunning verifies that CleanMediaOrphans
// returns ErrIndexingInProgress when indexing status is "running".
func TestCleanMediaOrphans_GuardIndexingRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, db.SetIndexingStatus(IndexingStatusRunning))

	_, err := db.CleanMediaOrphans(context.Background())
	assert.ErrorIs(t, err, ErrIndexingInProgress)
}

// TestCleanMediaOrphans_GuardIndexingPending verifies that CleanMediaOrphans
// returns ErrIndexingInProgress when indexing status is "pending".
func TestCleanMediaOrphans_GuardIndexingPending(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, db.SetIndexingStatus(IndexingStatusPending))

	_, err := db.CleanMediaOrphans(context.Background())
	assert.ErrorIs(t, err, ErrIndexingInProgress)
}

// TestCleanMediaOrphans_GuardTransactionActive verifies that CleanMediaOrphans
// returns ErrTransactionActive when a batch transaction is open.
func TestCleanMediaOrphans_GuardTransactionActive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, db.BeginTransaction(false))
	defer func() { _ = db.RollbackTransaction() }()

	_, err := db.CleanMediaOrphans(context.Background())
	assert.ErrorIs(t, err, ErrTransactionActive)
}

// TestCleanMediaOrphans_GuardOptimizationInProgress verifies that
// CleanMediaOrphans returns ErrOptimizationInProgress when background
// optimisation is active.
func TestCleanMediaOrphans_GuardOptimizationInProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	db.isOptimizing.Store(true)
	defer db.isOptimizing.Store(false)

	_, err := db.CleanMediaOrphans(context.Background())
	assert.ErrorIs(t, err, ErrOptimizationInProgress)
}

// TestCleanMediaOrphans_AllowedWhenIndexingCompleted verifies that
// CleanMediaOrphans runs successfully when indexing status is "completed".
func TestCleanMediaOrphans_AllowedWhenIndexingCompleted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, db.SetIndexingStatus(IndexingStatusCompleted))

	_, err := db.CleanMediaOrphans(context.Background())
	assert.NoError(t, err)
}

// TestCleanMediaOrphans_InvalidatesCaches verifies that after a successful
// cleanup the in-memory slug search and tag caches are cleared.
func TestCleanMediaOrphans_InvalidatesCaches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	// Seed both in-memory caches with a non-nil sentinel to confirm clearing.
	db.slugSearchCache.Store(&SlugSearchCache{})
	db.inMemoryTagCache.Store(&tagCache{})

	_, err := db.CleanMediaOrphans(context.Background())
	require.NoError(t, err)

	assert.Nil(t, db.slugSearchCache.Load(), "slug search cache must be cleared after cleanup")
	assert.Nil(t, db.inMemoryTagCache.Load(), "tag cache must be cleared after cleanup")
}

// TestCleanMediaOrphans_IdempotentOnRerun verifies that running
// CleanMediaOrphans twice in a row is safe and returns 0 on the second call.
func TestCleanMediaOrphans_IdempotentOnRerun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	first, err := db.CleanMediaOrphans(context.Background())
	require.NoError(t, err)
	assert.Positive(t, first, "first run must delete rows")

	second, err := db.CleanMediaOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), second, "second run must be a no-op")
}

// TestCleanMediaOrphans_PreservesTables verifies that TagTypes and Systems
// are never deleted by CleanMediaOrphans.
func TestCleanMediaOrphans_PreservesTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	db, cleanup := setupCleanOrphansDB(t)
	defer cleanup()

	_, err := db.CleanMediaOrphans(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, countRows(t, db, "TagTypes"), "TagTypes must never be deleted")
	assert.Equal(t, 2, countRows(t, db, "Systems"), "Systems must never be deleted")
}
