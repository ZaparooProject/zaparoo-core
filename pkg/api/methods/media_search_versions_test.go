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
	"slices"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	discDir    = "/roms/PSX/Disc Game/"
	discCue    = discDir + "Disc Game.cue"
	discTrack1 = discDir + "Disc Game (Track 1).bin"
	discTrack2 = discDir + "Disc Game (Track 2).bin"
	discLoose  = "/roms/PSX/Disc Game.chd"
)

func regionRow(id int64, path, region string) database.SearchResultWithCursor {
	return database.SearchResultWithCursor{
		SystemID: "NES", Name: "Mario", Path: path, MediaID: id, MediaTitleID: 10,
		Tags:          []database.TagInfo{{Type: "region", Tag: region}},
		ZapScriptTags: []database.TagInfo{{Type: "region", Tag: region}},
	}
}

func discRows() []database.SearchResultWithCursor {
	return []database.SearchResultWithCursor{
		{SystemID: "PSX", Name: "Disc Game", Path: discTrack1, MediaID: 21, MediaTitleID: 20},
		{SystemID: "PSX", Name: "Disc Game", Path: discCue, MediaID: 22, MediaTitleID: 20},
		{SystemID: "PSX", Name: "Disc Game", Path: discTrack2, MediaID: 23, MediaTitleID: 20},
		{SystemID: "PSX", Name: "Disc Game", Path: discLoose, MediaID: 24, MediaTitleID: 20},
	}
}

func searchEnv(t *testing.T, mediaDB *helpers.MockMediaDBI, params *models.SearchParams) requests.RequestEnv {
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

func TestHandleMediaSearch_GroupedByTitle(t *testing.T) {
	t.Parallel()

	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaTitleGroups", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return filters.Limit == 3 && filters.Sort == "name-asc" && filters.SortCursor == nil
		})).Return([]database.SearchTitleGroup{
		{TitleDBID: 20, SortValue: "Disc Game", Rows: discRows()},
		{TitleDBID: 10, SortValue: "Mario", Rows: []database.SearchResultWithCursor{
			regionRow(11, "/roms/NES/Mario (Europe).nes", "eu"),
			regionRow(12, "/roms/NES/Mario (USA).nes", "us"),
			regionRow(13, "/roms/NES/Mario (Japan).nes", "jp"),
		}},
		{TitleDBID: 30, SortValue: "Zelda", Rows: []database.SearchResultWithCursor{
			{SystemID: "NES", Name: "Zelda", Path: "/roms/NES/Zelda.nes", MediaID: 31, MediaTitleID: 30},
		}},
	}, nil)
	mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
		Return(&database.Media{DBID: 22, Path: discCue}, nil)

	groupBy := "title"
	sortOrder := "name-asc"
	maxResults := 2
	result, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{
		GroupBy: &groupBy, Sort: &sortOrder, MaxResults: &maxResults,
	}))
	require.NoError(t, err)

	response, ok := result.(models.SearchResults)
	require.True(t, ok)
	require.Len(t, response.Results, 2)

	disc := response.Results[0]
	require.NotNil(t, disc.VersionCount)
	assert.Equal(t, 2, *disc.VersionCount, "the cue and its tracks are one version, the loose image another")
	assert.Equal(t, int64(20), disc.TitleID)
	assert.Contains(t, []string{discCue, discLoose}, disc.Path, "a track is never the default")

	mario := response.Results[1]
	require.NotNil(t, mario.VersionCount)
	assert.Equal(t, 3, *mario.VersionCount)
	assert.Equal(t, int64(10), mario.TitleID)
	assert.Equal(t, "/roms/NES/Mario (USA).nes", mario.Path, "the preferred region is the default")
	assert.Equal(t, "@NES/Mario (region:us)", mario.ZapScript, "the zapScript launches that exact version")

	require.NotNil(t, response.Pagination)
	assert.True(t, response.Pagination.HasNextPage)
	require.NotNil(t, response.Pagination.NextCursor)
	cursor, err := decodeGroupedSearchCursor(*response.Pagination.NextCursor, sortOrder)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, int64(10), cursor.LastID, "the cursor holds the last title, not a media row")
	assert.Equal(t, "Mario", cursor.SortValue)

	_, _, err = decodeMediaSearchCursor(*response.Pagination.NextCursor, sortOrder)
	require.Error(t, err, "a grouped cursor cannot continue an ungrouped search")
	mediaDB.AssertExpectations(t)
}

func TestHandleMediaSearch_GroupedCursorRoundTrip(t *testing.T) {
	t.Parallel()

	cursor, err := encodeGroupedSearchCursor(
		&database.SearchTitleGroup{TitleDBID: 10, SortValue: "Mario"}, "", searchVisibility{})
	require.NoError(t, err)

	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaTitleGroups", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return filters.SortCursor != nil && filters.SortCursor.LastID == 10 && filters.Sort == ""
		})).Return([]database.SearchTitleGroup{}, nil)

	groupBy := "title"
	_, err = HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{GroupBy: &groupBy, Cursor: &cursor}))
	require.NoError(t, err)
	mediaDB.AssertExpectations(t)
}

func TestHandleMediaSearch_GroupedRejectsInvalidCombinations(t *testing.T) {
	t.Parallel()

	groupBy := "title"
	filename := "filename-asc"
	titleID := int64(10)
	ungroupedCursor, err := encodeSearchCursor(5, searchVisibility{})
	require.NoError(t, err)
	unknownGroup := "system"

	for name, params := range map[string]models.SearchParams{
		"filename sort":     {GroupBy: &groupBy, Sort: &filename},
		"with titleId":      {GroupBy: &groupBy, TitleID: &titleID},
		"ungrouped cursor":  {GroupBy: &groupBy, Cursor: &ungroupedCursor},
		"unknown group key": {GroupBy: &unknownGroup},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := HandleMediaSearch(searchEnv(t, helpers.NewMockMediaDBI(), &params))
			require.Error(t, err)
		})
	}
}

func TestHandleMediaSearch_TitleVersions(t *testing.T) {
	t.Parallel()

	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return filters.TitleDBID == 20 && filters.Limit == titleVersionsLimit &&
				filters.Cursor == nil && filters.SortCursor == nil
		})).Return(discRows(), nil)
	mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
		Return(&database.Media{DBID: 22, Path: discCue}, nil)

	titleID := int64(20)
	maxResults := 1
	result, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{
		TitleID: &titleID, MaxResults: &maxResults,
	}))
	require.NoError(t, err)
	response, ok := result.(models.SearchResults)
	require.True(t, ok)
	require.Len(t, response.Results, 1)
	assert.Equal(t, discCue, response.Results[0].Path)
	assert.Nil(t, response.Results[0].VersionCount)
	require.NotNil(t, response.Pagination)
	require.True(t, response.Pagination.HasNextPage)
	require.NotNil(t, response.Pagination.NextCursor)

	result, err = HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{
		TitleID: &titleID, MaxResults: &maxResults, Cursor: response.Pagination.NextCursor,
	}))
	require.NoError(t, err)
	response, ok = result.(models.SearchResults)
	require.True(t, ok)
	require.Len(t, response.Results, 1)
	assert.Equal(t, discLoose, response.Results[0].Path)
	assert.False(t, response.Pagination.HasNextPage)
}

func TestFoldContainerVersions(t *testing.T) {
	t.Parallel()

	t.Run("launch row filtered out keeps members apart", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
			Return(&database.Media{DBID: 99, Path: discDir + "Disc Game.m3u"}, nil)
		rows := discRows()
		folded, err := foldContainerVersions(context.Background(), mediaDB, rows)
		require.NoError(t, err)
		assert.Len(t, folded, len(rows))
	})

	t.Run("no container leaves rows alone", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
			Return(nil, nil)
		rows := discRows()
		folded, err := foldContainerVersions(context.Background(), mediaDB, rows)
		require.NoError(t, err)
		assert.Len(t, folded, len(rows))
	})

	t.Run("lookup failure fails the fold", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		mediaDB.On("FindSingleContainerLaunchMediaBySystemID", mock.Anything, "PSX", discDir).
			Return(nil, errors.New("disk gone"))
		_, err := foldContainerVersions(context.Background(), mediaDB, discRows())
		require.Error(t, err)
	})

	t.Run("lone container member is not looked up", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		rows := discRows()[3:]
		folded, err := foldContainerVersions(context.Background(), mediaDB, rows)
		require.NoError(t, err)
		assert.Equal(t, rows, folded)
		mediaDB.AssertNotCalled(t, "FindSingleContainerLaunchMediaBySystemID")
	})
}

func TestPageTitleVersions(t *testing.T) {
	t.Parallel()

	versions := []database.SearchResultWithCursor{
		{Path: "/b", MediaID: 1, SortValue: "/b"},
		{Path: "/a", MediaID: 2, SortValue: "/a"},
		{Path: "/c", MediaID: 3, SortValue: "/c"},
	}
	page := pageTitleVersions(slices.Clone(versions), "filename-desc", nil,
		&database.SearchCursor{Sort: "filename-desc", SortValue: "/c", LastID: 3}, 5)
	require.Len(t, page, 2)
	assert.Equal(t, "/b", page[0].Path)
	assert.Equal(t, "/a", page[1].Path)

	lastID := int64(1)
	page = pageTitleVersions(slices.Clone(versions), "", &lastID, nil, 1)
	require.Len(t, page, 1)
	assert.Equal(t, int64(2), page[0].MediaID)
}

// TestHandleMediaSearch_GroupedLargeTitleUsesLaunchResolution pins that a
// title with more versions than a launch weighs takes its default from the
// launch resolver, so the shown version is the one its zapScript launches.
// Not parallel: it replaces the package resolver.
func TestHandleMediaSearch_GroupedLargeTitleUsesLaunchResolution(t *testing.T) {
	const rankedID = 130
	rows := make([]database.SearchResultWithCursor, 0, database.SlugSearchLimit+1)
	for i := range database.SlugSearchLimit + 1 {
		row := database.SearchResultWithCursor{
			SystemID: "SNESMusic", Name: "Big Soundtrack", MediaID: int64(100 + i), MediaTitleID: 7,
			Path: fmt.Sprintf("/roms/SNESMusic/Big Soundtrack/track %02d.spc", i),
		}
		// The only version in a preferred region, so ranking picks it.
		if row.MediaID == rankedID {
			row.Tags = []database.TagInfo{{Type: "region", Tag: "us"}}
		}
		rows = append(rows, row)
	}

	for name, tc := range map[string]struct {
		resolveErr error
		resolvedID int64
		wantID     int64
		wantErr    bool
	}{
		"resolver pick wins":            {resolvedID: 140, wantID: 140},
		"no match keeps the ranked one": {resolveErr: titles.ErrNoMatch, wantID: rankedID},
		"resolver failure fails":        {resolveErr: errors.New("boom"), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			var gotName string
			original := resolveTitle
			resolveTitle = func(_ context.Context, p *titles.ResolveParams) (*titles.ResolveResult, error) {
				gotName = p.GameName
				if tc.resolveErr != nil {
					return nil, tc.resolveErr
				}
				return &titles.ResolveResult{Result: database.SearchResultWithCursor{MediaID: tc.resolvedID}}, nil
			}
			t.Cleanup(func() { resolveTitle = original })

			mediaDB := helpers.NewMockMediaDBI()
			mediaDB.On("SearchMediaTitleGroups", mock.Anything, mock.Anything).
				Return([]database.SearchTitleGroup{{TitleDBID: 7, Rows: rows}}, nil)

			groupBy := "title"
			result, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{GroupBy: &groupBy}))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			response, ok := result.(models.SearchResults)
			require.True(t, ok)
			require.Len(t, response.Results, 1)
			assert.Equal(t, "Big Soundtrack", gotName)
			got := response.Results[0]
			require.NotNil(t, got.VersionCount)
			assert.Equal(t, database.SlugSearchLimit+1, *got.VersionCount)
			assert.Equal(t, tc.wantID, got.MediaID)
		})
	}
}

func TestHandleMediaSearch_TitleVersionsIgnoreSystems(t *testing.T) {
	t.Parallel()

	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool { return filters.TitleDBID == 10 })).
		Return([]database.SearchResultWithCursor{regionRow(11, "/roms/NES/Mario (Europe).nes", "eu")}, nil)

	titleID := int64(10)
	systems := []string{"NotASystem"}
	result, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{TitleID: &titleID, Systems: &systems}))
	require.NoError(t, err)
	response, ok := result.(models.SearchResults)
	require.True(t, ok)
	assert.Len(t, response.Results, 1)
}

func TestHandleMediaSearch_TitleVersionsFailAtRowLimit(t *testing.T) {
	t.Parallel()

	rows := make([]database.SearchResultWithCursor, titleVersionsLimit)
	for i := range rows {
		rows[i] = database.SearchResultWithCursor{SystemID: "NES", MediaID: int64(i + 1), MediaTitleID: 10}
	}
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).Return(rows, nil)

	titleID := int64(10)
	_, err := HandleMediaSearch(searchEnv(t, mediaDB, &models.SearchParams{TitleID: &titleID}))
	require.Error(t, err, "a version list cut off at the row limit must not be returned as complete")
}
