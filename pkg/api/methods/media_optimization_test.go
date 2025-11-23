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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMedia_OptimizationStatus(t *testing.T) {
	tests := []struct {
		optimizationStatusErr error
		optimizationStepErr   error
		expectedStepDisplay   *string
		name                  string
		optimizationStatus    string
		optimizationStep      string
		indexing              bool
		expectedOptimizing    bool
		expectedExists        bool
	}{
		{
			name:                "optimization running with step",
			optimizationStatus:  "running",
			optimizationStep:    "indexes",
			indexing:            false,
			expectedOptimizing:  true,
			expectedExists:      true,
			expectedStepDisplay: stringPtr("indexes"),
		},
		{
			name:                "optimization running without step",
			optimizationStatus:  "running",
			optimizationStep:    "",
			indexing:            false,
			expectedOptimizing:  true,
			expectedExists:      true,
			expectedStepDisplay: nil,
		},
		{
			name:                "optimization completed",
			optimizationStatus:  "completed",
			indexing:            false,
			expectedOptimizing:  false,
			expectedExists:      true,
			expectedStepDisplay: nil,
		},
		{
			name:                "indexing in progress",
			optimizationStatus:  "",
			indexing:            true,
			expectedOptimizing:  false,
			expectedExists:      false,
			expectedStepDisplay: nil,
		},
		{
			name:                  "optimization status error",
			optimizationStatusErr: errors.New("database error"),
			indexing:              false,
			expectedOptimizing:    false,
			expectedExists:        true,
			expectedStepDisplay:   nil,
		},
		{
			name:                "optimization step error",
			optimizationStatus:  "running",
			optimizationStepErr: errors.New("step error"),
			indexing:            false,
			expectedOptimizing:  true,
			expectedExists:      true,
			expectedStepDisplay: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := helpers.NewMockMediaDBI()
			mockUserDB := &helpers.MockUserDBI{}
			mockPlatform := mocks.NewMockPlatform()
			testState, _ := state.NewState(mockPlatform, "test-boot-uuid")

			// Mock optimization status calls
			mockMediaDB.On("GetOptimizationStatus").Return(tt.optimizationStatus, tt.optimizationStatusErr)

			if tt.optimizationStatus == "running" && tt.optimizationStatusErr == nil {
				mockMediaDB.On("GetOptimizationStep").Return(tt.optimizationStep, tt.optimizationStepErr)
			}

			// Mock indexing status
			statusInstance.set(indexingStatusVals{
				indexing:    tt.indexing,
				totalSteps:  10,
				currentStep: 5,
				currentDesc: "Processing files",
				totalFiles:  1000,
			})

			if !tt.indexing && (tt.optimizationStatus != "running" || tt.optimizationStatusErr != nil) {
				// Mock GetLastGenerated for normal operation
				mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
				// Mock GetTotalMediaCount for database that exists and is not indexing
				mockMediaDB.On("GetTotalMediaCount").Return(100, nil)
			} else if !tt.indexing && tt.optimizationStatus == "running" && tt.optimizationStatusErr == nil {
				// Mock GetTotalMediaCount for database that exists but is optimizing
				mockMediaDB.On("GetTotalMediaCount").Return(100, nil)
			}

			db := &database.Database{
				MediaDB: mockMediaDB,
				UserDB:  mockUserDB,
			}
			env := requests.RequestEnv{
				Database: db,
				State:    testState,
			}

			result, err := HandleMedia(env)
			require.NoError(t, err)

			response, ok := result.(models.MediaResponse)
			require.True(t, ok, "result should be MediaResponse")

			assert.Equal(t, tt.expectedOptimizing, response.Database.Optimizing)
			assert.Equal(t, tt.expectedExists, response.Database.Exists)

			if tt.expectedStepDisplay != nil {
				require.NotNil(t, response.Database.CurrentStepDisplay)
				assert.Equal(t, *tt.expectedStepDisplay, *response.Database.CurrentStepDisplay)
			} else if tt.indexing {
				// During indexing, should show indexing step
				require.NotNil(t, response.Database.CurrentStepDisplay)
				assert.Equal(t, "Processing files", *response.Database.CurrentStepDisplay)
			}

			mockMediaDB.AssertExpectations(t)
		})
	}
}

func TestHandleMedia_IndexingAndOptimizationPriority(t *testing.T) {
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := &helpers.MockUserDBI{}
	mockPlatform := mocks.NewMockPlatform()
	testState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Both indexing and optimization are "running"
	mockMediaDB.On("GetOptimizationStatus").Return("running", nil)

	// Set indexing as active - use set() to avoid data race
	statusInstance.set(indexingStatusVals{
		indexing:    true,
		totalSteps:  10,
		currentStep: 5,
		currentDesc: "Indexing files",
		totalFiles:  1000,
	})

	db := &database.Database{
		MediaDB: mockMediaDB,
		UserDB:  mockUserDB,
	}
	env := requests.RequestEnv{
		Database: db,
		State:    testState,
	}

	result, err := HandleMedia(env)
	require.NoError(t, err)

	response, ok := result.(models.MediaResponse)
	require.True(t, ok, "result should be MediaResponse")

	// Indexing should take priority over optimization
	assert.True(t, response.Database.Indexing)
	assert.False(t, response.Database.Exists)     // During indexing, database is considered non-existent
	assert.False(t, response.Database.Optimizing) // Should not show optimizing during indexing

	// Should show indexing details
	require.NotNil(t, response.Database.TotalSteps)
	assert.Equal(t, 10, *response.Database.TotalSteps)
	require.NotNil(t, response.Database.CurrentStep)
	assert.Equal(t, 5, *response.Database.CurrentStep)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMedia_OptimizationStatusIntegration(t *testing.T) {
	tests := []struct {
		expectedResponse   func(response models.MediaResponse)
		name               string
		optimizationStatus string
	}{
		{
			name:               "pending optimization",
			optimizationStatus: "pending",
			expectedResponse: func(response models.MediaResponse) {
				assert.False(t, response.Database.Optimizing)
				assert.True(t, response.Database.Exists)
			},
		},
		{
			name:               "failed optimization",
			optimizationStatus: "failed",
			expectedResponse: func(response models.MediaResponse) {
				assert.False(t, response.Database.Optimizing)
				assert.True(t, response.Database.Exists)
			},
		},
		{
			name:               "unknown optimization status",
			optimizationStatus: "unknown",
			expectedResponse: func(response models.MediaResponse) {
				assert.False(t, response.Database.Optimizing)
				assert.True(t, response.Database.Exists)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := helpers.NewMockMediaDBI()
			mockUserDB := &helpers.MockUserDBI{}
			mockPlatform := mocks.NewMockPlatform()
			testState, _ := state.NewState(mockPlatform, "test-boot-uuid")

			// Mock optimization status
			mockMediaDB.On("GetOptimizationStatus").Return(tt.optimizationStatus, nil)

			ClearIndexingStatus()
			mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
			mockMediaDB.On("GetTotalMediaCount").Return(100, nil)

			db := &database.Database{
				MediaDB: mockMediaDB,
				UserDB:  mockUserDB,
			}
			env := requests.RequestEnv{
				Database: db,
				State:    testState,
			}

			result, err := HandleMedia(env)
			require.NoError(t, err)

			response, ok := result.(models.MediaResponse)
			require.True(t, ok, "result should be MediaResponse")
			tt.expectedResponse(response)

			mockMediaDB.AssertExpectations(t)
		})
	}
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}
