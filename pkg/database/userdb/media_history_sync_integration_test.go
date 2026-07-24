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

package userdb

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addSyncTestEntry inserts one closed session with the given UUID (may be
// empty to simulate legacy rows) and UpdatedAt.
func addSyncTestEntry(t *testing.T, db *UserDB, id, mediaName string, updatedAt time.Time) int64 {
	t.Helper()
	startTime := updatedAt.Add(-30 * time.Minute)
	endTime := updatedAt
	dbid, err := db.AddMediaHistory(&database.MediaHistoryEntry{
		ID:            id,
		StartTime:     startTime,
		SystemID:      "snes",
		SystemName:    "Super Nintendo",
		MediaPath:     "/games/" + mediaName + ".sfc",
		MediaName:     mediaName,
		LauncherID:    "test",
		PlayTime:      1800,
		BootUUID:      "boot-1",
		ClockReliable: true,
		ClockSource:   "system",
		Tags:          []string{"region:us", "rev:1"},
		CreatedAt:     startTime,
		UpdatedAt:     updatedAt.Add(-time.Second),
	})
	require.NoError(t, err)
	require.NoError(t, db.CloseMediaHistory(dbid, endTime, 1800))
	return dbid
}

func TestBackfillMediaHistoryUUIDs_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	base := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	legacyID := addSyncTestEntry(t, userDB, "", "Legacy Game", base)
	addSyncTestEntry(t, userDB, "11111111-1111-4111-8111-111111111111", "Modern Game", base.Add(time.Hour))

	backfilled, err := userDB.BackfillMediaHistoryUUIDs()
	require.NoError(t, err)
	assert.Equal(t, int64(1), backfilled, "only the legacy row needs a UUID")

	// Idempotent
	backfilled, err = userDB.BackfillMediaHistoryUUIDs()
	require.NoError(t, err)
	assert.Zero(t, backfilled)

	// The legacy row now appears in sync batches with a UUID
	batch, err := userDB.GetMediaHistorySyncBatch(time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, batch, 2)
	for _, entry := range batch {
		assert.NotEmpty(t, entry.ID)
		if entry.DBID == legacyID {
			assert.NotEmpty(t, entry.ID)
		}
	}
}

func TestGetMediaHistorySyncBatch_CursorWalk_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	base := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	addSyncTestEntry(t, userDB, "11111111-1111-4111-8111-111111111111", "Game A", base)
	addSyncTestEntry(t, userDB, "22222222-2222-4222-8222-222222222222", "Game B", base.Add(time.Hour))
	// Same UpdatedAt as Game B: DBID breaks the tie
	addSyncTestEntry(t, userDB, "33333333-3333-4333-8333-333333333333", "Game C", base.Add(time.Hour))
	// A row without a UUID never syncs
	addSyncTestEntry(t, userDB, "", "No UUID", base.Add(2*time.Hour))

	// Full walk from zero, one row at a time, ordered (UpdatedAt, DBID)
	var seen []string
	cursor, cursorDBID := time.Time{}, int64(0)
	for {
		batch, err := userDB.GetMediaHistorySyncBatch(cursor, cursorDBID, 1)
		require.NoError(t, err)
		if len(batch) == 0 {
			break
		}
		seen = append(seen, batch[0].MediaName)
		cursor = batch[0].UpdatedAt
		cursorDBID = batch[0].DBID
	}
	assert.Equal(t, []string{"Game A", "Game B", "Game C"}, seen)

	// Resuming from a mid-tie cursor returns only rows after it
	batch, err := userDB.GetMediaHistorySyncBatch(base.Add(time.Hour), 0, 10)
	require.NoError(t, err)
	require.Len(t, batch, 2, "both rows sharing the cursor timestamp re-send")
	assert.Equal(t, "Game B", batch[0].MediaName)
	assert.Equal(t, "Game C", batch[1].MediaName)
	assert.Equal(t, []string{"region:us", "rev:1"}, batch[0].Tags,
		"snapshotted tags round-trip through the sync batch")
}

func TestSyncedWatermarkRows_RequeueOnlyAfterMutation_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	updatedAt := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	dbid := addSyncTestEntry(
		t, userDB, "11111111-1111-4111-8111-111111111111", "Game A", updatedAt,
	)
	ref := database.MediaHistorySyncRef{DBID: dbid, UpdatedAt: updatedAt}
	require.NoError(t, userDB.MarkMediaHistorySynced(
		[]database.MediaHistorySyncRef{ref}, time.Now().Truncate(time.Second),
	))

	batch, err := userDB.GetMediaHistorySyncBatch(updatedAt, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, batch, "acknowledged rows at the server watermark must not repeat")

	// A same-second update advances UpdatedAt and clears SyncedAt. A stale
	// acknowledgement arriving afterward must not mark the newer version.
	require.NoError(t, userDB.CloseMediaHistory(dbid, updatedAt, 1900))
	require.NoError(t, userDB.MarkMediaHistorySynced(
		[]database.MediaHistorySyncRef{ref}, time.Now().Truncate(time.Second),
	))
	batch, err = userDB.GetMediaHistorySyncBatch(updatedAt, 0, 10)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, dbid, batch[0].DBID)
	assert.Equal(t, 1900, batch[0].PlayTime)
	assert.True(t, batch[0].UpdatedAt.After(updatedAt))
	assert.Nil(t, batch[0].SyncedAt)
}

func TestGetMediaHistorySyncBatch_IncludesUnsyncedBeforeServerWatermark_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	unreliableTime := time.Unix(60, 0)
	dbid := addSyncTestEntry(
		t, userDB, "11111111-1111-4111-8111-111111111111", "Epoch Game", unreliableTime,
	)
	serverWatermark := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, userDB.ResetMediaHistorySyncAfter(&serverWatermark))

	batch, err := userDB.GetMediaHistorySyncBatch(time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, dbid, batch[0].DBID)
	assert.True(t, batch[0].UpdatedAt.Before(serverWatermark))
}

func TestResetMediaHistorySyncAfter_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	base := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	dbidA := addSyncTestEntry(t, userDB, "11111111-1111-4111-8111-111111111111", "Game A", base)
	dbidB := addSyncTestEntry(
		t, userDB, "22222222-2222-4222-8222-222222222222", "Game B", base.Add(time.Hour),
	)
	refs := []database.MediaHistorySyncRef{
		{DBID: dbidA, UpdatedAt: base},
		{DBID: dbidB, UpdatedAt: base.Add(time.Hour)},
	}
	require.NoError(t, userDB.MarkMediaHistorySynced(refs, time.Now().Truncate(time.Second)))

	watermark := base
	require.NoError(t, userDB.ResetMediaHistorySyncAfter(&watermark))
	batch, err := userDB.GetMediaHistorySyncBatch(time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, dbidB, batch[0].DBID, "rows newer than server state must requeue")

	require.NoError(t, userDB.ResetMediaHistorySyncAfter(nil))
	batch, err = userDB.GetMediaHistorySyncBatch(time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, batch, 2, "nil server watermark must requeue every local row")
}

func TestMarkMediaHistorySynced_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	base := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	dbidA := addSyncTestEntry(t, userDB, "11111111-1111-4111-8111-111111111111", "Game A", base)
	addSyncTestEntry(t, userDB, "22222222-2222-4222-8222-222222222222", "Game B", base.Add(time.Hour))

	// No-op on empty input
	require.NoError(t, userDB.MarkMediaHistorySynced(nil, time.Now()))

	syncedAt := time.Now().Truncate(time.Second)
	require.NoError(t, userDB.MarkMediaHistorySynced([]database.MediaHistorySyncRef{
		{DBID: dbidA, UpdatedAt: base},
	}, syncedAt))

	batch, err := userDB.GetMediaHistorySyncBatch(time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, "Game B", batch[0].MediaName)
	assert.NotEqual(t, dbidA, batch[0].DBID, "acknowledged exact version must be excluded")
}
