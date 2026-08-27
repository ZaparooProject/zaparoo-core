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

// The browse-cache migrations stamp OptimizationStatus=pending unconditionally
// so that existing databases rebuild their browse cache on upgrade. A brand-new
// database runs the same chain, so it is born pending and the service then
// "resumes" an optimization over zero media on first start.
//
// The stamp must stay for the upgrade path; these tests pin the narrow rule that
// it is only cleared when there is provably nothing to optimize.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateUp_ClearsOptimizationStampOnEmptyDB_Integration is the fix: a fresh
// database must not come out of migration asking to be optimized.
func TestMigrateUp_ClearsOptimizationStampOnEmptyDB_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	// setupTempMediaDB already migrated; a fresh DB has no media.
	hasMedia, err := sqlMediaExists(context.Background(), mediaDB.sql.Load())
	require.NoError(t, err)
	require.False(t, hasMedia, "fixture should start with an empty database")

	status, err := mediaDB.GetOptimizationStatus()
	require.NoError(t, err)
	assert.Empty(t, status,
		"a fresh database must not be left with a pending optimization stamp; "+
			"the service would resume a browse cache rebuild over zero media")

	step, err := sqlGetOptimizationStep(context.Background(), mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Empty(t, step, "optimization step should be cleared alongside the status")
}

// TestClearOptimizationStampIfEmpty_KeepsStampWhenMediaExists_Integration is the
// other half: the upgrade path depends on the stamp surviving on a populated
// database, so the guard must not touch it there.
func TestClearOptimizationStampIfEmpty_KeepsStampWhenMediaExists_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	insertSystemWithMedia(t, mediaDB, "SNES", "Super RPG",
		filepath.ToSlash(filepath.Join(string(filepath.Separator), "roms", "snes", "super-rpg.sfc")))

	require.NoError(t, mediaDB.SetOptimizationStatus(IndexingStatusPending))
	require.NoError(t, sqlSetOptimizationStep(ctx, mediaDB.sql.Load(), "browse_cache"))

	require.NoError(t, mediaDB.clearOptimizationStampIfEmpty(ctx))

	status, err := mediaDB.GetOptimizationStatus()
	require.NoError(t, err)
	assert.Equal(t, IndexingStatusPending, status,
		"a database with media must keep its pending stamp — this is the upgrade path")

	step, err := sqlGetOptimizationStep(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Equal(t, "browse_cache", step, "the resume step must survive too")
}

// TestAllocate_SeedsPlannerStats_Integration covers the Allocate path getting
// the planner stat seed.
//
// This is the path Recreate uses: it reopens into a fresh database and starts a
// reindex immediately, with no MigrateUp in between. The seed used to be hooked
// only on MigrateUp, so every user-triggered rebuild reindexed against an empty
// sqlite_stat1 — the planner regression #1279 started from.
func TestAllocate_SeedsPlannerStats_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	// setupTempMediaDB goes through OpenMediaDB, whose fresh-database branch
	// calls Allocate — not MigrateUp.
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	var seeded int
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM sqlite_stat1 WHERE tbl = 'Media'").Scan(&seeded))
	assert.Positive(t, seeded,
		"a freshly allocated database must carry seeded planner statistics; "+
			"without them a rebuild reindexes against an empty sqlite_stat1")
}

// TestClearOptimizationStampIfEmpty_NoStampIsNoop_Integration guards against the
// guard itself writing to a database that had nothing stamped.
func TestClearOptimizationStampIfEmpty_NoStampIsNoop_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, sqlClearOptimizationStamp(ctx, mediaDB.sql.Load()))
	require.NoError(t, mediaDB.clearOptimizationStampIfEmpty(ctx))

	status, err := mediaDB.GetOptimizationStatus()
	require.NoError(t, err)
	assert.Empty(t, status)
}
