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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeMediaImageEnv(t *testing.T, mockMediaDB *testhelpers.MockMediaDBI, params json.RawMessage) requests.RequestEnv {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)

	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	return requests.RequestEnv{
		Context:  context.Background(),
		State:    st,
		Database: &database.Database{MediaDB: mockMediaDB},
		Config:   cfg,
		Params:   params,
	}
}

func makeMediaFullRow(mediaDBID, titleDBID int64) *database.MediaFullRow {
	return &database.MediaFullRow{
		Media: database.Media{DBID: mediaDBID, Path: "/games/test.rom"},
		Title: database.MediaTitle{DBID: titleDBID, Slug: "test-game", Name: "Test Game"},
		System: database.System{SystemID: "NES", Name: "NES"},
	}
}

// TestHandleMediaImage_DefaultPrefs_TitleBlobFound verifies that when no imageTypes
// param is given, the handler uses the default preference order and returns the
// first matching title-level property with inline binary data.
func TestHandleMediaImage_DefaultPrefs_TitleBlobFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte("fake-png-bytes")

	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(1)).
		Return(makeMediaFullRow(1, 10), nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: blobData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 1}`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blobData), resp.Data)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_ExplicitPrefs_MediaLevelPriority verifies that media-level
// properties take priority over title-level properties for the same TypeTag.
func TestHandleMediaImage_ExplicitPrefs_MediaLevelPriority(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mediaBlob := []byte("media-level-screenshot")
	titleBlob := []byte("title-level-screenshot")

	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(2)).
		Return(makeMediaFullRow(2, 20), nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(2)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", ContentType: "image/jpeg", Binary: mediaBlob},
		}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(20)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", ContentType: "image/jpeg", Binary: titleBlob},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 2, "imageTypes": ["screenshot"]}`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, base64.StdEncoding.EncodeToString(mediaBlob), resp.Data, "expected media-level blob, not title-level")
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_BinaryNil_LoadsFromFile verifies that when a matching
// property has no inline binary, the handler reads the file at the Text path.
func TestHandleMediaImage_BinaryNil_LoadsFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "boxart.png")
	fileContents := []byte("real-png-data")
	require.NoError(t, os.WriteFile(imgPath, fileContents, 0600))

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(3)).
		Return(makeMediaFullRow(3, 30), nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(3)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(30)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Text: imgPath, Binary: nil},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 3, "imageTypes": ["boxart"]}`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	decoded, decErr := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, decErr)
	assert.Equal(t, fileContents, decoded)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_NoMatchFound verifies that an error is returned when no
// image property matches the preference list.
func TestHandleMediaImage_NoMatchFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(4)).
		Return(makeMediaFullRow(4, 40), nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(4)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(40)).
		Return([]database.MediaProperty{}, nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 4}`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_MediaNotFound verifies that an error is returned when no
// media record exists for the given mediaId.
func TestHandleMediaImage_MediaNotFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(99)).
		Return((*database.MediaFullRow)(nil), nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 99}`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_ImageAliasResolvesToBoxart verifies that "image" in the
// imageTypes list is treated as an alias for "boxart".
func TestHandleMediaImage_ImageAliasResolvesToBoxart(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte("boxart-via-image-alias")

	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(5)).
		Return(makeMediaFullRow(5, 50), nil)
	mockDB.On("GetMediaProperties", mock.Anything, int64(5)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(50)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: blobData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 5, "imageTypes": ["image"]}`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blobData), resp.Data)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_FileReadError_DeletesAndRecurses verifies that when a
// property's Text path is unreadable, the stale property is deleted and the
// handler recurses. With no remaining image the final result is a ClientError.
func TestHandleMediaImage_FileReadError_DeletesAndRecurses(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(6, 60)

	// Both call #1 (initial) and call #2 (recursive) fetch the media row.
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(6)).
		Return(row, nil)

	// Stale title-level property with missing file — only present on first fetch.
	staleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        "/nonexistent/path/boxart.png",
		Binary:      nil,
		TypeTagDBID: 42,
	}
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(60)).
		Return([]database.MediaProperty{staleProp}, nil).Once()
	// After deletion the second fetch returns nothing.
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(60)).
		Return([]database.MediaProperty{}, nil).Once()

	mockDB.On("GetMediaProperties", mock.Anything, int64(6)).
		Return([]database.MediaProperty{}, nil)

	// Expect the stale title-level property to be deleted.
	mockDB.On("DeleteMediaTitleProperty", mock.Anything, int64(60), int64(42)).
		Return(nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 6, "imageTypes": ["boxart"]}`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr, "expected ClientError after exhausting all preferences")
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_FileReadError_FallsBackToNextPref verifies that when a
// title-level property's file is unreadable, the stale entry is deleted and the
// recursive call finds the next-preference image successfully.
func TestHandleMediaImage_FileReadError_FallsBackToNextPref(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	screenshotPath := filepath.Join(dir, "screenshot.png")
	screenshotData := []byte("screenshot-bytes")
	require.NoError(t, os.WriteFile(screenshotPath, screenshotData, 0600))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(7, 70)

	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, int64(7)).
		Return(row, nil)

	staleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        "/nonexistent/boxart.png",
		Binary:      nil,
		TypeTagDBID: 55,
	}
	screenshotProp := database.MediaProperty{
		TypeTag:     "property:image-screenshot",
		ContentType: "image/png",
		Text:        screenshotPath,
		Binary:      nil,
		TypeTagDBID: 56,
	}

	// First fetch: both properties present.
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(70)).
		Return([]database.MediaProperty{staleProp, screenshotProp}, nil).Once()
	// After deletion: only screenshot remains.
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(70)).
		Return([]database.MediaProperty{screenshotProp}, nil).Once()

	mockDB.On("GetMediaProperties", mock.Anything, int64(7)).
		Return([]database.MediaProperty{}, nil)

	mockDB.On("DeleteMediaTitleProperty", mock.Anything, int64(70), int64(55)).
		Return(nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"mediaId": 7, "imageTypes": ["boxart", "screenshot"]}`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-screenshot", resp.TypeTag)
	decoded, decErr := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, decErr)
	assert.Equal(t, screenshotData, decoded)
	mockDB.AssertExpectations(t)
}
