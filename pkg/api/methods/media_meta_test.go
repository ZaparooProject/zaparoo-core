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
	"encoding/base64"
	"errors"
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

func TestHandleMediaMeta_FullResult(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()

	mediaPath := filepath.Join("roms", "nes", "mario.nes")
	parentDir := filepath.Join("roms", "nes")
	row := &database.MediaFullRow{
		Media:  database.Media{DBID: 1, Path: mediaPath, ParentDir: parentDir},
		Title:  database.MediaTitle{DBID: 10, Slug: "super-mario-bros", Name: "Super Mario Bros"},
		System: database.System{SystemID: "NES", Name: "NES"},
	}
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(1)).Return(row, nil)
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

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 1}`)
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	assert.Equal(t, int64(1), resp.Media.ID)
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

	row := &database.MediaFullRow{
		Media: database.Media{DBID: 2, Path: "roms/snes/zelda.sfc"},
		Title: database.MediaTitle{
			DBID:          20,
			Slug:          "the-legend-of-zelda",
			SecondarySlug: sql.NullString{String: "zelda", Valid: true},
			Name:          "The Legend of Zelda",
		},
		System: database.System{SystemID: "SNES", Name: "SNES"},
	}
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(2)).Return(row, nil)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(2)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(20)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(2)).Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(20)).Return([]database.MediaProperty{}, nil)

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 2}`)
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Media.Title.SecondarySlug)
	assert.Equal(t, "zelda", *resp.Media.Title.SecondarySlug)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_BinaryPropertyBase64Encoded(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes

	row := makeMediaFullRow(3, 30)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(3)).Return(row, nil)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(3)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(30)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(3)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image", ContentType: "image/png", Binary: blobData},
		}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(30)).Return([]database.MediaProperty{}, nil)

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 3}`)
	result, err := HandleMediaMeta(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	require.Contains(t, resp.Media.Properties, "property:image")
	prop := resp.Media.Properties["property:image"]
	require.NotNil(t, prop.Data)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blobData), *prop.Data)
	assert.Equal(t, "image/png", prop.ContentType)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_MediaNotFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(99)).
		Return((*database.MediaFullRow)(nil), nil)

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 99}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media not found")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_InvalidParams(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	// mediaId is required and must be > 0
	env := makeMediaMetaEnv(t, mockDB, `{}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
}

func TestHandleMediaMeta_DBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(1)).
		Return((*database.MediaFullRow)(nil), errors.New("connection reset"))

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 1}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get media")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_TagsDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(1)).Return(row, nil)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).
		Return([]database.TagInfo{}, errors.New("tags query failed"))

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 1}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get media tags")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_TitleTagsDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(1)).Return(row, nil)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).
		Return([]database.TagInfo{}, errors.New("title tags query failed"))

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 1}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get title tags")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_MediaPropertiesDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(1)).Return(row, nil)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).
		Return([]database.MediaProperty{}, errors.New("media properties query failed"))

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 1}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get media properties")
	mockDB.AssertExpectations(t)
}

func TestHandleMediaMeta_TitlePropertiesDBError(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(1, 10)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(1)).Return(row, nil)
	mockDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(1)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaTitleTagsByMediaTitleDBID", mock.Anything, int64(10)).Return([]database.TagInfo{}, nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{}, errors.New("title properties query failed"))

	env := makeMediaMetaEnv(t, mockDB, `{"mediaId": 1}`)
	_, err := HandleMediaMeta(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get title properties")
	mockDB.AssertExpectations(t)
}
