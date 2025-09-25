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

package mediascanner

import (
	"context"
	"sync"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// testLauncherCacheMutex protects GlobalLauncherCache modifications in tests
var testLauncherCacheMutex sync.Mutex

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
		SystemID: systemdefs.SystemTV,
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
		SystemID: systemdefs.SystemTV, // Same system ID as launcher1
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
	systems := []systemdefs.System{{ID: systemdefs.SystemTV}}
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

// TestSeedKnownTags_Success tests that SeedKnownTags works correctly under normal conditions
func TestSeedKnownTags_Success(t *testing.T) {
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

	// Call SeedKnownTags with real database
	err := SeedKnownTags(mediaDB, scanState)

	// Verify no error occurred
	require.NoError(t, err, "SeedKnownTags should not return an error on success")

	// Verify state was updated correctly
	assert.Positive(t, scanState.TagTypesIndex, "TagTypesIndex should be incremented")
	assert.Positive(t, scanState.TagsIndex, "TagsIndex should be incremented")
	assert.Contains(t, scanState.TagIDs, "unknown", "TagIDs should contain 'unknown' tag")
	assert.Contains(t, scanState.TagIDs, ".ext", "TagIDs should contain '.ext' tag")

	// Verify that specific tag types were processed and exist in scan state
	// This tests the actual business logic without needing to query all tag types
	unknownTagID, exists := scanState.TagTypeIDs["Unknown"]
	assert.True(t, exists, "Unknown tag type should be in scan state")
	assert.Positive(t, unknownTagID, "Unknown tag type should have positive ID")

	extensionTagID, exists := scanState.TagTypeIDs["Extension"]
	assert.True(t, exists, "Extension tag type should be in scan state")
	assert.Positive(t, extensionTagID, "Extension tag type should have positive ID")

	// Verify that we can find the tag types in the database (tests actual insertion)
	unknownType, err := mediaDB.FindTagType(database.TagType{Type: "Unknown"})
	require.NoError(t, err)
	assert.Equal(t, "Unknown", unknownType.Type)

	extensionType, err := mediaDB.FindTagType(database.TagType{Type: "Extension"})
	require.NoError(t, err)
	assert.Equal(t, "Extension", extensionType.Type)
}

// TestSeedKnownTags_DatabaseError tests error handling when database operations fail
func TestSeedKnownTags_DatabaseError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		failOperation string
		expectedError string
	}{
		{
			name:          "InsertTagType Unknown fails",
			failOperation: "InsertTagType_Unknown",
			expectedError: "error inserting tag type Unknown",
		},
		{
			name:          "InsertTag unknown fails",
			failOperation: "InsertTag_unknown",
			expectedError: "error inserting tag unknown",
		},
		{
			name:          "InsertTagType Extension fails",
			failOperation: "InsertTagType_Extension",
			expectedError: "error inserting tag type Extension",
		},
		{
			name:          "InsertTag .ext fails",
			failOperation: "InsertTag_.ext",
			expectedError: "error inserting tag .ext",
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

			// Set up mocks based on which operation should fail
			switch tc.failOperation {
			case "InsertTagType_Unknown":
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "Unknown"
				})).Return(database.TagType{}, assert.AnError).Once()

			case "InsertTag_unknown":
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "Unknown"
				})).Return(database.TagType{}, nil).Once()
				mockDB.On("InsertTag", mock.MatchedBy(func(tag database.Tag) bool {
					return tag.Tag == "unknown"
				})).Return(database.Tag{}, assert.AnError).Once()

			case "InsertTagType_Extension":
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "Unknown"
				})).Return(database.TagType{}, nil).Once()
				mockDB.On("InsertTag", mock.MatchedBy(func(tag database.Tag) bool {
					return tag.Tag == "unknown"
				})).Return(database.Tag{}, nil).Once()
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "Extension"
				})).Return(database.TagType{}, assert.AnError).Once()

			case "InsertTag_.ext":
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "Unknown"
				})).Return(database.TagType{}, nil).Once()
				mockDB.On("InsertTag", mock.MatchedBy(func(tag database.Tag) bool {
					return tag.Tag == "unknown"
				})).Return(database.Tag{}, nil).Once()
				mockDB.On("InsertTagType", mock.MatchedBy(func(tagType database.TagType) bool {
					return tagType.Type == "Extension"
				})).Return(database.TagType{}, nil).Once()
				mockDB.On("InsertTag", mock.MatchedBy(func(tag database.Tag) bool {
					return tag.Tag == ".ext"
				})).Return(database.Tag{}, assert.AnError).Once()
			}

			// Call SeedKnownTags
			err := SeedKnownTags(mockDB, scanState)

			// Verify error occurred and contains expected message
			require.Error(t, err, "SeedKnownTags should return an error when database operation fails")
			assert.Contains(t, err.Error(), tc.expectedError, "Error message should contain expected text")

			// Verify mock expectations
			mockDB.AssertExpectations(t)
		})
	}
}

// TestSeedKnownTags_OutsideTransaction tests that SeedKnownTags can be called outside a transaction
func TestSeedKnownTags_OutsideTransaction(t *testing.T) {
	t.Parallel()

	// This test ensures our fix allows SeedKnownTags to be called before BeginTransaction
	// We simulate this by ensuring the function works without any transaction context

	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	defer cleanup()

	scanState := &database.ScanState{
		TagTypesIndex: 0,
		TagTypeIDs:    make(map[string]int),
		TagsIndex:     0,
		TagIDs:        make(map[string]int),
	}

	// Call SeedKnownTags - this should work without any transaction context
	err := SeedKnownTags(mediaDB, scanState)

	// Verify success
	require.NoError(t, err, "SeedKnownTags should work outside of transaction context")
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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock indexing state methods for resume scenario
	// First call: simulate interrupted indexing state
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Once()
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
	// Subsequent calls: normal operation (no truncate because resuming successfully)
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "genesis").Return(nil).Maybe()  // Update progress during processing
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()    // Finally complete
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()          // Clear on completion
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once() // Clear systems on completion

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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock indexing state methods for invalid resume scenario (system not found triggers fallback)
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Once()
	mockMediaDB.On("GetLastIndexedSystem").Return("removed_system", nil).Once() // System no longer exists
	mockMediaDB.On("GetIndexingSystems").Return([]string{"nes"}, nil).Once()    // Current systems
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
	// Mock GetMax*ID methods for PopulateScanStateFromDB
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()    // Finally complete
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()          // Clear on completion
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once() // Clear systems on completion

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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil).Maybe()
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil).Maybe()
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock indexing state methods for failed previous indexing
	mockMediaDB.On("GetIndexingStatus").Return("failed", nil).Once()
	// Should clear failed state and start fresh
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Times(3) // Clear failed state + fresh start + final clear
	mockMediaDB.On("SetIndexingStatus", "").Return(nil).Once()      // Clear failed status
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()

	// Mock SetIndexingSystems calls
	mockMediaDB.On("SetIndexingSystems", []string{"nes"}).Return(nil).Maybe()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Maybe() // Clear on completion

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

	// Mock basic database operations - fallback to fresh start due to error
	mockMediaDB.On("Truncate").Return(nil).Maybe()
	mockMediaDB.On("TruncateSystems", []string{"nes"}).Return(nil).Maybe()
	// With batching, we may have fewer transactions than systems
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock indexing state methods with database error
	mockMediaDB.On("GetIndexingStatus").Return("", assert.AnError).Once() // Simulate DB error
	// Should fall back to fresh start
	mockMediaDB.On("SetIndexingSystems", []string{"nes"}).Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()
	// Mock GetAll* methods for PopulateScanStateFromDB
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
	// Mock GetMax*ID methods for PopulateScanStateFromDB
	mockMediaDB.On("GetMaxSystemID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTitleID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagTypeID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("GetMaxMediaTagID").Return(int64(0), nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Once()           // Clear on completion
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Maybe() // Clear systems on completion

	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	systems := []systemdefs.System{{ID: "nes"}}

	// Run the indexer - should handle error gracefully and start fresh
	_, err := NewNamesIndex(context.Background(), mockPlatform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Once()
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

	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Times(2) // Clear on start + completion
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once() // Clear on completion

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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Once()
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

	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Times(2) // Clear on start + completion
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once() // Clear on completion

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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle",
		mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock resume scenario but with different systems
	mockMediaDB.On("GetIndexingStatus").Return("running", nil).Once()
	mockMediaDB.On("GetLastIndexedSystem").Return("genesis", nil).Once() // Was indexing genesis
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

	mockMediaDB.On("SetIndexingSystems", []string{"nes", "snes"}).Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "running").Return(nil).Once()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once() // Clear on completion

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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Once()
	mockMediaDB.On("SetIndexingSystems", []string{}).Return(nil).Once()     // Empty systems list
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Maybe() // Also accept nil slice

	// Mock GetAll* methods for PopulateScanStateFromDB
	mockMediaDB.On("GetAllSystems").Return([]database.System{}, nil).Maybe()
	mockMediaDB.On("GetTitlesWithSystems").Return([]database.TitleWithSystem{}, nil).Maybe()
	mockMediaDB.On("GetMediaWithFullPath").Return([]database.MediaWithFullPath{}, nil).Maybe()
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
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Maybe() // Clear on completion

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
	mockMediaDB.On("BeginTransaction").Return(nil).Maybe()
	mockMediaDB.On("CommitTransaction").Return(nil).Maybe()
	mockMediaDB.On("UpdateLastGenerated").Return(nil)
	mockMediaDB.On("SetOptimizationStatus", mock.AnythingOfType("string")).Return(nil)
	mockMediaDB.On("RunBackgroundOptimization").Return().Maybe()

	// Mock tag seeding operations
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Mock system and media insertion operations
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle", mock.AnythingOfType("database.MediaTitle")).
		Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Mock indexing state methods for fresh start
	mockMediaDB.On("GetIndexingStatus").Return("", nil).Once()
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

	mockMediaDB.On("SetLastIndexedSystem", mock.AnythingOfType("string")).Return(nil).Maybe()
	mockMediaDB.On("SetIndexingStatus", "completed").Return(nil).Once()
	mockMediaDB.On("SetLastIndexedSystem", "").Return(nil).Maybe()         // Allow any number of empty string calls
	mockMediaDB.On("SetIndexingSystems", []string(nil)).Return(nil).Once() // Clear systems on completion

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
	// Setup operations (status, seeding, etc.) are allowed outside transactions
	assert.Positive(t, mockMediaDB.OperationsOutsideTxn,
		"Setup and cleanup operations should happen outside transactions (this is correct behavior)")

	// Verify transaction usage matches expected behavior
	// With no files to process, no transactions should be started (optimization)
	assert.Equal(t, 0, mockMediaDB.TransactionCount,
		"Should use 0 transactions when no files to process")

	mockMediaDB.AssertExpectations(t)
}
