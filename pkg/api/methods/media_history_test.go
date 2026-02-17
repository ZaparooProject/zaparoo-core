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
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaHistory_NoParams(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()
	endTime := now.Add(30 * time.Minute)

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID:       1,
			SystemID:   "NES",
			SystemName: "Nintendo Entertainment System",
			MediaName:  "Super Mario Bros",
			MediaPath:  "/games/nes/smb.nes",
			LauncherID: "retroarch-nes",
			StartTime:  now,
			EndTime:    &endTime,
			PlayTime:   1800,
		},
	}, nil)

	env := requests.RequestEnv{
		Context: context.Background(),
		Database: &database.Database{
			UserDB: mockUserDB,
		},
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "NES", resp.Entries[0].SystemID)
	assert.Equal(t, "Super Mario Bros", resp.Entries[0].MediaName)
	assert.Equal(t, 1800, resp.Entries[0].PlayTime)
	assert.NotNil(t, resp.Entries[0].EndedAt)
	assert.NotNil(t, resp.Pagination)
	assert.False(t, resp.Pagination.HasNextPage)
	assert.Equal(t, 25, resp.Pagination.PageSize)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_WithLimit(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	entries := make([]database.MediaHistoryEntry, 6)
	for i := range entries {
		entries[i] = database.MediaHistoryEntry{
			DBID: int64(i + 1), SystemID: "NES", SystemName: "NES",
			MediaName:  fmt.Sprintf("Game %d", i+1),
			MediaPath:  fmt.Sprintf("/g%d", i+1),
			LauncherID: "l1", StartTime: now,
			PlayTime: (i + 1) * 100,
		}
	}
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 6).Return(entries, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"limit": 5}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 5)
	require.NotNil(t, resp.Pagination)
	assert.True(t, resp.Pagination.HasNextPage)
	assert.NotNil(t, resp.Pagination.NextCursor)
	assert.Equal(t, 5, resp.Pagination.PageSize)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_WithCursor(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	cursorStr, err := encodeCursor(5)
	require.NoError(t, err)

	mockUserDB.On("GetMediaHistory", []string(nil), int64(5), 26).Return([]database.MediaHistoryEntry{
		{
			DBID: 6, SystemID: "SNES", SystemName: "SNES",
			MediaName: "Game 6", MediaPath: "/g6",
			LauncherID: "l1", StartTime: now, PlayTime: 100,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"cursor": "` + cursorStr + `"}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_EmptyHistory(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)
	assert.Nil(t, resp.Pagination)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_InvalidCursor(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"cursor": "not-valid-base64!!!"}`),
	}

	_, err := HandleMediaHistory(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cursor")
}

func TestHandleMediaHistory_NullEndTime(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID:       1,
			SystemID:   "Genesis",
			SystemName: "Sega Genesis",
			MediaName:  "Sonic",
			MediaPath:  "/games/gen/sonic.md",
			LauncherID: "retroarch-gen",
			StartTime:  now,
			EndTime:    nil,
			PlayTime:   60,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	require.Len(t, resp.Entries, 1)
	assert.Nil(t, resp.Entries[0].EndedAt)
	assert.Equal(t, 60, resp.Entries[0].PlayTime)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_SystemFilter(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On("GetMediaHistory", []string{"SNES"}, int64(0), 26).Return([]database.MediaHistoryEntry{
		{
			DBID:       1,
			SystemID:   "SNES",
			SystemName: "Super Nintendo Entertainment System",
			MediaName:  "Super Mario World",
			MediaPath:  "/games/snes/smw.sfc",
			LauncherID: "retroarch-snes",
			StartTime:  now,
			PlayTime:   3600,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["SNES"]}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_FuzzySystemResolution(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistory", []string{"Genesis"}, int64(0), 26).
		Return([]database.MediaHistoryEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["sega genesis"], "fuzzySystem": true}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_EmptySystemsArray(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 26).
		Return([]database.MediaHistoryEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": []}`),
	}

	result, err := HandleMediaHistory(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistory_InvalidSystemID(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"systems": ["NOT_A_REAL_SYSTEM"]}`),
	}

	_, err := HandleMediaHistory(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_A_REAL_SYSTEM")
}

func TestHandleMediaHistory_InvalidParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{invalid json`),
	}

	_, err := HandleMediaHistory(env)
	require.Error(t, err)
}
