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
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSystems_IncludesIndexedAndAvailableLauncherSystems(t *testing.T) {
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("IndexedSystems").Return([]string{"NES"}, nil)

	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "indexed", SystemID: "NES"},
		{ID: "available", SystemID: "SNES"},
		{
			ID:           "unavailable",
			SystemID:     "Genesis",
			Availability: func(*config.Instance) error { return errors.New("not installed") },
		},
	})

	result, err := HandleSystems(requests.RequestEnv{
		Database:      &database.Database{MediaDB: mockMediaDB},
		LauncherCache: cache,
	})
	require.NoError(t, err)

	response, ok := result.(models.SystemsResponse)
	require.True(t, ok)
	assert.Equal(t, []string{"NES", "SNES"}, systemResponseIDs(response))
	mockMediaDB.AssertExpectations(t)
}

func TestHandleSystems_AllIncludesUnavailableLauncherSystems(t *testing.T) {
	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockMediaDB.On("IndexedSystems").Return([]string{"NES"}, nil)

	cache := &helpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{
		{ID: "available", SystemID: "SNES"},
		{
			ID:           "unavailable",
			SystemID:     "Genesis",
			Availability: func(*config.Instance) error { return errors.New("not installed") },
		},
	})

	result, err := HandleSystems(requests.RequestEnv{
		Database:      &database.Database{MediaDB: mockMediaDB},
		LauncherCache: cache,
		Params:        json.RawMessage(`{"all":true}`),
	})
	require.NoError(t, err)

	response, ok := result.(models.SystemsResponse)
	require.True(t, ok)
	assert.Equal(t, []string{"NES", "SNES", "Genesis"}, systemResponseIDs(response))
	mockMediaDB.AssertExpectations(t)
}

func TestHandleSystems_RejectsInvalidParams(t *testing.T) {
	result, err := HandleSystems(requests.RequestEnv{Params: json.RawMessage(`{"all":"yes"}`)})

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
}

func systemResponseIDs(response models.SystemsResponse) []string {
	ids := make([]string, len(response.Systems))
	for i := range response.Systems {
		ids[i] = response.Systems[i].ID
	}
	return ids
}
