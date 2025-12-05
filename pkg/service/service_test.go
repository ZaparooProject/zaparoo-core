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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Set up interrupted indexing state in real database
	// Use a minimal system list to make the test fast
	err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning)
	require.NoError(t, err)
	err = db.MediaDB.SetLastIndexedSystem("")
	require.NoError(t, err)
	err = db.MediaDB.SetIndexingSystems([]string{"NES"})
	require.NoError(t, err)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Wait for async operation to start and complete
	// With minimal system list (just NES), this should complete quickly
	// Use longer timeout for slower CI environments (especially Windows)
	var status string
	maxWait := 10 * time.Second
	pollInterval := 100 * time.Millisecond
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		var err error
		status, err = db.MediaDB.GetIndexingStatus()
		require.NoError(t, err)
		if status != mediadb.IndexingStatusRunning {
			break
		}
		time.Sleep(pollInterval)
	}

	// If indexing is still running after timeout, cancel it to prevent
	// "database is closed" errors during cleanup
	if status == mediadb.IndexingStatusRunning {
		t.Logf("indexing did not complete within %v, cancelling to prevent cleanup race", maxWait)
		methods.CancelIndexing()
		// Wait for cancellation to complete
		cancelDeadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(cancelDeadline) {
			var err error
			status, err = db.MediaDB.GetIndexingStatus()
			require.NoError(t, err)
			if status != mediadb.IndexingStatusRunning {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Verify that indexing resume was triggered and completed
	// With proper tag seeding, indexing should now complete successfully
	assert.Contains(t, []string{mediadb.IndexingStatusCompleted, mediadb.IndexingStatusFailed}, status,
		"Indexing should complete or fail")
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

	// Set up interrupted indexing state in real database with "pending" status
	// Use a minimal system list to make the test fast
	err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusPending)
	require.NoError(t, err)
	err = db.MediaDB.SetLastIndexedSystem("")
	require.NoError(t, err)
	err = db.MediaDB.SetIndexingSystems([]string{"NES"})
	require.NoError(t, err)

	// Call the function
	checkAndResumeIndexing(mockPlatform, cfg, db, st)

	// Wait for async operation to complete with polling loop
	// With minimal system list (just NES), this should complete quickly
	var status string
	maxWait := time.Second
	pollInterval := 50 * time.Millisecond
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		var getStatusErr error
		status, getStatusErr = db.MediaDB.GetIndexingStatus()
		require.NoError(t, getStatusErr)
		if status != mediadb.IndexingStatusPending {
			break
		}
		time.Sleep(pollInterval)
	}

	// Verify that indexing resume was triggered (it will fail due to test database limitations)
	// With the minimal test database setup, indexing fails at the DBConfig table step
	// This is expected behavior - the test verifies that resume logic is triggered
	// Note: Status could also be "running" if async operation hasn't completed yet
	validStatuses := []string{
		mediadb.IndexingStatusCompleted,
		mediadb.IndexingStatusFailed,
		mediadb.IndexingStatusRunning,
	}
	assert.Contains(t, validStatuses, status,
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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

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
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")

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

func TestStartPublishers_NoPublishers(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel := startPublishers(st, cfg, notifChan)
	defer cancel()

	assert.Empty(t, publishers, "should return empty slice when no publishers configured")
}

func TestStartPublishers_DisabledPublisher(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	configDir := t.TempDir()

	// Create config with explicitly disabled publisher
	configContent := `
schema_version = 1

[service]
api_port = 7497

[[service.publishers.mqtt]]
enabled = false
broker = "localhost:1883"
topic = "zaparoo/events"
`
	err := fs.WriteFile(configDir+"/config.toml", []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, err := testhelpers.NewTestConfigWithPort(fs, configDir, 7497)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel := startPublishers(st, cfg, notifChan)
	defer cancel()

	assert.Empty(t, publishers, "should skip disabled publishers")
}

func TestStartPublishers_InvalidBroker(t *testing.T) {
	t.Parallel()

	fs := testhelpers.NewMemoryFS()
	configDir := t.TempDir()

	// Create config with invalid broker that will fail to connect
	configContent := `
schema_version = 1

[service]
api_port = 7497

[[service.publishers.mqtt]]
broker = "invalid-broker-does-not-exist:1883"
topic = "zaparoo/events"
`
	err := fs.WriteFile(configDir+"/config.toml", []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, err := testhelpers.NewTestConfigWithPort(fs, configDir, 7497)
	require.NoError(t, err)

	mockPlatform := mocks.NewMockPlatform()
	st, _ := state.NewState(mockPlatform, "test-boot-uuid")
	notifChan := make(chan models.Notification)

	publishers, cancel := startPublishers(st, cfg, notifChan)
	defer cancel()

	// Should return empty slice when publisher fails to start
	assert.Empty(t, publishers, "should not include publishers that fail to start")
}

// TestCheckAndResumeIndexing_WaitGroupRace is a regression test for a race condition
// where WaitGroup.Add() was called after WaitGroup.Wait() had already returned.
// The bug occurred when optimization was started as a separate goroutine with its own
// Add(1) inside, creating a window where the indexing goroutine's Done() could cause
// Wait() to return before optimization's Add(1) ran.
//
// This test runs multiple iterations to increase the likelihood of triggering the race
// condition if the bug is reintroduced. Run with: go test -race -run TestCheckAndResumeIndexing_WaitGroupRace
func TestCheckAndResumeIndexing_WaitGroupRace(t *testing.T) {
	// Note: Not using t.Parallel() due to global statusInstance usage in GenerateMediaDB
	// Run multiple iterations to stress-test the race condition
	const iterations = 10

	for range iterations {
		t.Run("iteration", func(t *testing.T) {
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

			// Create mock state
			st, _ := state.NewState(mockPlatform, "test-boot-uuid")

			// Set up interrupted indexing state
			err = db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning)
			require.NoError(t, err)
			err = db.MediaDB.SetLastIndexedSystem("")
			require.NoError(t, err)
			err = db.MediaDB.SetIndexingSystems([]string{"NES"})
			require.NoError(t, err)

			// Call the function - this starts async indexing + optimization
			checkAndResumeIndexing(mockPlatform, cfg, db, st)

			// The critical test: WaitForBackgroundOperations should NOT panic
			// If the race condition exists, this could panic with:
			// "sync: WaitGroup is reused before previous Wait has returned"
			db.MediaDB.WaitForBackgroundOperations()

			// Clean up after waiting for all operations
			cleanup()
		})
	}
}
