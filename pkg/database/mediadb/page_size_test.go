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

package mediadb

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mediaDBPageSizeOf(t *testing.T, db *MediaDB) int64 {
	t.Helper()
	var pageSize int64
	require.NoError(t, db.sql.Load().QueryRowContext(context.Background(), "PRAGMA page_size").Scan(&pageSize))
	return pageSize
}

func TestOpenMediaDB_NewDatabaseUsesConfiguredPageSize(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform")
	db := &MediaDB{pl: pl, ctx: context.Background(), dbPath: filepath.Join(t.TempDir(), "media.db")}
	require.NoError(t, db.Open())
	t.Cleanup(func() { _ = db.Close() })

	assert.Equal(t, int64(mediaDBPageSize), mediaDBPageSizeOf(t, db))
	var journalMode string
	require.NoError(t, db.sql.Load().QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode))
	assert.Equal(t, "wal", journalMode)

	// A rebuild replaces the file, so it picks the page size up too.
	require.NoError(t, db.Recreate(false))
	assert.Equal(t, int64(mediaDBPageSize), mediaDBPageSizeOf(t, db))
}

func TestOpenMediaDB_ExistingDatabaseKeepsItsPageSize(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform")
	path := filepath.Join(t.TempDir(), "media.db")

	db := &MediaDB{pl: pl, ctx: context.Background(), dbPath: path}
	require.NoError(t, db.Open())
	_, err := db.sql.Load().ExecContext(context.Background(),
		"PRAGMA journal_mode=DELETE; PRAGMA page_size=4096; VACUUM")
	require.NoError(t, err)
	require.Equal(t, int64(4096), mediaDBPageSizeOf(t, db))
	require.NoError(t, db.Close())

	reopened := &MediaDB{pl: pl, ctx: context.Background(), dbPath: path}
	require.NoError(t, reopened.Open())
	t.Cleanup(func() { _ = reopened.Close() })
	assert.Equal(t, int64(4096), mediaDBPageSizeOf(t, reopened))
	assert.True(t, reopened.StoragePageSizeOutdated())

	// Replacing the file is what brings an older database to the current size.
	require.NoError(t, reopened.Recreate(false))
	assert.Equal(t, int64(mediaDBPageSize), mediaDBPageSizeOf(t, reopened))
	assert.False(t, reopened.StoragePageSizeOutdated())
}
