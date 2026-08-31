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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corruptMediaDBPages scribbles over a stretch of pages past the header, which
// is what a torn write on the storage looks like: the file still opens, and the
// damage is only found when SQLite reads one of the ruined pages.
func corruptMediaDBPages(t *testing.T, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, config.MediaDbFile)

	f, err := os.OpenFile(path, os.O_RDWR, 0o600) //nolint:gosec // test-owned temp dir
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	info, err := f.Stat()
	require.NoError(t, err)
	const pageSize = 4096
	require.Greater(t, info.Size(), int64(pageSize*4),
		"the seeded database must be big enough to damage past its header")

	// Page 1 is left intact so the file still opens as a database and the damage
	// is found while reading, which is the shape a torn write takes. Everything
	// after it goes, so the b-tree the schema check walks is certain to be hit
	// however the pages happen to be laid out.
	junk := make([]byte, info.Size()-pageSize)
	for i := range junk {
		junk[i] = byte(i%251) ^ 0xA5
	}
	_, err = f.WriteAt(junk, pageSize)
	require.NoError(t, err)
	require.NoError(t, f.Sync())

	for _, sidecar := range []string{
		path + "-wal",
		path + "-shm",
		filepath.Join(dataDir, config.CacheDir, config.MediaDbFile+".schema_version.json"),
	} {
		if err := os.Remove(sidecar); err != nil {
			require.ErrorIs(t, err, os.ErrNotExist)
		}
	}
}

// TestMakeDatabase_CorruptMediaDBRebuilds covers a media database damaged badly
// enough that the migration check cannot read it.
//
// There is a recovery path for corruption found once the database is open, in
// index_resume.go, but startup never reached it: makeDatabase returned the
// migration error, Start gave up, and the next boot failed in exactly the same
// place. Observed on a MiSTer — after damaging the file, Core would not start at
// all, twice in a row, writing media.db.corrupt each time and never acting on
// it. That is a device that cannot run Zaparoo over a file a reindex rebuilds
// from the filesystem, which is the same trade the schema-ahead branch already
// resolves in favour of starting.
func TestMakeDatabase_CorruptMediaDBRebuilds(t *testing.T) {
	ctx := context.Background()
	pl, dataDir := newMediaDBPlatform(t)
	seedMigratedMediaDB(ctx, t, pl)
	corruptMediaDBPages(t, dataDir)

	db, mediaDBReset, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err, "a corrupt media database must not stop startup")
	require.NotNil(t, mediaDBReset, "the caller has to know the database was discarded")
	assert.True(t, mediaDBReset.corrupt,
		"the notice must say damage, not a version change: nobody changed versions")

	_, err = db.MediaDB.FindSystemBySystemID(testSystemID)
	require.Error(t, err, "the damaged database's rows must not survive the rebuild")

	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, mediadb.IndexingStatusPending, status,
		"the rebuilt database must be left pending so startup reindexes it")

	assert.False(t, db.MediaDB.IsMarkedCorrupt(),
		"the corrupt marker must be cleared, or the next boot rebuilds a healthy database")

	version := mediaDBSchemaVersion(ctx, t, dataDir)
	assert.Positive(t, version, "the rebuilt database must be migrated to this build's schema")
}

// The rebuilt database has to be usable immediately, not merely openable: the
// reindex that follows writes to it straight away.
func TestMakeDatabase_RebuiltCorruptMediaDBIsWritable(t *testing.T) {
	ctx := context.Background()
	pl, dataDir := newMediaDBPlatform(t)
	seedMigratedMediaDB(ctx, t, pl)
	corruptMediaDBPages(t, dataDir)

	db, mediaDBReset, err := makeDatabase(ctx, pl)
	t.Cleanup(func() { closeDatabase(db) })
	require.NoError(t, err)
	require.NotNil(t, mediaDBReset)

	system, err := db.MediaDB.InsertSystem(database.System{
		Name: "Rebuilt System", SystemID: "rebuilt-system",
	})
	require.NoError(t, err, "the rebuilt database must accept writes")
	assert.Positive(t, system.DBID)
}
