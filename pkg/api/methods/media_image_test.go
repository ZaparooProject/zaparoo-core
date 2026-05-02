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
	"fmt"
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

func makeMediaImageEnv(
	t *testing.T, mockMediaDB *testhelpers.MockMediaDBI, params json.RawMessage,
) requests.RequestEnv {
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
		Media:  database.Media{DBID: mediaDBID, Path: filepath.Join("games", fmt.Sprintf("test-%d.rom", mediaDBID))},
		Title:  database.MediaTitle{DBID: titleDBID, Slug: "test-game", Name: "Test Game"},
		System: database.System{DBID: 100, SystemID: "NES", Name: "NES"},
	}
}

func expectMediaImageResolve(mockDB *testhelpers.MockMediaDBI, row *database.MediaFullRow) {
	mockDB.On("FindSystemBySystemID", row.System.SystemID).Return(row.System, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, row.System.DBID, row.Path).
		Return(&row.Media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, row.DBID).Return(row, nil)
}

func mediaImageParams(row *database.MediaFullRow, extra string) json.RawMessage {
	if extra != "" {
		extra = ", " + extra
	}
	return json.RawMessage(fmt.Sprintf(`{"system": %q, "path": %q%s}`, row.System.SystemID, row.Path, extra))
}

// TestHandleMediaImage_DefaultPrefs_TitleBlobFound verifies that when no imageTypes
// param is given, the handler uses the default preference order and returns the
// first matching title-level property with inline binary data.
func TestHandleMediaImage_DefaultPrefs_TitleBlobFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte("fake-png-bytes")

	row := makeMediaFullRow(1, 10)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(1)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(10)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: blobData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, ""))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.NotNil(t, resp.Extension)
	assert.Equal(t, "png", *resp.Extension)
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

	row := makeMediaFullRow(2, 20)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(2)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", ContentType: "image/jpeg", Binary: mediaBlob},
		}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(20)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-screenshot", ContentType: "image/jpeg", Binary: titleBlob},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["screenshot"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t,
		base64.StdEncoding.EncodeToString(mediaBlob), resp.Data, "expected media-level blob, not title-level")
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_BinaryNil_LoadsFromFile verifies that when a matching
// property has no inline binary, the handler reads the file at the Text path.
func TestHandleMediaImage_BinaryNil_LoadsFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "boxart.png")
	fileContents := []byte("real-png-data")
	require.NoError(t, os.WriteFile(imgPath, fileContents, 0o600))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(3, 30)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(3)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(30)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-boxart", ContentType: "image/png", Text: imgPath, Binary: nil},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
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
	row := makeMediaFullRow(4, 40)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(4)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(40)).
		Return([]database.MediaProperty{}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, ""))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_MediaNotFound verifies that an error is returned when no
// media record exists for the given system and path.
func TestHandleMediaImage_MediaNotFound(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	system := database.System{DBID: 100, SystemID: "NES", Name: "NES"}
	mediaPath := filepath.Join("games", "missing.rom")
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, mediaPath).
		Return((*database.Media)(nil), nil)

	env := makeMediaImageEnv(t, mockDB, json.RawMessage(`{"system":"NES","path":"games/missing.rom"}`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_ImageTypeResolvesToImageImage verifies that "image" in the
// imageTypes list resolves to "property:image-image" (no assumed context).
func TestHandleMediaImage_ImageTypeResolvesToImageImage(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	blobData := []byte("generic-image-bytes")

	row := makeMediaFullRow(5, 50)
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, int64(5)).
		Return([]database.MediaProperty{}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(50)).
		Return([]database.MediaProperty{
			{TypeTag: "property:image-image", ContentType: "image/png", Binary: blobData},
		}, nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["image"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-image", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blobData), resp.Data)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_FileReadError_DeletesAndContinues verifies that when a
// property's Text path is unreadable, the stale property is deleted (DB + in-memory)
// and the handler continues to the next preference. With no remaining image the
// final result is a ClientError.
func TestHandleMediaImage_FileReadError_DeletesAndContinues(t *testing.T) {
	t.Parallel()

	// Use a path inside a real temp dir so the dir exists, but the file does not.
	stalePath := filepath.Join(t.TempDir(), "missing_boxart.png")

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(6, 60)
	expectMediaImageResolve(mockDB, row)

	// Stale title-level property with missing file.
	staleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        stalePath,
		Binary:      nil,
		TypeTagDBID: 42,
	}
	// Properties are fetched once; the iterative loop deletes from the in-memory map.
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(60)).
		Return([]database.MediaProperty{staleProp}, nil)

	mockDB.On("GetMediaProperties", mock.Anything, int64(6)).
		Return([]database.MediaProperty{}, nil)

	// Expect the stale title-level property to be deleted from the DB.
	mockDB.On("DeleteMediaTitleProperty", mock.Anything, int64(60), int64(42)).
		Return(nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	_, err := HandleMediaImage(env)
	require.Error(t, err)

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr, "expected ClientError after exhausting all preferences")
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_StaleMedia_FallsBackToTitle verifies that when a
// media-level property has a stale file path, the handler deletes the stale
// entry and falls back to the title-level property for the same TypeTag before
// moving on to the next preference in the list.
func TestHandleMediaImage_StaleMedia_FallsBackToTitle(t *testing.T) {
	t.Parallel()

	stalePath := filepath.Join(t.TempDir(), "missing_boxart.png")
	// File is intentionally not created — it must be missing.

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(8, 80)

	expectMediaImageResolve(mockDB, row)

	staleMediaProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        stalePath,
		Binary:      nil,
		TypeTagDBID: 77,
	}
	titleBlob := []byte("title-boxart-bytes")
	titleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Binary:      titleBlob,
	}

	mockDB.On("GetMediaProperties", mock.Anything, int64(8)).
		Return([]database.MediaProperty{staleMediaProp}, nil)
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(80)).
		Return([]database.MediaProperty{titleProp}, nil)

	// Expect only the stale media-level property to be deleted.
	mockDB.On("DeleteMediaProperty", mock.Anything, int64(8), int64(77)).
		Return(nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart"]`))
	result, err := HandleMediaImage(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "property:image-boxart", resp.TypeTag)
	assert.Equal(t, base64.StdEncoding.EncodeToString(titleBlob), resp.Data)
	mockDB.AssertExpectations(t)
}

// TestHandleMediaImage_FileReadError_FallsBackToNextPref verifies that when a
// title-level property's file is unreadable, the stale entry is deleted from the
// DB and the in-memory map, and the handler continues to find the next-preference
// image successfully without a second round-trip.
func TestHandleMediaImage_FileReadError_FallsBackToNextPref(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stalePath := filepath.Join(dir, "missing_boxart.png")
	screenshotPath := filepath.Join(dir, "screenshot.png")
	screenshotData := []byte("screenshot-bytes")
	require.NoError(t, os.WriteFile(screenshotPath, screenshotData, 0o600))

	mockDB := testhelpers.NewMockMediaDBI()
	row := makeMediaFullRow(7, 70)

	expectMediaImageResolve(mockDB, row)

	staleProp := database.MediaProperty{
		TypeTag:     "property:image-boxart",
		ContentType: "image/png",
		Text:        stalePath,
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

	// Properties are fetched once; the iterative loop handles both in a single pass.
	mockDB.On("GetMediaTitleProperties", mock.Anything, int64(70)).
		Return([]database.MediaProperty{staleProp, screenshotProp}, nil)

	mockDB.On("GetMediaProperties", mock.Anything, int64(7)).
		Return([]database.MediaProperty{}, nil)

	mockDB.On("DeleteMediaTitleProperty", mock.Anything, int64(70), int64(55)).
		Return(nil)

	env := makeMediaImageEnv(t, mockDB, mediaImageParams(row, `"imageTypes": ["boxart", "screenshot"]`))
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
