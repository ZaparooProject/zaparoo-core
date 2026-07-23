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

package methods

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// favouriteRow is a resolved media row carrying the (SystemID, Path) key the
// UserDB write needs.
func favouriteRow(path string) database.MediaFullRow {
	return database.MediaFullRow{
		Media:  database.Media{DBID: 1, Path: path},
		Title:  database.MediaTitle{DBID: 10},
		System: database.System{DBID: 100, SystemID: "NES", Name: "NES"},
	}
}

// TestMediaTagsUpdate_PersistsUserDBTruthWhenProjectionFails proves the truth-first
// ordering: the UserDB favourite is saved even though the media.db projection write
// then fails, so the next reindex can re-materialize it.
func TestMediaTagsUpdate_PersistsUserDBTruthWhenProjectionFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	userDB, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)

	path := filepath.Join("roms", "NES", "Game.nes")
	mockDB := testhelpers.NewMockMediaDBI()
	// The identity snapshot after a user-data write is best-effort; an empty
	// search result means no snapshot and is not part of what these tests prove.
	mockDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.SearchResult{}, nil).Maybe()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: favouriteRow(path)}, nil).Once()
	mockDB.On("BeginTransaction", false).Return(errors.New("projection boom")).Once()

	env := requests.RequestEnv{
		Context:  ctx,
		Database: &database.Database{MediaDB: mockDB, UserDB: userDB},
		Params:   []byte(`{"mediaId":1,"add":["user:favorite"]}`),
	}
	_, err := HandleMediaTagsUpdate(env)
	require.Error(t, err)

	data, found, getErr := userDB.GetMediaUserData("NES", path)
	require.NoError(t, getErr)
	require.True(t, found, "UserDB truth must persist despite projection failure")
	assert.True(t, data.IsFavorite)
	mockDB.AssertExpectations(t)
}

// TestMediaMetaUpdate_PersistsUserDBTruthWhenProjectionFails proves the same for the
// launcher-override path.
func TestMediaMetaUpdate_PersistsUserDBTruthWhenProjectionFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	userDB, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)

	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{{ID: "RetroArch", SystemID: "NES"}})

	path := filepath.Join("roms", "NES", "Game.nes")
	mockDB := testhelpers.NewMockMediaDBI()
	// The identity snapshot after a user-data write is best-effort; an empty
	// search result means no snapshot and is not part of what these tests prove.
	mockDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.SearchResult{}, nil).Maybe()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: favouriteRow(path)}, nil).Once()
	mockDB.On("FindOrInsertTagType", database.TagType{
		Type:        string(tags.TagTypeProperty),
		IsExclusive: false,
	}).Return(database.TagType{}, errors.New("projection boom")).Once()

	env := requests.RequestEnv{
		Context:       ctx,
		Database:      &database.Database{MediaDB: mockDB, UserDB: userDB},
		LauncherCache: launcherCache,
		Params:        []byte(`{"mediaId":1,"media":{"launcherOverride":"retroarch"}}`),
	}
	_, err := HandleMediaMetaUpdate(env)
	require.Error(t, err)

	data, found, getErr := userDB.GetMediaUserData("NES", path)
	require.NoError(t, getErr)
	require.True(t, found, "UserDB truth must persist despite projection failure")
	assert.Equal(t, "RetroArch", data.LauncherOverride)
	mockDB.AssertExpectations(t)
}

// TestMediaTagsUpdate_UserDBTruthAddThenRemove verifies the favourite truth is
// written on add and deleted on remove, exercised against real databases.
func TestMediaTagsUpdate_UserDBTruthAddThenRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	gamePath := filepath.Join("roms", "NES", "Favorite Game.nes")
	mediaIDs := addTestMediaPaths(t, mediaDB, gamePath)

	baseEnv := requests.RequestEnv{
		Context:  ctx,
		Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
	}

	_, err := HandleMediaTagsUpdate(withParams(&baseEnv,
		fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite"]}`, mediaIDs[0])))
	require.NoError(t, err)

	data, found, err := userDB.GetMediaUserData("NES", gamePath)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, data.IsFavorite)

	_, err = HandleMediaTagsUpdate(withParams(&baseEnv,
		fmt.Sprintf(`{"mediaId":%d,"remove":["user:favorite"]}`, mediaIDs[0])))
	require.NoError(t, err)

	_, found, err = userDB.GetMediaUserData("NES", gamePath)
	require.NoError(t, err)
	assert.False(t, found, "row with no favourite and no override is deleted")
}
