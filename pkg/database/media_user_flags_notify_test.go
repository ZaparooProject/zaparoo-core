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
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flagChange struct {
	system string
	path   string
	tags   []database.TagInfo
}

func notifyingDB(t *testing.T) (db *database.Database, changes *[]flagChange) {
	t.Helper()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	changes = &[]flagChange{}
	db = &database.Database{MediaDB: mediaDB, UserDB: userDB}
	db.OnMediaUserFlagsChanged = func(systemID, path string, data *database.MediaUserData) {
		*changes = append(*changes, flagChange{system: systemID, path: path, tags: data.UserTags()})
	}
	return db, changes
}

func TestApplyMediaUserFlagsTellsTheListenerWhatChanged(t *testing.T) {
	t.Parallel()
	db, changes := notifyingDB(t)
	path := filepath.Join("roms", "NES", "Game.nes")

	_, err := database.ApplyMediaUserFlags(context.Background(), db, "NES", path, 0,
		map[database.MediaUserFlag]bool{database.MediaUserFlagLiked: true, database.MediaUserFlagPlayLater: true})
	require.NoError(t, err)
	require.Len(t, *changes, 1)
	assert.Equal(t, "NES", (*changes)[0].system)
	assert.Equal(t, path, (*changes)[0].path)
	assert.Equal(t, []database.TagInfo{{Type: "user", Tag: "liked"}, {Type: "user", Tag: "playlater"}},
		(*changes)[0].tags)

	// Disliking clears the like, and the listener gets the full set after it.
	_, err = database.ApplyMediaUserFlags(context.Background(), db, "NES", path, 0,
		map[database.MediaUserFlag]bool{database.MediaUserFlagDisliked: true})
	require.NoError(t, err)
	require.Len(t, *changes, 2)
	assert.Equal(t, []database.TagInfo{{Type: "user", Tag: "disliked"}, {Type: "user", Tag: "playlater"}},
		(*changes)[1].tags)

	// An edit that changes nothing says nothing.
	_, err = database.ApplyMediaUserFlags(context.Background(), db, "NES", path, 0,
		map[database.MediaUserFlag]bool{database.MediaUserFlagDisliked: true})
	require.NoError(t, err)
	assert.Len(t, *changes, 2)

	// Clearing the last flags reports an empty set.
	_, err = database.ApplyMediaUserFlags(context.Background(), db, "NES", path, 0,
		map[database.MediaUserFlag]bool{database.MediaUserFlagDisliked: false, database.MediaUserFlagPlayLater: false})
	require.NoError(t, err)
	require.Len(t, *changes, 3)
	assert.Empty(t, (*changes)[2].tags)
	assert.NotNil(t, (*changes)[2].tags, "an empty set is a list, not null")
}

func TestApplyDirectoryHiddenTellsTheListener(t *testing.T) {
	t.Parallel()
	db, changes := notifyingDB(t)
	dir := filepath.Join("roms", "NES", "Folder")
	scantest.IndexMediaPaths(t, db.MediaDB, "NES", filepath.Join(dir, "Game.nes"))

	_, err := database.ApplyDirectoryHidden(context.Background(), db, "NES", dir, true)
	require.NoError(t, err)
	require.Len(t, *changes, 1)
	assert.Equal(t, []database.TagInfo{{Type: "user", Tag: "hidden"}}, (*changes)[0].tags)
}
