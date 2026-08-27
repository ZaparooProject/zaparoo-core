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
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

// openTestSQLite opens a real (non-memory) SQLite file with the given DSN
// query string appended. WAL mode has no effect against ":memory:" databases
// — SQLite silently forces "memory" journal mode there instead — so the
// pragma read-back tests need an actual file to be meaningful.
func openTestSQLite(t *testing.T, dsnParams string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pragma_test.db")
	db, err := sql.Open("sqlite3", dbPath+dsnParams)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(context.Background()))
	return db
}

func TestReadEffectivePragmas_WAL(t *testing.T) {
	t.Parallel()
	db := openTestSQLite(t, "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON")

	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	pragmas, err := ReadEffectivePragmas(context.Background(), conn)
	require.NoError(t, err)
	require.Equal(t, "wal", pragmas.JournalMode)
	require.Equal(t, int64(SynchronousNormal), pragmas.Synchronous)
	require.Equal(t, int64(4096), pragmas.PageSize, "SQLite's compiled default page size")
	require.Equal(t, int64(1000), pragmas.WALAutoCheckpoint, "SQLite's default wal_autocheckpoint is 1000 pages")
	require.Equal(t, int64(1), pragmas.ForeignKeys)
}

func TestReadEffectivePragmas_NonWALJournalMode(t *testing.T) {
	t.Parallel()
	db := openTestSQLite(t, "?_journal_mode=DELETE")

	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	pragmas, err := ReadEffectivePragmas(context.Background(), conn)
	require.NoError(t, err)
	require.Equal(t, "delete", pragmas.JournalMode)
}

func TestReadEffectivePragmas_ErrorsOnClosedConnection(t *testing.T) {
	t.Parallel()
	db := openTestSQLite(t, "?_journal_mode=WAL")

	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, err = ReadEffectivePragmas(context.Background(), conn)
	require.Error(t, err)
}

// The three tests below cannot run in parallel with each other or with any
// other test that swaps log.Logger (matching the same constraint already
// documented in mediascanner_test.go): log.Logger is a shared package
// global, and t.Parallel() here raced two tests' loggers, at one point
// leaving this test's capture buffer empty because a sibling test's deferred
// restore fired mid-call.
func TestLogEffectivePragmasForDB_MatchesExpectation_LogsInfo(t *testing.T) {
	db := openTestSQLite(t, "?_journal_mode=WAL&_synchronous=NORMAL")

	var buf strings.Builder
	logger := zerolog.New(&buf)
	prevLogger := log.Logger
	log.Logger = logger
	defer func() { log.Logger = prevLogger }()

	LogEffectivePragmasForDB(context.Background(), db, "test-db", SynchronousNormal, 4096)

	output := buf.String()
	require.Contains(t, output, `"level":"info"`)
	require.Contains(t, output, `"journalMode":"wal"`)
	require.Contains(t, output, `"db":"test-db"`)
}

func TestLogEffectivePragmasForDB_UnexpectedSettings_LogsWarn(t *testing.T) {
	db := openTestSQLite(t, "?_journal_mode=WAL&_synchronous=NORMAL")

	var buf strings.Builder
	logger := zerolog.New(&buf)
	prevLogger := log.Logger
	log.Logger = logger
	defer func() { log.Logger = prevLogger }()

	LogEffectivePragmasForDB(context.Background(), db, "test-db", SynchronousNormal, 8192)

	output := buf.String()
	require.Contains(t, output, `"level":"warn"`)
}

// TestLogEffectivePragmasForDB_UnsetPageSizeDoesNotWarn covers the callers'
// actual configuration: none of them set _page_size, so an existing database
// carries whatever page size it was created with. That is not a misconfiguration
// and must not raise the line to warn, or the warnings that do matter — a WAL
// fallback, a synchronous mismatch — get lost in noise on every open.
func TestLogEffectivePragmasForDB_UnsetPageSizeDoesNotWarn(t *testing.T) {
	db := openTestSQLite(t, "?_journal_mode=WAL&_synchronous=NORMAL")

	logPragmas := func(wantPageSize int64) string {
		var buf strings.Builder
		prevLogger := log.Logger
		log.Logger = zerolog.New(&buf)
		defer func() { log.Logger = prevLogger }()
		LogEffectivePragmasForDB(context.Background(), db, "test-db", SynchronousNormal, wantPageSize)
		return buf.String()
	}

	// 8192 is deliberately not this database's page size, so a non-zero
	// expectation must warn. Establishes that the comparison is live at all,
	// which is what makes the UnsetPageSize case below meaningful rather than
	// vacuously passing.
	require.Contains(t, logPragmas(8192), `"level":"warn"`)

	unset := logPragmas(UnsetPageSize)
	require.Contains(t, unset, `"level":"info"`)
	require.NotContains(t, unset, `"level":"warn"`)
	require.Contains(t, unset, `"pageSize":`, "the value is still reported, just not judged")
}

func TestLogEffectivePragmasForDB_AcquireFailure_LogsWarnWithoutPanic(t *testing.T) {
	db := openTestSQLite(t, "?_journal_mode=WAL")
	require.NoError(t, db.Close())

	var buf strings.Builder
	logger := zerolog.New(&buf)
	prevLogger := log.Logger
	log.Logger = logger
	defer func() { log.Logger = prevLogger }()

	require.NotPanics(t, func() {
		LogEffectivePragmasForDB(context.Background(), db, "test-db", SynchronousNormal, 4096)
	})
	require.Contains(t, buf.String(), `"level":"warn"`)
}

// TestLogEffectivePragmasForDB_NilHandle_LogsWarnWithoutPanic covers the case
// where the database was never opened, or was swapped out from under a caller.
// A diagnostic that dereferences a nil handle would take down whatever it was
// only trying to describe, and this used to be papered over by a recover() in
// the indexing caller rather than handled here.
func TestLogEffectivePragmasForDB_NilHandle_LogsWarnWithoutPanic(t *testing.T) {
	var buf strings.Builder
	logger := zerolog.New(&buf)
	prevLogger := log.Logger
	log.Logger = logger
	defer func() { log.Logger = prevLogger }()

	require.NotPanics(t, func() {
		LogEffectivePragmasForDB(context.Background(), nil, "test-db", SynchronousNormal, 4096)
	})
	require.Contains(t, buf.String(), `"level":"warn"`)
}

// TestSynchronousConstantsMatchSQLite pins the PRAGMA synchronous integer
// values these constants stand in for, so a future SQLite change (unlikely,
// but these are otherwise unexplained magic numbers everywhere they're used)
// is caught here rather than silently miscomparing.
func TestSynchronousConstantsMatchSQLite(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, SynchronousOff)
	require.Equal(t, 1, SynchronousNormal)
	require.Equal(t, 2, SynchronousFull)
}
