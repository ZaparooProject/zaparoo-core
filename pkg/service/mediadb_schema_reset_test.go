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

package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testmocks "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaAheadVersion is higher than any migration this build embeds, so a
// database carrying it looks like one a newer binary migrated.
const schemaAheadVersion int64 = 99999999999999

// testSystemID identifies the row used to tell a preserved database from a
// rebuilt one.
const testSystemID = "test"

func newMediaDBPlatform(t *testing.T) (pl platforms.Platform, dataDir string) {
	t.Helper()
	dataDir = t.TempDir()
	mockPlatform := testmocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: dataDir})
	return mockPlatform, dataDir
}

// seedMigratedMediaDB creates a media database at this build's schema holding
// one identifiable row.
func seedMigratedMediaDB(ctx context.Context, t *testing.T, pl platforms.Platform) {
	t.Helper()
	db, err := mediadb.OpenMediaDB(ctx, pl)
	require.NoError(t, err)
	require.NoError(t, db.MigrateUp())
	_, err = db.InsertSystem(database.System{Name: "Test System", SystemID: testSystemID})
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

// markSchemaAhead records a migration version no build of this binary knows
// about, which is what a newer binary having run against the file looks like to
// goose. The schema version sidecar is dropped so the version check is not
// skipped: on a real downgrade the sidecar is present but records the newer
// binary's version, which fails the fast path's strict equality test the same
// way a missing sidecar does.
func markSchemaAhead(ctx context.Context, t *testing.T, dataDir, dbFile string) {
	t.Helper()
	conn, err := sql.Open("sqlite3", filepath.Join(dataDir, dbFile))
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()
	_, err = conn.ExecContext(ctx,
		"INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", schemaAheadVersion)
	require.NoError(t, err)

	sidecar := filepath.Join(dataDir, config.CacheDir, dbFile+".schema_version.json")
	if err := os.Remove(sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
	}
}

// mediaDBSchemaVersion reads the migration version straight out of the file, so
// the assertion does not depend on the handle under test.
func mediaDBSchemaVersion(ctx context.Context, t *testing.T, dataDir string) int64 {
	t.Helper()
	conn, err := sql.Open("sqlite3", filepath.Join(dataDir, config.MediaDbFile))
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()

	var version int64
	require.NoError(t, conn.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version").Scan(&version))
	return version
}

func TestMakeDatabase_SchemaAheadRebuildsMediaDB(t *testing.T) {
	ctx := context.Background()
	pl, dataDir := newMediaDBPlatform(t)
	seedMigratedMediaDB(ctx, t, pl)
	markSchemaAhead(ctx, t, dataDir, config.MediaDbFile)

	db, mediaDBReset, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err, "a media database from a newer build must not stop startup")
	assert.True(t, mediaDBReset, "the caller has to know the database was discarded")

	_, err = db.MediaDB.FindSystemBySystemID(testSystemID)
	require.Error(t, err, "the unreadable database's rows must not survive the rebuild")

	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, mediadb.IndexingStatusPending, status,
		"the rebuilt database must be left pending so startup reindexes it")

	version := mediaDBSchemaVersion(ctx, t, dataDir)
	assert.Less(t, version, schemaAheadVersion, "the newer build's migration version must be gone")
	assert.Positive(t, version, "the rebuilt database must be migrated to this build's schema")
}

func TestMakeDatabase_CompatibleMediaDBIsKept(t *testing.T) {
	ctx := context.Background()
	pl, _ := newMediaDBPlatform(t)
	seedMigratedMediaDB(ctx, t, pl)

	db, mediaDBReset, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err)
	assert.False(t, mediaDBReset, "a readable database must not be reported as rebuilt")

	system, err := db.MediaDB.FindSystemBySystemID(testSystemID)
	require.NoError(t, err, "a readable database must be left alone")
	assert.Equal(t, "Test System", system.Name)
}

// A user database from a newer build has to stay fatal: unlike the media
// database, nothing can reconstruct what is in it, so starting up and writing
// against a schema this build does not understand would lose data. The update
// rollback path restores a snapshot instead.
func TestMakeDatabase_SchemaAheadUserDBIsFatal(t *testing.T) {
	ctx := context.Background()
	pl, dataDir := newMediaDBPlatform(t)

	db, _, err := makeDatabase(ctx, pl)
	require.NoError(t, err)
	closeDatabase(db)
	markSchemaAhead(ctx, t, dataDir, config.UserDbFile)

	db, mediaDBReset, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.ErrorIs(t, err, database.ErrSchemaAhead)
	assert.False(t, mediaDBReset)
}

func TestNotifyMediaDBSchemaReset_PostsInboxMessage(t *testing.T) {
	ctx := context.Background()
	pl, _ := newMediaDBPlatform(t)

	db, _, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err)

	st, _ := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(st.StopService)
	st.SetInbox(inbox.NewService(db.UserDB, st.Notifications))

	notifyMediaDBSchemaReset(st)

	messages, err := db.UserDB.GetInboxMessages()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, inbox.CategoryMediaDBSchemaReset, messages[0].Category)
	assert.Equal(t, inbox.SeverityWarning, messages[0].Severity)
}

func TestNotifyMediaDBSchemaReset_WithoutInboxIsNoOp(_ *testing.T) {
	// Must not panic before the inbox service exists, or with no state at all.
	notifyMediaDBSchemaReset(nil)
	st, _ := state.NewState(testmocks.NewMockPlatform(), "test-boot-uuid")
	defer st.StopService()
	notifyMediaDBSchemaReset(st)
}
