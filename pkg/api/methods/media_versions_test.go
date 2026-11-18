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
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	contraTitle      = int64(10)
	probotectorTitle = int64(11)
	gryzorTitle      = int64(12)
)

func versionRow(id, titleID int64, name, path string) database.SearchResultWithCursor {
	return database.SearchResultWithCursor{
		SystemID: "NES", Name: name, Path: path, MediaID: id, MediaTitleID: titleID,
	}
}

// contraRows is one game under three names: two releases titled Contra and one
// each titled Probotector and Gryzor.
func contraRows() []database.SearchResultWithCursor {
	return []database.SearchResultWithCursor{
		versionRow(101, contraTitle, "Contra", "/roms/NES/Contra (USA).nes"),
		versionRow(102, contraTitle, "Contra", "/roms/NES/Contra (Japan).nes"),
		versionRow(111, probotectorTitle, "Probotector", "/roms/NES/Probotector (Europe).nes"),
		versionRow(121, gryzorTitle, "Gryzor", "/roms/NES/Gryzor (Europe).nes"),
	}
}

func contraGroup() []int64 {
	return []int64{contraTitle, probotectorTitle, gryzorTitle}
}

func versionsEnv(t *testing.T, mediaDB *helpers.MockMediaDBI, params any) requests.RequestEnv {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)
	return requests.RequestEnv{
		Context:  context.Background(),
		Params:   paramsJSON,
		Database: &database.Database{MediaDB: mediaDB},
		Config:   &config.Instance{},
	}
}

func mediaFullRow(mediaID, titleID int64, path string) database.MediaFullRow {
	return database.MediaFullRow{
		System: database.System{DBID: 1, SystemID: "NES"},
		Media:  database.Media{DBID: mediaID, MediaTitleDBID: titleID, Path: path},
		Title:  database.MediaTitle{DBID: titleID},
	}
}

// expectVersionsRead stubs what media.versions reads for mediaID, a row of
// titleID, when the title's group holds group and rows are its media.
func expectVersionsRead(
	mediaDB *helpers.MockMediaDBI,
	mediaID, titleID int64,
	group []int64,
	rows []database.SearchResultWithCursor,
) {
	mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{mediaID}).
		Return(map[int64]database.MediaFullRow{mediaID: mediaFullRow(mediaID, titleID, "")}, nil)
	members := map[int64][]int64{}
	want := []int64{titleID}
	if len(group) > 0 {
		members[titleID] = group
		want = group
	}
	mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{titleID}).Return(members, nil)
	mediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return assert.ObjectsAreEqual(want, filters.TitleDBIDs) && filters.TitleDBID == 0
		})).Return(rows, nil)
}

func callVersions(t *testing.T, env requests.RequestEnv) models.MediaVersionsResponse { //nolint:gocritic // test helper
	t.Helper()
	result, err := HandleMediaVersions(env)
	require.NoError(t, err)
	response, ok := result.(models.MediaVersionsResponse)
	require.True(t, ok)
	return response
}

func versionIDs(versions []models.MediaVersion) []int64 {
	ids := make([]int64, 0, len(versions))
	for i := range versions {
		ids = append(ids, versions[i].MediaID)
	}
	return ids
}

func TestHandleMediaVersions_SameTitle(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	rows := contraRows()[:2]
	expectVersionsRead(mediaDB, 102, contraTitle, nil, rows)

	mediaID := int64(102)
	response := callVersions(t, versionsEnv(t, mediaDB, models.MediaVersionsParams{MediaID: &mediaID}))

	assert.Equal(t, 2, response.Total)
	require.Len(t, response.Versions, 2)
	for i := range response.Versions {
		version := response.Versions[i]
		assert.Equal(t, models.VersionReasonTitle, version.Reason)
		assert.Empty(t, version.Sources)
		assert.Equal(t, version.MediaID == 102, version.Current)
		assert.Equal(t, contraTitle, version.TitleID)
		assert.NotEmpty(t, version.ZapScript)
	}
	require.NotNil(t, response.Pagination)
	assert.False(t, response.Pagination.HasNextPage)
	assert.Nil(t, response.Pagination.NextCursor)
	mediaDB.AssertNotCalled(t, "GetVersionKeysByTitleDBIDs", mock.Anything, mock.Anything)
}

// A game filed under other names lists them too, each with why it belongs.
func TestHandleMediaVersions_LinkedTitles(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	expectVersionsRead(mediaDB, 111, probotectorTitle, contraGroup(), contraRows())
	gameID := database.VersionKey{Type: database.VersionKeyGameDatabaseID, Key: "contra"}
	set := database.VersionKey{Type: database.VersionKeyMAMESet, Key: "contra"}
	mediaDB.On("GetVersionKeysByTitleDBIDs", mock.Anything, contraGroup()).
		Return(map[int64][]database.VersionKey{
			contraTitle:      {gameID, set, {Type: database.VersionKeyAltFolder, Key: "only-contra"}},
			probotectorTitle: {gameID},
			gryzorTitle:      {set},
		}, nil)

	mediaID := int64(111)
	response := callVersions(t, versionsEnv(t, mediaDB, models.MediaVersionsParams{MediaID: &mediaID}))

	assert.Equal(t, 4, response.Total)
	// The asked-about title leads, then the others by name, each by path.
	assert.Equal(t, []int64{111, 102, 101, 121}, versionIDs(response.Versions))

	probotector := response.Versions[0]
	assert.True(t, probotector.Current)
	assert.Equal(t, models.VersionReasonTitle, probotector.Reason)
	assert.Empty(t, probotector.Sources)

	for _, version := range response.Versions[1:3] {
		assert.False(t, version.Current)
		assert.Equal(t, models.VersionReasonLink, version.Reason)
		assert.Equal(t, []string{database.VersionKeyGameDatabaseID, database.VersionKeyMAMESet}, version.Sources,
			"a key no other title holds ties the title to nothing")
	}
	gryzor := response.Versions[3]
	assert.Equal(t, models.VersionReasonLink, gryzor.Reason)
	assert.Equal(t, []string{database.VersionKeyMAMESet}, gryzor.Sources)
}

func TestHandleMediaVersions_BySystemAndPath(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	path := "/roms/NES/Contra (USA).nes"
	mediaDB.On("FindSystemBySystemID", "NES").Return(database.System{DBID: 1, SystemID: "NES"}, nil)
	mediaDB.On("FindMediaBySystemAndPaths", mock.Anything, int64(1), []string{path}).
		Return(map[string]database.Media{path: {DBID: 101, MediaTitleDBID: contraTitle, Path: path}}, nil)
	expectVersionsRead(mediaDB, 101, contraTitle, nil, contraRows()[:2])

	response := callVersions(t, versionsEnv(t, mediaDB, models.MediaVersionsParams{System: "NES", Path: path}))
	require.Len(t, response.Versions, 2)
	assert.Equal(t, int64(101), response.Versions[0].MediaID)
	assert.True(t, response.Versions[0].Current)
}

// An alias is the same file under another folder, so it gives way to its
// target, and asking about the alias marks the target.
func TestHandleMediaVersions_AliasFoldsIntoItsTarget(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	rows := []database.SearchResultWithCursor{
		versionRow(201, 20, "ASO", "/media/fat/_Arcade/ASO.mra"),
		versionRow(202, 20, "ASO", "/media/fat/_Arcade/_Organized/_A-E/ASO.mra"),
		versionRow(203, 20, "ASO", "/media/fat/_Arcade/_alternatives/_ASO/ASO (Japan).mra"),
	}
	expectVersionsRead(mediaDB, 202, 20, nil, rows)
	mediaDB.On("GetMediaLinkTargets", mock.Anything, []int64{201, 202, 203}).
		Return(map[int64]int64{202: 201}, nil)
	mediaDB.On("GetMediaLinkTarget", mock.Anything, int64(202)).Return(int64(201), true, nil)

	mediaID := int64(202)
	response := callVersions(t, versionsEnv(t, mediaDB, models.MediaVersionsParams{MediaID: &mediaID}))

	assert.Equal(t, 2, response.Total)
	assert.ElementsMatch(t, []int64{201, 203}, versionIDs(response.Versions))
	for i := range response.Versions {
		assert.Equal(t, response.Versions[i].MediaID == 201, response.Versions[i].Current)
	}
}

// Asking about a track marks the cue it folds into as the current version.
func TestHandleMediaVersions_ContainerMemberMarksItsLaunchFile(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	track := database.MediaFullRow{
		System: database.System{DBID: 2, SystemID: "PSX"},
		Media:  database.Media{DBID: 21, MediaTitleDBID: 20, Path: discTrack1},
		Title:  database.MediaTitle{DBID: 20},
	}
	mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{21}).
		Return(map[int64]database.MediaFullRow{21: track}, nil)
	mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{20}).Return(map[int64][]int64{}, nil)
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).Return(discRows(), nil)
	mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
		Return(&database.Media{DBID: 22}, nil)
	mediaDB.On("GetMediaLinkTarget", mock.Anything, int64(21)).Return(int64(0), false, nil)

	mediaID := int64(21)
	response := callVersions(t, versionsEnv(t, mediaDB, models.MediaVersionsParams{MediaID: &mediaID}))

	assert.ElementsMatch(t, []int64{22, 24}, versionIDs(response.Versions))
	for i := range response.Versions {
		assert.Equal(t, response.Versions[i].MediaID == 22, response.Versions[i].Current,
			"media %d", response.Versions[i].MediaID)
	}
}

func TestHandleMediaVersions_Paging(t *testing.T) {
	t.Parallel()
	newDB := func() *helpers.MockMediaDBI {
		mediaDB := helpers.NewMockMediaDBI()
		expectVersionsRead(mediaDB, 101, contraTitle, contraGroup(), contraRows())
		return mediaDB
	}
	mediaID := int64(101)
	limit := 3

	first := callVersions(t, versionsEnv(t, newDB(),
		models.MediaVersionsParams{MediaID: &mediaID, MaxResults: &limit}))
	assert.Equal(t, 4, first.Total, "the total covers every page")
	require.Len(t, first.Versions, 3)
	require.NotNil(t, first.Pagination)
	require.True(t, first.Pagination.HasNextPage)
	require.NotNil(t, first.Pagination.NextCursor)
	assert.Equal(t, limit, first.Pagination.PageSize)

	second := callVersions(t, versionsEnv(t, newDB(), models.MediaVersionsParams{
		MediaID: &mediaID, MaxResults: &limit, Cursor: first.Pagination.NextCursor,
	}))
	require.Len(t, second.Versions, 1)
	assert.False(t, second.Pagination.HasNextPage)
	assert.ElementsMatch(t, []int64{101, 102, 111, 121},
		append(versionIDs(first.Versions), versionIDs(second.Versions)...))

	// A cursor from another method, or one whose row has gone, is refused.
	foreign, err := encodeMediaSearchCursorData(cursorData{Version: groupedSearchCursorVersion, LastID: 101})
	require.NoError(t, err)
	_, err = HandleMediaVersions(versionsEnv(t, newDB(),
		models.MediaVersionsParams{MediaID: &mediaID, Cursor: &foreign}))
	require.Error(t, err)
	gone, err := encodeMediaSearchCursorData(cursorData{Version: mediaVersionsCursorVersion, LastID: 999})
	require.NoError(t, err)
	_, err = HandleMediaVersions(versionsEnv(t, newDB(),
		models.MediaVersionsParams{MediaID: &mediaID, Cursor: &gone}))
	require.Error(t, err)
}

func TestHandleMediaVersions_HiddenVersionsAreOptIn(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		includeHidden *bool
		wantExclude   bool
	}{
		"hidden excluded by default": {wantExclude: true},
		"hidden included on request": {includeHidden: ptrTo(true)},
		"explicitly excluded":        {includeHidden: ptrTo(false), wantExclude: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mediaDB := helpers.NewMockMediaDBI()
			mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{101}).
				Return(map[int64]database.MediaFullRow{101: mediaFullRow(101, contraTitle, "")}, nil)
			mediaDB.On("SearchMediaWithFilters", mock.Anything,
				mock.MatchedBy(func(filters *database.SearchFilters) bool {
					return filters.ExcludeHidden == tc.wantExclude
				})).Return(contraRows()[:2], nil)

			mediaID := int64(101)
			response := callVersions(t, versionsEnv(t, mediaDB,
				models.MediaVersionsParams{MediaID: &mediaID, IncludeHidden: tc.includeHidden}))
			assert.Len(t, response.Versions, 2)
		})
	}
}

func TestHandleMediaVersions_RejectsBadRequests(t *testing.T) {
	t.Parallel()
	mediaID := int64(101)
	zero := int64(0)
	tooMany := 101
	for name, params := range map[string]models.MediaVersionsParams{
		"no media named":           {},
		"path without system":      {Path: "/roms/NES/Contra.nes"},
		"mediaId mixed with path":  {MediaID: &mediaID, System: "NES", Path: "/roms/NES/Contra.nes"},
		"non-positive mediaId":     {MediaID: &zero},
		"page larger than allowed": {MediaID: &mediaID, MaxResults: &tooMany},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := HandleMediaVersions(versionsEnv(t, helpers.NewMockMediaDBI(), params))
			require.Error(t, err)
		})
	}

	t.Run("unknown media", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{101}).
			Return(map[int64]database.MediaFullRow{}, nil)
		_, err := HandleMediaVersions(versionsEnv(t, mediaDB, models.MediaVersionsParams{MediaID: &mediaID}))
		require.Error(t, err)
	})

	t.Run("no media database", func(t *testing.T) {
		t.Parallel()
		env := versionsEnv(t, helpers.NewMockMediaDBI(), models.MediaVersionsParams{MediaID: &mediaID})
		env.Database = nil
		_, err := HandleMediaVersions(env)
		require.Error(t, err)
	})

	t.Run("read failure", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{101}).
			Return(map[int64]database.MediaFullRow{101: mediaFullRow(101, contraTitle, "")}, nil)
		mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).
			Return([]database.SearchResultWithCursor(nil), errors.New("boom"))
		_, err := HandleMediaVersions(versionsEnv(t, mediaDB, models.MediaVersionsParams{MediaID: &mediaID}))
		require.Error(t, err)
	})
}

func TestFoldLinkedAliases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rows := []database.SearchResultWithCursor{
		versionRow(1, 5, "ASO", "/a/ASO.mra"),
		versionRow(2, 5, "ASO", "/organized/ASO.mra"),
		versionRow(3, 5, "ASO", "/orphan/ASO.mra"),
	}

	t.Run("an alias gives way to a target in the set", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetMediaLinkTargets", mock.Anything, []int64{1, 2, 3}).
			Return(map[int64]int64{2: 1, 3: 99}, nil)
		folded, err := foldLinkedAliases(ctx, mediaDB, rows)
		require.NoError(t, err)
		assert.Equal(t, []int64{1, 3}, resultIDs(folded),
			"an alias whose target is not in the set stands for it")
	})

	t.Run("a library with no links keeps every row", func(t *testing.T) {
		t.Parallel()
		folded, err := foldLinkedAliases(ctx, helpers.NewMockMediaDBI(), rows)
		require.NoError(t, err)
		assert.Equal(t, rows, folded)
	})

	t.Run("one row has nothing to fold into", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		folded, err := foldLinkedAliases(ctx, mediaDB, rows[:1])
		require.NoError(t, err)
		assert.Equal(t, rows[:1], folded)
		mediaDB.AssertNotCalled(t, "GetMediaLinkTargets", mock.Anything, mock.Anything)
	})

	t.Run("a lookup failure is reported", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetMediaLinkTargets", mock.Anything, mock.Anything).
			Return(map[int64]int64(nil), errors.New("boom"))
		_, err := foldLinkedAliases(ctx, mediaDB, rows)
		require.Error(t, err)
	})
}

func resultIDs(rows []database.SearchResultWithCursor) []int64 {
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].MediaID)
	}
	return ids
}

func TestTitleVersionCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const marioTitle = int64(30)
	marioRow := versionRow(301, marioTitle, "Mario", "/roms/NES/Mario.nes")

	t.Run("a game counts the versions under every one of its titles", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{probotectorTitle, marioTitle}).
			Return(map[int64][]int64{probotectorTitle: contraGroup()}, nil)
		filters := &database.SearchFilters{ExcludeHidden: true, PathPrefix: "/roms/", Query: "ignored"}
		mediaDB.On("GetTitleVersionRows", mock.Anything,
			[]int64{contraTitle, probotectorTitle, gryzorTitle, marioTitle},
			mock.MatchedBy(func(got *database.SearchFilters) bool {
				return got.ExcludeHidden && got.PathPrefix == "/roms/" && got.Query == ""
			})).Return(append(contraRows(), marioRow), nil)

		counts, err := titleVersionCounts(ctx, mediaDB, []int64{probotectorTitle, marioTitle}, filters, false)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{probotectorTitle: 4, marioTitle: 1}, counts)
	})

	t.Run("linked only skips titles that are their own game", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{probotectorTitle, marioTitle}).
			Return(map[int64][]int64{probotectorTitle: contraGroup()}, nil)
		mediaDB.On("GetTitleVersionRows", mock.Anything, contraGroup(), mock.Anything).
			Return(contraRows(), nil)

		counts, err := titleVersionCounts(ctx, mediaDB, []int64{probotectorTitle, marioTitle}, nil, true)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{probotectorTitle: 4}, counts)
	})

	t.Run("linked only reads no rows when nothing is linked", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		counts, err := titleVersionCounts(ctx, mediaDB, []int64{marioTitle}, nil, true)
		require.NoError(t, err)
		assert.Empty(t, counts)
		mediaDB.AssertNotCalled(t, "GetTitleVersionRows", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("container members and aliases are one version", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		rows := append(discRows(), versionRow(25, 20, "Disc Game", "/roms/PSX/_Favorites/Disc Game.chd"))
		mediaDB.On("GetTitleVersionRows", mock.Anything, []int64{20}, mock.Anything).Return(rows, nil)
		mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
			Return(&database.Media{DBID: 22}, nil)
		mediaDB.On("GetMediaLinkTargets", mock.Anything, mock.Anything).
			Return(map[int64]int64{25: 24}, nil)

		counts, err := titleVersionCounts(ctx, mediaDB, []int64{20}, nil, false)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{20: 2}, counts, "the cue's tracks and the alias add nothing")
	})

	t.Run("a single-file title of its own is one version without a row read", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("CountTitleVersionRows", mock.Anything, []int64{marioTitle, 20}, mock.Anything).
			Return(map[int64]database.TitleVersionRowCount{
				marioTitle: {Rows: 1},
				20:         {Rows: 4, Foldable: 4},
			}, nil)
		mediaDB.On("GetTitleVersionRows", mock.Anything, []int64{20}, mock.Anything).
			Return(discRows()[3:], nil)

		counts, err := titleVersionCounts(ctx, mediaDB, []int64{marioTitle, 20}, nil, false)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{marioTitle: 1, 20: 1}, counts)
	})

	t.Run("a page of single-file titles reads no rows at all", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("CountTitleVersionRows", mock.Anything, mock.Anything, mock.Anything).
			Return(map[int64]database.TitleVersionRowCount{marioTitle: {Rows: 1}}, nil)
		counts, err := titleVersionCounts(ctx, mediaDB, []int64{marioTitle}, nil, false)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{marioTitle: 1}, counts)
		mediaDB.AssertNotCalled(t, "GetTitleVersionRows", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("a game with nothing to fold is counted without a row read", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetVersionGroupMembers", mock.Anything, mock.Anything).
			Return(map[int64][]int64{1: {1, 2}}, nil)
		mediaDB.On("CountTitleVersionRows", mock.Anything, []int64{1, 2, marioTitle}, mock.Anything).
			Return(map[int64]database.TitleVersionRowCount{
				1:          {Rows: 2},
				2:          {Rows: 1},
				marioTitle: {Rows: 3},
			}, nil)
		counts, err := titleVersionCounts(ctx, mediaDB, []int64{1, marioTitle}, nil, false)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{1: 3, marioTitle: 3}, counts)
		mediaDB.AssertNotCalled(t, "GetTitleVersionRows", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("a foldable row reads its whole game, and only that game", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetVersionGroupMembers", mock.Anything, mock.Anything).
			Return(map[int64][]int64{1: {1, 20}}, nil)
		mediaDB.On("CountTitleVersionRows", mock.Anything, mock.Anything, mock.Anything).
			Return(map[int64]database.TitleVersionRowCount{
				1:          {Rows: 1},
				20:         {Rows: 4, Foldable: 4},
				marioTitle: {Rows: 2},
			}, nil)
		usa := versionRow(5, 1, "Disc Game", "/roms/PSX/Disc Game (USA).chd")
		rows := append([]database.SearchResultWithCursor{usa}, discRows()...)
		mediaDB.On("GetTitleVersionRows", mock.Anything, []int64{1, 20}, mock.Anything).Return(rows, nil)
		mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
			Return(&database.Media{DBID: 22}, nil)
		counts, err := titleVersionCounts(ctx, mediaDB, []int64{1, marioTitle}, nil, false)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{1: 3, marioTitle: 2}, counts, "the cue's tracks fold into it")
	})

	t.Run("linkedOnly counts only grouped titles", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetVersionGroupMembers", mock.Anything, mock.Anything).
			Return(map[int64][]int64{1: {1, 2}}, nil)
		mediaDB.On("CountTitleVersionRows", mock.Anything, []int64{1, 2}, mock.Anything).
			Return(map[int64]database.TitleVersionRowCount{1: {Rows: 1}, 2: {Rows: 1}}, nil)
		counts, err := titleVersionCounts(ctx, mediaDB, []int64{1, marioTitle}, nil, true)
		require.NoError(t, err)
		assert.Equal(t, map[int64]int{1: 2}, counts)
	})

	t.Run("no titles asks nothing", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		counts, err := titleVersionCounts(ctx, mediaDB, nil, nil, false)
		require.NoError(t, err)
		assert.Empty(t, counts)
		mediaDB.AssertNotCalled(t, "GetVersionGroupMembers", mock.Anything, mock.Anything)
	})

	t.Run("failures are reported", func(t *testing.T) {
		t.Parallel()
		groupFails := helpers.NewMockMediaDBI()
		groupFails.On("GetVersionGroupMembers", mock.Anything, mock.Anything).
			Return(map[int64][]int64(nil), errors.New("boom"))
		_, err := titleVersionCounts(ctx, groupFails, []int64{marioTitle}, nil, false)
		require.Error(t, err)

		countFails := helpers.NewMockMediaDBI()
		countFails.On("CountTitleVersionRows", mock.Anything, mock.Anything, mock.Anything).
			Return(map[int64]database.TitleVersionRowCount(nil), errors.New("boom"))
		_, err = titleVersionCounts(ctx, countFails, []int64{marioTitle}, nil, false)
		require.Error(t, err)

		rowsFail := helpers.NewMockMediaDBI()
		rowsFail.On("GetTitleVersionRows", mock.Anything, mock.Anything, mock.Anything).
			Return([]database.SearchResultWithCursor(nil), errors.New("boom"))
		_, err = titleVersionCounts(ctx, rowsFail, []int64{marioTitle}, nil, false)
		require.Error(t, err)
	})
}

func TestVersionSources(t *testing.T) {
	t.Parallel()
	shared := database.VersionKey{Type: database.VersionKeyMAMESet, Key: "aso"}
	folder := database.VersionKey{Type: database.VersionKeyAltFolder, Key: "aso"}
	sources := versionSources(map[int64][]database.VersionKey{
		1: {shared, folder, {Type: database.VersionKeyGameDatabaseID, Key: "lonely"}},
		2: {folder, shared},
		3: {{Type: database.VersionKeyMAMESet, Key: "other"}},
	})
	assert.Equal(t, []string{database.VersionKeyAltFolder, database.VersionKeyMAMESet}, sources[1])
	assert.Equal(t, []string{database.VersionKeyAltFolder, database.VersionKeyMAMESet}, sources[2])
	assert.Empty(t, sources[3])
}

// A title tile counts the versions filed under the game's other names, and
// listing the title returns them.
func TestHandleMediaSearch_GroupedCountsLinkedTitles(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaTitleGroups", mock.Anything, mock.Anything).Return([]database.SearchTitleGroup{
		{TitleDBID: probotectorTitle, Rows: contraRows()[2:3]},
		{TitleDBID: 10_000, Rows: []database.SearchResultWithCursor{
			versionRow(900, 10_000, "Pro Wrestling", "/roms/NES/Pro Wrestling.nes"),
		}},
	}, nil)
	mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{probotectorTitle, 10_000}).
		Return(map[int64][]int64{probotectorTitle: contraGroup()}, nil)
	mediaDB.On("GetTitleVersionRows", mock.Anything, contraGroup(), mock.Anything).Return(contraRows(), nil)

	groupBy := "title"
	result, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{GroupBy: &groupBy}))
	require.NoError(t, err)
	response, ok := result.(models.SearchResults)
	require.True(t, ok)
	require.Len(t, response.Results, 2)
	assert.Equal(t, "Probotector", response.Results[0].Name, "the tile keeps its own name")
	require.NotNil(t, response.Results[0].VersionCount)
	assert.Equal(t, 4, *response.Results[0].VersionCount)
	require.NotNil(t, response.Results[1].VersionCount)
	assert.Equal(t, 1, *response.Results[1].VersionCount)
}

// Words matching only aliases, and not the file they point to, do not count
// more versions than the whole game has.
func TestHandleMediaSearch_GroupedCountIgnoresOrphanedAliases(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	alias := func(id int64) database.SearchResultWithCursor {
		return versionRow(id, contraTitle, "Contra", "/roms/NES/_Favorites/Contra "+strconv.FormatInt(id, 10)+".nes")
	}
	mediaDB.On("SearchMediaTitleGroups", mock.Anything, mock.Anything).Return([]database.SearchTitleGroup{
		{TitleDBID: contraTitle, Rows: []database.SearchResultWithCursor{alias(103), alias(104), alias(105)}},
	}, nil)
	mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{contraTitle}).
		Return(map[int64][]int64{contraTitle: {contraTitle, probotectorTitle}}, nil)
	mediaDB.On("GetTitleVersionRows", mock.Anything, []int64{contraTitle, probotectorTitle}, mock.Anything).
		Return([]database.SearchResultWithCursor{
			contraRows()[0], alias(103), alias(104), alias(105), contraRows()[2],
		}, nil)
	mediaDB.On("GetMediaLinkTargets", mock.Anything, mock.Anything).
		Return(map[int64]int64{103: 101, 104: 101, 105: 101}, nil)

	groupBy := "title"
	result, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{GroupBy: &groupBy}))
	require.NoError(t, err)
	response, ok := result.(models.SearchResults)
	require.True(t, ok)
	require.Len(t, response.Results, 1)
	require.NotNil(t, response.Results[0].VersionCount)
	assert.Equal(t, 2, *response.Results[0].VersionCount, "Contra and Probotector")
}

func TestHandleMediaSearch_TitleVersionsListTheWholeGroup(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{probotectorTitle}).
		Return(map[int64][]int64{probotectorTitle: contraGroup()}, nil)
	mediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return assert.ObjectsAreEqual(contraGroup(), filters.TitleDBIDs) && filters.TitleDBID == 0
		})).Return(contraRows(), nil)

	titleID := probotectorTitle
	result, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{TitleID: &titleID}))
	require.NoError(t, err)
	response, ok := result.(models.SearchResults)
	require.True(t, ok)
	assert.Len(t, response.Results, 4)
}

func TestAttachBrowseVersionCounts(t *testing.T) {
	t.Parallel()
	files := []database.SearchResultWithCursor{
		versionRow(111, probotectorTitle, "Probotector", "/roms/NES/Probotector (Europe).nes"),
		versionRow(301, 30, "Mario", "/roms/NES/Mario.nes"),
		versionRow(401, 40, "Hidden Game", "/roms/NES/Hidden Game.nes"),
		versionRow(501, 50, "Uncounted", "/roms/GBA/Uncounted.gba"),
	}
	newEntries := func() []models.BrowseEntry {
		return []models.BrowseEntry{
			{Type: "directory", Name: "Folder", MediaID: 111},
			{Type: "media", Name: "Probotector", MediaID: 111},
			{Type: "media", Name: "Mario", MediaID: 301},
			{Type: "media", Name: "Hidden Game", MediaID: 401},
			{Type: "media", Name: "Uncounted", MediaID: 501},
			{Type: "media", Name: "Unindexed", MediaID: 0},
		}
	}

	t.Run("media entries get their stored count", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetStoredVersionCounts", mock.Anything, []int64{probotectorTitle, 30, 40, 50}, false).
			Return(database.StoredVersionCounts{
				Counts: map[int64]int{probotectorTitle: 4, 30: 1, 40: 2},
			}, nil)

		env := versionsEnv(t, mediaDB, struct{}{})
		entries := newEntries()
		attachBrowseVersionCounts(&env, entries, files)

		assert.Nil(t, entries[0].VersionCount, "a directory is not a version of anything")
		require.NotNil(t, entries[1].VersionCount)
		assert.Equal(t, 4, *entries[1].VersionCount)
		require.NotNil(t, entries[2].VersionCount)
		assert.Equal(t, 1, *entries[2].VersionCount)
		require.NotNil(t, entries[3].VersionCount)
		assert.Equal(t, 2, *entries[3].VersionCount)
		assert.Nil(t, entries[4].VersionCount, "a system not counted yet has no count")
		assert.Nil(t, entries[5].VersionCount)
		mediaDB.AssertNotCalled(t, "GetVersionGroupMembers", mock.Anything, mock.Anything)
	})

	t.Run("a game with a hidden version is counted live", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetStoredVersionCounts", mock.Anything, mock.Anything, true).
			Return(database.StoredVersionCounts{
				Counts:  map[int64]int{30: 1},
				Recount: []int64{probotectorTitle, 40},
			}, nil)
		mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{probotectorTitle, 40}).
			Return(map[int64][]int64{probotectorTitle: contraGroup()}, nil)
		mediaDB.On("GetTitleVersionRows", mock.Anything, mock.Anything,
			mock.MatchedBy(func(filters *database.SearchFilters) bool { return filters.ExcludeHidden })).
			Return(contraRows()[:3], nil)

		env := versionsEnv(t, mediaDB, struct{}{})
		env.ExcludeHidden = true
		entries := newEntries()
		attachBrowseVersionCounts(&env, entries, files)

		require.NotNil(t, entries[1].VersionCount)
		assert.Equal(t, 3, *entries[1].VersionCount, "the hidden version is not counted")
		require.NotNil(t, entries[2].VersionCount)
		assert.Equal(t, 1, *entries[2].VersionCount)
		require.NotNil(t, entries[3].VersionCount)
		assert.Equal(t, 1, *entries[3].VersionCount, "an entry the count could not see is still one version")
		assert.Nil(t, entries[4].VersionCount)
	})

	t.Run("a failed lookup leaves the page as it was", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("GetStoredVersionCounts", mock.Anything, mock.Anything, mock.Anything).
			Return(database.StoredVersionCounts{}, errors.New("boom"))
		env := versionsEnv(t, mediaDB, struct{}{})
		entries := newEntries()
		attachBrowseVersionCounts(&env, entries, files)
		for i := range entries {
			assert.Nil(t, entries[i].VersionCount)
		}
	})

	t.Run("nothing to count asks nothing", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		env := versionsEnv(t, mediaDB, struct{}{})
		attachBrowseVersionCounts(&env, newEntries(), nil)
		attachBrowseVersionCounts(nil, newEntries(), files)
		mediaDB.AssertNotCalled(t, "GetStoredVersionCounts", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestBuildBrowseResponse_ReadsStoredVersionCounts(t *testing.T) {
	t.Parallel()
	files := []database.SearchResultWithCursor{
		versionRow(111, probotectorTitle, "Probotector", "/roms/NES/Probotector (Europe).nes"),
	}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Maybe()
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("GetStoredVersionCounts", mock.Anything, []int64{probotectorTitle}, false).
		Return(database.StoredVersionCounts{Counts: map[int64]int{probotectorTitle: 4}}, nil)
	env := versionsEnv(t, mediaDB, struct{}{})
	env.Platform = mockPlatform

	result, err := buildBrowseResponse(&env, "/roms/NES", nil, slices.Clone(files),
		defaultMaxResults, 0, 0, nil, false, nil)
	require.NoError(t, err)
	response, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, response.Entries, 1)
	require.NotNil(t, response.Entries[0].VersionCount)
	assert.Equal(t, 4, *response.Entries[0].VersionCount)
	mediaDB.AssertNotCalled(t, "GetVersionGroupMembers", mock.Anything, mock.Anything)
	mediaDB.AssertNotCalled(t, "CountTitleVersionRows", mock.Anything, mock.Anything, mock.Anything)
}

// The count a regroup stores must be the count a live read works out, or a
// browse and a versions list would disagree.
func TestStoredVersionCountsMatchLiveCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	root := t.TempDir()
	game := func(parts ...string) string {
		return filepath.Join(append([]string{root, "PSX"}, parts...)...)
	}
	scantest.IndexMediaPaths(t, mediaDB, "PSX",
		game("Alpha (USA)", "Alpha (USA).cue"),
		game("Alpha (USA)", "Alpha (USA) (Track 1).bin"),
		game("Alpha (USA)", "Alpha (USA) (Track 2).bin"),
		game("Alpha (Japan)", "Alpha (Japan).cue"),
		game("Alpha (Japan)", "Alpha (Japan) (Track 1).bin"),
		game("Alpha (Europe).chd"),
		game("Beta", "Beta (Track 1).bin"),
		game("Beta", "Beta (Track 2).bin"),
		game("Beta", "Disc 2", "Beta.cue"),
		game("Gamma (USA).chd"),
		game("Gamma (USA) (Rev 1).chd"),
		game("Delta.chd"),
		// One folder holding Epsilon's disc and another game's tracks: a
		// page holding both counts each game on its own rows.
		game("Shared", "Epsilon.cue"),
		game("Shared", "Epsilon (Track 1).bin"),
		game("Shared", "Zeta (Track 1).bin"),
		game("Shared", "Zeta (Track 2).bin"),
	)
	require.NoError(t, mediaDB.RecomputeVersionGroups(ctx, []string{"PSX"}))

	rows, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{{ID: "PSX"}}, Limit: 100,
	})
	require.NoError(t, err)
	var titles []int64
	for i := range rows {
		if !slices.Contains(titles, rows[i].MediaTitleID) {
			titles = append(titles, rows[i].MediaTitleID)
		}
	}
	require.GreaterOrEqual(t, len(titles), 4)

	stored, err := mediaDB.GetStoredVersionCounts(ctx, titles, false)
	require.NoError(t, err)
	live, err := titleVersionCounts(ctx, mediaDB, titles, &database.SearchFilters{}, false)
	require.NoError(t, err)
	require.Len(t, stored.Counts, len(titles))
	for _, title := range titles {
		assert.Equal(t, max(live[title], 1), stored.Counts[title], "title %d", title)
	}
	assert.Contains(t, stored.Counts, titles[0])
	assert.Equal(t, 3, slices.Max(slices.Collect(maps.Values(stored.Counts))),
		"Alpha's two discs and its chd")
}
