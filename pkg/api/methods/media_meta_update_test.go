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
	"database/sql"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	pmocks "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeMediaMetaUpdateEnv(mockMediaDB *testhelpers.MockMediaDBI, params string) requests.RequestEnv {
	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "RetroArch", SystemID: "NES"},
		{ID: "OtherSystem", SystemID: "SNES"},
	})
	return requests.RequestEnv{
		Context:       context.Background(),
		Database:      &database.Database{MediaDB: mockMediaDB},
		LauncherCache: launcherCache,
		Params:        []byte(params),
	}
}

func mediaMetaUpdateRow() database.MediaFullRow {
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

func expectMediaMetaUpdateResponse(
	mockDB *testhelpers.MockMediaDBI,
	row *database.MediaFullRow,
	mediaProps []database.MediaProperty,
) {
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: *row}, nil).Once()
	mockDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, []int64{1}).
		Return(map[int64][]database.TagInfo{}, nil).Once()
	mockDB.On("GetMediaTitleTagsByMediaTitleDBIDs", mock.Anything, []int64{10}).
		Return(map[int64][]database.TagInfo{}, nil).Once()
	mockDB.On("GetMediaPropertyMetadataByMediaDBIDs", mock.Anything, []int64{1}).
		Return(map[int64][]database.MediaProperty{1: mediaProps}, nil).Once()
	mockDB.On("GetMediaTitlePropertyMetadataByMediaTitleDBIDs", mock.Anything, []int64{10}).
		Return(map[int64][]database.MediaProperty{}, nil).Once()
}

func TestHandleMediaMetaUpdate_SetsLauncherOverride(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := mediaMetaUpdateRow()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()
	mockDB.On("FindOrInsertTagType", database.TagType{
		Type:        string(tags.TagTypeProperty),
		IsExclusive: false,
	}).Return(database.TagType{DBID: 11, Type: string(tags.TagTypeProperty)}, nil).Once()
	mockDB.On("FindOrInsertTag", database.Tag{TypeDBID: 11, Tag: string(tags.TagPropertyLauncherOverride)}).
		Return(database.Tag{DBID: 12, TypeDBID: 11, Tag: string(tags.TagPropertyLauncherOverride)}, nil).Once()
	mockDB.On("UpsertMediaProperties", mock.Anything, int64(1), []database.MediaProperty{{
		TypeTag: launcherOverridePropertyTypeTag(),
		Text:    "RetroArch",
	}}).Return(nil).Once()
	expectMediaMetaUpdateResponse(mockDB, &row, []database.MediaProperty{{
		TypeTag: launcherOverridePropertyTypeTag(),
		Text:    "RetroArch",
	}})

	result, err := HandleMediaMetaUpdate(makeMediaMetaUpdateEnv(
		mockDB, `{"mediaId":1,"media":{"launcherOverride":"retroarch"}}`,
	))
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Media.LauncherOverride)
	assert.Equal(t, "RetroArch", *resp.Media.LauncherOverride)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMetaUpdate_SetsLauncherOverrideFromPlatformWhenCacheMissing(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockPlatform := pmocks.NewMockPlatform()
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{
		{ID: "RetroArch", SystemID: "NES"},
	}).Once()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Once()
	row := mediaMetaUpdateRow()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()
	mockDB.On("FindOrInsertTagType", database.TagType{
		Type:        string(tags.TagTypeProperty),
		IsExclusive: false,
	}).Return(database.TagType{DBID: 11, Type: string(tags.TagTypeProperty)}, nil).Once()
	mockDB.On("FindOrInsertTag", database.Tag{TypeDBID: 11, Tag: string(tags.TagPropertyLauncherOverride)}).
		Return(database.Tag{DBID: 12, TypeDBID: 11, Tag: string(tags.TagPropertyLauncherOverride)}, nil).Once()
	mockDB.On("UpsertMediaProperties", mock.Anything, int64(1), []database.MediaProperty{{
		TypeTag: launcherOverridePropertyTypeTag(),
		Text:    "RetroArch",
	}}).Return(nil).Once()
	expectMediaMetaUpdateResponse(mockDB, &row, []database.MediaProperty{{
		TypeTag: launcherOverridePropertyTypeTag(),
		Text:    "RetroArch",
	}})
	env := makeMediaMetaUpdateEnv(mockDB, `{"mediaId":1,"media":{"launcherOverride":"retroarch"}}`)
	env.LauncherCache = nil
	env.Platform = mockPlatform

	result, err := HandleMediaMetaUpdate(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Media.LauncherOverride)
	assert.Equal(t, "RetroArch", *resp.Media.LauncherOverride)
	mockDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestHandleMediaMetaUpdate_ClearsLauncherOverride(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := mediaMetaUpdateRow()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()
	mockDB.On("FindTagType", database.TagType{Type: string(tags.TagTypeProperty)}).
		Return(database.TagType{DBID: 11, Type: string(tags.TagTypeProperty)}, nil).Once()
	mockDB.On("FindTag", database.Tag{TypeDBID: 11, Tag: string(tags.TagPropertyLauncherOverride)}).
		Return(database.Tag{DBID: 12, TypeDBID: 11, Tag: string(tags.TagPropertyLauncherOverride)}, nil).Once()
	mockDB.On("DeleteMediaProperty", mock.Anything, int64(1), int64(12)).Return(nil).Once()
	expectMediaMetaUpdateResponse(mockDB, &row, nil)

	result, err := HandleMediaMetaUpdate(makeMediaMetaUpdateEnv(
		mockDB, `{"mediaId":1,"media":{"launcherOverride":null}}`,
	))
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	assert.Nil(t, resp.Media.LauncherOverride)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMetaUpdate_RejectsWrongSystemLauncher(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := mediaMetaUpdateRow()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()

	_, err := HandleMediaMetaUpdate(makeMediaMetaUpdateEnv(
		mockDB, `{"mediaId":1,"media":{"launcherOverride":"OtherSystem"}}`,
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support system")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMetaUpdate_RejectsUnknownLauncher(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := mediaMetaUpdateRow()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()

	_, err := HandleMediaMetaUpdate(makeMediaMetaUpdateEnv(
		mockDB, `{"mediaId":1,"media":{"launcherOverride":"Missing"}}`,
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "launcher not found")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMetaUpdate_ClearIgnoresMissingPropertyTag(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := mediaMetaUpdateRow()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{1}).
		Return(map[int64]database.MediaFullRow{1: row}, nil).Once()
	mockDB.On("FindTagType", database.TagType{Type: string(tags.TagTypeProperty)}).
		Return(database.TagType{}, sql.ErrNoRows).Once()
	expectMediaMetaUpdateResponse(mockDB, &row, nil)

	result, err := HandleMediaMetaUpdate(makeMediaMetaUpdateEnv(
		mockDB, `{"mediaId":1,"media":{"launcherOverride":null}}`,
	))
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	assert.Nil(t, resp.Media.LauncherOverride)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMetaUpdate_RejectsMissingOrInvalidMediaPatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		params  string
		wantErr string
	}{
		{name: "missing media", params: `{"mediaId":1}`, wantErr: "media update is required"},
		{name: "null media", params: `{"mediaId":1,"media":null}`, wantErr: "media must be an object"},
		{name: "empty media", params: `{"mediaId":1,"media":{}}`, wantErr: "no supported media updates provided"},
		{
			name:    "empty launcher override",
			params:  `{"mediaId":1,"media":{"launcherOverride":" "}}`,
			wantErr: "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := testhelpers.NewMockMediaDBI()
			_, err := HandleMediaMetaUpdate(makeMediaMetaUpdateEnv(mockDB, tt.params))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			mockDB.AssertExpectations(t)
		})
	}
}

func TestHandleMediaMetaUpdate_RejectsUnsupportedMediaField(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	_, err := HandleMediaMetaUpdate(makeMediaMetaUpdateEnv(
		mockDB, `{"mediaId":1,"media":{"unknown":"value"}}`,
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported media field")
	mockDB.AssertExpectations(t)
}

func TestParseMediaMetaUpdatePatchValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "missing media", raw: ``, wantErr: "media update is required"},
		{name: "null media", raw: `null`, wantErr: "media must be an object"},
		{name: "empty media object", raw: `{}`, wantErr: "no supported media updates provided"},
		{name: "empty override string", raw: `{"launcherOverride":" "}`, wantErr: "cannot be empty"},
		{name: "non-string override", raw: `{"launcherOverride":123}`, wantErr: "must be a string or null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMediaMetaUpdatePatch([]byte(tt.raw))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
