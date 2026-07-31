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
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeMediaTagsUpdateEnv(t *testing.T, mockMediaDB *testhelpers.MockMediaDBI, params string) requests.RequestEnv {
	t.Helper()
	userDB, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	return requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB, UserDB: userDB},
		Params:   []byte(params),
	}
}

func TestHandleMediaTagsUpdate_AddsFavoriteTag(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := database.MediaFullRow{
		Media: database.Media{DBID: 1},
		Title: database.MediaTitle{DBID: 10},
		System: database.System{
			DBID:     100,
			SystemID: "NES",
			Name:     "NES",
		},
	}
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()
	mockDB.On(
		"UpdateMediaTags",
		mock.Anything,
		int64(1),
		[]database.MediaTagRef(nil),
		[]database.MediaTagRef{{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)}},
	).Return(nil).Once()
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).
		Return([]database.TagInfo{{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)}}, nil).Once()
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).
		Return([]database.TagInfo{}, nil).Once()

	result, err := HandleMediaTagsUpdate(makeMediaTagsUpdateEnv(t, mockDB, `{"mediaId":1,"add":["user:favorite"]}`))
	require.NoError(t, err)

	resp, ok := result.(models.TagsResponse)
	require.True(t, ok)
	assert.Equal(t, []database.TagInfo{{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)}}, resp.Tags)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaTagsUpdate_RejectsSearchOperators(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	_, err := HandleMediaTagsUpdate(makeMediaTagsUpdateEnv(t, mockDB, `{"mediaId":1,"add":["~user:favorite"]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tag operators are not allowed")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaTagsUpdate_RejectsEmptyTags(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	_, err := HandleMediaTagsUpdate(makeMediaTagsUpdateEnv(t, mockDB, `{"mediaId":1,"add":[" "]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tag cannot be empty")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaTagsUpdate_RejectsUnsupportedTags(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	_, err := HandleMediaTagsUpdate(makeMediaTagsUpdateEnv(t, mockDB, `{"mediaId":1,"add":["genre:platform"]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only user:favorite can be mutated")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaTagsUpdate_ReturnsProjectionError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: mediaTagsUpdateRow()}, nil).Once()
	mockDB.On(
		"UpdateMediaTags",
		mock.Anything,
		int64(1),
		[]database.MediaTagRef(nil),
		[]database.MediaTagRef{{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)}},
	).Return(errors.New("projection failed")).Once()

	_, err := HandleMediaTagsUpdate(makeMediaTagsUpdateEnv(t, mockDB, `{"mediaId":1,"add":["user:favorite"]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update media tag projection")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaTagsUpdate_RemovesFavoriteTag(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: mediaTagsUpdateRow()}, nil).Once()
	mockDB.On(
		"UpdateMediaTags",
		mock.Anything,
		int64(1),
		[]database.MediaTagRef{{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserFavorite)}},
		[]database.MediaTagRef(nil),
	).Return(nil).Once()
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).
		Return([]database.TagInfo{}, nil).Once()
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).
		Return([]database.TagInfo{}, nil).Once()

	result, err := HandleMediaTagsUpdate(makeMediaTagsUpdateEnv(
		t, mockDB, `{"mediaId":1,"remove":["user:favorite"]}`,
	))
	require.NoError(t, err)
	assertTagsDoNotContainFavorite(t, result)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaTagsUpdate_RealMediaDBFavoriteFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	favoritePath := filepath.Join("roms", "NES", "Favorite Game.nes")
	otherPath := filepath.Join("roms", "NES", "Other Game.nes")
	mediaIDs := addTestMediaPaths(t, mediaDB, favoritePath, otherPath)
	favoriteID := mediaIDs[0]
	otherID := mediaIDs[1]

	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	baseEnv := requests.RequestEnv{
		Context:  ctx,
		Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
	}

	addParams := fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite"]}`, favoriteID)
	result, err := HandleMediaTagsUpdate(withParams(&baseEnv, addParams))
	require.NoError(t, err)
	assertTagsContainFavorite(t, result)

	searchResult := searchByTags(t, &baseEnv, []string{"user:favorite"})
	require.Len(t, searchResult.Results, 1)
	assert.Equal(t, favoriteID, searchResult.Results[0].MediaID)
	assert.Contains(t, searchResult.Results[0].Tags, database.TagInfo{
		Type: string(tags.TagTypeUser),
		Tag:  string(tags.TagUserFavorite),
	})

	searchResult = searchByTags(t, &baseEnv, []string{"-user:favorite"})
	require.Len(t, searchResult.Results, 1)
	assert.Equal(t, otherID, searchResult.Results[0].MediaID)

	tagsResult, err := HandleMediaTags(baseEnv)
	require.NoError(t, err)
	assertTagsContainFavorite(t, tagsResult)

	removeParams := fmt.Sprintf(`{"mediaId":%d,"remove":["user:favorite"]}`, favoriteID)
	result, err = HandleMediaTagsUpdate(withParams(&baseEnv, removeParams))
	require.NoError(t, err)
	assertTagsDoNotContainFavorite(t, result)

	searchResult = searchByTags(t, &baseEnv, []string{"user:favorite"})
	assert.Empty(t, searchResult.Results)

	result, err = HandleMediaTagsUpdate(withParams(
		&baseEnv,
		fmt.Sprintf(`{"system":"NES","path":%q,"add":["user:favorite"]}`, filepath.ToSlash(favoritePath)),
	))
	require.NoError(t, err)
	assertTagsContainFavorite(t, result)
}

func mediaTagsUpdateRow() database.MediaFullRow {
	return database.MediaFullRow{
		Media: database.Media{DBID: 1},
		Title: database.MediaTitle{DBID: 10},
		System: database.System{
			DBID:     100,
			SystemID: "NES",
			Name:     "NES",
		},
	}
}

func addTestMediaPaths(t *testing.T, mediaDB database.MediaDBI, paths ...string) []int64 {
	t.Helper()

	scantest.IndexMediaPaths(t, mediaDB, "NES", paths...)

	rows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	byPath := make(map[string]int64, len(rows))
	for _, row := range rows {
		byPath[row.Path] = row.DBID
	}
	mediaIDs := make([]int64, 0, len(paths))
	for _, path := range paths {
		dbid, ok := byPath[pathutil.CanonicalMediaPath(path)]
		require.True(t, ok, "indexed media not found for path %s", path)
		mediaIDs = append(mediaIDs, dbid)
	}
	return mediaIDs
}

func withParams(env *requests.RequestEnv, params string) requests.RequestEnv {
	next := *env
	next.Params = []byte(params)
	return next
}

func searchByTags(t *testing.T, env *requests.RequestEnv, rawTags []string) models.SearchResults {
	t.Helper()

	params := models.SearchParams{Tags: &rawTags}
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	result, err := HandleMediaSearch(withParams(env, string(paramsJSON)))
	require.NoError(t, err)
	searchResult, ok := result.(models.SearchResults)
	require.True(t, ok)

	return searchResult
}

func assertTagsContainFavorite(t *testing.T, result any) {
	t.Helper()

	resp, ok := result.(models.TagsResponse)
	require.True(t, ok)
	assert.True(t, hasFavoriteTag(resp.Tags), "expected favorite tag in %+v", resp.Tags)
}

func assertTagsDoNotContainFavorite(t *testing.T, result any) {
	t.Helper()

	resp, ok := result.(models.TagsResponse)
	require.True(t, ok)
	assert.False(t, hasFavoriteTag(resp.Tags), "expected no favorite tag in %+v", resp.Tags)
}

func hasFavoriteTag(tagList []database.TagInfo) bool {
	for _, tag := range tagList {
		if tag.Type == string(tags.TagTypeUser) && tag.Tag == string(tags.TagUserFavorite) {
			return true
		}
	}
	return false
}
