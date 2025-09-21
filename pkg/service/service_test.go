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

package service

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCheckAndResumeIndexing_NoInterruption(t *testing.T) {
	t.Parallel()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform)

	// Mock database to return "completed" status (no interruption)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Give async goroutine a brief moment to start
	time.Sleep(50 * time.Millisecond)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)

	// Verify that no indexing was triggered (only GetIndexingStatus should be called)
	mockMediaDB.AssertNotCalled(t, "Truncate")
	mockMediaDB.AssertNotCalled(t, "BeginTransaction")
}

func TestCheckAndResumeIndexing_WithRunningStatus(t *testing.T) {
	t.Parallel()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform)

	// Mock database to return "running" status (interrupted indexing)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusRunning, nil)

	// Mock optimization status check (GenerateMediaDB calls this)
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)

	// Mock platform methods needed by NewNamesIndex
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	// Mock methods needed by NewNamesIndex for resuming
	mockMediaDB.On("GetLastIndexedSystem").Return("snes", nil)
	mockMediaDB.On("SetIndexingStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("SetLastIndexedSystem", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("BeginTransaction").Return(nil)
	mockMediaDB.On("CommitTransaction").Return(nil)
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", "pending").Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return()
	mockMediaDB.On("PopulateScanStateFromDB", mock.Anything).Return(nil)
	mockMediaDB.On("Truncate").Return(nil)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Give it time to complete the async operations
	time.Sleep(100 * time.Millisecond)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)

	// Verify that GetIndexingStatus was called to check for interruption
	mockMediaDB.AssertCalled(t, "GetIndexingStatus")
	// GenerateMediaDB should be called, which checks optimization status
	mockMediaDB.AssertCalled(t, "GetOptimizationStatus")
}

func TestCheckAndResumeIndexing_WithPendingStatus(t *testing.T) {
	t.Parallel()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform)

	// Mock database to return "pending" status (interrupted indexing)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusPending, nil)

	// Mock optimization status check (GenerateMediaDB calls this)
	mockMediaDB.On("GetOptimizationStatus").Return("", nil)

	// Mock platform methods needed by NewNamesIndex
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	// Mock methods needed by NewNamesIndex for resuming
	mockMediaDB.On("GetLastIndexedSystem").Return("snes", nil)
	mockMediaDB.On("SetIndexingStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("SetLastIndexedSystem", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("BeginTransaction").Return(nil)
	mockMediaDB.On("CommitTransaction").Return(nil)
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", "pending").Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return()
	mockMediaDB.On("PopulateScanStateFromDB", mock.Anything).Return(nil)
	mockMediaDB.On("Truncate").Return(nil)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Give it time to complete the async operations
	time.Sleep(100 * time.Millisecond)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)

	// Verify that GetIndexingStatus was called to check for interruption
	mockMediaDB.AssertCalled(t, "GetIndexingStatus")
	// GenerateMediaDB should be called, which checks optimization status
	mockMediaDB.AssertCalled(t, "GetOptimizationStatus")
}

func TestCheckAndResumeIndexing_DatabaseError(t *testing.T) {
	t.Parallel()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform)

	// Mock database to return error when checking indexing status
	mockMediaDB.On("GetIndexingStatus").Return("", assert.AnError)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Give async goroutine a brief moment to start
	time.Sleep(50 * time.Millisecond)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)

	// Verify that only GetIndexingStatus was called and no further operations
	mockMediaDB.AssertNotCalled(t, "GetOptimizationStatus")
}

func TestCheckAndResumeIndexing_FailedStatus(t *testing.T) {
	t.Parallel()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Create mock state
	st, _ := state.NewState(mockPlatform)

	// Mock database to return "failed" status (should not resume)
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusFailed, nil)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Give async goroutine a brief moment to start
	time.Sleep(50 * time.Millisecond)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)

	// Verify that no indexing was triggered for failed status
	mockMediaDB.AssertNotCalled(t, "GetOptimizationStatus")
}
