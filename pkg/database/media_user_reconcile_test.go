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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reconcileFixture struct {
	db  *database.Database
	ids map[string]int64
}

// newReconcileFixture indexes the named NES files and returns their media IDs
// by name.
func newReconcileFixture(t *testing.T, names ...string) *reconcileFixture {
	t.Helper()
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, nesPath(name))
	}
	scantest.IndexMediaPaths(t, mediaDB, "NES", paths...)
	rows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, len(names))
	f := &reconcileFixture{
		db:  &database.Database{MediaDB: mediaDB, UserDB: userDB},
		ids: make(map[string]int64, len(names)),
	}
	for i := range rows {
		f.ids[filepath.Base(rows[i].Path)] = rows[i].DBID
	}
	return f
}

func nesPath(name string) string {
	return filepath.Join("roms", "NES", name)
}

// projectedOverrides returns the launcher override media.db holds per file name.
func (f *reconcileFixture) projectedOverrides(t *testing.T) map[string]string {
	t.Helper()
	rows, err := f.db.MediaDB.GetExistingMediaUserData(context.Background())
	require.NoError(t, err)
	got := make(map[string]string)
	for i := range rows {
		if rows[i].LauncherOverride != "" {
			got[filepath.Base(rows[i].Path)] = rows[i].LauncherOverride
		}
	}
	return got
}

// flags returns the user flags media.db holds for the file, deck tags aside.
func (f *reconcileFixture) flags(t *testing.T, name string) map[database.MediaUserFlag]bool {
	t.Helper()
	got := projectedUserTags(t, f.db, f.ids[name])
	for flag := range got {
		if !tags.IsMutableUserTag(tags.TagValue(flag)) {
			delete(got, flag)
		}
	}
	return got
}

// otherTags returns the file's tags outside the five user flags.
func (f *reconcileFixture) otherTags(t *testing.T, name string) []string {
	t.Helper()
	fileTags, err := f.db.MediaDB.GetMediaTagsByMediaDBID(context.Background(), f.ids[name])
	require.NoError(t, err)
	var got []string
	for _, tag := range fileTags {
		if tag.Type == string(tags.TagTypeUser) && tags.IsMutableUserTag(tags.TagValue(tag.Tag)) {
			continue
		}
		got = append(got, tag.Type+":"+tag.Tag)
	}
	return got
}

func (f *reconcileFixture) revision(t *testing.T) string {
	t.Helper()
	revision, err := f.db.MediaDB.MediaPreferencesRevision(context.Background())
	require.NoError(t, err)
	return revision
}

// A restore replaces UserDB under a projection built from the old one. The
// reconcile must remove what the restored database does not hold as well as
// add what it does, for every flag and for launcher overrides.
func TestReconcileMediaUserDataMatchesReplacedUserDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newReconcileFixture(t, "Stale (USA).nes", "Restored (USA).nes", "Changed (USA).nes", "Kept (USA).nes")

	// The projection of the database that was replaced.
	for _, flag := range database.MediaUserFlags {
		require.NoError(t, f.db.MediaDB.UpdateMediaTags(ctx, f.ids["Stale (USA).nes"], nil,
			[]database.MediaTagRef{{Type: string(tags.TagTypeUser), Tag: string(flag)}}))
	}
	keptTags := []database.MediaTagRef{
		{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)},
		{Type: string(tags.TagTypeUser), Tag: string(tags.DeckTag("0123456789ab"))},
	}
	require.NoError(t, f.db.MediaDB.UpdateMediaTags(ctx, f.ids["Kept (USA).nes"], nil, keptTags))
	require.NoError(t, f.db.MediaDB.UpdateMediaTags(ctx, f.ids["Stale (USA).nes"], nil, keptTags[1:]))
	require.NoError(t, database.ApplyMediaUserLauncherOverride(
		ctx, f.db, "NES", nesPath("Stale (USA).nes"), f.ids["Stale (USA).nes"], "OldLauncher"))
	require.NoError(t, database.ApplyMediaUserLauncherOverride(
		ctx, f.db, "NES", nesPath("Changed (USA).nes"), f.ids["Changed (USA).nes"], "OldLauncher"))
	staleOther := f.otherTags(t, "Stale (USA).nes")
	require.Contains(t, staleOther, "user:deck:0123456789ab")
	require.Greater(t, len(staleOther), 1, "the file carries metadata tags too")

	// The restored database.
	require.NoError(t, f.db.UserDB.DeleteMediaUserData("NES", nesPath("Stale (USA).nes")))
	require.NoError(t, f.db.UserDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: nesPath("Restored (USA).nes"),
		IsFavorite: true, IsHidden: true, IsLiked: true, IsPlayLater: true, LauncherOverride: "NewLauncher",
	}))
	require.NoError(t, f.db.UserDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: nesPath("Changed (USA).nes"), IsDisliked: true, LauncherOverride: "NewLauncher",
	}))
	require.NoError(t, f.db.UserDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: nesPath("Kept (USA).nes"), IsFavorite: true,
	}))
	require.NoError(t, f.db.UserDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "NES", Path: nesPath("Not Indexed (USA).nes"), IsFavorite: true, LauncherOverride: "NewLauncher",
	}))
	require.NoError(t, f.db.UserDB.UpsertMediaUserData(&database.MediaUserData{
		SystemID: "SNES", Path: filepath.Join("roms", "SNES", "Other.sfc"), IsFavorite: true,
	}))

	before := f.revision(t)
	require.NoError(t, database.ReconcileMediaUserData(ctx, f.db))

	assert.Empty(t, f.flags(t, "Stale (USA).nes"), "every stale flag is removed")
	assert.Equal(t, map[database.MediaUserFlag]bool{
		database.MediaUserFlagFavorite: true, database.MediaUserFlagHidden: true,
		database.MediaUserFlagLiked: true, database.MediaUserFlagPlayLater: true,
	}, f.flags(t, "Restored (USA).nes"))
	assert.Equal(t, map[database.MediaUserFlag]bool{database.MediaUserFlagDisliked: true},
		f.flags(t, "Changed (USA).nes"))
	assert.Equal(t, map[database.MediaUserFlag]bool{database.MediaUserFlagFavorite: true},
		f.flags(t, "Kept (USA).nes"))
	assert.Equal(t, map[string]string{
		"Restored (USA).nes": "NewLauncher",
		"Changed (USA).nes":  "NewLauncher",
	}, f.projectedOverrides(t))
	assert.ElementsMatch(t, staleOther, f.otherTags(t, "Stale (USA).nes"),
		"deck and metadata tags are not the reconcile's to touch")
	assert.NotEqual(t, before, f.revision(t), "browse cursors built on the old flags are invalidated")

	settled := f.revision(t)
	require.NoError(t, database.ReconcileMediaUserData(ctx, f.db))
	assert.Equal(t, settled, f.revision(t), "a projection already in line is not written again")
}

// A backup taken before any flag was set restores an empty table, and the
// projection has to end up empty with it.
func TestReconcileMediaUserDataEmptyUserDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newReconcileFixture(t, "Game (USA).nes")
	path, id := nesPath("Game (USA).nes"), f.ids["Game (USA).nes"]

	_, err := database.ApplyMediaUserFlags(ctx, f.db, "NES", path, id, map[database.MediaUserFlag]bool{
		database.MediaUserFlagFavorite: true, database.MediaUserFlagHidden: true,
	})
	require.NoError(t, err)
	require.NoError(t, database.ApplyMediaUserLauncherOverride(ctx, f.db, "NES", path, id, "RetroArch"))
	require.NoError(t, f.db.UserDB.DeleteMediaUserData("NES", path))

	require.NoError(t, database.ReconcileMediaUserData(ctx, f.db))

	assert.Empty(t, f.flags(t, "Game (USA).nes"))
	assert.Empty(t, f.projectedOverrides(t))
}

// A media database that never stored a flag or an override has no tags for
// them, which is not an error.
func TestReconcileMediaUserDataNothingProjected(t *testing.T) {
	t.Parallel()
	f := newReconcileFixture(t, "Game (USA).nes")

	require.NoError(t, database.ReconcileMediaUserData(context.Background(), f.db))

	assert.Empty(t, f.flags(t, "Game (USA).nes"))
	assert.Empty(t, f.projectedOverrides(t))
}

func TestApplyMediaUserLauncherOverrideSetsAndClears(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newReconcileFixture(t, "Game (USA).nes")
	path, id := nesPath("Game (USA).nes"), f.ids["Game (USA).nes"]

	require.NoError(t, database.ApplyMediaUserLauncherOverride(ctx, f.db, "NES", path, id, "RetroArch"))
	row, found, err := f.db.UserDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "RetroArch", row.LauncherOverride)
	assert.Equal(t, map[string]string{"Game (USA).nes": "RetroArch"}, f.projectedOverrides(t))

	require.NoError(t, database.ApplyMediaUserLauncherOverride(ctx, f.db, "NES", path, id, ""))
	_, found, err = f.db.UserDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	assert.False(t, found, "a row with no intent left is deleted")
	assert.Empty(t, f.projectedOverrides(t))

	require.NoError(t, database.ApplyMediaUserLauncherOverride(ctx, f.db, "NES", path, 0, "RetroArch"))
	assert.Empty(t, f.projectedOverrides(t), "a media ID of 0 skips the projection")
}
