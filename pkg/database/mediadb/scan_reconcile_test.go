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

// Package mediadb_test drives the scan-staging/reconcile pipeline
// (sqlReconcileStagedSystem, sqlSeedCanonicalTags, and their MediaDB
// wrappers) through MediaDB's public API. It exists as an external test
// package specifically so its coverage is attributed to package mediadb: the
// same pipeline is already exercised end-to-end, scenario by scenario, in
// pkg/database/mediascanner's test suite, but per-package coverage (what CI
// and codecov measure) never credits mediadb for tests that live in
// mediascanner. These tests pin the contract at the mediadb boundary rather
// than re-deriving every mediascanner behavioral case.
package mediadb_test

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mediaDBIDsBySystem(t *testing.T, db *mediadb.MediaDB, systemID string) map[string]database.MediaWithFullPath {
	t.Helper()
	rows, err := db.GetMediaBySystemID(systemID)
	require.NoError(t, err)
	byPath := make(map[string]database.MediaWithFullPath, len(rows))
	for _, row := range rows {
		byPath[row.Path] = row
	}
	return byPath
}

func canonicalWorldTagDBID(t *testing.T, mediaDB *mediadb.MediaDB) int64 {
	t.Helper()
	var tagDBID int64
	err := mediaDB.UnsafeGetSQLDb().QueryRowContext(context.Background(), `
		SELECT t.DBID
		FROM Tags t
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE tt.Type = 'region' AND t.Tag = 'world'
	`).Scan(&tagDBID)
	require.NoError(t, err)
	return tagDBID
}

func attachCanonicalWorldTagToMedia(t *testing.T, mediaDB *mediadb.MediaDB, tagDBID int64, missing bool) {
	t.Helper()
	missingValue := 0
	if missing {
		missingValue = 1
	}
	mediaPath := filepath.ToSlash(filepath.Join("roms", "cleanup", "game.rom"))
	_, err := mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(), `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'Cleanup', 'Cleanup');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'game', 'Game');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, IsMissing) VALUES (1, 1, 1, ?, ?);
		INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (1, ?);
	`, mediaPath, missingValue, tagDBID)
	require.NoError(t, err)
}

func assertCanonicalWorldRestoredAfterCleanup(t *testing.T, mediaDB *mediadb.MediaDB) {
	t.Helper()
	ctx := context.Background()
	conn := mediaDB.UnsafeGetSQLDb()

	var worldCount int
	err := conn.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM Tags t
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE tt.Type = 'region' AND t.Tag = 'world'
	`).Scan(&worldCount)
	require.NoError(t, err)
	require.Zero(t, worldCount, "cleanup path must delete the canonical tag")

	var stampCount int
	err = conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM DBConfig WHERE Name = ?", mediadb.DBConfigCanonicalTagVocabHash,
	).Scan(&stampCount)
	require.NoError(t, err)
	require.Zero(t, stampCount, "cleanup path must invalidate the vocabulary stamp")

	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))
	err = conn.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM Tags t
		JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE tt.Type = 'region' AND t.Tag = 'world'
	`).Scan(&worldCount)
	require.NoError(t, err)
	assert.Equal(t, 1, worldCount)
}

func TestReconcileStagedSystem_FullScanInsertsTitlesMediaAndTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.ToSlash(filepath.Join(
		string(filepath.Separator), "roms", "SNES", "Super Game (USA) (Rev 2).sfc",
	))
	stats := scantest.IndexMediaPaths(t, mediaDB, "SNES", gamePath)

	assert.True(t, stats.SystemKnown)
	assert.Positive(t, stats.SystemDBID)
	assert.Equal(t, int64(1), stats.TitlesInserted)
	assert.Equal(t, int64(1), stats.MediaUpserted)
	assert.Equal(t, int64(0), stats.MediaMissing)

	total, err := mediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, total)

	byPath := mediaDBIDsBySystem(t, mediaDB, "SNES")
	require.Contains(t, byPath, gamePath)
	assert.False(t, byPath[gamePath].IsMissing)
}

// TestReconcileStagedSystem_FreshSystemMatchesFullPath is the correctness gate
// for #1279's fresh-system fast path. Indexing an empty database skips the steps
// that reconcile against pre-existing rows, on the argument that they are
// provably no-ops there. This proves it by outcome: index a set of paths into a
// fresh database, then index the identical set into a database that already
// holds them (so every step runs), and require the resulting media, titles,
// missing flags and tag links to be identical. A skip that was not actually a
// no-op would show up as a difference here.
func TestReconcileStagedSystem_FreshSystemMatchesFullPath(t *testing.T) {
	t.Parallel()

	paths := []string{
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Super Game (USA) (Rev 2).sfc")),
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Super Game (Japan).sfc")),
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Other Game (Europe) [!].sfc")),
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Solo Title (World).sfc")),
	}

	// Keyed by path, not DBID: DBIDs legitimately differ between the two
	// databases, the media and tags they describe must not.
	snapshot := func(t *testing.T, db *mediadb.MediaDB) (map[string]bool, map[string][]string) {
		t.Helper()
		media := map[string]bool{}
		tagsByPath := map[string][]string{}
		for path, row := range mediaDBIDsBySystem(t, db, "SNES") {
			media[path] = row.IsMissing
			tagList, err := db.GetMediaTagsByMediaDBID(context.Background(), row.DBID)
			require.NoError(t, err)
			names := make([]string, 0, len(tagList))
			for _, tag := range tagList {
				names = append(names, tag.Type+":"+tag.Tag)
			}
			sort.Strings(names)
			tagsByPath[path] = names
		}
		return media, tagsByPath
	}

	freshDB, freshCleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(freshCleanup)
	freshStats := scantest.IndexMediaPaths(t, freshDB, "SNES", paths...)
	freshMedia, freshTags := snapshot(t, freshDB)

	fullDB, fullCleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(fullCleanup)
	// Seed first so the second pass finds an existing Systems row and therefore
	// runs every step, including the ones the fresh path skips.
	scantest.IndexMediaPaths(t, fullDB, "SNES", paths...)
	fullStats := scantest.IndexMediaPaths(t, fullDB, "SNES", paths...)
	fullMedia, fullTags := snapshot(t, fullDB)

	assert.Equal(t, freshMedia, fullMedia, "media rows and missing flags must match")
	assert.Equal(t, freshTags, fullTags, "tag links must match")

	// The skipped steps are exactly the ones that report pre-existing-state
	// changes, so both runs must report none of them.
	assert.Equal(t, int64(0), freshStats.MediaMissing)
	assert.Equal(t, int64(0), freshStats.TitlesRenamed)
	assert.Equal(t, int64(0), freshStats.TagLinksDeleted)
	assert.Equal(t, int64(0), fullStats.MediaMissing)
	assert.Equal(t, int64(0), fullStats.TitlesRenamed)
	assert.Equal(t, int64(0), fullStats.TagLinksDeleted)
}

func TestReconcileStagedSystem_IdempotentRescanIsNoOp(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "Genesis", "Game.md"))
	scantest.IndexMediaPaths(t, mediaDB, "Genesis", gamePath)

	stats := scantest.IndexMediaPaths(t, mediaDB, "Genesis", gamePath)
	assert.Equal(t, int64(0), stats.TitlesInserted)
	assert.Equal(t, int64(0), stats.MediaUpserted)
	assert.Equal(t, int64(0), stats.MediaMissing)
	assert.Equal(t, int64(0), stats.TouchedTitles)
}

// TestReconcileStagedSystem_YieldsBetweenSQLSteps checks reconcile paces between
// its set-based SQL steps. It counts a fresh system and a re-index separately
// because #1279 made a fresh system skip the steps that reconcile against
// pre-existing rows, which necessarily removes their yields too. Fewer yields on
// a fresh reconcile is the intended consequence, not a pacing regression: there
// are fewer statements to pace between. The re-index count is the one that must
// stay high, since that is the long path — so the re-index stages a changed set;
// an identical one would be skipped outright (#1317) and pace only its
// fingerprint read, which TestReconcileStagedSystem_UnchangedRescanYieldsOnce
// covers.
func TestReconcileStagedSystem_YieldsBetweenSQLSteps(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Game.sfc"))
	addedPath := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Other Game.sfc"))

	freshYields := 0
	scantest.IndexMediaPathsWithOpts(t, mediaDB, "SNES", database.ScanReconcileOpts{
		Yield: func() error {
			freshYields++
			return nil
		},
	}, gamePath)

	rescanYields := 0
	scantest.IndexMediaPathsWithOpts(t, mediaDB, "SNES", database.ScanReconcileOpts{
		Yield: func() error {
			rescanYields++
			return nil
		},
	}, gamePath, addedPath)

	assert.GreaterOrEqual(t, freshYields, 5,
		"a fresh reconcile should still pace between the steps it does run")
	assert.GreaterOrEqual(t, rescanYields, 10,
		"a changed re-index runs every step and should pace between them")
	assert.Greater(t, rescanYields, freshYields,
		"a fresh system skips pre-existing-state steps, so it should yield fewer times")
}

// TestReconcileStagedSystem_UnchangedRescanYieldsOnce pins the pacing shape of
// a skipped reconcile: the staged-set digest is a real read of the staging
// tables, so it yields once after it, and nothing else runs.
func TestReconcileStagedSystem_UnchangedRescanYieldsOnce(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Game.sfc"))
	scantest.IndexMediaPaths(t, mediaDB, "SNES", gamePath)

	rescanYields := 0
	stats := scantest.IndexMediaPathsWithOpts(t, mediaDB, "SNES", database.ScanReconcileOpts{
		Yield: func() error {
			rescanYields++
			return nil
		},
	}, gamePath)

	assert.True(t, stats.Unchanged, "an identical re-index must be skipped")
	assert.Equal(t, 1, rescanYields, "a skipped reconcile paces only after its fingerprint read")
}

func TestReconcileStagedSystem_PropagatesYieldError(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	require.NoError(t, mediascanner.SeedCanonicalTags(ctx, mediaDB))
	require.NoError(t, mediaDB.BeginTransaction(true))
	t.Cleanup(func() { _ = mediaDB.RollbackTransaction() })
	require.NoError(t, mediaDB.ClearScanStage())
	gamePath := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Game.sfc"))
	require.NoError(t, mediascanner.StageMediaPath(&mediascanner.StageMediaPathParams{
		DB:       mediaDB,
		SystemID: "SNES",
		Path:     gamePath,
	}))

	yieldErr := errors.New("pacing stopped")
	_, err := mediaDB.ReconcileStagedSystem(ctx, "SNES", database.ScanReconcileOpts{
		Yield: func() error { return yieldErr },
	})
	require.ErrorIs(t, err, yieldErr)
	// The staged-set fingerprint is the first paced step of a full scan.
	assert.Contains(t, err.Error(), "scan reconcile pacing after fingerprint failed")
}

// TestReconcileStagedSystem_IncompleteScanPreservesMissingState pins
// ScanReconcileOpts.IncompleteScan: a scan known to have only partially
// collected a system's files must not flag the unstaged remainder missing,
// unlike an ordinary full scan of the same reduced set.
func TestReconcileStagedSystem_IncompleteScanPreservesMissingState(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	fileA := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Game A.sfc"))
	fileB := filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "SNES", "Game B.sfc"))
	scantest.IndexMediaPaths(t, mediaDB, "SNES", fileA, fileB)

	stats := scantest.IndexMediaPathsWithOpts(
		t, mediaDB, "SNES", database.ScanReconcileOpts{IncompleteScan: true}, fileA,
	)
	assert.Equal(t, int64(0), stats.MediaMissing, "incomplete scan must not flag the omitted file missing")

	missing, err := mediaDB.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 0, missing, "fileB must remain present after a known-incomplete scan omitted it")

	// A full (non-incomplete) rescan of the same reduced set does flag the
	// omitted file missing, confirming the difference is IncompleteScan and
	// not some other property of the reduced staged set.
	stats = scantest.IndexMediaPaths(t, mediaDB, "SNES", fileA)
	assert.Equal(t, int64(1), stats.MediaMissing)
	missing, err = mediaDB.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, missing)
}

// TestReconcileStagedSystem_UnknownSystemNoStagedIsNoop pins
// sqlResolveScanSystem's early-out: reconciling a system with no existing
// Systems row and nothing staged for it must not create one.
func TestReconcileStagedSystem_UnknownSystemNoStagedIsNoop(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, mediaDB.BeginTransaction(true))
	stats, err := mediaDB.ReconcileStagedSystem(context.Background(), "NeverSeen", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.False(t, stats.SystemKnown)
	assert.Zero(t, stats.SystemDBID)

	systems, err := mediaDB.GetAllSystems()
	require.NoError(t, err)
	assert.Empty(t, systems)
}

// TestReconcileStagedSystem_RequiresOpenTransaction pins the guard that
// reconcile must run inside the scanner's open batch transaction.
func TestReconcileStagedSystem_RequiresOpenTransaction(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	_, err := mediaDB.ReconcileStagedSystem(context.Background(), "SNES", database.ScanReconcileOpts{})
	require.ErrorIs(t, err, mediadb.ErrTransactionRequired)
}

// TestSeedCanonicalTagDefinitions_IdempotentNoDuplicates pins the anti-join
// dedup in sqlSeedCanonicalTags: seeding twice must not create duplicate
// TagTypes or Tags rows.
func TestSeedCanonicalTagDefinitions_IdempotentNoDuplicates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))

	var typesAfterFirst, tagsAfterFirst int
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM TagTypes").Scan(&typesAfterFirst))
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Tags").Scan(&tagsAfterFirst))
	require.Positive(t, typesAfterFirst)
	require.Positive(t, tagsAfterFirst)

	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))

	var typesAfterSecond, tagsAfterSecond int
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM TagTypes").Scan(&typesAfterSecond))
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Tags").Scan(&tagsAfterSecond))
	assert.Equal(t, typesAfterFirst, typesAfterSecond)
	assert.Equal(t, tagsAfterFirst, tagsAfterSecond)
}

// TestSeedCanonicalTagDefinitions_VocabStampSkipsReseed pins the DBConfig
// vocabulary stamp: a matching stamp skips the seeding statements entirely,
// and a stale stamp reruns them.
func TestSeedCanonicalTagDefinitions_VocabStampSkipsReseed(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	conn := mediaDB.UnsafeGetSQLDb()
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))

	var stamp string
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?", mediadb.DBConfigCanonicalTagVocabHash).Scan(&stamp))
	assert.Len(t, stamp, 64, "stamp is a hex sha256 of the vocabulary")

	// Remove a seeded row, then seed again with the stamp current: the pass
	// must be skipped, so the row stays gone.
	res, err := conn.ExecContext(ctx, "DELETE FROM Tags WHERE Tag = 'world'")
	require.NoError(t, err)
	deleted, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))
	var worldCount int
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Tags WHERE Tag = 'world'").Scan(&worldCount))
	assert.Zero(t, worldCount, "matching stamp must skip the seeding statements")

	// A stale stamp (vocabulary changed in a newer build) forces a re-seed
	// which restores the row and rewrites the stamp.
	_, err = conn.ExecContext(ctx,
		"UPDATE DBConfig SET Value = 'stale' WHERE Name = ?", mediadb.DBConfigCanonicalTagVocabHash)
	require.NoError(t, err)
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM Tags WHERE Tag = 'world'").Scan(&worldCount))
	assert.Equal(t, 1, worldCount)
	var restamped string
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?", mediadb.DBConfigCanonicalTagVocabHash).Scan(&restamped))
	assert.Equal(t, stamp, restamped)

	cleanupPaths := []struct {
		run  func(*testing.T, *mediadb.MediaDB, int64)
		name string
	}{
		{
			name: "sqlTruncateSystems",
			run: func(t *testing.T, mediaDB *mediadb.MediaDB, tagDBID int64) {
				attachCanonicalWorldTagToMedia(t, mediaDB, tagDBID, false)
				require.NoError(t, mediaDB.TruncateSystems([]string{"Cleanup"}))
			},
		},
		{
			name: "sqlCleanMediaOrphans",
			run: func(t *testing.T, mediaDB *mediadb.MediaDB, tagDBID int64) {
				attachCanonicalWorldTagToMedia(t, mediaDB, tagDBID, true)
				deleted, cleanupErr := mediaDB.CleanMediaOrphans(context.Background())
				require.NoError(t, cleanupErr)
				require.EqualValues(t, 1, deleted)
			},
		},
		{
			name: "clearMediaTagsForTagDBIDs",
			run: func(t *testing.T, mediaDB *mediadb.MediaDB, tagDBID int64) {
				ctx := context.Background()
				conn := mediaDB.UnsafeGetSQLDb()
				_, cleanupErr := conn.ExecContext(ctx,
					"INSERT INTO TagTypes (Type, IsExclusive) VALUES ('scraper-run.cleanup', 0)")
				require.NoError(t, cleanupErr)
				_, cleanupErr = conn.ExecContext(ctx, `
					UPDATE Tags
					SET TypeDBID = (SELECT DBID FROM TagTypes WHERE Type = 'scraper-run.cleanup')
					WHERE DBID = ?
				`, tagDBID)
				require.NoError(t, cleanupErr)
				attachCanonicalWorldTagToMedia(t, mediaDB, tagDBID, false)
				require.NoError(t, mediaDB.ClearScrapeRunMarkers(ctx, "cleanup", "world"))
			},
		},
	}
	for _, cleanupPath := range cleanupPaths {
		t.Run(cleanupPath.name, func(t *testing.T) {
			cleanupDB, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			require.NoError(t, cleanupDB.SeedCanonicalTagDefinitions(context.Background()))
			tagDBID := canonicalWorldTagDBID(t, cleanupDB)

			cleanupPath.run(t, cleanupDB, tagDBID)
			assertCanonicalWorldRestoredAfterCleanup(t, cleanupDB)
		})
	}
}

func TestSeedCanonicalTagDefinitions_RestoresCanonicalTypeExclusivity(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	conn := mediaDB.UnsafeGetSQLDb()
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))

	typeName := string(tags.TagTypeExtension)
	expectedExclusive := tags.IsExclusiveType(tags.TagTypeExtension)
	var typeDBID int64
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT DBID FROM TagTypes WHERE Type = ?", typeName).Scan(&typeDBID))
	_, err := conn.ExecContext(ctx,
		"UPDATE TagTypes SET IsExclusive = ? WHERE DBID = ?", !expectedExclusive, typeDBID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx,
		"DELETE FROM DBConfig WHERE Name = ?", mediadb.DBConfigCanonicalTagVocabHash)
	require.NoError(t, err)

	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(ctx))
	var restoredDBID int64
	var restoredExclusive bool
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT DBID, IsExclusive FROM TagTypes WHERE Type = ?", typeName,
	).Scan(&restoredDBID, &restoredExclusive))
	assert.Equal(t, typeDBID, restoredDBID, "upsert must preserve the existing type row")
	assert.Equal(t, expectedExclusive, restoredExclusive)
}
