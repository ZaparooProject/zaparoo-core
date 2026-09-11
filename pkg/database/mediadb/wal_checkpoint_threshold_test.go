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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/jonboulle/clockwork"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

	mediaDB := &MediaDB{
		ctx:    ctx,
		dbPath: dbPath,
		clock:  clockwork.NewRealClock(),
	}
	// Close through MediaDB rather than the *sql.DB: it rolls back an open
	// transaction first, and a transaction still holds its connection out of
	// the pool, so sql.DB.Close() alone reports success while the file stays
	// open. On Windows the t.TempDir removal that runs next then fails, hiding
	// whatever actually failed the test behind a cleanup error. Registered
	// before the handle is stored because Close is a no-op until then, and
	// cleanups run last-registered-first.
	t.Cleanup(func() {
		require.NoError(t, mediaDB.Close())
	})

	sqlDB, err := sql.Open(sqliteDriverName(), dbPath+getSqliteConnParams())
	require.NoError(t, err)
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
			Path:           filepath.Join("test", "path", fmt.Sprintf("game%d.bin", i)),
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
			Path:           filepath.Join("test", "path", fmt.Sprintf("game%d.bin", i)),
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

// TestCheckpointLog_IncludesPoolStats asserts the checkpoint-completed log line
// carries the connection pool's open/inUse/idle counts alongside the existing
// busy/frame fields — added for #1279 to help tell whether a concurrent reader
// (not just indexing's own writer) was checked out at the moment a checkpoint
// couldn't fully reclaim the WAL. Cannot run in parallel: swaps zerolog.Logger.
func TestCheckpointLog_IncludesPoolStats(t *testing.T) {
	h := newWALTestDB(t)

	orig := mediaWALCheckpointThreshold
	mediaWALCheckpointThreshold = 32 * 1024
	t.Cleanup(func() { mediaWALCheckpointThreshold = orig })

	var buf strings.Builder
	prevLogger := log.Logger
	prevLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = prevLogger
		zerolog.SetGlobalLevel(prevLevel)
	})

	require.NoError(t, h.db.BeginTransaction(false))
	for i := range 4000 {
		media := database.Media{
			MediaTitleDBID: h.title.DBID,
			SystemDBID:     h.system.DBID,
			Path:           filepath.Join("test", "path", fmt.Sprintf("game%d.bin", i)),
		}
		_, err := h.db.InsertMedia(media)
		require.NoError(t, err)
	}
	require.NoError(t, h.db.CommitTransaction())

	output := buf.String()
	require.Contains(t, output, "media database WAL checkpoint completed")
	for _, field := range []string{`"poolOpen":`, `"poolInUse":`, `"poolIdle":`} {
		require.Contains(t, output, field)
	}
}

// TestCommitTransaction_LogsBreakdown asserts CommitTransactionWithOptions logs its
// four-segment timing breakdown (flush/sqliteCommit/invalidate/checkpoint) plus the
// WAL size immediately before and after tx.Commit() — the pair of numbers issue #1279
// asks for to tell whether SQLite's automatic checkpointing ran inside the commit.
// Cannot run in parallel with other tests: it swaps the shared zerolog.Logger/level.
func TestCommitTransaction_LogsBreakdown(t *testing.T) {
	h := newWALTestDB(t)

	var buf strings.Builder
	prevLogger := log.Logger
	prevLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = prevLogger
		zerolog.SetGlobalLevel(prevLevel)
	})

	require.NoError(t, h.db.BeginTransaction(false))
	media := database.Media{
		MediaTitleDBID: h.title.DBID,
		SystemDBID:     h.system.DBID,
		Path:           filepath.Join("test", "path", "game.bin"),
	}
	_, err := h.db.InsertMedia(media)
	require.NoError(t, err)
	require.NoError(t, h.db.CommitTransaction())

	output := buf.String()
	require.Contains(t, output, "media database commit breakdown")
	for _, field := range []string{
		`"flush":`, `"sqliteCommit":`, `"invalidate":`, `"checkpoint":`, `"total":`,
		`"walSizeBeforeCommit":`, `"walSizeAfterCommit":`,
		`"poolOpen":`, `"poolInUse":`, `"poolIdle":`,
	} {
		require.Contains(t, output, field)
	}
}

// TestCommitTransaction_PreservesSlugCacheDuringIndexing asserts a batch commit
// taken mid-indexing still takes the indexing branch of cache invalidation and
// leaves the slug search cache intact, so foreground launches and searches keep
// working off last-good coverage while an index is in progress.
func TestCommitTransaction_PreservesSlugCacheDuringIndexing(t *testing.T) {
	t.Parallel()

	h := newWALTestDB(t)
	require.NoError(t, h.db.SetIndexingStatus(IndexingStatusRunning))

	sentinelCache := &SlugSearchCache{}
	h.db.slugSearchCache.Store(sentinelCache)

	require.NoError(t, h.db.BeginTransaction(false))
	media := database.Media{
		MediaTitleDBID: h.title.DBID,
		SystemDBID:     h.system.DBID,
		Path:           filepath.Join("test", "path", "game.bin"),
	}
	_, err := h.db.InsertMedia(media)
	require.NoError(t, err)
	require.NoError(t, h.db.CommitTransaction())

	require.Same(t, sentinelCache, h.db.slugSearchCache.Load(),
		"PreserveSlugSearchCache must still be honored during an indexing batch commit")
}
