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
	require.NoError(t, MigrateUp(db, testMigrationFiles, "testdata/migrations"))

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
	require.NoError(t, MigrateUp(db, testMigrationFiles, "testdata/migrations"))

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
