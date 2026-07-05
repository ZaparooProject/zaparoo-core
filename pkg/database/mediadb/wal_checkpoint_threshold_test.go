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
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/jonboulle/clockwork"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// walTestDB is a file-based MediaDB seeded with a system/title to hang media rows off,
// with dbPath set so checkpointLargeWAL can stat the WAL.
type walTestDB struct {
	db     *MediaDB
	dbPath string
	system database.System
	title  database.MediaTitle
}

func newWALTestDB(t *testing.T) walTestDB {
	t.Helper()
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	sqlDB, err := sql.Open("sqlite3", dbPath+getSqliteConnParams())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	mediaDB := &MediaDB{
		ctx:    ctx,
		dbPath: dbPath,
		clock:  clockwork.NewRealClock(),
	}
	mediaDB.sql.Store(sqlDB)
	require.NoError(t, mediaDB.Allocate())

	system := database.System{SystemID: "test", Name: "Test System"}
	insertedSystem, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)

	title := database.MediaTitle{
		SystemDBID: insertedSystem.DBID,
		Name:       "Test Game",
		Slug:       "test-game",
	}
	insertedTitle, err := mediaDB.InsertMediaTitle(&title)
	require.NoError(t, err)

	return walTestDB{db: mediaDB, dbPath: dbPath, system: insertedSystem, title: insertedTitle}
}

func walSize(t *testing.T, dbPath string) int64 {
	t.Helper()
	info, err := os.Stat(dbPath + "-wal")
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return info.Size()
}

// commitBelowThresholdKeepsWAL asserts a batch commit does NOT checkpoint when the WAL
// is under mediaWALCheckpointThreshold, so the common run of tiny batches never pays the
// checkpoint cost. The WAL retains its frames (size stays non-zero) after the commit.
func TestCommitBelowThresholdKeepsWAL(t *testing.T) {
	h := newWALTestDB(t)

	// Threshold is the production default (96MB); a small commit stays well under it.
	require.NoError(t, h.db.BeginTransaction(false))
	for i := range 200 {
		media := database.Media{
			MediaTitleDBID: h.title.DBID,
			SystemDBID:     h.system.DBID,
			Path:           fmt.Sprintf("/test/path/game%d.bin", i),
		}
		_, err := h.db.InsertMedia(media)
		require.NoError(t, err)
	}
	require.NoError(t, h.db.CommitTransaction())

	require.Positive(t, walSize(t, h.dbPath),
		"WAL should not be checkpointed for a commit under the threshold")
}

// commitAboveThresholdTruncatesWAL asserts a batch commit checkpoints (TRUNCATE) once the
// WAL has grown past mediaWALCheckpointThreshold, bounding its size during a long index.
func TestCommitAboveThresholdTruncatesWAL(t *testing.T) {
	h := newWALTestDB(t)

	// Lower the threshold so a modest commit crosses it; restore afterwards.
	orig := mediaWALCheckpointThreshold
	mediaWALCheckpointThreshold = 32 * 1024
	t.Cleanup(func() { mediaWALCheckpointThreshold = orig })

	require.NoError(t, h.db.BeginTransaction(false))
	// Insert enough rows that the committed WAL comfortably exceeds 32KB.
	for i := range 4000 {
		media := database.Media{
			MediaTitleDBID: h.title.DBID,
			SystemDBID:     h.system.DBID,
			Path:           fmt.Sprintf("/test/path/game%d.bin", i),
		}
		_, err := h.db.InsertMedia(media)
		require.NoError(t, err)
	}
	require.NoError(t, h.db.CommitTransaction())

	require.Less(t, walSize(t, h.dbPath), mediaWALCheckpointThreshold,
		"WAL should be truncated back below the threshold after a commit that crossed it")

	// Data is still durable and queryable after the checkpoint.
	var count int
	require.NoError(t, h.db.sql.Load().QueryRowContext(
		h.db.ctx, "SELECT COUNT(*) FROM Media").Scan(&count))
	require.Equal(t, 4000, count)
}
