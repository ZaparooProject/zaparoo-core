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
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenSQLConnectionDoesNotPublish(t *testing.T) {
	t.Parallel()

	db := &UserDB{ctx: t.Context()}
	conn, err := db.openSQLConnection(filepath.Join(t.TempDir(), "user.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, conn.PingContext(t.Context()))
	assert.Nil(t, db.sql.Load())
}

func TestOpenSQLConnectionClosesConnectionFailure(t *testing.T) {
	t.Parallel()

	db := &UserDB{ctx: t.Context()}
	conn, err := db.openSQLConnection(filepath.Join(t.TempDir(), "missing", "user.db"))
	require.Error(t, err)
	assert.Nil(t, conn)
	assert.Nil(t, db.sql.Load())
}

func TestOpenMigratedDatabasePublishesAfterMigration(t *testing.T) {
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	previous := db.sql.Load()
	require.NoError(t, db.closeAndDrain())
	require.NoError(t, db.openMigratedDatabase())

	current := db.sql.Load()
	assert.NotSame(t, previous, current)
	require.NoError(t, current.PingContext(t.Context()))
}

func TestOpenMigratedDatabaseClosesMigrationFailureWithoutPublishing(t *testing.T) {
	db, cleanup := setupTempUserDB(t)
	defer cleanup()

	previous := db.sql.Load()
	require.NoError(t, db.closeAndDrain())
	dbPath := db.GetDBPath()
	database.RemoveSidecars(dbPath)
	require.NoError(t, os.Remove(dbPath))

	broken, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = broken.ExecContext(context.Background(), `CREATE TABLE goose_db_version (bad TEXT)`)
	require.NoError(t, err)
	require.NoError(t, broken.Close())

	err = db.openMigratedDatabase()
	require.Error(t, err)
	assert.Same(t, previous, db.sql.Load())
	require.NoError(t, os.Remove(dbPath), "failed migration connection must release the database file")
}
