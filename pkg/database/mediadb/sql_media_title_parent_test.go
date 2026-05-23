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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupParentTestDB seeds three canonical titles and two media rows.
//
//	System NES (DBID=1)
//	  Title A  (DBID=1, slug="mario")           ← media mario.nes (DBID=1)
//	  Title B  (DBID=2, slug="mario-usa")        ← media mario-usa.nes (DBID=2)
//	  Title C  (DBID=3, slug="mario-world")      ← no media
func setupParentTestDB(t *testing.T) (db *MediaDB, cleanup func()) {
	t.Helper()
	db, cleanup = setupTempMediaDB(t)
	ctx := context.Background()

	pathA := filepath.ToSlash(filepath.Join("roms", "mario.nes"))
	pathB := filepath.ToSlash(filepath.Join("roms", "mario-usa.nes"))

	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'NES', 'Nintendo');
		INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES
			(1, 1, 'mario',       'Super Mario Bros'),
			(2, 1, 'mario-usa',   'Super Mario Bros (USA)'),
			(3, 1, 'mario-world', 'Super Mario Bros (World)');
		INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path) VALUES
			(1, 1, 1, ?),
			(2, 2, 1, ?);
	`, pathA, pathB)
	require.NoError(t, err)

	return db, cleanup
}

// --- IsMediaTitleCanonical / IsMediaTitleAlias ---

func TestMediaTitle_IsMediaTitleCanonical(t *testing.T) {
	t.Parallel()
	assert.True(t, database.IsMediaTitleCanonical(&database.MediaTitle{ParentDBID: 0}))
	assert.False(t, database.IsMediaTitleCanonical(&database.MediaTitle{ParentDBID: 5}))
}

func TestMediaTitle_IsMediaTitleAlias(t *testing.T) {
	t.Parallel()
	assert.True(t, database.IsMediaTitleAlias(&database.MediaTitle{ParentDBID: 5}))
	assert.False(t, database.IsMediaTitleAlias(&database.MediaTitle{ParentDBID: 0}))
}

// --- ResolveCanonical ---

func TestResolveCanonical_OnCanonical(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	got, err := db.ResolveCanonical(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.DBID)
	assert.Equal(t, int64(0), got.ParentDBID)
}

func TestResolveCanonical_OnAlias(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	require.NoError(t, db.SetParentTitle(context.Background(), 2, 1))

	got, err := db.ResolveCanonical(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.DBID, "alias should resolve to canonical DBID 1")
}

// --- GetAliasesOf ---

func TestGetAliasesOf_Empty(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	aliases, err := db.GetAliasesOf(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, aliases)
}

func TestGetAliasesOf_WithAliases(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	require.NoError(t, db.SetParentTitle(context.Background(), 2, 1))
	require.NoError(t, db.SetParentTitle(context.Background(), 3, 1))

	aliases, err := db.GetAliasesOf(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, aliases, 2)
	for _, a := range aliases {
		assert.Equal(t, int64(1), a.ParentDBID)
	}
}

// --- SetParentTitle ---

func TestSetParentTitle_HappyPath(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	err := db.SetParentTitle(context.Background(), 2, 1)
	require.NoError(t, err)

	title, err := db.ResolveCanonical(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), title.DBID)
}

func TestSetParentTitle_SelfReference(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	err := db.SetParentTitle(context.Background(), 1, 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSelfReference)
}

func TestSetParentTitle_TargetIsMediaTitleAlias(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	require.NoError(t, db.SetParentTitle(context.Background(), 2, 1))

	// Try to make title C a child of title B (which is now an alias)
	err := db.SetParentTitle(context.Background(), 3, 2)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotCanonical)
}

func TestSetParentTitle_AliasAlreadyHasParent(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	require.NoError(t, db.SetParentTitle(context.Background(), 2, 1))

	// Try to re-parent title B (already an alias) to title C
	err := db.SetParentTitle(context.Background(), 2, 3)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyAlias)
}

func TestSetParentTitle_AliasHasChildren(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	// Make title C a child of title A
	require.NoError(t, db.SetParentTitle(context.Background(), 3, 1))

	// Try to make title A (which now has a child) a child of title B — should fail
	err := db.SetParentTitle(context.Background(), 1, 2)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHasAliases)
}

// --- UnsetParentTitle ---

func TestUnsetParentTitle_HappyPath(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	require.NoError(t, db.SetParentTitle(context.Background(), 2, 1))
	require.NoError(t, db.UnsetParentTitle(context.Background(), 2))

	title, err := db.ResolveCanonical(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), title.DBID, "after unset, title 2 should be its own canonical")
	assert.True(t, database.IsMediaTitleCanonical(&title))
}

func TestUnsetParentTitle_AlreadyCanonical(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	err := db.UnsetParentTitle(context.Background(), 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotCanonical)
}

// --- GetMediaUnderCanonical ---

func TestGetMediaUnderCanonical_DirectOnly(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	media, err := db.GetMediaUnderCanonical(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, media, 1)
	assert.Equal(t, int64(1), media[0].DBID)
}

func TestGetMediaUnderCanonical_IncludesAliasMedia(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	require.NoError(t, db.SetParentTitle(context.Background(), 2, 1))

	media, err := db.GetMediaUnderCanonical(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, media, 2, "canonical + alias media should both be returned")
}

func TestGetMediaUnderCanonical_AutoResolvesAlias(t *testing.T) {
	t.Parallel()
	db, cleanup := setupParentTestDB(t)
	defer cleanup()

	require.NoError(t, db.SetParentTitle(context.Background(), 2, 1))

	// Pass alias DBID — should auto-resolve and return both media
	media, err := db.GetMediaUnderCanonical(context.Background(), 2)
	require.NoError(t, err)
	assert.Len(t, media, 2, "querying via alias DBID should return all media under canonical")
}
