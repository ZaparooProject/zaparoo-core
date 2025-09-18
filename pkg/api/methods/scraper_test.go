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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	scraperService "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleScraperSearch(t *testing.T) {
	// Setup mocks
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockConfig := &config.Instance{}

	// Setup mock databases
	mockUserDB := helpers.NewMockUserDBI()
	mockMediaDB := helpers.NewMockMediaDBI()

	// Create a mock state and notification channel
	mockState, _ := state.NewState(mockPlatform)

	// Create notification channel for scraper service
	scraperNotifications := make(chan models.Notification, 10)

	// Create real scraper service with mocked dependencies
	originalService := ScraperServiceInstance
	defer func() { ScraperServiceInstance = originalService }()

	ScraperServiceInstance = scraperService.NewScraperService(
		mockMediaDB,
		mockUserDB,
		mockConfig,
		mockPlatform,
		scraperNotifications,
	)

	// Setup request environment
	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   mockConfig,
		State:    mockState,
	}

	tests := []struct {
		name        string
		params      map[string]interface{}
		wantErr     bool
		checkResult func(t *testing.T, result any)
	}{
		{
			name: "valid search request",
			params: map[string]interface{}{
				"system": "snes",
				"name":   "Super Mario World",
			},
			wantErr: false,
			checkResult: func(t *testing.T, result any) {
				resultMap, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Contains(t, resultMap, "results")
				assert.Contains(t, resultMap, "scraper")
			},
		},
		{
			name: "search with all params",
			params: map[string]interface{}{
				"system":   "genesis",
				"name":     "Sonic",
				"scraper":  "screenscraper",
				"region":   "us",
				"language": "en",
			},
			wantErr: false,
			checkResult: func(t *testing.T, result any) {
				resultMap, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "screenscraper", resultMap["scraper"])
				assert.Equal(t, "us", resultMap["region"])
				assert.Equal(t, "en", resultMap["language"])
			},
		},
		{
			name:    "invalid JSON",
			params:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal params to JSON
			var paramBytes []byte
			if tt.params != nil {
				var err error
				paramBytes, err = json.Marshal(tt.params)
				require.NoError(t, err)
			} else {
				paramBytes = []byte("invalid json")
			}

			env.Params = paramBytes

			result, err := HandleScraperSearch(env)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}

func TestHandleScraperScrapeGame(t *testing.T) {
	// Setup mocks
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := helpers.NewMockUserDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockConfig := &config.Instance{}

	// Setup mock data
	testMedia := &database.Media{
		DBID:           1,
		MediaTitleDBID: 1,
		Path:           "/games/snes/Super Mario World.sfc",
	}
	testMediaTitle := &database.MediaTitle{
		DBID: 1,
		Name: "Super Mario World",
	}

	// Configure mocks
	mockMediaDB.On("Exists").Return(true)
	mockMediaDB.On("GetMediaByID", int64(1)).Return(testMedia, nil)
	mockMediaDB.On("GetMediaTitleByID", int64(1)).Return(testMediaTitle, nil)

	// Mock scraper service
	originalService := ScraperServiceInstance
	defer func() { ScraperServiceInstance = originalService }()
	// ScraperServiceInstance will be nil for this test, which is handled by the error check

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
		name    string
		params  map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name: "scraper service not initialized",
			params: map[string]interface{}{
				"mediaDBID": 1,
			},
			wantErr: true,
			errMsg:  "scraper service not initialized",
		},
		{
			name: "invalid mediaDBID type",
			params: map[string]interface{}{
				"mediaDBID": "invalid",
			},
			wantErr: true,
			errMsg:  "invalid mediaDBID format",
		},
		{
			name: "missing mediaDBID",
			params: map[string]interface{}{
				"overwrite": true,
			},
			wantErr: true,
			errMsg:  "mediaDBID must be a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramBytes, err := json.Marshal(tt.params)
			require.NoError(t, err)
			env.Params = paramBytes

			result, err := HandleScraperScrapeGame(env)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Nil(t, result)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

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
		name    string
		params  map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name: "scraper service not initialized",
			params: map[string]interface{}{
				"system": "snes",
			},
			wantErr: true,
			errMsg:  "scraper service not initialized",
		},
		{
			name:    "missing system parameter",
			params:  map[string]interface{}{},
			wantErr: true,
			errMsg:  "system parameter is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramBytes, err := json.Marshal(tt.params)
			require.NoError(t, err)
			env.Params = paramBytes

			result, err := HandleScraperScrapeSystem(env)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Nil(t, result)
				return
			}

			assert.NoError(t, err)
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

	result, err := HandleScraperProgress(env)

	// Should error because service is not initialized
	assert.Error(t, err)
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
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scraper service not initialized")
	assert.Nil(t, result)
}

func TestHandleScraperConfig(t *testing.T) {
	// Setup mocks
	mockPlatform := mocks.NewMockPlatform()
	mockConfig := &config.Instance{}

	// Setup request environment
	env := requests.RequestEnv{
		Platform: mockPlatform,
		Config:   mockConfig,
	}

	tests := []struct {
		name        string
		params      map[string]interface{}
		wantErr     bool
		checkResult func(t *testing.T, result any)
	}{
		{
			name:   "get config",
			params: map[string]interface{}{"action": "get"},
			checkResult: func(t *testing.T, result any) {
				// Should return a config object
				assert.NotNil(t, result)
			},
		},
		{
			name:   "get config (default action)",
			params: map[string]interface{}{},
			checkResult: func(t *testing.T, result any) {
				assert.NotNil(t, result)
			},
		},
		{
			name: "update config",
			params: map[string]interface{}{
				"action": "update",
				"config": map[string]interface{}{
					"default": "screenscraper",
					"region":  "us",
				},
			},
			checkResult: func(t *testing.T, result any) {
				resultMap, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, true, resultMap["updated"])
				assert.Contains(t, resultMap, "config")
			},
		},
		{
			name:    "update config without config param",
			params:  map[string]interface{}{"action": "update"},
			wantErr: true,
		},
		{
			name:    "invalid action",
			params:  map[string]interface{}{"action": "invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramBytes, err := json.Marshal(tt.params)
			require.NoError(t, err)
			env.Params = paramBytes

			result, err := HandleScraperConfig(env)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}