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
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeMediaMetaEnv(t *testing.T, mockMediaDB *testhelpers.MockMediaDBI, params string) requests.RequestEnv {
	t.Helper()
	return requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Params:   []byte(params),
	}
}

func expectMediaMetaResolve(mockDB *testhelpers.MockMediaDBI, row *database.MediaFullRow) {
	mockDB.On("FindSystemBySystemID", row.System.SystemID).Return(row.System, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, row.System.DBID, row.Path).
		Return(&row.Media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, row.DBID).Return(row, nil)
}

func mediaMetaParams(row *database.MediaFullRow) string {
	return fmt.Sprintf(`{"system": %q, "path": %q}`, row.System.SystemID, row.Path)
}

func TestHandleMediaMeta_FullResult(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()

	mediaPath := filepath.Join("roms", "nes", "mario.nes")
	parentDir := filepath.Join("roms", "nes")
	row := &database.MediaFullRow{
		Media:  database.Media{DBID: 1, Path: mediaPath, ParentDir: parentDir},
		Title:  database.MediaTitle{DBID: 10, Slug: "super-mario-bros", Name: "Super Mario Bros"},
		System: database.System{DBID: 100, SystemID: "NES", Name: "NES"},
	}
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).
		Return([]database.TagInfo{{Tag: "genre:platformer", Type: "genre"}}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).
		Return([]database.TagInfo{{Tag: "developer:nintendo", Type: "developer"}}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{
			{TypeTag: "property:description", Text: "A classic platformer"},
		}, nil)

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	assert.Equal(t, mediaPath, resp.Media.Path)
	assert.Equal(t, parentDir, resp.Media.ParentDir)
	assert.Equal(t, "NES", resp.Media.Title.System.ID)
	assert.Equal(t, "NES", resp.Media.Title.System.Name)
	assert.Equal(t, "super-mario-bros", resp.Media.Title.Slug)
	assert.Len(t, resp.Media.Tags, 1)
	assert.Equal(t, "genre:platformer", resp.Media.Tags[0].Tag)
	assert.Len(t, resp.Media.Title.Tags, 1)
	assert.Equal(t, "developer:nintendo", resp.Media.Title.Tags[0].Tag)
	assert.Contains(t, resp.Media.Title.Properties, "property:description")
	assert.Equal(t, "A classic platformer", resp.Media.Title.Properties["property:description"].Text)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_SecondarySlug(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mediaPath := filepath.Join("roms", "snes", "zelda.sfc")
	row := &database.MediaFullRow{
		Media: database.Media{DBID: 2, Path: mediaPath},
		Title: database.MediaTitle{
			DBID:          20,
			Slug:          "the-legend-of-zelda",
			SecondarySlug: sql.NullString{String: "zelda", Valid: true},
			Name:          "The Legend of Zelda",
		},
		System: database.System{DBID: 200, SystemID: "SNES", Name: "SNES"},
	}
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(2)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(20)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(2)).Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(20)).Return([]database.MediaProperty{}, nil)

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Media.Title.SecondarySlug)
	assert.Equal(t, "zelda", *resp.Media.Title.SecondarySlug)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_BinaryPropertyMetadataOnly(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes

	row := makeMediaFullRow(3, 30)
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(3)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(30)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(3)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image", ContentType: "image/png", Binary: blobData, BlobSize: int64(len(blobData))},
		}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(30)).Return([]database.MediaProperty{}, nil)

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	require.Contains(t, resp.Media.Properties, "property:image")
	prop := resp.Media.Properties["property:image"]
	assert.Equal(t, int64(len(blobData)), prop.BlobSize)
	assert.Equal(t, "image/png", prop.ContentType)
	assert.NotNil(t, prop.Extension)
	assert.Equal(t, "png", *prop.Extension)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_OversizedBinaryPropertyMetadataOnly(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobID := int64(99)

	row := makeMediaFullRow(4, 40)
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(4)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(40)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(4)).
		Return([]database.MediaProperty{
			{
				TypeTag:     "property:image",
				ContentType: "image/png",
				BlobDBID:    &blobID,
				BlobSize:    database.MaxMediaPropertyBinaryBytes + 1,
			},
		}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(40)).Return([]database.MediaProperty{}, nil)

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	require.Contains(t, resp.Media.Properties, "property:image")
	prop := resp.Media.Properties["property:image"]
	assert.Equal(t, int64(database.MaxMediaPropertyBinaryBytes+1), prop.BlobSize)
	assert.Equal(t, "image/png", prop.ContentType)
	assert.NotNil(t, prop.Extension)
	assert.Equal(t, "png", *prop.Extension)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_BatchByMediaIDPartialSuccess(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(3, 30)
	row.System = database.System{DBID: 100, SystemID: "NES", Name: "NES"}
	row.Title.Name = "Batch Game"

	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, mock.Anything).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil)
	mockDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.TagInfo{row.DBID: {{Tag: "genre:platformer", Type: "genre"}}}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.TagInfo{row.Title.DBID: {{Tag: "publisher:nintendo", Type: "publisher"}}}, nil)
	mockDB.On("GetMediaPropertiesByMediaDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.MediaProperty{
			row.DBID: {{TypeTag: "property:description", Text: "media desc"}},
		}, nil)
	mockDB.On("GetMediaTitlePropertiesByMediaTitleDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.MediaProperty{}, nil)

	env := makeMediaMetaEnv(t, mockDB, `{"items":[{"mediaId":3},{"mediaId":999}]}`)
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaBatchResponse)
	require.True(t, ok)
	require.Len(t, resp.Items, 2)
	require.NotNil(t, resp.Items[0].Media)
	assert.Equal(t, row.Path, resp.Items[0].Media.Path)
	assert.Equal(t, "Batch Game", resp.Items[0].Media.Title.Name)
	assert.Contains(t, resp.Items[0].Media.Properties, "property:description")
	require.NotNil(t, resp.Items[1].Error)
	assert.Contains(t, *resp.Items[1].Error, "mediaId 999")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_MediaIDSuccess(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(3, 30)
	row.System = database.System{DBID: 100, SystemID: "NES", Name: "NES"}
	row.Title.Name = "Media ID Game"

	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, mock.Anything).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil)
	mockDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.TagInfo{row.DBID: {{Tag: "genre:platformer", Type: "genre"}}}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.TagInfo{row.Title.DBID: {{Tag: "publisher:nintendo", Type: "publisher"}}}, nil)
	mockDB.On("GetMediaPropertiesByMediaDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.MediaProperty{
			row.DBID: {{TypeTag: "property:description", Text: "media desc"}},
		}, nil)
	mockDB.On("GetMediaTitlePropertiesByMediaTitleDBIDs", mock.Anything, mock.Anything).
		Return(map[int64][]database.MediaProperty{
			row.Title.DBID: {{TypeTag: "property:description", Text: "title desc"}},
		}, nil)

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId":3}`)
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	assert.Equal(t, row.Path, resp.Media.Path)
	assert.Equal(t, "Media ID Game", resp.Media.Title.Name)
	require.Len(t, resp.Media.Tags, 1)
	assert.Equal(t, "genre:platformer", resp.Media.Tags[0].Tag)
	require.Len(t, resp.Media.Title.Tags, 1)
	assert.Equal(t, "publisher:nintendo", resp.Media.Title.Tags[0].Tag)
	assert.Equal(t, "media desc", resp.Media.Properties["property:description"].Text)
	assert.Equal(t, "title desc", resp.Media.Title.Properties["property:description"].Text)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_MediaNotFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	system := database.System{DBID: 100, SystemID: "NES", Name: "NES"}
	mediaPath := filepath.ToSlash(filepath.Join("games", "missing.rom"))
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, mediaPath).
		Return((*database.Media)(nil), nil)

	env := makeMediaMetaEnv(t, mockDB, `{"system":"NES","path":"games/missing.rom"}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media not found")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_MediaIDNotFoundSkipsMetadataFetch(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, mock.Anything).
		Return(map[int64]database.MediaFullRow{}, nil)

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId":999}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mediaId 999")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_BatchPathMissesSkipMediaIDFetch(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	missingPath := filepath.Join("games", "missing.rom")
	mockDB.On("FindSystemBySystemID", "NES").Return(database.System{}, sql.ErrNoRows)

	env := makeMediaMetaEnv(t, mockDB, fmt.Sprintf(`{"items":[{"system":"NES","path":%q}]}`, missingPath))
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaBatchResponse)
	require.True(t, ok)
	require.Len(t, resp.Items, 1)
	require.NotNil(t, resp.Items[0].Error)
	assert.Contains(t, *resp.Items[0].Error, "system not found: NES")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_BatchAllMediaIDMissesSkipMetadataFetch(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, mock.Anything).
		Return(map[int64]database.MediaFullRow{}, nil)

	env := makeMediaMetaEnv(t, mockDB, `{"items":[{"mediaId":999},{"mediaId":1000}]}`)
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaBatchResponse)
	require.True(t, ok)
	require.Len(t, resp.Items, 2)
	require.NotNil(t, resp.Items[0].Error)
	require.NotNil(t, resp.Items[1].Error)
	assert.Contains(t, *resp.Items[0].Error, "mediaId 999")
	assert.Contains(t, *resp.Items[1].Error, "mediaId 1000")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_InvalidParams(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	// system and path are required.
	env := makeMediaMetaEnv(t, mockDB, `{}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
}

func TestHandleMediaMeta_DBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	mockDB.On("FindSystemBySystemID", row.System.SystemID).Return(row.System, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, row.System.DBID, row.Path).
		Return(&row.Media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, row.DBID).
		Return((*database.MediaFullRow)(nil), errors.New("connection reset"))

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get media")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_TagsDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).
		Return([]database.TagInfo{}, errors.New("tags query failed"))

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get media tags")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_TitleTagsDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).
		Return([]database.TagInfo{}, errors.New("title tags query failed"))

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get title tags")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_MediaPropertiesDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).
		Return([]database.MediaProperty{}, errors.New("media properties query failed"))

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get media property metadata")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_TitlePropertiesDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	expectMediaMetaResolve(mockDB, row)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{}, errors.New("title properties query failed"))

	env := makeMediaMetaEnv(t, mockDB, mediaMetaParams(row))
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get title property metadata")
	mockDB.AssertExpectations(t)
}
