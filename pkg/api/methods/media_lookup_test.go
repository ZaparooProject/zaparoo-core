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
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaLookup_MatchFound(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	mockMediaDB := testhelpers.NewMockMediaDBI()
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	year := "1985"
	// Cache miss
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return(int64(0), "", false)

	// Strategy 1: exact match with tags returns a result
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Year:     &year,
		},
	}, nil)

	// Cache the resolution
	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything, int64(1), mock.AnythingOfType("string"),
	).Return(nil)

	launcherCache := &helpers.LauncherCache{}

	env := requests.RequestEnv{
		State:         st,
		Database:      &database.Database{MediaDB: mockMediaDB},
		Config:        cfg,
		LauncherCache: launcherCache,
		Params:        json.RawMessage(`{"name": "Super Mario Bros", "system": "NES"}`),
	}

	result, err := HandleMediaLookup(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaLookupResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Match)
	assert.Equal(t, "Super Mario Bros", resp.Match.Name)
	assert.Equal(t, "/games/nes/smb.nes", resp.Match.Path)
	assert.Equal(t, "NES", resp.Match.System.ID)
	assert.Greater(t, resp.Match.Confidence, 0.0)
	assert.Contains(t, resp.Match.ZapScript, "@NES/Super Mario Bros")
}

func TestHandleMediaLookup_NilLauncherCache(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	mockMediaDB := testhelpers.NewMockMediaDBI()
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	year := "1985"
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return(int64(0), "", false)

	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Year:     &year,
		},
	}, nil)

	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything, int64(1), mock.AnythingOfType("string"),
	).Return(nil)

	env := requests.RequestEnv{
		State:         st,
		Database:      &database.Database{MediaDB: mockMediaDB},
		Config:        cfg,
		LauncherCache: nil,
		Params:        json.RawMessage(`{"name": "Super Mario Bros", "system": "NES"}`),
	}

	result, err := HandleMediaLookup(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaLookupResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Match)
	assert.Equal(t, "Super Mario Bros", resp.Match.Name)
}

func TestHandleMediaLookup_NoMatch(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	mockMediaDB := testhelpers.NewMockMediaDBI()
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	// Cache miss
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(int64(0), "", false)

	// All slug-based strategies return empty
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySecondarySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugPrefix",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)
	mockMediaDB.On("SearchMediaBySlugIn",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{}, nil)

	// Fuzzy matching returns empty
	mockMediaDB.On("GetTitlesWithPreFilter",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.MediaTitle{}, nil)

	launcherCache := &helpers.LauncherCache{}

	env := requests.RequestEnv{
		State:         st,
		Database:      &database.Database{MediaDB: mockMediaDB},
		Config:        cfg,
		LauncherCache: launcherCache,
		Params:        json.RawMessage(`{"name": "Nonexistent Game", "system": "NES"}`),
	}

	result, err := HandleMediaLookup(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaLookupResponse)
	require.True(t, ok)
	assert.Nil(t, resp.Match)
}

func TestHandleMediaLookup_MissingParams(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Params: json.RawMessage(`{}`),
	}

	_, err := HandleMediaLookup(env)
	require.Error(t, err)
}

func TestHandleMediaLookup_InvalidSystem(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Params: json.RawMessage(`{"name": "Test", "system": "INVALID_SYSTEM_XYZ"}`),
	}

	_, err := HandleMediaLookup(env)
	require.Error(t, err)
}

func TestHandleMediaLookup_EmptyName(t *testing.T) {
	t.Parallel()

	env := requests.RequestEnv{
		Params: json.RawMessage(`{"name": "", "system": "NES"}`),
	}

	_, err := HandleMediaLookup(env)
	require.Error(t, err)
}

func TestHandleMediaLookup_TagsReturned(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	mockMediaDB := testhelpers.NewMockMediaDBI()
	cfg, err := testhelpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)

	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return(int64(0), "", false)

	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything,
	).Return([]database.SearchResultWithCursor{
		{
			MediaID:  1,
			SystemID: "NES",
			Name:     "Super Mario Bros",
			Path:     "/games/nes/smb.nes",
			Tags: []database.TagInfo{
				{Tag: "platformer", Type: "genre"},
				{Tag: "1985", Type: "year"},
			},
		},
	}, nil)

	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, "NES", mock.AnythingOfType("string"), mock.Anything, int64(1), mock.AnythingOfType("string"),
	).Return(nil)

	launcherCache := &helpers.LauncherCache{}

	env := requests.RequestEnv{
		State:         st,
		Database:      &database.Database{MediaDB: mockMediaDB},
		Config:        cfg,
		LauncherCache: launcherCache,
		Params:        json.RawMessage(`{"name": "Super Mario Bros", "system": "NES"}`),
	}

	result, err := HandleMediaLookup(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaLookupResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Match)
	require.Len(t, resp.Match.Tags, 2)
	assert.Equal(t, "platformer", resp.Match.Tags[0].Tag)
	assert.Equal(t, "genre", resp.Match.Tags[0].Type)
	assert.Equal(t, "1985", resp.Match.Tags[1].Tag)
	assert.Equal(t, "year", resp.Match.Tags[1].Type)
}
