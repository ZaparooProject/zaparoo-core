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
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaHistoryLatest_Success(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	startedAt := time.Unix(1_770_000_000, 0).UTC()
	mediaPath := filepath.ToSlash(filepath.Join("roms", "snes", "Super Mario World (USA).sfc"))
	mockUserDB.On("GetLatestMediaHistory").Return(database.MediaHistoryEntry{
		DBID:       12,
		SystemID:   "SNES",
		SystemName: "Super Nintendo Entertainment System",
		MediaName:  "Super Mario World",
		MediaPath:  mediaPath,
		LauncherID: "SNES",
		StartTime:  startedAt,
	}, true, nil)

	result, err := HandleMediaHistoryLatest(requests.RequestEnv{
		Context: context.Background(),
		Database: &database.Database{
			UserDB: mockUserDB,
		},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryLatestResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Entry)
	assert.Equal(t, "SNES", resp.Entry.SystemID)
	assert.Equal(t, "Super Nintendo Entertainment System", resp.Entry.SystemName)
	assert.Equal(t, "Super Mario World", resp.Entry.MediaName)
	assert.Equal(t, mediaPath, resp.Entry.MediaPath)
	assert.Equal(t, "SNES", resp.Entry.LauncherID)
	assert.Equal(t, startedAt.Format(time.RFC3339), resp.Entry.StartedAt)
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryLatest_EmptyParamsObject(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetLatestMediaHistory").Return(database.MediaHistoryEntry{}, false, nil)

	result, err := HandleMediaHistoryLatest(requests.RequestEnv{
		Params: []byte("{}"),
		Database: &database.Database{
			UserDB: mockUserDB,
		},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryLatestResponse)
	require.True(t, ok)
	assert.Nil(t, resp.Entry)
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryLatest_NoHistory(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetLatestMediaHistory").Return(database.MediaHistoryEntry{}, false, nil)

	result, err := HandleMediaHistoryLatest(requests.RequestEnv{
		Database: &database.Database{
			UserDB: mockUserDB,
		},
	})
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryLatestResponse)
	require.True(t, ok)
	assert.Nil(t, resp.Entry)
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryLatest_RejectsParams(t *testing.T) {
	t.Parallel()

	result, err := HandleMediaHistoryLatest(requests.RequestEnv{
		Params: []byte(`{"limit":1}`),
	})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestHandleMediaHistoryLatest_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetLatestMediaHistory").Return(database.MediaHistoryEntry{}, false, errors.New("boom"))

	result, err := HandleMediaHistoryLatest(requests.RequestEnv{
		Database: &database.Database{
			UserDB: mockUserDB,
		},
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "error getting latest media history")
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryLatest_ResponseHasNoTagsAndNoMediaDBCalls(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB.On("GetLatestMediaHistory").Return(database.MediaHistoryEntry{
		DBID:       12,
		SystemID:   "SNES",
		SystemName: "Super Nintendo Entertainment System",
		MediaName:  "Super Mario World",
		MediaPath:  filepath.ToSlash(filepath.Join("roms", "snes", "smw.sfc")),
		LauncherID: "SNES",
		StartTime:  time.Unix(1_770_000_000, 0).UTC(),
	}, true, nil)

	result, err := HandleMediaHistoryLatest(requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
	})
	require.NoError(t, err)

	raw, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded struct {
		Entry map[string]any `json:"entry"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NotNil(t, decoded.Entry)
	assert.NotContains(t, decoded.Entry, "tags")
	assert.NotContains(t, decoded.Entry, "mediaId")
	assert.Empty(t, mockMediaDB.Calls, "media.history.latest must not touch the media database")
	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryLatest_RelativePath(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockPlatform := mocks.NewMockPlatform()
	rootDir := filepath.Join(string(filepath.Separator), "mock", "roms")
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{rootDir})
	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{
		{ID: "snes", SystemID: "SNES", Folders: []string{"SNES"}},
	})

	tests := []struct {
		wantRel   *string
		name      string
		mediaPath string
	}{
		{
			name:      "under the launcher folder",
			mediaPath: filepath.Join(rootDir, "SNES", "USA", "Super Mario World.sfc"),
			wantRel:   stringPtr("SNES/USA/Super Mario World.sfc"),
		},
		{
			name:      "outside every launcher folder",
			mediaPath: filepath.Join(string(filepath.Separator), "elsewhere", "Super Mario World.sfc"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockUserDB.On("GetLatestMediaHistory").Return(database.MediaHistoryEntry{
				SystemID:  "SNES",
				MediaPath: tt.mediaPath,
				StartTime: time.Unix(1_770_000_000, 0).UTC(),
			}, true, nil).Once()

			result, err := HandleMediaHistoryLatest(requests.RequestEnv{
				Context:       context.Background(),
				Database:      &database.Database{UserDB: mockUserDB},
				Platform:      mockPlatform,
				Config:        &config.Instance{},
				LauncherCache: launcherCache,
			})
			require.NoError(t, err)

			resp, ok := result.(models.MediaHistoryLatestResponse)
			require.True(t, ok)
			require.NotNil(t, resp.Entry)
			assert.Equal(t, tt.mediaPath, resp.Entry.MediaPath)
			if tt.wantRel == nil {
				assert.Nil(t, resp.Entry.RelPath)
				return
			}
			require.NotNil(t, resp.Entry.RelPath)
			assert.Equal(t, *tt.wantRel, *resp.Entry.RelPath)
		})
	}
}
