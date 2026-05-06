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
	"database/sql"
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/migrations/*.sql
var testMigrationFiles embed.FS

func TestLatestEmbeddedVersion(t *testing.T) {
	t.Parallel()

	version, err := latestEmbeddedVersion(testMigrationFiles, "testdata/migrations")
	require.NoError(t, err)
	assert.Equal(t, int64(20250702000000), version)
}

func TestLatestEmbeddedVersion_InvalidDir(t *testing.T) {
	t.Parallel()

	_, err := latestEmbeddedVersion(embed.FS{}, "nonexistent")
	assert.Error(t, err)
}

func TestCheckSchemaVersion_FreshDB(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Fresh DB with no goose table — should pass.
	require.NoError(t, goose.SetDialect("sqlite"))
	err = CheckSchemaVersion(db, testMigrationFiles, "testdata/migrations")
	require.NoError(t, err)
}

func TestCheckSchemaVersion_CurrentVersion(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Apply all test migrations so DB is at the latest version.
	require.NoError(t, MigrateUp(db, testMigrationFiles, "testdata/migrations", "", ""))

	// Version matches — should pass.
	require.NoError(t, goose.SetDialect("sqlite"))
	err = CheckSchemaVersion(db, testMigrationFiles, "testdata/migrations")
	require.NoError(t, err)
}

func TestCheckSchemaVersion_SchemaAhead(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Apply migrations then manually bump the version beyond what we know.
	require.NoError(t, MigrateUp(db, testMigrationFiles, "testdata/migrations", "", ""))

	require.NoError(t, goose.SetDialect("sqlite"))
	// Insert a fake future migration record.
	_, err = db.ExecContext(t.Context(),
		"INSERT INTO goose_db_version (version_id, is_applied) VALUES (99990101000000, 1)",
	)
	require.NoError(t, err)

	err = CheckSchemaVersion(db, testMigrationFiles, "testdata/migrations")
	require.ErrorIs(t, err, ErrSchemaAhead)
	assert.Contains(t, err.Error(), "99990101000000")
	assert.Contains(t, err.Error(), "20250702000000")
}

// migrateUpAndOpen sets up an on-disk SQLite DB at dbPath and runs
// migrations against it via MigrateUp with the supplied sidecar path. It
// returns the open *sql.DB so subsequent assertions can run queries; the
// test cleanup closes it.
func migrateUpAndOpen(t *testing.T, dbPath, sidecarPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, MigrateUp(db, testMigrationFiles, "testdata/migrations", dbPath, sidecarPath))
	return db
}

func TestMigrateUp_WritesSidecarOnSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	sidecarPath := filepath.Join(dir, "cache", "test.db.schema_version.json")

	migrateUpAndOpen(t, dbPath, sidecarPath)

	data, err := os.ReadFile(sidecarPath) //nolint:gosec // path is from t.TempDir
	require.NoError(t, err)

	var sc schemaVersionSidecar
	require.NoError(t, json.Unmarshal(data, &sc))
	assert.Equal(t, int64(20250702000000), sc.SchemaVersion)
	assert.NotZero(t, sc.DBMtimeUnixNs)
	assert.NotZero(t, sc.DBSizeBytes)
}

func TestMigrateUp_FastPathSkipsGooseOnMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	sidecarPath := filepath.Join(dir, "cache", "test.db.schema_version.json")

	// First run writes the sidecar.
	db := migrateUpAndOpen(t, dbPath, sidecarPath)
	require.NoError(t, db.Close())

	// Second run with a fresh handle hits the fast path. The proof: drop
	// the goose_db_version table behind goose's back, then re-run
	// MigrateUp. If the fast path runs, MigrateUp returns nil. If goose
	// runs, it would re-create the table — assert it stayed dropped.
	db2, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })
	_, err = db2.ExecContext(t.Context(), "DROP TABLE goose_db_version")
	require.NoError(t, err)
	require.NoError(t, db2.Close())

	// Sidecar mtime/size won't have changed because we only ran a DROP
	// TABLE on the connection that's now closed, but SQLite writes pages.
	// To make this test deterministic, refresh the sidecar with the
	// post-drop file stats so the fast path can match.
	info, err := os.Stat(dbPath)
	require.NoError(t, err)
	updated := schemaVersionSidecar{
		SchemaVersion: 20250702000000,
		DBMtimeUnixNs: info.ModTime().UnixNano(),
		DBSizeBytes:   info.Size(),
	}
	updatedData, err := json.Marshal(updated)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sidecarPath, updatedData, 0o600))

	db3, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db3.Close() })
	require.NoError(t, MigrateUp(db3, testMigrationFiles, "testdata/migrations", dbPath, sidecarPath))

	var n int
	row := db3.QueryRowContext(t.Context(),
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'")
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 0, n, "goose ran when it should have been skipped")
}

func TestMigrateUp_FallsThroughOnMtimeMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	sidecarPath := filepath.Join(dir, "cache", "test.db.schema_version.json")

	db := migrateUpAndOpen(t, dbPath, sidecarPath)
	require.NoError(t, db.Close())

	// Tamper with the recorded mtime so it no longer matches the live file.
	data, err := os.ReadFile(sidecarPath) //nolint:gosec // path is from t.TempDir
	require.NoError(t, err)
	var sc schemaVersionSidecar
	require.NoError(t, json.Unmarshal(data, &sc))
	sc.DBMtimeUnixNs = 1
	tampered, err := json.Marshal(sc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sidecarPath, tampered, 0o600))

	v, ok := loadSchemaVersionSidecar(sidecarPath, dbPath)
	assert.False(t, ok)
	assert.Zero(t, v)
}

func TestMigrateUp_FallsThroughOnSizeMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	sidecarPath := filepath.Join(dir, "cache", "test.db.schema_version.json")

	db := migrateUpAndOpen(t, dbPath, sidecarPath)
	require.NoError(t, db.Close())

	data, err := os.ReadFile(sidecarPath) //nolint:gosec // path is from t.TempDir
	require.NoError(t, err)
	var sc schemaVersionSidecar
	require.NoError(t, json.Unmarshal(data, &sc))
	sc.DBSizeBytes = 1
	tampered, err := json.Marshal(sc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sidecarPath, tampered, 0o600))

	v, ok := loadSchemaVersionSidecar(sidecarPath, dbPath)
	assert.False(t, ok)
	assert.Zero(t, v)
}

func TestMigrateUp_FallsThroughOnMissingSidecar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("not a real db"), 0o600))
	sidecarPath := filepath.Join(dir, "cache", "test.db.schema_version.json")

	v, ok := loadSchemaVersionSidecar(sidecarPath, dbPath)
	assert.False(t, ok)
	assert.Zero(t, v)
}

func TestMigrateUp_FallsThroughOnMalformedSidecar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("not a real db"), 0o600))
	sidecarPath := filepath.Join(dir, "cache", "test.db.schema_version.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sidecarPath), 0o750))
	require.NoError(t, os.WriteFile(sidecarPath, []byte("not json"), 0o600))

	v, ok := loadSchemaVersionSidecar(sidecarPath, dbPath)
	assert.False(t, ok)
	assert.Zero(t, v)
}

func TestMigrateUp_EmptyPathsDisableFastPath(t *testing.T) {
	t.Parallel()

	// In-memory DB with empty dbPath/sidecarPath should run goose normally
	// (the existing behaviour). Sanity check this still works.
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, MigrateUp(db, testMigrationFiles, "testdata/migrations", "", ""))

	var n int
	row := db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'")
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 1, n, "goose should have run when fast path is disabled")
}
