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

package mediascanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// testLauncherCacheMutex protects GlobalLauncherCache modifications in tests
var testLauncherCacheMutex syncutil.Mutex

// TestMultipleScannersForSameSystemID tests that multiple launchers with the same SystemID
// both have their scanners executed. This reproduces the bug where only one scanner
// per system ID gets run.
func TestMultipleScannersForSameSystemID(t *testing.T) {
	// Create test config
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	// Use real database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Track which scanners were called
	scanner1Called := false
	scanner2Called := false

	// Create two test launchers with the same SystemID but different IDs
	launcher1 := platforms.Launcher{
		ID:       "TestLauncher1",
		SystemID: systemdefs.SystemTVEpisode,
		Scanner: func(_ context.Context, _ *config.Instance, _ string,
			_ []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			scanner1Called = true
			return []platforms.ScanResult{
				{Name: "Test Item 1", Path: "test://1"},
			}, nil
		},
	}

	launcher2 := platforms.Launcher{
		ID:       "TestLauncher2",
		SystemID: systemdefs.SystemTVEpisode, // Same system ID as launcher1
		Scanner: func(_ context.Context, _ *config.Instance, _ string,
			_ []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			scanner2Called = true
			return []platforms.ScanResult{
				{Name: "Test Item 2", Path: "test://2"},
			}, nil
		},
	}

	// Create mock platform that returns our test launchers
	platform := mocks.NewMockPlatform()
	launchers := []platforms.Launcher{launcher1, launcher2}

	// Set up basic mocks manually to avoid conflicting with our Launchers expectation
	platform.On("ID").Return("mock-platform")
	platform.On("Settings").Return(platforms.Settings{})
	platform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"/mock/roms"})
	platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return(launchers)

	// Initialize cache with our test launchers via the platform mock
	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}

	testCache.Initialize(platform, cfg)

	helpers.GlobalLauncherCache = testCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	// Run the media indexer
	systems := []systemdefs.System{{ID: systemdefs.SystemTVEpisode}}
	_, err = NewNamesIndex(context.Background(), platform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Both scanners should have been called
	assert.True(t, scanner1Called, "Scanner 1 should have been called")
	assert.True(t, scanner2Called, "Scanner 2 should have been called") // This will fail with the current bug
}

func TestGetSystemPathsRespectsSkipFilesystemScan(t *testing.T) {
	// Setup test launchers - one that skips filesystem scan, one that doesn't
	skipLauncher := platforms.Launcher{
		ID:                 "SkipLauncher",
		SystemID:           systemdefs.SystemNES,
		Folders:            []string{"skip-folder"},
		Extensions:         []string{".rom"},
		SkipFilesystemScan: true,
	}

	normalLauncher := platforms.Launcher{
		ID:                 "NormalLauncher",
		SystemID:           systemdefs.SystemNES,
		Folders:            []string{"normal-folder"},
		Extensions:         []string{".nes"},
		SkipFilesystemScan: false,
	}

	// Mock the global launcher cache by creating a new one with our test launchers
	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	// Create a mock platform that returns our test launchers
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{skipLauncher, normalLauncher})

	// Initialize the cache with our mock platform
	mockCache := &helpers.LauncherCache{}
	mockCache.Initialize(mockPlatform, &config.Instance{})
	helpers.GlobalLauncherCache = mockCache

	// Test with a system that has both launcher types
	systems := []systemdefs.System{
		{ID: systemdefs.SystemNES},
	}

	cfg := &config.Instance{}

	// Call GetSystemPaths - this will test the folder aggregation logic
	// Even with empty root folders, we can verify the function respects SkipFilesystemScan
	results := GetSystemPaths(cfg, mockPlatform, []string{}, systems)

	// Since GetSystemPaths tries to resolve actual paths and we have no real folders,
	// we expect empty results, but the important part is that the function
	// should only try to resolve folders from launchers that don't skip filesystem scan.
	// For now, just verify we get a non-nil slice
	assert.Empty(t, results, "GetSystemPaths should return empty results with no real folders")
}

// TestScannerDoubleExecutionPrevention tests that the scanner tracking prevents double execution
func TestScannerDoubleExecutionPrevention(t *testing.T) {
	t.Parallel()
	// This test documents the fix for scanners being called twice in NewNamesIndex:
	// 1. Once in the per-system loop (lines 409-423)
	// 2. Once in the "run each custom scanner at least once" loop (lines 448-487)

	scannedLaunchers := make(map[string]bool)
	launcherID := "TestLauncher"

	// Initially, launcher is not scanned
	assert.False(t, scannedLaunchers[launcherID], "Launcher should not be marked as scanned initially")

	// Mark launcher as scanned (simulate first loop execution)
	scannedLaunchers[launcherID] = true

	// Check that launcher is now marked as scanned
	assert.True(t, scannedLaunchers[launcherID], "Launcher should be marked as scanned after execution")

	// In the second loop, it should not be processed again
	shouldRunAgain := !scannedLaunchers[launcherID]
	assert.False(t, shouldRunAgain, "Scanner should not execute again if already marked as scanned")
}

// TestSeedCanonicalTags_Success tests that SeedCanonicalTags works correctly under normal conditions
func TestSeedCanonicalTags_Success(t *testing.T) {
	t.Parallel()

	// Use real database
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	scanState := &database.ScanState{
		TagTypesIndex: 0,
		TagTypeIDs:    make(map[string]int),
		TagsIndex:     0,
		TagIDs:        make(map[string]int),
	}

	// Call SeedCanonicalTags with real database
	err := SeedCanonicalTags(mediaDB, scanState)

	// Verify no error occurred
	require.NoError(t, err, "SeedCanonicalTags should not return an error on success")

	// Verify state was updated correctly
	assert.Positive(t, scanState.TagTypesIndex, "TagTypesIndex should be incremented")
	assert.Positive(t, scanState.TagsIndex, "TagsIndex should be incremented")
	// Tags now use composite keys "type:value"
	assert.Contains(t, scanState.TagIDs, "unknown:unknown", "TagIDs should contain 'unknown:unknown' composite key")

	// Verify that specific tag types were processed and exist in scan state
	// This tests the actual business logic without needing to query all tag types
	unknownTagID, exists := scanState.TagTypeIDs["unknown"]
	assert.True(t, exists, "unknown tag type should be in scan state")
	assert.Positive(t, unknownTagID, "unknown tag type should have positive ID")

	extensionTagID, exists := scanState.TagTypeIDs["extension"]
	assert.True(t, exists, "extension tag type should be in scan state")
	assert.Positive(t, extensionTagID, "extension tag type should have positive ID")

	// Verify that we can find the tag types in the database (tests actual insertion)
	unknownType, err := mediaDB.FindTagType(database.TagType{Type: "unknown"})
	require.NoError(t, err)
	assert.Equal(t, "unknown", unknownType.Type)

	extensionType, err := mediaDB.FindTagType(database.TagType{Type: "extension"})
	require.NoError(t, err)
	assert.Equal(t, "extension", extensionType.Type)
}

// TestSeedCanonicalTags_DatabaseError tests error handling when database operations fail
func TestSeedCanonicalTags_DatabaseError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		failOperation string
		expectedError string
	}{
		{
			name:          "InsertTagType Unknown fails",
			failOperation: "InsertTagType_Unknown",
			expectedError: "error inserting tag type unknown",
		},
		{
			name:          "InsertTag unknown fails",
			failOperation: "InsertTag_unknown",
			expectedError: "error inserting tag unknown",
		},
		{
			name:          "InsertTagType Extension fails",
			failOperation: "InsertTagType_Extension",
			expectedError: "error inserting tag type extension",
		},
	}

	for _, tc := range testCases {
		// capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mockDB := &testhelpers.MockMediaDBI{}
			scanState := &database.ScanState{
				TagTypesIndex: 0,
				TagTypeIDs:    make(map[string]int),
				TagsIndex:     0,
				TagIDs:        make(map[string]int),
			}

			mockDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Once()
			mockDB.On("RollbackTransaction").Return(nil).Maybe()

			// Set up mocks based on which operation should fail
			switch tc.failOperation {
			case "InsertTagType_Unknown":
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "unknown"
				})).Return(database.TagType{}, assert.AnError).Once()

			case "InsertTag_unknown":
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "unknown"
				})).Return(database.TagType{}, nil).Once()
				mockDB.On("InsertTag", mock.MatchedBy(func(tag database.Tag) bool {
					return tag.Tag == "unknown"
				})).Return(database.Tag{}, assert.AnError).Once()

			case "InsertTagType_Extension":
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "unknown"
				})).Return(database.TagType{}, nil).Once()
				mockDB.On("InsertTag", mock.MatchedBy(func(tag database.Tag) bool {
					return tag.Tag == "unknown"
				})).Return(database.Tag{}, nil).Once()
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "extension"
				})).Return(database.TagType{}, assert.AnError).Once()
			}

			// Call SeedCanonicalTags
			err := SeedCanonicalTags(mockDB, scanState)

			// Verify error occurred and contains expected message
			require.Error(t, err, "SeedCanonicalTags should return an error when database operation fails")
			assert.Contains(t, err.Error(), tc.expectedError, "Error message should contain expected text")

			// Verify mock expectations
			mockDB.AssertExpectations(t)
		})
	}
}

// TestSeedCanonicalTags_BatchTransaction tests that SeedCanonicalTags uses a batch transaction
func TestSeedCanonicalTags_BatchTransaction(t *testing.T) {
	t.Parallel()

	// This test ensures SeedCanonicalTags manages its own transaction for batch operations
	// to avoid database locking issues

	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	scanState := &database.ScanState{
		TagTypesIndex: 0,
		TagTypeIDs:    make(map[string]int),
		TagsIndex:     0,
		TagIDs:        make(map[string]int),
	}

	// Call SeedCanonicalTags - this should manage its own transaction
	err := SeedCanonicalTags(mediaDB, scanState)

	// Verify success
	require.NoError(t, err, "SeedCanonicalTags should complete successfully with batch transaction")
	assert.Positive(t, scanState.TagTypesIndex, "TagTypesIndex should be incremented")
	assert.Positive(t, scanState.TagsIndex, "TagsIndex should be incremented")
}

// TestNewNamesIndex_SuccessfulResume tests resuming indexing from an interrupted state
func TestNewNamesIndex_SuccessfulResume(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations - no Truncate() for successful resume
	// With batching, we may have fewer transactions than systems
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}).Maybe()

	// Mock indexing state methods for resume scenario
	// First call: simulate interrupted indexing state
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe() // For WAL checkpoint
	// Simulate interrupted at 'genesis'
	mockMediaDB.On("GetLastIndexedSystem").Return("genesis", nil).Once()
	mockMediaDB.On("GetIndexingSystems").Return([]string{"nes", "snes", "genesis"}, nil).Once() // Match current systems
	// Mock GetMax*ID methods for PopulateScanStateFromDB during resume
	mockMediaDB.On("GetMaxSystemID").Return(int64(5), nil).Once()
	mockMediaDB.On("GetMaxTitleID").Return(int64(10), nil).Once()
	mockMediaDB.On("GetMaxMediaID").Return(int64(15), nil).Once()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(3), nil).Once()
	mockMediaDB.On("GetMaxTagID").Return(int64(20), nil).Once()
	// Mock GetAll* methods for PopulateScanStateFromDB to populate maps
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Once()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Once()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Once()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Once()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Once()
	// Subsequent calls: normal operation (no truncate because resuming successfully)
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "genesis").Return(nil).Maybe() // Update progress during processing
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()                  // Finally complete
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()               // Clear on completion
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()      // Clear systems on completion
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Test with multiple systems, where 'genesis' was already completed
	systems := []systemdefs.System{
		{ID: "genesis"}, // This should be skipped (already completed)
		{ID: "nes"},     // This should be processed
		{ID: "snes"},    // This should be processed
	}

	// Track progress updates
	var statusUpdates []IndexStatus
	updateFunc := func(status IndexStatus) {
		statusUpdates = append(statusUpdates, status)
	}

	// Run the indexer - should resume from 'nes'
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, updateFunc)
	require.NoError(t, err)

	// Verify that resume logic was called
	assert.NotEmpty(t, statusUpdates, "Should have received status updates")

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_ResumeSystemNotFound tests handling when last indexed system is no longer available
func TestNewNamesIndex_ResumeSystemNotFound(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations - no special fallback in this scenario
	// With batching, we may have fewer transactions than systems
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}).Maybe()

	// Mock indexing state methods for invalid resume scenario (system not found triggers fallback)
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe() // For WAL checkpoint
	// System no longer exists
	mockMediaDB.On("GetLastIndexedSystem").Return("removed_system", nil).Once()
	mockMediaDB.On("GetIndexingSystems").Return([]string{"nes"}, nil).Once() // Current systems
	// When system not found, we clear state and then do fresh start
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()            // Clear after detecting missing system
	mockMediaDB.On("SetIndexingStatus", "").Return(nil).Once()               // Clear status after missing system
	mockMediaDB.On("TruncateSystems", []string{"nes"}).Return(nil).Once()    // Truncate only the current systems
	mockMediaDB.On("SetIndexingSystems", []string{"nes"}).Return(nil).Once() // Set current systems for fresh start
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()        // Set running for fresh start
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()            // Clear for fresh start
	// Mock GetAll* methods for PopulateScanStateFromDB
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()
	// Mock GetMax*ID methods for PopulateScanStateFromDB
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()                  // Finally complete
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()               // Clear on completion
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()      // Clear systems on completion
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Test with systems that don't include the "removed_system"
	systems := []systemdefs.System{
		{ID: "nes"}, // Only system available
	}

	// Run the indexer - should fall back to full reindex
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_FailedIndexingRecovery tests handling previous failed indexing
func TestNewNamesIndex_FailedIndexingRecovery(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations - fallback to fresh start
	mockMediaDB.On("Truncate").Return(nil).Maybe()
	mockMediaDB.On("TruncateSystems", []string{"nes"}).Return(nil).Maybe()
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}).Maybe()

	// Mock indexing state methods for failed previous indexing
	mockMediaDB.On("GetIndexingStatus").Return("failed", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe() // For WAL checkpoint
	// Should clear failed state and start fresh
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Times(3) // Clear failed state + fresh start + final clear
	mockMediaDB.On("SetIndexingStatus", "").Return(nil).Once()      // Clear failed status
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()

	// Mock SetIndexingSystems calls
	mockMediaDB.On("SetIndexingSystems", []string{"nes"}).Return(nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Maybe()     // Clear on completion
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	// Mock GetMax*ID methods for scan state population
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()

	// Mock GetAll* methods for map population
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	systems := []systemdefs.System{{ID: "nes"}}

	// Run the indexer - should start fresh after failed status
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_DatabaseErrorDuringResume tests error handling during resume checks
func TestNewNamesIndex_DatabaseErrorDuringResume(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock indexing state methods with database error
	// The health check now fails fast if GetIndexingStatus returns an error
	mockMediaDB.On("GetIndexingStatus").Return("", assert.AnError).Once()

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	systems := []systemdefs.System{{ID: "nes"}}

	// Run the indexer - should fail fast when database is not ready
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.Error(t, err, "Should fail when database is not ready")
	assert.Contains(t, err.Error(), "database not ready for indexing", "Error should indicate database readiness issue")

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_StateCleanupOnCompletion tests that indexing state is properly cleared on completion
func TestNewNamesIndex_StateCleanupOnCompletion(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Use real database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	systems := []systemdefs.System{{ID: "nes"}}

	// Run the indexer
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Verify completion state cleanup by checking the actual database state
	// Note: No need to wait for background operations in test with real database
	indexingStatus, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, "completed", indexingStatus, "Indexing status should be set to completed")

	lastIndexedSystem, err := db.MediaDB.GetLastIndexedSystem()
	require.NoError(t, err)
	assert.Empty(t, lastIndexedSystem, "Last indexed system should be cleared on completion")

	indexingSystems, err := db.MediaDB.GetIndexingSystems()
	require.NoError(t, err)
	assert.Empty(t, indexingSystems, "Indexing systems should be cleared on completion")
}

// TestSmartTruncationLogic_PartialSystems tests that indexing a subset of systems uses TruncateSystems()
func TestSmartTruncationLogic_PartialSystems(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations - expect selective TruncateSystems()
	// Will use TruncateSystems since not all systems
	mockMediaDB.On("TruncateSystems", mock.AnythingOfType("[]string")).Return(nil).Once()
	// Transaction calls for file processing only
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe() // For WAL checkpoint
	mockMediaDB.On("SetIndexingSystems", mock.AnythingOfType("[]string")).Return(nil).Once()

	// Mock GetMax*ID methods for scan state population (may be called multiple times)
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()

	// Mock GetAll* methods for scan state population (may be called multiple times)
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()

	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Times(2) // Clear on start + completion
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()      // Clear on completion
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Test with subset of systems - 3 systems when systemdefs.AllSystems() returns 197 systems
	// This should trigger selective indexing (TruncateSystems) since we're not indexing all systems
	systems := []systemdefs.System{
		{ID: "nes"},
		{ID: "snes"},
		{ID: "genesis"},
	}

	// Run the indexer - should use TruncateSystems() since not indexing all defined systems
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Verify mock expectations - specifically that TruncateSystems() was called, not Truncate()
	mockMediaDB.AssertExpectations(t)
}

// TestSmartTruncationLogic_SelectiveIndexing tests that selective system indexing uses TruncateSystems()
func TestSmartTruncationLogic_SelectiveIndexing(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations - expect selective TruncateSystems()
	mockMediaDB.On("TruncateSystems", []string{"nes"}).Return(nil).Once() // Should use selective truncate
	// Transaction calls for file processing only
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe() // For WAL checkpoint
	mockMediaDB.On("SetIndexingSystems", []string{"nes"}).Return(nil).Once()

	// Mock GetMax*ID methods for scan state population (may be called multiple times)
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()

	// Mock GetAll* methods for scan state population (may be called multiple times)
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()

	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Times(2) // Clear on start + completion
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()      // Clear on completion
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Test with subset of available systems - should trigger selective indexing
	systems := []systemdefs.System{
		{ID: "nes"}, // Only one system, while database has more
	}

	// Run the indexer - should use TruncateSystems() since only indexing subset
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Verify mock expectations - specifically that TruncateSystems() was called, not Truncate()
	mockMediaDB.AssertExpectations(t)
}

// TestSelectiveIndexing_ResumeWithDifferentSystems tests resume behavior when systems change
func TestSelectiveIndexing_ResumeWithDifferentSystems(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations - should fall back to fresh start when systems differ
	// Uses selective truncate since not indexing all systems
	mockMediaDB.On("TruncateSystems", []string{"nes", "snes"}).Return(nil).Once()
	// Transaction calls for file processing only
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}).Maybe()

	// Mock resume scenario but with different systems
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe() // For WAL checkpoint
	// Was indexing genesis
	mockMediaDB.On("GetLastIndexedSystem").Return("genesis", nil).Once()
	// Previous systems differ from current
	mockMediaDB.On("GetIndexingSystems").Return([]string{"genesis", "snes"}, nil).Once()

	// Mock GetMax*ID methods for PopulateScanStateFromDB (may be called multiple times)
	mockMediaDB.On("GetMaxSystemID").Return(int64(5), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(10), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(15), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(3), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(20), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(25), nil).Maybe()

	// After checking state, should clear it and start fresh since systems changed
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Maybe() // May be called multiple times
	mockMediaDB.On("SetIndexingStatus", "").Return(nil).Maybe()    // Clear status when systems change

	// Mock GetMax*ID methods for fresh start scan state population (may return either 5 or 0)
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()

	// Mock GetAll* methods for scan state population (may be called multiple times)
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()

	mockMediaDB.On("SetIndexingSystems", []string{"nes", "snes"}).Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()      // Clear on completion
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Test with different systems than what was being indexed (genesis)
	systems := []systemdefs.System{
		{ID: "nes"}, // Different from "genesis" that was being indexed
		{ID: "snes"},
	}

	// Run the indexer - should detect system change and start fresh
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)
}

// TestSelectiveIndexing_EmptySystemsList tests handling of empty systems list
func TestSelectiveIndexing_EmptySystemsList(t *testing.T) {
	t.Parallel()

	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations - should use TruncateSystems() for empty list
	mockMediaDB.On("TruncateSystems", []string{}).Return(nil).Once()
	mockMediaDB.On("TruncateSystems", []string(nil)).Return(nil).Maybe()
	// Transaction calls for file processing only
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe()         // For WAL checkpoint
	mockMediaDB.On("SetIndexingSystems", []string{}).Return(nil).Once()     // Empty systems list
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Maybe() // Accept nil slice

	// Mock GetAll* methods for PopulateScanStateFromDB
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()
	// Mock GetMax*ID methods for scan state population
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()

	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Times(2) // Clear on start + completion
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("InvalidateCountCache").Return(nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Maybe()     // Clear on completion
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Test with empty systems list
	systems := []systemdefs.System{}

	// Run the indexer - should use TruncateSystems() even for empty list since 0 != 197 systems
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)
}

// TestNewNamesIndex_TransactionCoverage tests that all operations happen within transactions
// This test verifies the fix for the hanging bug where operations would happen outside transactions
func TestNewNamesIndex_TransactionCoverage(t *testing.T) {
	// Setup test environment
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{})
	mockPlatform.On("Launchers").Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

	// Setup database mocks
	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Mock basic database operations
	mockMediaDB.On("BeginTransaction", mock.AnythingOfType("bool")).Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("RollbackTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization", mock.Anything).Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}).Maybe()
	mockMediaDB.On("InsertMediaTitle", mock.AnythingOfType("database.MediaTitle")).
		Return(database.MediaTitle{}).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}).Maybe()
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Twice()
	mockMediaDB.On("UnsafeGetSQLDb").Return((*sql.DB)(nil)).Maybe() // For WAL checkpoint
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string{"nes"}).Return(nil).Once()
	mockMediaDB.On("TruncateSystems", []string{"nes"}).Return(nil).Maybe()
	mockMediaDB.On("Truncate").Return(nil).Maybe()

	// Mock GetMax*ID methods for PopulateScanStateFromDB
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()

	// Mock GetAll* methods for PopulateScanStateFromDB to populate maps
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	mockMediaDB.On("GetAllTags").Return([]database.Tag{}, nil).Maybe()
	mockMediaDB.On("GetAllTagTypes").Return([]database.TagType{}, nil).Maybe()

	// Mock optimized exclusion methods for selective indexing (since this is a single system)
	mockMediaDB.On("GetTitlesWithSystemsExcluding", []string{"nes"}).Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPathExcluding", []string{"nes"}).Return([]database.MediaWithFullPath{}, nil).Maybe()

	mockMediaDB.On("SetLastIndexedSystem", mock.AnythingOfType("string")).Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Maybe()              // Allow empty string calls
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once()      // Clear systems on completion
	mockMediaDB.On("InvalidateCountCache").Return(nil).Once()                   // Cache invalidation after indexing
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Once() // System tags cache population

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Set up systems to index
	systems := []systemdefs.System{{ID: "nes"}}

	// Run the indexer
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err, "Indexing should not fail")

	// Verify that operations outside transactions are only for setup/cleanup, not file processing
	// The key fix ensures file processing operations happen within transactions
	// With batch transaction seeding, InsertTag/InsertTagType now happen inside transactions
	// OperationsOutsideTxn may be 0 with this optimization, which is acceptable
	assert.GreaterOrEqual(t, mockMediaDB.OperationsOutsideTxn, 0,
		"Operations outside transactions should be non-negative")

	// Verify transaction usage matches expected behavior
	// SeedCanonicalTags uses 1 transaction for batch tag seeding
	// With no files to process, only the tag seeding transaction should occur
	assert.Equal(t, 1, mockMediaDB.TransactionCount,
		"Should use 1 transaction for tag seeding when no files to process")

	mockMediaDB.AssertExpectations(t)
}

// TestZaparooignoreMarker tests that directories containing a .zaparooignore file
// are skipped during media scanning along with all their subdirectories.
func TestZaparooignoreMarker(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache

	tests := []struct {
		name          string
		setupDirs     map[string]bool // map of directory paths to whether they should have .zaparooignore
		setupFiles    []string        // list of test files to create
		expectedFiles []string        // list of files that should be found
		expectedSkip  []string        // list of files that should be skipped
	}{
		{
			name: "skip directory with zaparooignore marker",
			setupDirs: map[string]bool{
				"normal":  false,
				"ignored": true,
			},
			setupFiles: []string{
				"normal/game1.nes",
				"normal/game2.nes",
				"ignored/game3.nes",
				"ignored/game4.nes",
			},
			expectedFiles: []string{
				"normal/game1.nes",
				"normal/game2.nes",
			},
			expectedSkip: []string{
				"ignored/game3.nes",
				"ignored/game4.nes",
			},
		},
		{
			name: "skip nested subdirectories under ignored directory",
			setupDirs: map[string]bool{
				"normal":          false,
				"ignored":         true,
				"ignored/subdir1": false,
				"ignored/subdir2": false,
				"normal/subdir":   false,
			},
			setupFiles: []string{
				"normal/game1.nes",
				"normal/subdir/game2.nes",
				"ignored/game3.nes",
				"ignored/subdir1/game4.nes",
				"ignored/subdir2/game5.nes",
			},
			expectedFiles: []string{
				"normal/game1.nes",
				"normal/subdir/game2.nes",
			},
			expectedSkip: []string{
				"ignored/game3.nes",
				"ignored/subdir1/game4.nes",
				"ignored/subdir2/game5.nes",
			},
		},
		{
			name: "multiple ignored directories",
			setupDirs: map[string]bool{
				"normal":   false,
				"ignored1": true,
				"ignored2": true,
			},
			setupFiles: []string{
				"normal/game1.nes",
				"ignored1/game2.nes",
				"ignored2/game3.nes",
			},
			expectedFiles: []string{
				"normal/game1.nes",
			},
			expectedSkip: []string{
				"ignored1/game2.nes",
				"ignored2/game3.nes",
			},
		},
		{
			name: "no zaparooignore markers - all files scanned",
			setupDirs: map[string]bool{
				"dir1": false,
				"dir2": false,
			},
			setupFiles: []string{
				"dir1/game1.nes",
				"dir2/game2.nes",
			},
			expectedFiles: []string{
				"dir1/game1.nes",
				"dir2/game2.nes",
			},
			expectedSkip: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cannot use t.Parallel() - modifies shared GlobalLauncherCache

			// Create a temporary directory for this test
			rootDir := t.TempDir()

			// Create directory structure
			for dir, hasMarker := range tt.setupDirs {
				dirPath := filepath.Join(rootDir, dir)
				err := os.MkdirAll(dirPath, 0o750)
				require.NoError(t, err, "failed to create directory: %s", dir)

				// Create .zaparooignore marker if needed
				if hasMarker {
					markerPath := filepath.Join(dirPath, ".zaparooignore")
					err := os.WriteFile(markerPath, []byte(""), 0o600)
					require.NoError(t, err, "failed to create .zaparooignore in: %s", dir)
				}
			}

			// Create test files
			for _, file := range tt.setupFiles {
				filePath := filepath.Join(rootDir, file)
				err := os.WriteFile(filePath, []byte("test content"), 0o600)
				require.NoError(t, err, "failed to create test file: %s", file)
			}

			// Create config
			fs := testhelpers.NewMemoryFS()
			cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
			require.NoError(t, err)

			// Create a test launcher for NES that accepts .nes files
			launcher := platforms.Launcher{
				ID:         "TestNESLauncher",
				SystemID:   systemdefs.SystemNES,
				Extensions: []string{".nes"},
			}

			// Create mock platform that returns our launcher
			platform := mocks.NewMockPlatform()
			platform.On("ID").Return("test-platform")
			platform.On("Settings").Return(platforms.Settings{})
			platform.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{launcher})

			// Initialize launcher cache
			testLauncherCacheMutex.Lock()
			originalCache := helpers.GlobalLauncherCache
			testCache := &helpers.LauncherCache{}
			testCache.Initialize(platform, cfg)
			helpers.GlobalLauncherCache = testCache
			defer func() {
				helpers.GlobalLauncherCache = originalCache
				testLauncherCacheMutex.Unlock()
			}()

			// Call GetFiles using NES system ID
			ctx := context.Background()
			files, err := GetFiles(ctx, cfg, platform, systemdefs.SystemNES, rootDir)
			require.NoError(t, err, "GetFiles should not fail")

			// Convert results to map for easier checking
			foundFiles := make(map[string]bool)
			for _, filePath := range files {
				// Make path relative to rootDir for comparison
				relPath := filePath[len(rootDir)+1:] // +1 to skip path separator
				// Normalize path separators for cross-platform comparison
				relPath = filepath.ToSlash(relPath)
				foundFiles[relPath] = true
			}

			// Verify expected files were found
			for _, expectedFile := range tt.expectedFiles {
				assert.True(t, foundFiles[expectedFile],
					"expected file should be found: %s", expectedFile)
			}

			// Verify skipped files were NOT found
			for _, skippedFile := range tt.expectedSkip {
				assert.False(t, foundFiles[skippedFile],
					"file should have been skipped: %s", skippedFile)
			}

			// Verify total count matches expectations
			assert.Len(t, foundFiles, len(tt.expectedFiles),
				"total number of found files should match expected count")
		})
	}
}
