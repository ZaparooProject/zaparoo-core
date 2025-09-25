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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCheckAndResumeIndexing_NoInterruption(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

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
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	// Use real database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Create mock state
	st, _ := state.NewState(mockPlatform)

	// Set up interrupted indexing state in real database
	err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning)
	require.NoError(t, err)
	err = db.MediaDB.SetLastIndexedSystem("")
	require.NoError(t, err)
	err = db.MediaDB.SetIndexingSystems([]string{})
	require.NoError(t, err)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Wait for async operation to start and complete
	time.Sleep(100 * time.Millisecond)

	// Verify that indexing resume was triggered (it will fail due to test database limitations)
	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	// With the minimal test database setup, indexing fails at the DBConfig table step
	// This is expected behavior - the test verifies that resume logic is triggered
	assert.Contains(t, []string{mediadb.IndexingStatusCompleted, mediadb.IndexingStatusFailed}, status,
		"Indexing should complete or fail (due to test database limitations)")

	// The important part is that the resume logic was triggered, which we can verify
	// by checking that the status changed from "running" (it may stay running if it failed quickly)
	assert.Contains(t,
		[]string{mediadb.IndexingStatusCompleted, mediadb.IndexingStatusFailed, mediadb.IndexingStatusRunning},
		status, "Status should be in a valid post-resume state")
}

func TestCheckAndResumeIndexing_WithPendingStatus(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

	// Create test dependencies
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{"/test/roms"})

	// Use real database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Create mock state
	st, _ := state.NewState(mockPlatform)

	// Set up interrupted indexing state in real database with "pending" status
	err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusPending)
	require.NoError(t, err)
	err = db.MediaDB.SetLastIndexedSystem("")
	require.NoError(t, err)
	err = db.MediaDB.SetIndexingSystems([]string{})
	require.NoError(t, err)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Wait for async operation to start and complete
	time.Sleep(100 * time.Millisecond)

	// Verify that indexing resume was triggered (it will fail due to test database limitations)
	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	// With the minimal test database setup, indexing fails at the DBConfig table step
	// This is expected behavior - the test verifies that resume logic is triggered
	assert.Contains(t, []string{mediadb.IndexingStatusCompleted, mediadb.IndexingStatusFailed}, status,
		"Indexing should complete or fail (due to test database limitations)")

	// The important part is that the resume logic was triggered, which we can verify
	// by checking that the status changed from "pending"
	assert.NotEqual(t, mediadb.IndexingStatusPending, status, "Status should have changed from pending")
}

func TestCheckAndResumeIndexing_DatabaseError(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

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
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	methods.ClearIndexingStatus()

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
