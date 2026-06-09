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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestParseMediaRequest_RejectsMixedBatchAndTopLevelRef(t *testing.T) {
	t.Parallel()

	_, err := parseMediaRequest(json.RawMessage(`{
		"mediaId": 1,
		"items": [{"mediaId": 2}]
	}`), maxMediaMetaBatchItems)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "items cannot be mixed")
}

func TestParseMediaRequest_RejectsInvalidBatchItemImageTypes(t *testing.T) {
	t.Parallel()

	_, err := parseMediaRequest(json.RawMessage(`{
		"items": [{"mediaId": 2, "imageTypes": [""]}]
	}`), maxMediaImageBatchItems)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "items[0]")
	assert.Contains(t, err.Error(), "imageTypes entries must be non-empty")
}

func TestParseMediaRequest_RejectsTopLevelImageTypesInBatch(t *testing.T) {
	t.Parallel()

	_, err := parseMediaRequest(json.RawMessage(`{
		"imageTypes": [""],
		"items": [{"mediaId": 2}]
	}`), maxMediaImageBatchItems)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "imageTypes entries must be non-empty")
}

func TestResolveMediaRefs_UsesSingletonFallbackForBatchPath(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()

	system := database.System{DBID: 1, SystemID: "NES", Name: "NES"}
	containerPath := filepath.ToSlash(filepath.Join("roms", "Game.zip"))
	childPath := filepath.ToSlash(filepath.Join(containerPath, "Game.nes"))
	media := database.Media{DBID: 20, Path: childPath}
	row := database.MediaFullRow{
		Media:  media,
		Title:  database.MediaTitle{DBID: 30, Name: "Game"},
		System: system,
	}

	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil).Once()
	mockDB.On("FindMediaBySystemAndPaths", mock.Anything, system.DBID, []string{containerPath}).
		Return(map[string]database.Media{}, nil).Once()
	mockDB.On("FindSingleContainerLaunchMedia", mock.Anything, system.DBID, containerPath).
		Return(&media, nil).Once()
	mockDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{media.DBID}).
		Return(map[int64]database.MediaFullRow{media.DBID: row}, nil).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockDB},
		Platform: platform,
	}
	resolved, err := resolveMediaRefs(env, []mediaRefParam{{System: "NES", Path: containerPath}})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.NoError(t, resolved[0].Err)
	require.NotNil(t, resolved[0].Row)
	assert.Equal(t, childPath, resolved[0].Row.Path)
	mockDB.AssertExpectations(t)
	platform.AssertExpectations(t)
}
