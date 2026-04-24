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

// Regression tests for sqlTruncateSystems correctness.
//
// These tests guard against two bugs that were fixed together:
//
//  1. CASCADE DELETE stall: the old implementation deleted Systems rows with FK CASCADE
//     enabled, causing SQLite to walk 50K–200K child rows on SD card. The fix uses
//     PRAGMA foreign_keys=OFF + explicit child-first DELETEs.
//
//  2. Orphan tag cleanup full-table scan: the old implementation used
//     NOT IN (SELECT ... UNION SELECT ... UNION SELECT ...)
//     against the full MediaTags/MediaTitleTags/SupportingMedia tables (hundreds of
//     thousands of rows after other systems are indexed). The fix bounds the cleanup to
//     a pre-collected candidate set and uses NOT EXISTS for index-friendly lookups.
//
// Both bugs only manifested at scale — small test DBs worked fine — so these tests
// use enough systems/files to exercise the shared-tag and FK-integrity invariants
// that distinguish the buggy from the correct implementation.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertFKIntegrity fails the test if any child table contains rows whose parent
// key no longer exists. Catches regressions where TruncateSystems leaves orphaned
// rows in a sibling system's data.
func assertFKIntegrity(t *testing.T, ctx context.Context, sqlDB *sql.DB) { //nolint:revive // t before ctx is standard test helper convention
	t.Helper()

	checks := []struct {
		name  string
		query string
	}{
		{
			"Media.MediaTitleDBID → MediaTitles",
			`SELECT COUNT(*) FROM Media m
			 LEFT JOIN MediaTitles mt ON mt.DBID = m.MediaTitleDBID
			 WHERE mt.DBID IS NULL`,
		},
		{
			"Media.SystemDBID → Systems",
			`SELECT COUNT(*) FROM Media m
			 LEFT JOIN Systems s ON s.DBID = m.SystemDBID
			 WHERE s.DBID IS NULL`,
		},
		{
			"MediaTags.MediaDBID → Media",
			`SELECT COUNT(*) FROM MediaTags mtg
			 LEFT JOIN Media m ON m.DBID = mtg.MediaDBID
			 WHERE m.DBID IS NULL`,
		},
		{
			"MediaTags.TagDBID → Tags",
			`SELECT COUNT(*) FROM MediaTags mtg
			 LEFT JOIN Tags t ON t.DBID = mtg.TagDBID
			 WHERE t.DBID IS NULL`,
		},
		{
			"MediaTitles.SystemDBID → Systems",
			`SELECT COUNT(*) FROM MediaTitles mt
			 LEFT JOIN Systems s ON s.DBID = mt.SystemDBID
			 WHERE s.DBID IS NULL`,
		},
	}

	for _, c := range checks {
		var orphans int
		require.NoError(t, sqlDB.QueryRowContext(ctx, c.query).Scan(&orphans),
			"FK integrity query failed: %s", c.name)
		assert.Equal(t, 0, orphans, "FK violation: %s has %d orphaned rows", c.name, orphans)
	}
}

// indexSystems fully indexes the given systems via AddMediaPath inside a single transaction.
func indexSystems(
	t *testing.T,
	db database.MediaDBI,
	state *database.ScanState,
	systems []string,
	batch testdata.TestBatch,
) {
	t.Helper()
	require.NoError(t, db.BeginTransaction(false))
	for _, sys := range systems {
		for _, entry := range batch.Entries[sys] {
			_, _, err := AddMediaPath(db, state, sys, entry.Path, false, false, nil, "")
			require.NoError(t, err, "AddMediaPath failed for system %s", sys)
		}
	}
	require.NoError(t, db.CommitTransaction())
}

// TestTruncateResume_SharedTagsPreserved guards against the orphan tag cleanup
// over-deleting tags that are shared with still-indexed systems.
//
// Before the fix, the NOT IN (SELECT UNION ...) orphan cleanup ran against ALL rows
// in MediaTags. After truncating one system, it correctly excluded tags still referenced
// by other systems — but this was a full table scan that could take seconds. If the query
// logic ever had a bug, shared tags could be incorrectly deleted, corrupting remaining data.
//
// This test verifies that tags referenced by multiple systems survive truncation of one.
func TestTruncateResume_SharedTagsPreserved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Use enough games per system that genre tags (from the fixed generator list of 14
	// genres) will collide across systems — i.e., both NES and C64 will have an "Action"
	// or "RPG" MediaTag, creating shared Tags rows in the DB.
	batch := testdata.CreateReproducibleBatch([]string{"NES", "C64"}, 15)

	state := &database.ScanState{
		SystemIDs:  make(map[string]int),
		TitleIDs:   make(map[string]int),
		MediaIDs:   make(map[string]int),
		TagTypeIDs: make(map[string]int),
		TagIDs:     make(map[string]int),
	}
	require.NoError(t, SeedCanonicalTags(db, state))

	// Fully index NES, then partially index C64 (5 of 15 files — simulates interrupt).
	indexSystems(t, db, state, []string{"NES"}, batch)

	require.NoError(t, db.BeginTransaction(false))
	for _, entry := range batch.Entries["C64"][:5] {
		_, _, err := AddMediaPath(db, state, "C64", entry.Path, false, false, nil, "")
		require.NoError(t, err)
	}
	require.NoError(t, db.CommitTransaction())

	// Record how many tags the NES system references before truncating C64.
	sqlDB := db.UnsafeGetSQLDb()
	var nesTagCount int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT mtg.TagDBID)
		FROM MediaTags mtg
		INNER JOIN Media m ON m.DBID = mtg.MediaDBID
		INNER JOIN Systems s ON s.DBID = m.SystemDBID
		WHERE s.SystemID = 'NES'
	`).Scan(&nesTagCount))
	require.Positive(t, nesTagCount, "NES must have tags for this test to be meaningful")

	// Truncate the partially-indexed C64 system.
	require.NoError(t, db.TruncateSystems([]string{"C64"}))

	// All NES tags must still exist — the orphan cleanup must not touch them.
	var survivingNESTags int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT mtg.TagDBID)
		FROM MediaTags mtg
		INNER JOIN Media m ON m.DBID = mtg.MediaDBID
		INNER JOIN Systems s ON s.DBID = m.SystemDBID
		WHERE s.SystemID = 'NES'
	`).Scan(&survivingNESTags))
	assert.Equal(t, nesTagCount, survivingNESTags,
		"all tags referenced by NES must survive C64 truncation")

	// NES media count must be unchanged.
	var nesMedia int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM Media m
		INNER JOIN Systems s ON s.DBID = m.SystemDBID
		WHERE s.SystemID = 'NES'
	`).Scan(&nesMedia))
	assert.Equal(t, 15, nesMedia, "NES media must be untouched after C64 truncation")

	// C64 system row must be gone.
	var c64Systems int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM Systems WHERE SystemID = 'C64'
	`).Scan(&c64Systems))
	assert.Equal(t, 0, c64Systems, "C64 system must be gone after truncation")

	// No FK violations anywhere in the remaining data.
	assertFKIntegrity(t, ctx, sqlDB)
}

// TestTruncateResume_MultipleSystemsIndexed is the primary regression test for the
// stall reported in PR #705: the indexer froze during C64 resume because TruncateSystems
// had to CASCADE-delete through ~200K rows already indexed by other systems, and then
// run a NOT IN full-table scan across those same rows for orphan cleanup.
//
// This test replicates the essential structure:
//   - N systems fully indexed (large dataset that exercises the orphan scan cost)
//   - One system partially indexed (simulates the interrupted C64 run)
//   - TruncateSystems on the partial system
//   - Re-index the partial system from scratch
//   - Verify no duplicates, no FK violations, correct final counts
func TestTruncateResume_MultipleSystemsIndexed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	// Multiple fully-indexed systems (simulates the 239K-file MiSTer library that
	// made the orphan table scan expensive in the bug report).
	const gamesPerSystem = 20
	const partialGameCount = 8
	const partialSystem = "C64"
	fullSystems := []string{"NES", "SNES", "Genesis", "GBA", "PSX"}
	allSystems := make([]string, len(fullSystems)+1)
	copy(allSystems, fullSystems)
	allSystems[len(fullSystems)] = partialSystem
	batch := testdata.CreateReproducibleBatch(allSystems, gamesPerSystem)

	state := &database.ScanState{
		SystemIDs:  make(map[string]int),
		TitleIDs:   make(map[string]int),
		MediaIDs:   make(map[string]int),
		TagTypeIDs: make(map[string]int),
		TagIDs:     make(map[string]int),
	}
	require.NoError(t, SeedCanonicalTags(db, state))

	// Step 1: fully index the non-C64 systems.
	indexSystems(t, db, state, fullSystems, batch)

	// Step 2: partially index C64 (simulates the mid-index interrupt).
	require.NoError(t, db.BeginTransaction(false))
	for _, entry := range batch.Entries[partialSystem][:partialGameCount] {
		_, _, err := AddMediaPath(db, state, partialSystem, entry.Path, false, false, nil, "")
		require.NoError(t, err)
	}
	require.NoError(t, db.CommitTransaction())

	sqlDB := db.UnsafeGetSQLDb()

	var beforeTotalMedia int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&beforeTotalMedia))
	expectedPartial := len(fullSystems)*gamesPerSystem + partialGameCount
	assert.Equal(t, expectedPartial, beforeTotalMedia, "pre-truncation count should match indexed files")

	// Step 3: simulate resume — TruncateSystems on the partial C64 data.
	// This is where the stall occurred in the bug report.
	require.NoError(t, db.TruncateSystems([]string{partialSystem}))

	// Step 4: C64 rows must be gone; all other systems intact.
	var afterTruncateMedia int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&afterTruncateMedia))
	assert.Equal(t, len(fullSystems)*gamesPerSystem, afterTruncateMedia,
		"after truncating C64, only fully-indexed system media should remain")

	for _, sys := range fullSystems {
		var count int
		require.NoError(t, sqlDB.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM Media m
			INNER JOIN Systems s ON s.DBID = m.SystemDBID
			WHERE s.SystemID = ?`, sys).Scan(&count))
		assert.Equal(t, gamesPerSystem, count, "system %s must have all games after C64 truncation", sys)
	}

	// Step 5: no FK violations in the remaining data.
	assertFKIntegrity(t, ctx, sqlDB)

	// Step 6: re-index C64 from scratch — mirrors the production resume path.
	// PopulateScanStateFromDB is called before the main loop in mediascanner.go,
	// then the partial system is truncated and re-indexed.
	resumeState := &database.ScanState{
		SystemIDs:  make(map[string]int),
		TitleIDs:   make(map[string]int),
		MediaIDs:   make(map[string]int),
		TagTypeIDs: state.TagTypeIDs,
		TagIDs:     state.TagIDs,
	}
	require.NoError(t, PopulateScanStateFromDB(ctx, db, resumeState))
	delete(resumeState.SystemIDs, partialSystem)

	allTags, err := db.GetAllTags()
	require.NoError(t, err)
	tagTypeByDBID := make(map[int64]string, len(resumeState.TagTypeIDs))
	for tt, id := range resumeState.TagTypeIDs {
		tagTypeByDBID[int64(id)] = tt
	}
	resumeState.TagIDs = make(map[string]int, len(allTags))
	for _, tag := range allTags {
		resumeState.TagIDs[database.TagKey(tagTypeByDBID[tag.TypeDBID], tag.Tag)] = int(tag.DBID)
	}
	require.NoError(t, SeedCanonicalTags(db, resumeState))

	require.NoError(t, db.BeginTransaction(false))
	for _, entry := range batch.Entries[partialSystem] {
		_, _, addErr := AddMediaPath(db, resumeState, partialSystem, entry.Path, false, false, nil, "")
		require.NoError(t, addErr, "re-index must not fail with UNIQUE or FK violation")
	}
	require.NoError(t, db.CommitTransaction())

	// Step 7: final counts — all systems complete, no duplicates, no FK violations.
	var finalTotal int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Media").Scan(&finalTotal))
	assert.Equal(t, len(allSystems)*gamesPerSystem, finalTotal,
		"after full re-index, total media must equal all systems × games per system")

	duplicates, err := db.CheckForDuplicateMediaTitles()
	require.NoError(t, err)
	assert.Empty(t, duplicates, "no duplicate MediaTitles allowed after resume re-index")

	assertFKIntegrity(t, ctx, sqlDB)
}
