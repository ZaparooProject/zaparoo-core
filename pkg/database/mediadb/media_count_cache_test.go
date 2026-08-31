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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetCachedStats_PrunesOldestEntries(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()
	db := mediaDB.sql.Load()

	for i := range mediaCountCacheMaxEntries {
		_, err := db.ExecContext(ctx, `
			INSERT INTO MediaCountCache
				(QueryHash, QueryParams, Count, MinDBID, MaxDBID, LastUpdated)
			VALUES (?, '{}', 1, 1, 1, ?)`, fmt.Sprintf("seed-%04d", i), i)
		require.NoError(t, err)
	}

	query := &database.MediaQuery{PathPrefix: filepath.Join("roms", "SNES")}
	stats := MediaStats{Count: 4, MinDBID: 10, MaxDBID: 40}
	require.NoError(t, mediaDB.SetCachedStats(ctx, query, stats))

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaCountCache").Scan(&count))
	assert.Equal(t, mediaCountCacheMaxEntries, count)

	var oldestCount int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MediaCountCache WHERE QueryHash = ?", "seed-0000",
	).Scan(&oldestCount))
	assert.Zero(t, oldestCount)

	cached, found := mediaDB.GetCachedStats(ctx, query)
	require.True(t, found)
	assert.Equal(t, stats, cached)
}
