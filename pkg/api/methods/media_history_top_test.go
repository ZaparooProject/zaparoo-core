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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaHistoryTop_NoParams(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "SNES",
			SystemName:    "Super Nintendo Entertainment System",
			MediaName:     "Super Mario World",
			MediaPath:     "/games/snes/smw.sfc",
			TotalPlayTime: 7200,
			SessionCount:  12,
			LastPlayedAt:  now,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)
	assert.Equal(t, "Super Mario World", resp.Entries[0].MediaName)
	assert.Equal(t, 7200, resp.Entries[0].TotalPlayTime)
	assert.Equal(t, 12, resp.Entries[0].SessionCount)
	assert.Equal(t, now.Format(time.RFC3339), resp.Entries[0].LastPlayedAt)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_CustomLimit(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 10).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"limit": 10}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_EmptySystemsArray(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": []}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_SystemFilter(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string{"SNES"}, (*time.Time)(nil), 25).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "SNES",
			SystemName:    "Super Nintendo Entertainment System",
			MediaName:     "Super Mario World",
			MediaPath:     "/games/snes/smw.sfc",
			TotalPlayTime: 3600,
			SessionCount:  5,
			LastPlayedAt:  time.Now(),
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["SNES"]}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_MultipleSystems(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	now := time.Now()

	mockUserDB.On(
		"GetMediaHistoryTop", []string{"SNES", "NES"}, (*time.Time)(nil), 25,
	).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "SNES",
			SystemName:    "Super Nintendo Entertainment System",
			MediaName:     "Super Mario World",
			MediaPath:     "/games/snes/smw.sfc",
			TotalPlayTime: 7200,
			SessionCount:  12,
			LastPlayedAt:  now,
		},
		{
			SystemID:      "NES",
			SystemName:    "Nintendo Entertainment System",
			MediaName:     "Super Mario Bros",
			MediaPath:     "/games/nes/smb.nes",
			TotalPlayTime: 3600,
			SessionCount:  5,
			LastPlayedAt:  now,
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["SNES", "NES"]}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 2)
	assert.Equal(t, "SNES", resp.Entries[0].SystemID)
	assert.Equal(t, "NES", resp.Entries[1].SystemID)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_FuzzySystemResolution(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string{"Genesis"}, (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["sega genesis"], "fuzzySystem": true}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_SinceFilter(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	sinceStr := "2026-01-01T00:00:00Z"
	sinceTime, _ := time.Parse(time.RFC3339, sinceStr)

	mockUserDB.On("GetMediaHistoryTop", []string(nil), &sinceTime, 25).Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"since": "` + sinceStr + `"}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_AllFilters(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	sinceStr := "2026-02-01T00:00:00Z"
	sinceTime, _ := time.Parse(time.RFC3339, sinceStr)

	mockUserDB.On("GetMediaHistoryTop", []string{"Genesis"}, &sinceTime, 5).Return([]database.MediaHistoryTopEntry{
		{
			SystemID:      "Genesis",
			SystemName:    "Sega Genesis",
			MediaName:     "Sonic the Hedgehog",
			MediaPath:     "/games/gen/sonic.md",
			TotalPlayTime: 1800,
			SessionCount:  3,
			LastPlayedAt:  time.Now(),
		},
	}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
		Params:   json.RawMessage(`{"systems": ["Genesis"], "since": "` + sinceStr + `", "limit": 5}`),
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, "Genesis", resp.Entries[0].SystemID)
	assert.Equal(t, 1800, resp.Entries[0].TotalPlayTime)
	assert.Equal(t, 3, resp.Entries[0].SessionCount)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_EmptyResults(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).
		Return([]database.MediaHistoryTopEntry{}, nil)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := HandleMediaHistoryTop(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaHistoryTopResponse)
	require.True(t, ok)
	assert.Empty(t, resp.Entries)

	mockUserDB.AssertExpectations(t)
}

func TestHandleMediaHistoryTop_InvalidSystemID(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"systems": ["NOT_A_REAL_SYSTEM"]}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_A_REAL_SYSTEM")
}

func TestHandleMediaHistoryTop_InvalidParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{invalid json`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
}

func TestHandleMediaHistoryTop_LimitZero(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"limit": 0}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

func TestHandleMediaHistoryTop_LimitExceedsMax(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"limit": 200}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

func TestHandleMediaHistoryTop_InvalidSince(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
		Params:   json.RawMessage(`{"since": "not-a-timestamp"}`),
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid since timestamp")
}

func TestHandleMediaHistoryTop_DatabaseError(t *testing.T) {
	t.Parallel()

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("GetMediaHistoryTop", []string(nil), (*time.Time)(nil), 25).Return(
		[]database.MediaHistoryTopEntry{}, errors.New("db error"),
	)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}

	_, err := HandleMediaHistoryTop(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error getting media history top")

	mockUserDB.AssertExpectations(t)
}
