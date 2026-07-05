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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countMediaStat1Rows(t *testing.T, mediaDB *MediaDB) int {
	t.Helper()
	var count int
	err := mediaDB.sql.Load().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM sqlite_stat1 WHERE tbl = 'Media'").Scan(&count)
	require.NoError(t, err)
	return count
}

// TestMigrateUp_SeedsPlannerStatsOnFreshDatabase verifies a fresh database
// gets the captured sqlite_stat1 seed after migrations, so mid-index queries
// have planner statistics before the first real ANALYZE runs.
func TestMigrateUp_SeedsPlannerStatsOnFreshDatabase(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, mediaDB.MigrateUp())
	seeded := countMediaStat1Rows(t, mediaDB)
	assert.Positive(t, seeded, "fresh database must receive seeded Media statistics")

	// Idempotent: a second migration pass must not duplicate rows.
	require.NoError(t, mediaDB.MigrateUp())
	assert.Equal(t, seeded, countMediaStat1Rows(t, mediaDB))
}

// TestSeedPlannerStats_NeverOverwritesRealStats verifies real ANALYZE output
// is left untouched by the seeding pass.
func TestSeedPlannerStats_NeverOverwritesRealStats(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, mediaDB.MigrateUp())

	// Simulate real statistics: replace the seed with a marker row.
	sqlDB := mediaDB.sql.Load()
	_, err := sqlDB.ExecContext(ctx, "DELETE FROM sqlite_stat1")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx,
		"INSERT INTO sqlite_stat1 (tbl, idx, stat) VALUES ('Media', 'media_path_idx', '12345 1')")
	require.NoError(t, err)

	require.NoError(t, sqlSeedPlannerStats(ctx, sqlDB))

	var stat string
	err = sqlDB.QueryRowContext(ctx,
		"SELECT stat FROM sqlite_stat1 WHERE tbl = 'Media' AND idx = 'media_path_idx'").Scan(&stat)
	require.NoError(t, err)
	assert.Equal(t, "12345 1", stat, "existing statistics must never be overwritten by the seed")
	assert.Equal(t, 1, countMediaStat1Rows(t, mediaDB))
}
