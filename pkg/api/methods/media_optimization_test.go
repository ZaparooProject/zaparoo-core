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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
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
		dbEmpty               bool
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
			name:                "first index in progress on empty database",
			optimizationStatus:  "",
			indexing:            true,
			dbEmpty:             true,
			expectedOptimizing:  false,
			expectedExists:      false,
			expectedStepDisplay: nil,
		},
		{
			name:                "reindex in progress keeps database available",
			optimizationStatus:  "",
			indexing:            true,
			expectedOptimizing:  false,
			expectedExists:      true,
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

			// Existence is derived from lastGenerated (with a HasAnyMedia
			// fallback) in every branch, including mid-index.
			lastGenerated := time.Now()
			if tt.dbEmpty {
				lastGenerated = time.Unix(0, 0)
			}
			mockMediaDB.On("GetLastGenerated").Return(lastGenerated, nil).Maybe()
			mockMediaDB.On("GetTotalMediaCount").Return(100, nil).Maybe()

			db := &database.Database{
				MediaDB: mockMediaDB,
				UserDB:  mockUserDB,
			}
			env := requests.RequestEnv{
				Context:  context.Background(),
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
	// Empty database: first index still running, nothing committed yet.
	mockMediaDB.On("GetLastGenerated").Return(time.Unix(0, 0), nil).Maybe()

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
		Context:  context.Background(),
		Database: db,
		State:    testState,
	}

	result, err := HandleMedia(env)
	require.NoError(t, err)

	response, ok := result.(models.MediaResponse)
	require.True(t, ok, "result should be MediaResponse")

	// Indexing should take priority over optimization
	assert.True(t, response.Database.Indexing)
	assert.False(t, response.Database.Exists)     // Empty DB mid-first-index: nothing queryable yet
	assert.False(t, response.Database.Optimizing) // Should not show optimizing during indexing

	// Should show indexing details
	require.NotNil(t, response.Database.TotalSteps)
	assert.Equal(t, 10, *response.Database.TotalSteps)
	require.NotNil(t, response.Database.CurrentStep)
	assert.Equal(t, 5, *response.Database.CurrentStep)

	mockMediaDB.AssertExpectations(t)
}

// TestHandleMedia_BrowseCacheRebuildShowsOptimizing covers a standalone browse-cache
// rebuild (e.g. the startup self-heal), which runs outside a full RunBackgroundOptimization
// pass and so does not set the persisted OptimizationStatus. A client polling the media
// status mid-rebuild must still see Optimizing:true, via MediaDB.IsOptimizing(), so it can
// show a "preparing library" indicator instead of silent slow browse.
func TestHandleMedia_BrowseCacheRebuildShowsOptimizing(t *testing.T) {
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := &helpers.MockUserDBI{}
	mockPlatform := mocks.NewMockPlatform()
	testState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Not indexing, no full optimization running, but a browse-cache rebuild is in flight.
	mockMediaDB.On("GetOptimizationStatus").Return("completed", nil)
	mockMediaDB.On("GetOptimizationStep").Return("", nil)
	mockMediaDB.On("GetTotalMediaCount").Return(100, nil)
	mockMediaDB.BeginBrowseCacheRebuild()

	statusInstance.set(indexingStatusVals{indexing: false})

	db := &database.Database{MediaDB: mockMediaDB, UserDB: mockUserDB}
	env := requests.RequestEnv{Context: context.Background(), Database: db, State: testState}

	result, err := HandleMedia(env)
	require.NoError(t, err)
	response, ok := result.(models.MediaResponse)
	require.True(t, ok, "result should be MediaResponse")

	assert.True(t, response.Database.Optimizing, "browse-cache rebuild should surface as optimizing")
	assert.True(t, response.Database.Exists)
	// A standalone rebuild has no persisted optimization step, so none is displayed.
	assert.Nil(t, response.Database.CurrentStepDisplay)
	mockMediaDB.AssertExpectations(t)
}

// TestHandleMedia_IndexingPriorityOverBrowseCacheRebuild confirms an active reindex
// still wins over a browse-cache rebuild in progress: indexing shows, optimizing does not.
func TestHandleMedia_IndexingPriorityOverBrowseCacheRebuild(t *testing.T) {
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := &helpers.MockUserDBI{}
	mockPlatform := mocks.NewMockPlatform()
	testState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	mockMediaDB.On("GetOptimizationStatus").Return("completed", nil)
	mockMediaDB.On("GetLastGenerated").Return(time.Unix(0, 0), nil).Maybe()
	mockMediaDB.BeginBrowseCacheRebuild()

	statusInstance.set(indexingStatusVals{indexing: true, totalSteps: 10, currentStep: 5})

	db := &database.Database{MediaDB: mockMediaDB, UserDB: mockUserDB}
	env := requests.RequestEnv{Context: context.Background(), Database: db, State: testState}

	result, err := HandleMedia(env)
	require.NoError(t, err)
	response, ok := result.(models.MediaResponse)
	require.True(t, ok, "result should be MediaResponse")

	assert.True(t, response.Database.Indexing)
	assert.False(t, response.Database.Optimizing, "indexing must take priority over browse-cache rebuild")
	assert.False(t, response.Database.Exists)
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
			optimizationStatus: mediadb.IndexingStatusPending,
			expectedResponse: func(response models.MediaResponse) {
				assert.True(t, response.Database.Optimizing)
				assert.True(t, response.Database.Exists)
				require.NotNil(t, response.Database.CurrentStepDisplay)
				assert.Equal(t, preparingDatabaseOptimizationDisplay, *response.Database.CurrentStepDisplay)
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
			if tt.optimizationStatus == mediadb.IndexingStatusPending {
				mockMediaDB.On("GetOptimizationStep").Return("", nil)
			}

			ClearIndexingStatus()
			if tt.optimizationStatus != mediadb.IndexingStatusPending {
				mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
			}
			mockMediaDB.On("GetTotalMediaCount").Return(100, nil)

			db := &database.Database{
				MediaDB: mockMediaDB,
				UserDB:  mockUserDB,
			}
			env := requests.RequestEnv{
				Context:  context.Background(),
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

func TestHandleMedia_PersistedIndexingStatusShowsPreparingResume(t *testing.T) {
	mockMediaDB := helpers.NewMockMediaDBI()
	mockUserDB := &helpers.MockUserDBI{}
	mockPlatform := mocks.NewMockPlatform()
	testState, _ := state.NewState(mockPlatform, "test-boot-uuid")

	ClearIndexingStatus()
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusRunning, nil)
	// Interrupted first index on an empty database awaiting resume.
	mockMediaDB.On("GetLastGenerated").Return(time.Unix(0, 0), nil).Maybe()

	db := &database.Database{MediaDB: mockMediaDB, UserDB: mockUserDB}
	env := requests.RequestEnv{Context: context.Background(), Database: db, State: testState}

	result, err := HandleMedia(env)
	require.NoError(t, err)
	response, ok := result.(models.MediaResponse)
	require.True(t, ok, "result should be MediaResponse")

	assert.True(t, response.Database.Indexing)
	assert.False(t, response.Database.Optimizing)
	assert.False(t, response.Database.Exists)
	require.NotNil(t, response.Database.CurrentStepDisplay)
	assert.Equal(t, preparingResumeMediaDatabaseUpdateDisplay, *response.Database.CurrentStepDisplay)
	mockMediaDB.AssertExpectations(t)
}

func TestGenerateMediaDB_NotifiesPreparingBeforeOptimizationPreflightFailure(t *testing.T) {
	ClearIndexingStatus()
	t.Cleanup(ClearIndexingStatus)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.Optimizing = true
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusRunning, nil).Once()
	mockMediaDB.On("GetLastGenerated").Return(time.Unix(0, 0), nil).Maybe()
	ns := make(chan models.Notification, 2)
	db := &database.Database{MediaDB: mockMediaDB}

	err := GenerateMediaDB(context.Background(), nil, nil, ns, nil, db, nil)
	require.Error(t, err)

	first := <-ns
	require.Equal(t, models.NotificationMediaIndexing, first.Method)
	var firstPayload models.IndexingStatusResponse
	require.NoError(t, json.Unmarshal(first.Params, &firstPayload))
	assert.True(t, firstPayload.Indexing)
	require.NotNil(t, firstPayload.CurrentStepDisplay)
	assert.Equal(t, preparingMediaDatabaseUpdateDisplay, *firstPayload.CurrentStepDisplay)

	second := <-ns
	require.Equal(t, models.NotificationMediaIndexing, second.Method)
	var secondPayload models.IndexingStatusResponse
	require.NoError(t, json.Unmarshal(second.Params, &secondPayload))
	assert.False(t, secondPayload.Indexing)
	mockMediaDB.AssertExpectations(t)
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}
