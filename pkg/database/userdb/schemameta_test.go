/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package userdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileState is enough to notice a write: size and modification time.
func fileState(t *testing.T, path string) [2]any {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return [2]any{info.Size(), info.ModTime()}
}

func migratedUserDBAt(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	require.NoError(t, sqlMigrateUp(db, dbPath))
	require.NoError(t, db.Close())
}

// The whole reason the version is recorded: an older binary that refuses this
// database can then name what to reinstall, instead of a goose timestamp.
func TestSchemaProvenance_NamesTheBuildThatMigratedTheDatabase(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), config.UserDbFile)
	migratedUserDBAt(t, dbPath)

	version, ok := SchemaProvenance(dbPath)
	require.True(t, ok, "a database this build migrated has to be able to say so")
	assert.Equal(t, config.AppVersion, version)
}

// Reading it must not migrate, write or otherwise touch the file: the caller
// is the path that has already refused to open it.
func TestSchemaProvenance_LeavesTheDatabaseAlone(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), config.UserDbFile)
	migratedUserDBAt(t, dbPath)

	before := fileState(t, dbPath)
	_, ok := SchemaProvenance(dbPath)
	require.True(t, ok)
	assert.Equal(t, before, fileState(t, dbPath))
}

// Every database written before this was recorded answers this way, which is
// every database in the field on the day it ships. The caller has to read well
// without an answer, so it must say "unknown" rather than guess.
func TestSchemaProvenance_SaysNothingWhenThereIsNothingRecorded(t *testing.T) {
	t.Parallel()

	t.Run("no database at all", func(t *testing.T) {
		t.Parallel()
		version, ok := SchemaProvenance(filepath.Join(t.TempDir(), config.UserDbFile))
		assert.False(t, ok)
		assert.Empty(t, version)
	})

	t.Run("no path", func(t *testing.T) {
		t.Parallel()
		version, ok := SchemaProvenance("")
		assert.False(t, ok)
		assert.Empty(t, version)
	})

	t.Run("a database from before the table existed", func(t *testing.T) {
		t.Parallel()

		dbPath := filepath.Join(t.TempDir(), config.UserDbFile)
		migratedUserDBAt(t, dbPath)

		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `drop table SchemaMeta`)
		require.NoError(t, err)
		require.NoError(t, db.Close())

		version, ok := SchemaProvenance(dbPath)
		assert.False(t, ok)
		assert.Empty(t, version)
	})

	t.Run("the row is there but empty", func(t *testing.T) {
		t.Parallel()

		dbPath := filepath.Join(t.TempDir(), config.UserDbFile)
		migratedUserDBAt(t, dbPath)

		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		_, err = db.ExecContext(
			t.Context(), `update SchemaMeta set Value = '' where Key = ?`, schemaMetaAppVersion)
		require.NoError(t, err)
		require.NoError(t, db.Close())

		version, ok := SchemaProvenance(dbPath)
		assert.False(t, ok, "an empty version names nothing, so it is not an answer")
		assert.Empty(t, version)
	})
}
