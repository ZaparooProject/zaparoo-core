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
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests reproduce, deterministically and without hardware, the failure
// boundaries the MiSTer corruption reports (#1279) cluster around: power loss
// after a commit but before its checkpoint, mid-transaction with dirty pages
// already spilled to the WAL, a torn write at the WAL tail, and a checkpoint
// interrupted part-way through copying frames into the main file. Each one
// drives the production indexing write path — staging, the tag-link reconcile,
// commit — against a real file-backed database, then copies the on-disk files
// to a fresh directory to stand in for what storage would have preserved, and
// damages the copy where the failure would have. Reopening the copy is the
// recovery SQLite performs at the next boot; what these pin down is that the
// data it recovers is exactly the committed set and the file passes an
// integrity check. The one storage failure they cannot model is a drive that
// acknowledges an fsync it did not perform, which is the case the corruption
// marker and rebuild exist for (see TestMediaDB_ZeroedPages_DetectedAndRecovered).

const crashTestFilesPerSystem = 150

// stageSystemFiles indexes n synthetic files for systemID through the
// production staging path — staging rows with canonical and dynamic tags, the
// set-based reconcile, and the batch commit — and returns the reconcile stats.
func stageSystemFiles(t *testing.T, h *walTestDB, systemID, prefix string, n int) database.ScanReconcileStats {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, h.db.BeginTransaction(true))
	require.NoError(t, h.db.ClearScanStage())
	for i := range n {
		path := fmt.Sprintf("/roms/%s/%s %03d (USA).bin", systemID, prefix, i)
		require.NoError(t, h.db.StageScannedMedia(&database.ScanStagedMedia{
			Path:          path,
			ParentDir:     ParentDirForMediaPath(path),
			Slug:          fmt.Sprintf("%s%03d", strings.ToLower(prefix), i),
			TitleName:     fmt.Sprintf("%s %03d", prefix, i),
			SortName:      fmt.Sprintf("%s %03d", prefix, i),
			SlugLength:    len(prefix) + 3,
			SlugWordCount: 2,
			Tags: []database.ScanStagedTag{
				{Type: string(tags.TagTypeRegion), Value: "us"},
				{Type: string(tags.TagTypeExtension), Value: "bin"},
			},
		}))
	}
	stats, err := h.db.ReconcileStagedSystem(ctx, systemID, database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, h.db.CommitTransaction())
	return stats
}

// newCrashTestDB is a file-backed MediaDB with the canonical tag vocabulary
// seeded and the WAL truncated to empty, so the frames a test then writes are
// the only ones in it and can be attributed to individual commits by offset.
func newCrashTestDB(t *testing.T) *walTestDB {
	t.Helper()
	h := newWALTestDB(t)
	h.db.batchSize = 5000 // production default; newWALTestDB only exercises the prepared-statement path
	ctx := context.Background()
	require.NoError(t, h.db.BeginTransaction(false))
	require.NoError(t, h.db.SeedCanonicalTagDefinitions(ctx))
	require.NoError(t, h.db.CommitTransaction())
	require.NoError(t, h.db.WALCheckpoint())
	require.Zero(t, walSize(t, h.dbPath), "the WAL must start empty so every frame is attributable")
	return &h
}

func copyFileIfExists(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src) //nolint:gosec // test-controlled temp paths
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o600)) //nolint:gosec // test-controlled temp path
}

// snapshotDatabase copies the database and whichever sidecars exist to a fresh
// directory: the on-disk state a power loss at that instant would have left
// for the next boot to recover from.
func snapshotDatabase(t *testing.T, dbPath string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "crash.db")
	copyFileIfExists(t, dbPath, target)
	copyFileIfExists(t, dbPath+"-wal", target+"-wal")
	copyFileIfExists(t, dbPath+"-shm", target+"-shm")
	return target
}

// openSnapshot reopens a snapshot exactly as production would, with the same
// connection parameters, which is when SQLite runs WAL recovery.
func openSnapshot(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", dbPath+getSqliteConnParams())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return sqlDB
}

func countRowsWhere(t *testing.T, sqlDB *sql.DB, table, where string, args ...any) int {
	t.Helper()
	query := "SELECT COUNT(*) FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	var n int
	require.NoError(t, sqlDB.QueryRowContext(context.Background(), query, args...).Scan(&n))
	return n
}

func countSystemMedia(t *testing.T, sqlDB *sql.DB, systemID string) int {
	t.Helper()
	return countRowsWhere(t, sqlDB, "Media",
		"SystemDBID = (SELECT DBID FROM Systems WHERE SystemID = ?)", systemID)
}

func requireIntegrityOK(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	require.Equal(t, []string{"ok"},
		database.IntegrityReport(context.Background(), sqlDB, database.DefaultIntegrityReportRows))
}

// walFrame is one frame of the WAL file format: a 24-byte header (page number,
// then the database size in pages for a commit frame and zero otherwise) and
// one page of content.
type walFrame struct {
	offset     int
	pageNo     uint32
	commitSize uint32
}

const walHeaderSize = 32

func parseWAL(t *testing.T, wal []byte) (pageSize int, frames []walFrame) {
	t.Helper()
	require.GreaterOrEqual(t, len(wal), walHeaderSize, "WAL has no header")
	magic := binary.BigEndian.Uint32(wal[0:4])
	require.Contains(t, []uint32{0x377f0682, 0x377f0683}, magic, "not a WAL file")
	pageSize = int(binary.BigEndian.Uint32(wal[8:12]))
	frameSize := 24 + pageSize
	for off := walHeaderSize; off+frameSize <= len(wal); off += frameSize {
		frames = append(frames, walFrame{
			offset:     off,
			pageNo:     binary.BigEndian.Uint32(wal[off : off+4]),
			commitSize: binary.BigEndian.Uint32(wal[off+4 : off+8]),
		})
	}
	return pageSize, frames
}

// framesFrom returns the frames written at or after walOffset — the ones a
// commit that began when the WAL was walOffset bytes long produced.
func framesFrom(frames []walFrame, walOffset int64) []walFrame {
	for i, frame := range frames {
		if int64(frame.offset) >= walOffset {
			return frames[i:]
		}
	}
	return nil
}

func firstCommitFrame(t *testing.T, frames []walFrame) walFrame {
	t.Helper()
	for _, frame := range frames {
		if frame.commitSize != 0 {
			return frame
		}
	}
	require.FailNow(t, "no commit frame found")
	return walFrame{}
}

func truncateFile(t *testing.T, path string, size int64) {
	t.Helper()
	require.NoError(t, os.Truncate(path, size))
}

func flipByte(t *testing.T, path string, offset int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0o600) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	b := make([]byte, 1)
	_, err = f.ReadAt(b, offset)
	require.NoError(t, err)
	b[0] ^= 0xff
	_, err = f.WriteAt(b, offset)
	require.NoError(t, err)
}

// applyWALFrames copies the content of frames into the main database file at
// their page positions, which is what a checkpoint does frame by frame; a
// checkpoint interrupted after n frames has done exactly this for the first n.
func applyWALFrames(t *testing.T, dbPath string, wal []byte, pageSize int, frames []walFrame) {
	t.Helper()
	f, err := os.OpenFile(dbPath, os.O_RDWR, 0o600) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	for _, frame := range frames {
		page := wal[frame.offset+24 : frame.offset+24+pageSize]
		_, err = f.WriteAt(page, int64(frame.pageNo-1)*int64(pageSize))
		require.NoError(t, err)
	}
}

// A commit is durable in the WAL alone. Power loss after the commit but before
// any checkpoint — the normal state between indexing batches now that
// automatic checkpointing is off during a run — must recover every committed
// row, including the tag links and the fingerprint written in the same
// transaction.
func TestCrashConsistency_CommittedTransactionSurvivesWithoutCheckpoint(t *testing.T) {
	h := newCrashTestDB(t)
	stats := stageSystemFiles(t, h, "SNES", "Alpha", crashTestFilesPerSystem)
	require.Equal(t, int64(crashTestFilesPerSystem), stats.MediaUpserted)
	require.Equal(t, int64(2*crashTestFilesPerSystem), stats.TagLinksAdded)
	require.Positive(t, walSize(t, h.dbPath), "the commit must still be in the WAL, not checkpointed")

	snapshot := openSnapshot(t, snapshotDatabase(t, h.dbPath))
	requireIntegrityOK(t, snapshot)
	assert.Equal(t, crashTestFilesPerSystem, countSystemMedia(t, snapshot, "SNES"))
	assert.Equal(t, 2*crashTestFilesPerSystem, countRowsWhere(t, snapshot, "MediaTags", ""))
	assert.Equal(t, 1, countRowsWhere(t, snapshot, "ScanSystemFingerprints", ""),
		"the fingerprint commits with the reconcile it describes")
}

// A large transaction spills dirty pages into the WAL before it commits. Those
// frames carry no commit marker, so a crash mid-transaction — or the rollback
// the scanner performs on cancellation — must leave none of them visible, and
// the database must be usable for the next transaction.
func TestCrashConsistency_SpilledUncommittedFramesAreInvisible(t *testing.T) {
	h := newCrashTestDB(t)
	ctx := context.Background()

	require.NoError(t, h.db.BeginTransaction(false))
	// Shrink the writer's page cache so the inserts below spill to the WAL
	// long before commit, as a system-sized batch does at the default size.
	_, err := h.db.tx.ExecContext(ctx, "PRAGMA cache_size = 2")
	require.NoError(t, err)
	_, err = h.db.tx.ExecContext(ctx, "PRAGMA cache_spill = 2")
	require.NoError(t, err)
	for i := range 2000 {
		_, err = h.db.InsertMedia(database.Media{
			MediaTitleDBID: h.title.DBID,
			SystemDBID:     h.system.DBID,
			Path:           fmt.Sprintf("/roms/test/spill %04d.bin", i),
		})
		require.NoError(t, err)
	}
	require.Positive(t, walSize(t, h.dbPath), "dirty pages must have spilled into the WAL before commit")

	midTransaction := snapshotDatabase(t, h.dbPath)
	require.NoError(t, h.db.RollbackTransaction())

	snapshot := openSnapshot(t, midTransaction)
	requireIntegrityOK(t, snapshot)
	assert.Zero(t, countRowsWhere(t, snapshot, "Media", ""), "spilled frames without a commit must not be recovered")

	// The original, after its rollback, is equally clean and still writable.
	requireIntegrityOK(t, h.db.sql.Load())
	assert.Zero(t, countRowsWhere(t, h.db.sql.Load(), "Media", ""))
	stats := stageSystemFiles(t, h, "SNES", "Alpha", 10)
	assert.Equal(t, int64(10), stats.MediaUpserted)
	assert.Equal(t, 10, countSystemMedia(t, h.db.sql.Load(), "SNES"))
}

// A torn write at the WAL tail — the last frames of the newest commit missing
// or damaged — must drop that whole commit and nothing before it. This is the
// power-loss case synchronous=NORMAL accepts (the WAL is not fsynced per
// commit): the previous batch survives intact and the interrupted one is
// redone on resume, never half-applied.
func TestCrashConsistency_TornWALTailDropsOnlyTheLastCommit(t *testing.T) {
	h := newCrashTestDB(t)
	stageSystemFiles(t, h, "SNES", "Alpha", crashTestFilesPerSystem)
	walAfterFirst := walSize(t, h.dbPath)
	stageSystemFiles(t, h, "NES", "Beta", crashTestFilesPerSystem)

	wal, err := os.ReadFile(h.dbPath + "-wal") //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	pageSize, frames := parseWAL(t, wal)
	second := framesFrom(frames, walAfterFirst)
	require.NotEmpty(t, second, "the second commit must have written frames")
	secondCommit := firstCommitFrame(t, second)
	frameSize := int64(24 + pageSize)

	tests := []struct {
		damage func(t *testing.T, walPath string)
		name   string
	}{
		{
			name: "commit frame torn mid-page",
			damage: func(t *testing.T, walPath string) {
				truncateFile(t, walPath, int64(secondCommit.offset)+frameSize-10)
			},
		},
		{
			name: "commit frame never written",
			damage: func(t *testing.T, walPath string) {
				truncateFile(t, walPath, int64(secondCommit.offset))
			},
		},
		{
			name: "first frame of the commit corrupted",
			damage: func(t *testing.T, walPath string) {
				flipByte(t, walPath, int64(second[0].offset)+24+int64(pageSize)/2)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := snapshotDatabase(t, h.dbPath)
			tt.damage(t, path+"-wal")

			snapshot := openSnapshot(t, path)
			requireIntegrityOK(t, snapshot)
			assert.Equal(t, crashTestFilesPerSystem, countSystemMedia(t, snapshot, "SNES"),
				"the earlier commit is intact")
			assert.Zero(t, countSystemMedia(t, snapshot, "NES"), "the torn commit is dropped whole")
			assert.Zero(t, countRowsWhere(t, snapshot, "Systems", "SystemID = ?", "NES"),
				"nothing of the torn transaction is applied, not even its first statements")
			assert.Equal(t, 2*crashTestFilesPerSystem, countRowsWhere(t, snapshot, "MediaTags", ""),
				"tag links of the intact commit are all present and none of the torn one's")
		})
	}
}

// A checkpoint copies WAL frames into the main file one page at a time and
// only resets the WAL once every frame is durable there. Power loss part-way
// through leaves the main file with some pages new and some old, and the WAL
// still complete — the next open must replay the WAL over the partial copy and
// reach the fully committed state, whether the interrupted checkpoint had
// applied one frame or all of them.
func TestCrashConsistency_InterruptedCheckpointReplaysWAL(t *testing.T) {
	h := newCrashTestDB(t)
	stageSystemFiles(t, h, "SNES", "Alpha", crashTestFilesPerSystem)
	stageSystemFiles(t, h, "NES", "Beta", crashTestFilesPerSystem)

	wal, err := os.ReadFile(h.dbPath + "-wal") //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	pageSize, frames := parseWAL(t, wal)
	require.Greater(t, len(frames), 2)

	tests := []struct {
		applied func() []walFrame
		name    string
	}{
		{name: "one frame applied", applied: func() []walFrame { return frames[:1] }},
		{name: "half the frames applied", applied: func() []walFrame { return frames[:len(frames)/2] }},
		{name: "every frame applied but the WAL not reset", applied: func() []walFrame { return frames }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := snapshotDatabase(t, h.dbPath)
			applyWALFrames(t, path, wal, pageSize, tt.applied())

			snapshot := openSnapshot(t, path)
			requireIntegrityOK(t, snapshot)
			assert.Equal(t, crashTestFilesPerSystem, countSystemMedia(t, snapshot, "SNES"))
			assert.Equal(t, crashTestFilesPerSystem, countSystemMedia(t, snapshot, "NES"))
			assert.Equal(t, 4*crashTestFilesPerSystem, countRowsWhere(t, snapshot, "MediaTags", ""))
			assert.Equal(t, 2, countRowsWhere(t, snapshot, "ScanSystemFingerprints", ""))
		})
	}
}

// After a completed TRUNCATE checkpoint the main file is self-contained: a
// crash that loses the (empty) WAL and the SHM afterwards loses nothing.
func TestCrashConsistency_CheckpointedDatabaseStandsAlone(t *testing.T) {
	h := newCrashTestDB(t)
	stageSystemFiles(t, h, "SNES", "Alpha", crashTestFilesPerSystem)
	require.NoError(t, h.db.WALCheckpoint())
	require.Zero(t, walSize(t, h.dbPath), "TRUNCATE checkpoint must empty the WAL")

	target := filepath.Join(t.TempDir(), "alone.db")
	copyFileIfExists(t, h.dbPath, target)
	require.NoFileExists(t, target+"-wal")
	require.NoFileExists(t, target+"-shm")

	snapshot := openSnapshot(t, target)
	requireIntegrityOK(t, snapshot)
	assert.Equal(t, crashTestFilesPerSystem, countSystemMedia(t, snapshot, "SNES"))
	assert.Equal(t, 2*crashTestFilesPerSystem, countRowsWhere(t, snapshot, "MediaTags", ""))
}

// Cancelling indexing mid-reconcile takes the scanner's rollback path. The
// reconcile must stop at its next step, the rollback must leave the previous
// commit's rows and fingerprint exactly as they were, and the same system must
// then reconcile cleanly from scratch.
func TestCrashConsistency_CancelledReconcileRollsBackCleanly(t *testing.T) {
	h := newCrashTestDB(t)
	stageSystemFiles(t, h, "SNES", "Alpha", crashTestFilesPerSystem)
	fingerprintBefore := countRowsWhere(t, h.db.sql.Load(), "ScanSystemFingerprints", "")
	require.Equal(t, 1, fingerprintBefore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, h.db.BeginTransaction(true))
	require.NoError(t, h.db.ClearScanStage())
	for i := range 2 * crashTestFilesPerSystem {
		path := fmt.Sprintf("/roms/SNES/Alpha %03d (USA).bin", i)
		require.NoError(t, h.db.StageScannedMedia(&database.ScanStagedMedia{
			Path: path, ParentDir: ParentDirForMediaPath(path),
			Slug: fmt.Sprintf("alpha%03d", i), TitleName: fmt.Sprintf("Alpha %03d", i),
			SortName: fmt.Sprintf("Alpha %03d", i), SlugLength: 8, SlugWordCount: 2,
			Tags: []database.ScanStagedTag{{Type: string(tags.TagTypeRegion), Value: "us"}},
		}))
	}
	yields := 0
	_, err := h.db.ReconcileStagedSystem(ctx, "SNES", database.ScanReconcileOpts{
		Yield: func() error {
			yields++
			if yields == 3 {
				cancel()
			}
			return nil
		},
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, h.db.RollbackTransaction())

	live := h.db.sql.Load()
	requireIntegrityOK(t, live)
	assert.Equal(t, crashTestFilesPerSystem, countSystemMedia(t, live, "SNES"),
		"the cancelled reconcile's rows are rolled back")
	assert.Equal(t, fingerprintBefore, countRowsWhere(t, live, "ScanSystemFingerprints", ""))
	assert.Zero(t, countRowsWhere(t, live, "ScanStage", ""), "staged rows roll back with the transaction")

	stats := stageSystemFiles(t, h, "SNES", "Alpha", 2*crashTestFilesPerSystem)
	assert.False(t, stats.Unchanged, "a changed set after a cancelled run must reconcile")
	assert.Equal(t, int64(crashTestFilesPerSystem), stats.MediaUpserted, "only the new half is inserted")
	assert.Equal(t, 2*crashTestFilesPerSystem, countSystemMedia(t, live, "SNES"))
	requireIntegrityOK(t, live)
}
