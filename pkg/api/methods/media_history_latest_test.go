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
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
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
