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

package database_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newHiddenDirTestDB indexes one folder of two games and a game beside it.
func newHiddenDirTestDB(t *testing.T) (db *database.Database, folder, file string) {
	t.Helper()
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	folder = filepath.Join("roms", "NES", "Folder")
	file = filepath.Join("roms", "NES", "Beside.nes")
	scantest.IndexMediaPaths(t, mediaDB, "NES",
		filepath.Join(folder, "One.nes"), filepath.Join(folder, "Two.nes"), file)
	require.NoError(t, mediaDB.PopulateBrowseCache(context.Background()))
	return &database.Database{MediaDB: mediaDB, UserDB: userDB}, folder, file
}

// hiddenFolders lists the root with hidden entries included and returns the
// names of the folders it reports as hidden.
func hiddenFolders(t *testing.T, db *database.Database) []string {
	t.Helper()
	dirs, err := db.MediaDB.BrowseDirectories(context.Background(), database.BrowseDirectoriesOptions{
		PathPrefix: pathutil.CanonicalMediaPath(filepath.Join("roms", "NES")) + "/",
	})
	require.NoError(t, err)
	names := make([]string, 0, len(dirs))
	for i := range dirs {
		if dirs[i].Hidden {
			names = append(names, dirs[i].Name)
		}
	}
	return names
}

func TestApplyDirectoryHiddenProjectsOnlyFolders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, file := newHiddenDirTestDB(t)

	changed, err := database.ApplyDirectoryHidden(ctx, db, "NES", folder, true)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{"Folder"}, hiddenFolders(t, db))

	// The same request again changes nothing.
	changed, err = database.ApplyDirectoryHidden(ctx, db, "NES", folder, true)
	require.NoError(t, err)
	assert.False(t, changed)

	// A hidden file, a hidden path nothing is indexed under, and a hidden
	// path of a system that is not indexed are not folders.
	require.NoError(t, db.UserDB.SetMediaUserHidden("NES", file, true))
	require.NoError(t, db.UserDB.SetMediaUserHidden("NES", filepath.Join("roms", "NES", "Unplugged"), true))
	require.NoError(t, db.UserDB.SetMediaUserHidden("Unindexed", folder, true))
	require.NoError(t, database.SyncHiddenDirectories(ctx, db.UserDB, db.MediaDB))
	assert.Equal(t, []string{"Folder"}, hiddenFolders(t, db))

	changed, err = database.ApplyDirectoryHidden(ctx, db, "NES", folder, false)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Empty(t, hiddenFolders(t, db))
}

// A projection that was lost with its database is rebuilt from UserDB, which
// is what startup, a reindex and a backup restore rely on.
func TestSyncHiddenDirectoriesRestoresALostProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, _ := newHiddenDirTestDB(t)
	_, err := database.ApplyDirectoryHidden(ctx, db, "NES", folder, true)
	require.NoError(t, err)

	_, err = db.MediaDB.ReplaceHiddenDirectories(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, hiddenFolders(t, db))

	require.NoError(t, database.SyncHiddenDirectories(ctx, db.UserDB, db.MediaDB))
	assert.Equal(t, []string{"Folder"}, hiddenFolders(t, db))

	// The restore reconcile brings it in line too.
	_, err = db.MediaDB.ReplaceHiddenDirectories(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, database.ReconcileMediaUserData(ctx, db))
	assert.Equal(t, []string{"Folder"}, hiddenFolders(t, db))
}

type failingHiddenUserDB struct {
	database.UserDBI
	listErr error
	setErr  error
}

func (f failingHiddenUserDB) ListMediaUserData() ([]database.MediaUserData, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.UserDBI.ListMediaUserData() //nolint:wrapcheck // test passthrough
}

func (f failingHiddenUserDB) SetMediaUserHidden(systemID, path string, hidden bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	return f.UserDBI.SetMediaUserHidden(systemID, path, hidden) //nolint:wrapcheck // test passthrough
}

type failingHiddenMediaDB struct {
	database.MediaDBI
	systemErr  error
	findErr    error
	probeErr   error
	replaceErr error
}

func (f *failingHiddenMediaDB) FindSystemBySystemID(systemID string) (database.System, error) {
	if f.systemErr != nil {
		return database.System{}, f.systemErr
	}
	return f.MediaDBI.FindSystemBySystemID(systemID) //nolint:wrapcheck // test passthrough
}

func (f *failingHiddenMediaDB) FindMediaBySystemAndPaths(
	ctx context.Context, systemDBID int64, paths []string,
) (map[string]database.Media, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.MediaDBI.FindMediaBySystemAndPaths(ctx, systemDBID, paths) //nolint:wrapcheck // test passthrough
}

func (f *failingHiddenMediaDB) HasMediaUnderDirectory(
	ctx context.Context, systemDBID int64, path string,
) (bool, error) {
	if f.probeErr != nil {
		return false, f.probeErr
	}
	return f.MediaDBI.HasMediaUnderDirectory(ctx, systemDBID, path) //nolint:wrapcheck // test passthrough
}

func (f *failingHiddenMediaDB) ReplaceHiddenDirectories(
	ctx context.Context, dirs []database.HiddenDirectory,
) (bool, error) {
	if f.replaceErr != nil {
		return false, f.replaceErr
	}
	return f.MediaDBI.ReplaceHiddenDirectories(ctx, dirs) //nolint:wrapcheck // test passthrough
}

// Every step of the projection reports its failure, and a failed write to
// UserDB never reaches MediaDB.
func TestHiddenDirectoryFailuresAreReported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("boom")

	tests := []struct {
		user  failingHiddenUserDB
		media failingHiddenMediaDB
		name  string
	}{
		{name: "user write", user: failingHiddenUserDB{setErr: boom}},
		{name: "user list", user: failingHiddenUserDB{listErr: boom}},
		{name: "file lookup", media: failingHiddenMediaDB{findErr: boom}},
		{name: "directory probe", media: failingHiddenMediaDB{probeErr: boom}},
		{name: "projection write", media: failingHiddenMediaDB{replaceErr: boom}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			realDB, folder, _ := newHiddenDirTestDB(t)
			user, media := tt.user, tt.media
			user.UserDBI = realDB.UserDB
			media.MediaDBI = realDB.MediaDB
			db := &database.Database{UserDB: user, MediaDB: &media}

			_, err := database.ApplyDirectoryHidden(ctx, db, "NES", folder, true)
			require.ErrorIs(t, err, boom)
			assert.Empty(t, hiddenFolders(t, realDB))
		})
	}

	t.Run("system lookup", func(t *testing.T) {
		t.Parallel()
		realDB, folder, _ := newHiddenDirTestDB(t)
		require.NoError(t, realDB.UserDB.SetMediaUserHidden("NES", folder, true))
		// The file lookup resolves the system first; fail the second read,
		// which is the folder probe's.
		media := &countingSystemMediaDB{MediaDBI: realDB.MediaDB, failFrom: 2, err: boom}
		err := database.SyncHiddenDirectories(ctx, realDB.UserDB, media)
		require.ErrorIs(t, err, boom)
	})
}

type countingSystemMediaDB struct {
	database.MediaDBI
	err      error
	calls    int
	failFrom int
}

func (c *countingSystemMediaDB) FindSystemBySystemID(systemID string) (database.System, error) {
	c.calls++
	if c.calls >= c.failFrom {
		return database.System{}, c.err
	}
	return c.MediaDBI.FindSystemBySystemID(systemID) //nolint:wrapcheck // test passthrough
}
