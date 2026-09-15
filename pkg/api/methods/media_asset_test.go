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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeMediaAssetEnv(
	t *testing.T,
	mockDB *testhelpers.MockMediaDBI,
	root string,
	params json.RawMessage,
) requests.RequestEnv {
	t.Helper()
	return makeMediaAssetEnvWithSettings(t, mockDB, root, params, platforms.Settings{})
}

func makeMediaAssetEnvWithSettings(
	t *testing.T,
	mockDB *testhelpers.MockMediaDBI,
	root string,
	params json.RawMessage,
	settings platforms.Settings,
) requests.RequestEnv {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("RootDirs", mock.Anything).Return([]string{root})
	pl.On("Settings").Return(settings)
	pl.On("ID").Return("test")
	return requests.RequestEnv{
		Context:  t.Context(),
		Platform: pl,
		Database: &database.Database{MediaDB: mockDB},
		Params:   params,
	}
}

func writeTestPDF(t *testing.T, root, name string, body []byte) (path string, data []byte) {
	t.Helper()
	data = append([]byte("%PDF-1.7\n"), body...)
	path = filepath.Join(root, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path, data
}

func requireMediaAssetResponse(t *testing.T, result any) models.MediaAssetResponse {
	t.Helper()
	resp, ok := result.(models.MediaAssetResponse)
	require.True(t, ok)
	return resp
}

func expectAssetResolveByID(mockDB *testhelpers.MockMediaDBI, row *database.MediaFullRow) {
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{row.DBID}).
		Return(map[int64]database.MediaFullRow{row.DBID: *row}, nil).Once()
}

func expectAssetProperties(
	mockDB *testhelpers.MockMediaDBI,
	row *database.MediaFullRow,
	mediaProps, titleProps []database.MediaProperty,
) {
	mockDB.On("GetMediaPropertyMetadata", mock.Anything, row.DBID).Return(mediaProps, nil).Once()
	if findMediaAssetProperty(mediaProps, "property:manual") == nil {
		mockDB.On("GetMediaTitlePropertyMetadata", mock.Anything, row.Title.DBID).Return(titleProps, nil).Once()
	}
}

func TestHandleMediaAsset_SmallTitleManual(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manualPath, manual := writeTestPDF(t, root, "source-secret-name.pdf", []byte("manual contents"))
	row := makeMediaFullRow(42, 420)
	mockDB := testhelpers.NewMockMediaDBI()
	expectAssetResolveByID(mockDB, row)
	expectAssetProperties(mockDB, row, nil, []database.MediaProperty{{
		TypeTag: "property:manual", Text: manualPath,
	}})

	env := makeMediaAssetEnv(t, mockDB, root, json.RawMessage(`{"mediaId":42,"assetType":"manual"}`))
	result, err := HandleMediaAsset(env)
	require.NoError(t, err)
	resp := requireMediaAssetResponse(t, result)

	decoded, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Equal(t, manual, decoded)
	assert.Equal(t, "manual", resp.AssetType)
	assert.Equal(t, "property:manual", resp.TypeTag)
	assert.Equal(t, "application/pdf", resp.ContentType)
	assert.Equal(t, "pdf", resp.Extension)
	assert.Equal(t, int64(len(manual)), resp.Size)
	assert.Equal(t, int64(len(manual)), resp.Length)
	assert.True(t, resp.Complete)
	assert.Nil(t, resp.NextOffset)
	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), manualPath)
	assert.NotContains(t, string(encoded), filepath.Base(manualPath))
	mockDB.AssertExpectations(t)
}

func TestHandleMediaAsset_DefaultChunkIsBounded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	body := make([]byte, mediaAssetMaxChunkBytes+100)
	manualPath, _ := writeTestPDF(t, root, "large.pdf", body)
	row := makeMediaFullRow(41, 410)
	mockDB := testhelpers.NewMockMediaDBI()
	expectAssetResolveByID(mockDB, row)
	expectAssetProperties(mockDB, row, []database.MediaProperty{{
		TypeTag: "property:manual", Text: manualPath,
	}}, nil)

	result, err := HandleMediaAsset(makeMediaAssetEnv(
		t, mockDB, root, json.RawMessage(`{"mediaId":41,"assetType":"manual"}`),
	))
	require.NoError(t, err)
	resp := requireMediaAssetResponse(t, result)
	assert.Equal(t, mediaAssetMaxChunkBytes, resp.Length)
	assert.False(t, resp.Complete)
	require.NotNil(t, resp.NextOffset)
	assert.Equal(t, mediaAssetMaxChunkBytes, *resp.NextOffset)
	decoded, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Len(t, decoded, int(mediaAssetMaxChunkBytes))
	mockDB.AssertExpectations(t)
}

func TestHandleMediaAsset_CanceledRequest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manualPath, _ := writeTestPDF(t, root, "manual.pdf", []byte("body"))
	row := makeMediaFullRow(40, 400)
	mockDB := testhelpers.NewMockMediaDBI()
	expectAssetResolveByID(mockDB, row)
	expectAssetProperties(mockDB, row, []database.MediaProperty{{
		TypeTag: "property:manual", Text: manualPath,
	}}, nil)
	env := makeMediaAssetEnv(
		t, mockDB, root, json.RawMessage(`{"mediaId":40,"assetType":"manual"}`),
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	env.Context = ctx

	_, err := HandleMediaAsset(env)
	require.ErrorIs(t, err, context.Canceled)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaAsset_ChunkedReconstruction(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manualPath, manual := writeTestPDF(t, root, "manual.pdf", []byte("abcdefghijklmnopqrstuvwxyz"))
	row := makeMediaFullRow(43, 430)
	mockDB := testhelpers.NewMockMediaDBI()

	var rebuilt []byte
	var etag string
	offset := int64(0)
	for {
		expectAssetResolveByID(mockDB, row)
		expectAssetProperties(mockDB, row, []database.MediaProperty{{
			TypeTag: "property:manual", Text: manualPath,
		}}, nil)
		params := fmt.Sprintf(`{"mediaId":43,"assetType":"manual","offset":%d,"length":8`, offset)
		if offset > 0 {
			params += fmt.Sprintf(`,"etag":%q`, etag)
		}
		params += `}`

		result, err := HandleMediaAsset(makeMediaAssetEnv(t, mockDB, root, json.RawMessage(params)))
		require.NoError(t, err)
		resp := requireMediaAssetResponse(t, result)
		assert.Equal(t, offset, resp.Offset)
		assert.LessOrEqual(t, resp.Length, int64(8))
		chunk, err := base64.StdEncoding.DecodeString(resp.Data)
		require.NoError(t, err)
		rebuilt = append(rebuilt, chunk...)
		if etag == "" {
			etag = resp.ETag
		} else {
			assert.Equal(t, etag, resp.ETag)
		}
		if resp.Complete {
			break
		}
		require.NotNil(t, resp.NextOffset)
		offset = *resp.NextOffset
	}

	assert.Equal(t, manual, rebuilt)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaAsset_SystemPathIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manualPath, manual := writeTestPDF(t, root, "manual.pdf", []byte("system path"))
	row := makeMediaFullRow(47, 470)
	mockDB := testhelpers.NewMockMediaDBI()
	mockDB.On("FindSystemBySystemID", row.System.SystemID).Return(row.System, nil).Once()
	mockDB.On("FindMediaBySystemAndPaths", mock.Anything, row.System.DBID, []string{row.Path}).
		Return(map[string]database.Media{row.Path: row.Media}, nil).Once()
	expectAssetResolveByID(mockDB, row)
	expectAssetProperties(mockDB, row, []database.MediaProperty{{
		TypeTag: "property:manual", Text: manualPath,
	}}, nil)
	params := json.RawMessage(fmt.Sprintf(
		`{"system":%q,"path":%q,"assetType":"manual"}`, row.System.SystemID, row.Path,
	))

	result, err := HandleMediaAsset(makeMediaAssetEnv(t, mockDB, root, params))
	require.NoError(t, err)
	resp := requireMediaAssetResponse(t, result)
	decoded, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Equal(t, manual, decoded)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaAsset_UsesEquivalentMediaProperty(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manualPath, manual := writeTestPDF(t, root, "manual.pdf", []byte("alias"))
	row := makeMediaFullRow(48, 480)
	row.Path = filepath.Join("games", "archive.zip")
	alias := database.Media{DBID: 49, Path: filepath.Join(row.Path, "game.rom")}
	mockDB := testhelpers.NewMockMediaDBI()
	expectAssetResolveByID(mockDB, row)
	mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, row.System.DBID, row.Path).
		Return(&alias, nil).Once()
	mockDB.On("GetMediaPropertyMetadataByMediaDBIDs", mock.Anything, []int64{row.DBID, alias.DBID}).
		Return(map[int64][]database.MediaProperty{
			alias.DBID: {{TypeTag: "property:manual", Text: manualPath}},
		}, nil).Once()
	settings := platforms.Settings{ZipsAsDirs: true}

	result, err := HandleMediaAsset(makeMediaAssetEnvWithSettings(
		t, mockDB, root, json.RawMessage(`{"mediaId":48,"assetType":"manual"}`), settings,
	))
	require.NoError(t, err)
	resp := requireMediaAssetResponse(t, result)
	decoded, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Equal(t, manual, decoded)
	mockDB.AssertNotCalled(t, "GetMediaTitlePropertyMetadata", mock.Anything, row.Title.DBID)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaAsset_MediaPropertyPrecedesTitle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mediaPath, mediaManual := writeTestPDF(t, root, "media.pdf", []byte("media"))
	titlePath, _ := writeTestPDF(t, root, "title.pdf", []byte("title"))
	row := makeMediaFullRow(44, 440)
	mockDB := testhelpers.NewMockMediaDBI()
	expectAssetResolveByID(mockDB, row)
	expectAssetProperties(mockDB, row, []database.MediaProperty{{
		TypeTag: "property:manual", Text: mediaPath,
	}}, []database.MediaProperty{{TypeTag: "property:manual", Text: titlePath}})

	result, err := HandleMediaAsset(makeMediaAssetEnv(
		t, mockDB, root, json.RawMessage(`{"mediaId":44,"assetType":"manual"}`),
	))
	require.NoError(t, err)
	resp := requireMediaAssetResponse(t, result)
	decoded, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Equal(t, mediaManual, decoded)
	mockDB.AssertNotCalled(t, "GetMediaTitlePropertyMetadata", mock.Anything, row.Title.DBID)
	mockDB.AssertExpectations(t)
}

func TestHandleMediaAsset_ExactEOF(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manualPath, manual := writeTestPDF(t, root, "manual.pdf", []byte("body"))
	row := makeMediaFullRow(45, 450)
	mockDB := testhelpers.NewMockMediaDBI()

	expectAssetResolveByID(mockDB, row)
	expectAssetProperties(mockDB, row, []database.MediaProperty{{TypeTag: "property:manual", Text: manualPath}}, nil)
	first, err := HandleMediaAsset(makeMediaAssetEnv(
		t, mockDB, root, json.RawMessage(`{"mediaId":45,"assetType":"manual"}`),
	))
	require.NoError(t, err)
	etag := requireMediaAssetResponse(t, first).ETag

	expectAssetResolveByID(mockDB, row)
	expectAssetProperties(mockDB, row, []database.MediaProperty{{TypeTag: "property:manual", Text: manualPath}}, nil)
	params := json.RawMessage(fmt.Sprintf(
		`{"mediaId":45,"assetType":"manual","offset":%d,"etag":%q}`, len(manual), etag,
	))
	result, err := HandleMediaAsset(makeMediaAssetEnv(t, mockDB, root, params))
	require.NoError(t, err)
	resp := requireMediaAssetResponse(t, result)
	assert.Empty(t, resp.Data)
	assert.Zero(t, resp.Length)
	assert.True(t, resp.Complete)
	mockDB.AssertExpectations(t)
}

func TestParseMediaAssetRequest_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params string
	}{
		{"unknown asset", `{"mediaId":1,"assetType":"video"}`},
		{"raw property", `{"mediaId":1,"assetType":"property:manual"}`},
		{"unknown field", `{"mediaId":1,"assetType":"manual","delivery":"inline"}`},
		{"mixed identity", `{"mediaId":1,"system":"NES","path":"game.nes","assetType":"manual"}`},
		{"negative offset", `{"mediaId":1,"assetType":"manual","offset":-1}`},
		{"zero length", `{"mediaId":1,"assetType":"manual","length":0}`},
		{"oversized length", fmt.Sprintf(`{"mediaId":1,"assetType":"manual","length":%d}`, mediaAssetMaxChunkBytes+1)},
		{"missing continuation etag", `{"mediaId":1,"assetType":"manual","offset":1}`},
		{"trailing value", `{"mediaId":1,"assetType":"manual"} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseMediaAssetRequest(json.RawMessage(tt.params))
			require.Error(t, err)
			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr)
		})
	}
}

func TestHandleMediaAsset_RejectsChangedETagAndPastEOF(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manualPath, manual := writeTestPDF(t, root, "manual.pdf", []byte("body"))
	row := makeMediaFullRow(46, 460)

	for _, tt := range []struct {
		name   string
		etag   string
		offset int
	}{
		{name: "changed etag", offset: 1, etag: "stale"},
		{name: "past eof", offset: len(manual) + 1, etag: "current"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := testhelpers.NewMockMediaDBI()
			expectAssetResolveByID(mockDB, row)
			expectAssetProperties(mockDB, row, []database.MediaProperty{{
				TypeTag: "property:manual", Text: manualPath,
			}}, nil)
			etag := tt.etag
			if etag == "current" {
				info, err := os.Stat(manualPath)
				require.NoError(t, err)
				etag = mediaAssetETag(row, "property:manual", manualPath, info.Size(), info.ModTime().UnixNano())
			}
			params := json.RawMessage(fmt.Sprintf(
				`{"mediaId":46,"assetType":"manual","offset":%d,"etag":%q}`, tt.offset, etag,
			))
			_, err := HandleMediaAsset(makeMediaAssetEnv(t, mockDB, root, params))
			require.Error(t, err)
			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr)
			mockDB.AssertExpectations(t)
		})
	}
}

func TestHandleMediaAsset_RejectsUnsafeOrInvalidFiles(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsidePDF, _ := writeTestPDF(t, outside, "outside.pdf", []byte("secret"))
	textPath := filepath.Join(root, "manual.pdf")
	require.NoError(t, os.WriteFile(textPath, []byte("not a PDF"), 0o600))
	directoryPath := filepath.Join(root, "directory.pdf")
	require.NoError(t, os.Mkdir(directoryPath, 0o700))
	missingPath := filepath.Join(root, "missing.pdf")
	symlinkPath := filepath.Join(root, "escape.pdf")
	symlinkSupported := os.Symlink(outsidePDF, symlinkPath) == nil

	tests := []struct {
		name string
		prop database.MediaProperty
	}{
		{name: "outside trusted root", prop: database.MediaProperty{TypeTag: "property:manual", Text: outsidePDF}},
		{name: "invalid signature", prop: database.MediaProperty{TypeTag: "property:manual", Text: textPath}},
		{name: "directory", prop: database.MediaProperty{TypeTag: "property:manual", Text: directoryPath}},
		{name: "missing", prop: database.MediaProperty{TypeTag: "property:manual", Text: missingPath}},
		{name: "blob only", prop: database.MediaProperty{TypeTag: "property:manual", BlobDBID: new(int64)}},
		{name: "relative path", prop: database.MediaProperty{TypeTag: "property:manual", Text: "manual.pdf"}},
	}
	if symlinkSupported {
		tests = append(tests, struct {
			name string
			prop database.MediaProperty
		}{name: "symlink escape", prop: database.MediaProperty{TypeTag: "property:manual", Text: symlinkPath}})
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := makeMediaFullRow(int64(100+i), int64(200+i))
			mockDB := testhelpers.NewMockMediaDBI()
			expectAssetResolveByID(mockDB, row)
			expectAssetProperties(mockDB, row, []database.MediaProperty{tt.prop}, nil)
			params := json.RawMessage(fmt.Sprintf(
				`{"mediaId":%d,"assetType":"manual"}`, row.DBID,
			))
			_, err := HandleMediaAsset(makeMediaAssetEnv(t, mockDB, root, params))
			require.Error(t, err)
			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr)
			assert.Equal(t, "media.asset: no manual asset found", err.Error())
			mockDB.AssertExpectations(t)
		})
	}
}
