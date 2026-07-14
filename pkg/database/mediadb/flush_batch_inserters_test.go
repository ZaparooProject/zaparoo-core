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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countMediaInTx(t *testing.T, db *MediaDB) int {
	t.Helper()
	var n int
	require.NoError(t, db.tx.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM Media").Scan(&n))
	return n
}

// TestMediaDB_FlushBatchInserters verifies the primitive that lets one indexing
// transaction span multiple systems: flushing buffered batch inserts makes the
// rows visible to reads on the same (still-open) transaction — which per-system
// disambiguation relies on — while leaving the inserters usable for more rows.
func TestMediaDB_FlushBatchInserters(t *testing.T) {
	t.Run("no active transaction is a no-op", func(t *testing.T) {
		mediaDB, cleanup := setupTempMediaDB(t)
		defer cleanup()

		require.NoError(t, mediaDB.FlushBatchInserters())
	})

	t.Run("flush exposes buffered rows in the transaction and keeps inserters open", func(t *testing.T) {
		mediaDB, cleanup := setupTempMediaDB(t)
		defer cleanup()

		require.NoError(t, mediaDB.BeginTransaction(true))
		defer func() { _ = mediaDB.RollbackTransaction() }()

		_, err := mediaDB.InsertSystem(database.System{DBID: 1, SystemID: "NES", Name: "NES"})
		require.NoError(t, err)
		_, err = mediaDB.InsertMediaTitle(&database.MediaTitle{
			DBID: 1, SystemDBID: 1, Slug: "game", Name: "Game", SlugLength: 4, SlugWordCount: 1,
		})
		require.NoError(t, err)
		_, err = mediaDB.InsertMedia(database.Media{
			DBID: 1, MediaTitleDBID: 1, SystemDBID: 1,
			Path: filepath.Join("roms", "nes", "game.nes"), ParentDir: "roms/nes/", SortName: "Game",
		})
		require.NoError(t, err)

		// Inserts are buffered in the batch inserters, not yet in the transaction.
		assert.Equal(t, 0, countMediaInTx(t, mediaDB), "rows should be buffered before flush")

		require.NoError(t, mediaDB.FlushBatchInserters())

		// After flush they are visible to reads on the same transaction.
		assert.Equal(t, 1, countMediaInTx(t, mediaDB), "rows should be visible after flush")

		// The inserters remain usable: more rows can be added and flushed without
		// committing, as happens when the next system is indexed in the same batch.
		_, err = mediaDB.InsertMedia(database.Media{
			DBID: 2, MediaTitleDBID: 1, SystemDBID: 1,
			Path: filepath.Join("roms", "nes", "game2.nes"), ParentDir: "roms/nes/", SortName: "Game 2",
		})
		require.NoError(t, err)
		require.NoError(t, mediaDB.FlushBatchInserters())
		assert.Equal(t, 2, countMediaInTx(t, mediaDB), "second flush should expose the additional row")

		require.NoError(t, mediaDB.CommitTransaction())

		media, err := mediaDB.GetMediaBySystemID("NES")
		require.NoError(t, err)
		assert.Len(t, media, 2, "both media rows should persist after commit")
	})
}
