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
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeImageRewriter stands in for MediaDB's transactional rewrite: it records
// the call, runs apply, and fails before or after it on request.
type fakeImageRewriter struct {
	database.MediaDBI
	beforeErr error
	commitErr error
	markerErr error
	oldPrefix string
	newPrefix string
	sep       string
	calls     int
	committed bool
}

func (f *fakeImageRewriter) ImagePathsMigrated(context.Context) (bool, error) {
	return f.committed, f.markerErr
}

func (f *fakeImageRewriter) RewriteImagePropertyPrefix(
	_ context.Context, oldPrefix, newPrefix, sep string, apply func() error,
) (int64, error) {
	f.calls++
	f.oldPrefix, f.newPrefix, f.sep = oldPrefix, newPrefix, sep
	if f.beforeErr != nil {
		return 0, f.beforeErr
	}
	if apply != nil {
		if err := apply(); err != nil {
			return 0, err
		}
	}
	if f.commitErr != nil {
		return 0, f.commitErr
	}
	f.committed = true
	return 3, nil
}

var _ imagePropertyRewriter = (*mediadb.MediaDB)(nil)

// failRenameFs fails every rename, as a move to another filesystem does.
type failRenameFs struct{ afero.Fs }

func (failRenameFs) Rename(string, string) error { return errors.New("invalid cross-device link") }

var (
	migrationDataDir  = filepath.Join(string(filepath.Separator), "data")
	migrationImageDir = filepath.Join(migrationDataDir, "images")
	legacyLibretro    = filepath.Join(migrationDataDir, "libretro-thumbnails")
	legacyThumbs      = filepath.Join(migrationDataDir, "cache", "thumbs")
	newLibretro       = filepath.Join(migrationImageDir, "libretro")
	newThumbs         = filepath.Join(migrationImageDir, "thumbs")
	legacyBoxart      = filepath.Join(legacyLibretro, "NES", "Named_Boxarts", "A.png")
	legacyThumb       = filepath.Join(legacyThumbs, "v3", "ab", "abcd.webp")
)

func oldImageLayout(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, legacyBoxart, []byte("png"), 0o600))
	require.NoError(t, afero.WriteFile(fs, legacyThumb, []byte("webp"), 0o600))
	return fs
}

func imagePathExists(t *testing.T, fs afero.Fs, path string) bool {
	t.Helper()
	ok, err := afero.Exists(fs, path)
	require.NoError(t, err)
	return ok
}

func TestMigrateImageStorageFreshInstall(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	db := &fakeImageRewriter{}

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)

	assert.Zero(t, db.calls, "nothing to rewrite")
	assert.False(t, imagePathExists(t, fs, migrationImageDir), "nothing is created for nothing to move")
}

func TestMigrateImageStorageMovesTheOldLayout(t *testing.T) {
	t.Parallel()
	fs := oldImageLayout(t)
	db := &fakeImageRewriter{}

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)

	assert.True(t, db.committed)
	assert.Equal(t, legacyLibretro+string(filepath.Separator), db.oldPrefix)
	assert.Equal(t, "images://libretro/", db.newPrefix)
	assert.Equal(t, string(filepath.Separator), db.sep)
	assert.True(t, imagePathExists(t, fs, filepath.Join(newLibretro, "NES", "Named_Boxarts", "A.png")))
	assert.True(t, imagePathExists(t, fs, filepath.Join(newThumbs, "v3", "ab", "abcd.webp")))
	assert.False(t, imagePathExists(t, fs, legacyLibretro))
	assert.False(t, imagePathExists(t, fs, legacyThumbs))

	// Running again finds nothing left to move.
	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)
	assert.Equal(t, 1, db.calls)
	assert.True(t, imagePathExists(t, fs, filepath.Join(newLibretro, "NES", "Named_Boxarts", "A.png")))
}

// A database that never recorded the move, with the files already moved, gets
// its old paths rewritten once and the files stay where they are.
func TestMigrateImageStorageRepairsPathsLeftByAnEarlierMove(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	moved := filepath.Join(newLibretro, "NES", "Named_Boxarts", "A.png")
	require.NoError(t, afero.WriteFile(fs, moved, []byte("png"), 0o600))
	db := &fakeImageRewriter{}

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)

	assert.Equal(t, 1, db.calls)
	assert.True(t, db.committed)
	assert.Equal(t, legacyLibretro+string(filepath.Separator), db.oldPrefix)
	assert.Equal(t, "images://libretro/", db.newPrefix)
	assert.True(t, imagePathExists(t, fs, moved))

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)
	assert.Equal(t, 1, db.calls, "the marker stops a second scan")
}

// Stored paths into the old folder still load while it exists, and with no
// new folder there was no move to repair.
func TestMigrateImageStorageRepairNeedsOnlyTheNewFolder(t *testing.T) {
	t.Parallel()

	t.Run("both folders exist", func(t *testing.T) {
		t.Parallel()
		fs := oldImageLayout(t)
		require.NoError(t, fs.MkdirAll(newLibretro, 0o750))
		db := &fakeImageRewriter{}
		migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)
		assert.Zero(t, db.calls)
	})
	t.Run("neither folder exists", func(t *testing.T) {
		t.Parallel()
		db := &fakeImageRewriter{}
		migrateImageStorage(context.Background(), afero.NewMemMapFs(), migrationDataDir, migrationImageDir, db)
		assert.Zero(t, db.calls)
	})
	t.Run("the marker cannot be read", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(newLibretro, 0o750))
		db := &fakeImageRewriter{markerErr: errors.New("database is locked")}
		migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)
		assert.Zero(t, db.calls)
	})
}

// A destination that already exists is never merged into or replaced.
func TestMigrateImageStorageLeavesAnExistingDestination(t *testing.T) {
	t.Parallel()
	fs := oldImageLayout(t)
	mine := filepath.Join(newLibretro, "NES", "Named_Boxarts", "Mine.png")
	require.NoError(t, afero.WriteFile(fs, mine, []byte("mine"), 0o600))
	db := &fakeImageRewriter{}

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)

	assert.Zero(t, db.calls)
	assert.True(t, imagePathExists(t, fs, legacyBoxart))
	assert.True(t, imagePathExists(t, fs, mine))
	assert.False(t, imagePathExists(t, fs, filepath.Join(newLibretro, "NES", "Named_Boxarts", "A.png")))
	assert.True(t, imagePathExists(t, fs, filepath.Join(newThumbs, "v3", "ab", "abcd.webp")),
		"the thumbnail cache moves on its own")
}

// A rename that fails, such as across filesystems, leaves the files and the
// database alone.
func TestMigrateImageStorageFailedRename(t *testing.T) {
	t.Parallel()
	fs := failRenameFs{oldImageLayout(t)}
	db := &fakeImageRewriter{}

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)

	assert.Equal(t, 1, db.calls)
	assert.False(t, db.committed, "the rewrite is rolled back")
	assert.True(t, imagePathExists(t, fs, legacyBoxart))
	assert.True(t, imagePathExists(t, fs, legacyThumb))
	assert.False(t, imagePathExists(t, fs, newLibretro))
	assert.False(t, imagePathExists(t, fs, newThumbs))
}

func TestMigrateImageStorageFailedRewrite(t *testing.T) {
	t.Parallel()
	fs := oldImageLayout(t)
	db := &fakeImageRewriter{beforeErr: errors.New("database is locked")}

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)

	assert.True(t, imagePathExists(t, fs, legacyBoxart), "files are not moved without their paths")
	assert.False(t, imagePathExists(t, fs, newLibretro))
}

// A commit that fails after the move puts the files back where the stored
// paths point.
func TestMigrateImageStorageFailedCommitMovesFilesBack(t *testing.T) {
	t.Parallel()
	fs := oldImageLayout(t)
	db := &fakeImageRewriter{commitErr: errors.New("disk I/O error")}

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)

	assert.True(t, imagePathExists(t, fs, legacyBoxart))
	assert.False(t, imagePathExists(t, fs, newLibretro))

	// The next start tries again.
	db.commitErr = nil
	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, db)
	assert.True(t, db.committed)
	assert.True(t, imagePathExists(t, fs, filepath.Join(newLibretro, "NES", "Named_Boxarts", "A.png")))
}

func TestMigrateImageStorageWithoutAMediaDatabase(t *testing.T) {
	t.Parallel()
	fs := oldImageLayout(t)

	migrateImageStorage(context.Background(), fs, migrationDataDir, migrationImageDir, nil)

	assert.True(t, imagePathExists(t, fs, legacyBoxart))
	assert.True(t, imagePathExists(t, fs, filepath.Join(newThumbs, "v3", "ab", "abcd.webp")))
}
