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
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addPlaySession records one closed session and returns its row.
func addPlaySession(
	t *testing.T, db *UserDB, profileID *string, path string, start time.Time, playSecs int,
) int64 {
	t.Helper()
	end := start.Add(time.Duration(playSecs) * time.Second)
	dbid, err := db.AddMediaHistory(&database.MediaHistoryEntry{
		StartTime:  start,
		EndTime:    &end,
		SystemID:   "NES",
		SystemName: "Nintendo",
		MediaPath:  path,
		MediaName:  filepath.Base(path),
		LauncherID: "test",
		PlayTime:   playSecs,
		CreatedAt:  start,
		UpdatedAt:  start,
		ProfileID:  profileID,
	})
	require.NoError(t, err)
	require.NoError(t, db.CloseMediaHistory(dbid, end, playSecs))
	return dbid
}

func TestGetMediaPlayTotals(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	profileID := "profile-a"
	contra := filepath.Join("nes", "contra.nes")
	zelda := filepath.Join("nes", "zelda.nes")
	shared := filepath.Join("nes", "shared.nes")
	old := filepath.Join("nes", "old.nes")

	addPlaySession(t, db, &profileID, contra, now.Add(-48*time.Hour), 600)
	addPlaySession(t, db, &profileID, contra, now.Add(-24*time.Hour), 900)
	// A launch closed again within the minute is not play.
	addPlaySession(t, db, &profileID, contra, now.Add(-2*time.Hour), 59)
	addPlaySession(t, db, &profileID, zelda, now.Add(-1*time.Hour), 60)
	addPlaySession(t, db, nil, shared, now.Add(-3*time.Hour), 300)
	addPlaySession(t, db, &profileID, old, now.Add(-400*24*time.Hour), 3600)
	withdrawn := addPlaySession(t, db, &profileID, zelda, now.Add(-30*time.Minute), 5000)
	_, err := db.sql.Load().ExecContext(ctx, `UPDATE MediaHistory SET IsDeleted = 1 WHERE DBID = ?`, withdrawn)
	require.NoError(t, err)

	since := now.Add(-365 * 24 * time.Hour)
	totals, err := db.GetMediaPlayTotals(ctx, profileID, since, 60, 10)
	require.NoError(t, err)
	require.Len(t, totals, 2, "short, withdrawn, other-profile and out-of-window sessions are left out")
	assert.Equal(t, zelda, totals[0].MediaPath, "most recently played first")
	assert.Equal(t, 60, totals[0].PlayTime)
	assert.Equal(t, 1, totals[0].SessionCount)
	assert.Equal(t, contra, totals[1].MediaPath)
	assert.Equal(t, 1500, totals[1].PlayTime)
	assert.Equal(t, 2, totals[1].SessionCount)
	assert.Equal(t, now.Add(-24*time.Hour).Unix(), totals[1].LastPlayedAt.Unix())
	assert.Equal(t, "NES", totals[1].SystemID)

	sharedTotals, err := db.GetMediaPlayTotals(ctx, "", since, 60, 10)
	require.NoError(t, err)
	require.Len(t, sharedTotals, 1, "no profile means play made with none active, and only that")
	assert.Equal(t, shared, sharedTotals[0].MediaPath)

	limited, err := db.GetMediaPlayTotals(ctx, profileID, since, 60, 1)
	require.NoError(t, err)
	assert.Len(t, limited, 1)

	other, err := db.GetMediaPlayTotals(ctx, "profile-b", since, 60, 10)
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestMediaHistoryVersion(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	defer cleanup()
	ctx := context.Background()
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	empty, err := db.MediaHistoryVersion(ctx)
	require.NoError(t, err)

	dbid, err := db.AddMediaHistory(&database.MediaHistoryEntry{
		StartTime: start, SystemID: "NES", SystemName: "Nintendo",
		MediaPath: filepath.Join("nes", "contra.nes"), MediaName: "Contra", LauncherID: "test",
		CreatedAt: start, UpdatedAt: start,
	})
	require.NoError(t, err)
	started, err := db.MediaHistoryVersion(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, empty, started, "a session starting is a change")

	require.NoError(t, db.UpdateMediaHistoryTime(dbid, 60))
	ticking, err := db.MediaHistoryVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, started, ticking, "a running session's play time ticking is not a change")

	require.NoError(t, db.CloseMediaHistory(dbid, start.Add(2*time.Minute), 120))
	ended, err := db.MediaHistoryVersion(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, started, ended, "a session ending is a change")
}
