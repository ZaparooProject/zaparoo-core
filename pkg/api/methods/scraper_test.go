// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleScraperScrapeSystem(t *testing.T) {
	// Setup mocks
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockConfig := &config.Instance{}

	// Configure mocks
	mockMediaDB.On("Exists").Return(true)
	mockMediaDB.On("GetMediaTitlesBySystem", "snes").Return([]database.MediaTitle{
		{DBID: 1, Name: "Super Mario World"},
		{DBID: 2, Name: "The Legend of Zelda"},
	}, nil)
	mockMediaDB.On("GetGamesWithoutMetadata", "snes", 1000).Return([]database.MediaTitle{
		{DBID: 1, Name: "Super Mario World"},
	}, nil)

	// Mock scraper service - will be nil
	originalService := ScraperServiceInstance
	defer func() { ScraperServiceInstance = originalService }()

	// Setup database
	db := &database.Database{
		MediaDB: mockMediaDB,
		UserDB:  mockUserDB,
	}

	// Setup request environment
	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   mockConfig,
		Database: db,
	}

	tests := []struct {
		params  map[string]any
		name    string
		errMsg  string
		wantErr bool
	}{
		{
			name: "scraper service not initialized",
			params: map[string]any{
				"system": "snes",
			},
			wantErr: true,
			errMsg:  "scraper service not initialized",
		},
		{
			name:    "missing system parameter",
			params:  map[string]any{},
			wantErr: true,
			errMsg:  "system parameter is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramBytes, err := json.Marshal(tt.params)
			require.NoError(t, err)
			env.Params = paramBytes

			result, err := HandleScraperScrapeStart(env)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Nil(t, result)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

func TestHandleScraperProgress(t *testing.T) {
	// Setup mocks
	mockPlatform := mocks.NewMockPlatform()
	mockConfig := &config.Instance{}

	// Mock scraper service - will be nil
	originalService := ScraperServiceInstance
	defer func() { ScraperServiceInstance = originalService }()

	// Setup request environment
	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   mockConfig,
	}

	result, err := HandleScraper(env)

	// Should error because service is not initialized
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scraper service not initialized")
	assert.Nil(t, result)
}

func TestHandleScraperCancel(t *testing.T) {
	// Setup mocks
	mockPlatform := mocks.NewMockPlatform()
	mockConfig := &config.Instance{}

	// Mock scraper service - will be nil
	originalService := ScraperServiceInstance
	defer func() { ScraperServiceInstance = originalService }()

	// Setup request environment
	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   mockConfig,
	}

	result, err := HandleScraperCancel(env)

	// Should error because service is not initialized
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scraper service not initialized")
	assert.Nil(t, result)
}
