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

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCorruptionError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		name string
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unrelated", err: errors.New("syntax error"), want: false},
		{name: "sqlite corrupt code", err: sqlite3.Error{Code: sqlite3.ErrCorrupt}, want: true},
		{name: "sqlite not-a-db code", err: sqlite3.Error{Code: sqlite3.ErrNotADB}, want: true},
		{
			name: "wrapped malformed string",
			err:  fmt.Errorf("query failed: %w", errors.New("database disk image is malformed")),
			want: true,
		},
		{
			name: "wrapped not-a-database string",
			err:  fmt.Errorf("open failed: %w", errors.New("file is not a database")),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsCorruptionError(tt.err))
		})
	}
}

func TestIsStorageFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		name string
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unrelated", err: errors.New("constraint failed"), want: false},
		{name: "busy is contention, not storage", err: sqlite3.Error{Code: sqlite3.ErrBusy}, want: false},
		{name: "corrupt", err: sqlite3.Error{Code: sqlite3.ErrCorrupt}, want: true},
		{name: "not a database", err: sqlite3.Error{Code: sqlite3.ErrNotADB}, want: true},
		{name: "disk I/O error", err: fmt.Errorf("query: %w", sqlite3.Error{Code: sqlite3.ErrIoErr}), want: true},
		{name: "disk full", err: sqlite3.Error{Code: sqlite3.ErrFull}, want: true},
		{name: "wrapped I/O string", err: errors.New("exec: disk I/O error"), want: true},
		{name: "wrapped full string", err: errors.New("exec: database or disk is full"), want: true},
		{name: "os EIO", err: &os.PathError{Op: "write", Path: "x", Err: syscall.EIO}, want: true},
		{name: "os ENOSPC", err: &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}, want: true},
		{name: "os EROFS", err: &os.PathError{Op: "open", Path: "x", Err: syscall.EROFS}, want: true},
		{name: "os ENOENT", err: &os.PathError{Op: "open", Path: "x", Err: syscall.ENOENT}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsStorageFailure(tt.err))
			wantLevel := zerolog.ErrorLevel
			if tt.want {
				wantLevel = zerolog.WarnLevel
			}
			assert.Equal(t, wantLevel, StorageFailureLevel(tt.err))
		})
	}
}

func TestStartupFailureLevel(t *testing.T) {
	t.Parallel()
	ahead := fmt.Errorf("opening user database: %w",
		fmt.Errorf("%w: database is at version 9", ErrSchemaAhead))
	assert.Equal(t, zerolog.WarnLevel, StartupFailureLevel(ahead))
	assert.Equal(t, zerolog.ErrorLevel, StartupFailureLevel(errors.New("open failed")))
}

func TestLockContention(t *testing.T) {
	t.Parallel()
	for _, code := range []sqlite3.ErrNo{sqlite3.ErrBusy, sqlite3.ErrLocked} {
		got, ok := LockContention(fmt.Errorf("insert: %w", sqlite3.Error{Code: code}))
		assert.True(t, ok)
		assert.Equal(t, code, got.Code)
	}
	_, ok := LockContention(sqlite3.Error{Code: sqlite3.ErrIoErr})
	assert.False(t, ok)
	_, ok = LockContention(errors.New("database is locked"))
	assert.False(t, ok, "only a typed SQLite error carries the codes to report")
	_, ok = LockContention(nil)
	assert.False(t, ok)
}

// Every read of a corrupt database retries the marker, so storage that cannot
// take it would otherwise report the same failure over and over. Not parallel:
// it swaps the global logger.
func TestMarkCorruptReportsWriteFailureOnce(t *testing.T) {
	markerWriteFailureReported.Store(false)
	t.Cleanup(func() { markerWriteFailureReported.Store(false) })
	var buf strings.Builder
	prevLogger := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prevLogger })

	dbPath := filepath.Join(t.TempDir(), "missing-dir", "test.db")
	MarkCorrupt(dbPath, "first", time.Now())
	MarkCorrupt(dbPath, "second", time.Now())

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], `"level":"error"`)
	assert.Contains(t, lines[1], `"level":"debug"`)
	assert.False(t, IsMarkedCorrupt(dbPath))
}

func TestCorruptMarkerLifecycle(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	assert.False(t, IsMarkedCorrupt(dbPath))
	assert.Equal(t, dbPath+CorruptMarkerSuffix, CorruptMarkerPath(dbPath))

	MarkCorrupt(dbPath, "torn write", time.Now())
	assert.True(t, IsMarkedCorrupt(dbPath))

	contents, err := os.ReadFile(CorruptMarkerPath(dbPath))
	require.NoError(t, err)
	assert.Contains(t, string(contents), "torn write")

	require.NoError(t, ClearCorruptMarker(dbPath))
	assert.False(t, IsMarkedCorrupt(dbPath))
	// Clearing an absent marker is a no-op.
	require.NoError(t, ClearCorruptMarker(dbPath))
}

// Both databases rename files aside to this path, so the mapping has to stay
// distinct from the marker itself — otherwise keeping a copy would clear the flag
// saying why it was kept.
func TestCorruptBackupPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		dbPath string
		want   string
	}{
		{name: "database file", dbPath: "media.db", want: "media.db" + CorruptMarkerSuffix + ".bak"},
		{name: "sidecar file", dbPath: "media.db-wal", want: "media.db-wal" + CorruptMarkerSuffix + ".bak"},
		{
			name:   "nested path",
			dbPath: filepath.Join("data", "user.db"),
			want:   filepath.Join("data", "user.db") + CorruptMarkerSuffix + ".bak",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, CorruptBackupPath(tt.dbPath))
			assert.NotEqual(t, CorruptMarkerPath(tt.dbPath), CorruptBackupPath(tt.dbPath),
				"the kept copy must not collide with the marker")
		})
	}
}

func TestNoteCorruption(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	assert.False(t, NoteCorruption(dbPath, errors.New("not corruption"), time.Now()))
	assert.False(t, IsMarkedCorrupt(dbPath))

	assert.True(t, NoteCorruption(dbPath, sqlite3.Error{Code: sqlite3.ErrCorrupt}, time.Now()))
	assert.True(t, IsMarkedCorrupt(dbPath))
}

func TestRemoveSidecars(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		require.NoError(t, os.WriteFile(sidecar, []byte("x"), 0o600))
	}

	require.NoError(t, RemoveSidecars(dbPath))

	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		_, err := os.Stat(sidecar)
		assert.True(t, os.IsNotExist(err), "sidecar should be removed: %s", sidecar)
	}
	// Removing absent sidecars is a no-op.
	require.NoError(t, RemoveSidecars(dbPath))
}

func TestRemoveSidecarsReportsFailures(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	dbPath := filepath.Join("data", "test.db")
	require.NoError(t, fs.MkdirAll(filepath.Dir(dbPath), 0o750))
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		require.NoError(t, afero.WriteFile(fs, sidecar, []byte("x"), 0o600))
	}

	err := RemoveSidecarsFS(afero.NewReadOnlyFs(fs), dbPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "test.db-wal")
	assert.Contains(t, err.Error(), "test.db-shm")
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		exists, existsErr := afero.Exists(fs, sidecar)
		require.NoError(t, existsErr)
		assert.True(t, exists)
	}
}

func TestIntegrityReport_HealthyReturnsOK(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	report := IntegrityReport(context.Background(), db, DefaultIntegrityReportRows)
	assert.Equal(t, []string{"ok"}, report)
}

func TestIntegrityReport_QueryErrorReported(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	// Close the handle so the integrity_check query fails rather than panicking.
	require.NoError(t, db.Close())

	report := IntegrityReport(context.Background(), db, DefaultIntegrityReportRows)
	require.Len(t, report, 1)
	assert.Contains(t, report[0], "integrity check failed")
}

func TestIntegrityReport_ScanErrorReported(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Two columns against a single scan destination forces a Scan error.
	mock.ExpectQuery("PRAGMA integrity_check").
		WillReturnRows(sqlmock.NewRows([]string{"a", "b"}).AddRow("x", "y"))

	report := IntegrityReport(context.Background(), db, DefaultIntegrityReportRows)
	require.Len(t, report, 1)
	assert.Contains(t, report[0], "integrity check scan error")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIntegrityReport_RowsErrorReported(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("PRAGMA integrity_check").
		WillReturnRows(sqlmock.NewRows([]string{"integrity_check"}).
			AddRow("ok").
			RowError(0, errors.New("row iteration boom")))

	report := IntegrityReport(context.Background(), db, DefaultIntegrityReportRows)
	require.NotEmpty(t, report)
	assert.Contains(t, report[len(report)-1], "integrity check rows error")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestConnLoadStore(t *testing.T) {
	t.Parallel()
	var c Conn
	assert.Nil(t, c.Load())

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	c.Store(db)
	assert.Same(t, db, c.Load())

	c.Store(nil)
	assert.Nil(t, c.Load())
}

// A corrupt database is renamed aside so it survives the rebuild that replaces
// it. That copy is the only evidence of what went wrong, and the caller removes
// the original immediately afterwards, so a preserve that quietly does nothing
// destroys the thing it exists to keep.
func TestPreserveCorruptFile(t *testing.T) {
	t.Parallel()

	t.Run("renames the file aside", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "media.db")
		require.NoError(t, os.WriteFile(path, []byte("corrupt pages"), 0o600))

		PreserveCorruptFile(path, "media")

		assert.NoFileExists(t, path, "the original is renamed, not copied")
		kept, err := os.ReadFile(CorruptBackupPath(path))
		require.NoError(t, err)
		assert.Equal(t, "corrupt pages", string(kept))
	})

	t.Run("keeps the newest of repeated failures", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "media.db")
		require.NoError(t, os.WriteFile(CorruptBackupPath(path), []byte("older"), 0o600))
		require.NoError(t, os.WriteFile(path, []byte("newer"), 0o600))

		PreserveCorruptFile(path, "media")

		kept, err := os.ReadFile(CorruptBackupPath(path))
		require.NoError(t, err)
		assert.Equal(t, "newer", string(kept),
			"a second corruption must not be dropped in favour of the first")
	})

	// Sidecars come and go depending on what the close managed to checkpoint, so
	// being asked to preserve one that is not there is ordinary.
	t.Run("does nothing for a file that is not there", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "media.db-wal")

		PreserveCorruptFile(path, "media")

		assert.NoFileExists(t, CorruptBackupPath(path))
	})
}

func TestIsUnavailable(t *testing.T) {
	t.Parallel()
	assert.True(t, IsUnavailable(sqlite3.Error{Code: sqlite3.ErrCorrupt}))
	assert.True(t, IsUnavailable(fmt.Errorf("query: %w", sql.ErrConnDone)))
	assert.True(t, IsUnavailable(errors.New("query hidden media: sql: database is closed")))
	assert.False(t, IsUnavailable(errors.New("no such column: Foo")))
	assert.False(t, IsUnavailable(nil))
	assert.Equal(t, zerolog.WarnLevel, UnavailableLevel(sql.ErrConnDone))
	assert.Equal(t, zerolog.ErrorLevel, UnavailableLevel(errors.New("no such column: Foo")))
}
