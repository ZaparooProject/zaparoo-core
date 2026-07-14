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
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// CorruptMarkerSuffix names the sidecar file written next to a database to flag
// detected corruption. It is the DB-independent signal the recovery paths key on:
// unlike a status row it does not require writing to the (possibly unwritable)
// database itself.
const CorruptMarkerSuffix = ".corrupt"

// DefaultIntegrityReportRows caps PRAGMA integrity_check output so a badly corrupt
// database cannot flood the log; the first rows identify the damaged pages.
const DefaultIntegrityReportRows = 20

// IsCorruptionError reports whether err indicates SQLite database corruption (a
// malformed disk image or a non-database file). It matches both the typed sqlite3
// error codes and the message text, so corruption surfaced through a wrapped string
// error is still detected.
func IsCorruptionError(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrCorrupt || sqliteErr.Code == sqlite3.ErrNotADB
	}
	msg := err.Error()
	return strings.Contains(msg, "database disk image is malformed") ||
		strings.Contains(msg, "file is not a database")
}

// CorruptMarkerPath returns the sidecar marker path for a database file.
func CorruptMarkerPath(dbPath string) string {
	return dbPath + CorruptMarkerSuffix
}

// MarkCorrupt writes the corrupt marker sidecar next to dbPath recording that
// corruption was detected. Best-effort — failures are logged, not returned.
func MarkCorrupt(dbPath, reason string, now time.Time) {
	path := CorruptMarkerPath(dbPath)
	contents := fmt.Sprintf("%s\n%s\n", now.UTC().Format(time.RFC3339Nano), reason)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		log.Error().Err(err).Str("path", path).Msg("failed to write database corrupt marker")
		return
	}
	log.Warn().Str("path", path).Str("reason", reason).Msg("flagged database as corrupt")
}

// IsMarkedCorrupt reports whether the corrupt marker sidecar exists for dbPath.
func IsMarkedCorrupt(dbPath string) bool {
	_, err := os.Stat(CorruptMarkerPath(dbPath))
	return err == nil
}

// ClearCorruptMarker removes the corrupt marker sidecar for dbPath. No-op when absent.
func ClearCorruptMarker(dbPath string) error {
	if err := os.Remove(CorruptMarkerPath(dbPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to clear corrupt marker: %w", err)
	}
	return nil
}

// NoteCorruption flags dbPath corrupt when err indicates SQLite corruption, so any
// path that first touches a malformed page routes into the recovery flow instead of
// silently failing. It only writes the marker once. Returns true when err was a
// corruption error.
func NoteCorruption(dbPath string, err error, now time.Time) bool {
	if !IsCorruptionError(err) {
		return false
	}
	if !IsMarkedCorrupt(dbPath) {
		MarkCorrupt(dbPath, fmt.Sprintf("query error: %v", err), now)
	}
	return true
}

// RemoveSidecars deletes the -wal and -shm sidecar files for dbPath. A stale WAL
// left next to a freshly restored or recreated database would re-corrupt it.
func RemoveSidecars(dbPath string) {
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", sidecar).Msg("failed to remove database sidecar")
		}
	}
}

// IntegrityReport runs PRAGMA integrity_check(maxRows) against sqlDB and returns the
// result rows, capped so a badly corrupt database cannot flood the log. A healthy
// database returns a single "ok" row. Callers hold their own connection lock and
// nil-check; sqlDB must be non-nil.
func IntegrityReport(ctx context.Context, sqlDB *sql.DB, maxRows int) []string {
	rows, err := sqlDB.QueryContext(ctx, fmt.Sprintf("PRAGMA integrity_check(%d)", maxRows))
	if err != nil {
		return []string{fmt.Sprintf("integrity check failed: %v", err)}
	}
	defer func() { _ = rows.Close() }()

	report := make([]string, 0, 1)
	for rows.Next() {
		var line string
		if scanErr := rows.Scan(&line); scanErr != nil {
			report = append(report, fmt.Sprintf("integrity check scan error: %v", scanErr))
			break
		}
		report = append(report, line)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		report = append(report, fmt.Sprintf("integrity check rows error: %v", rowsErr))
	}
	return report
}
