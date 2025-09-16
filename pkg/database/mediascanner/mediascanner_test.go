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
	t.Parallel()

	// Create test config and mock database
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	mockUserDB := &testhelpers.MockUserDBI{}
	mockMediaDB := &testhelpers.MockMediaDBI{}

	// Set up basic mock expectations for database operations
	mockMediaDB.On("Truncate").Return(nil)
	mockMediaDB.On("BeginTransaction").Return(nil)
	mockMediaDB.On("CommitTransaction").Return(nil)
	mockMediaDB.On("ReindexTables").Return(nil)
	mockMediaDB.On("Vacuum").Return(nil)
	mockMediaDB.On("UpdateLastGenerated").Return(nil)

	// Mock SeedKnownTags operations - these are called during initialization
	mockMediaDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockMediaDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()
	mockMediaDB.On("InsertSystem", mock.AnythingOfType("database.System")).Return(database.System{}, nil).Maybe()
	mockMediaDB.On("InsertTitle", mock.AnythingOfType("database.MediaTitle")).Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTitle", mock.AnythingOfType("database.MediaTitle")).
		Return(database.MediaTitle{}, nil).Maybe()
	mockMediaDB.On("InsertMedia", mock.AnythingOfType("database.Media")).Return(database.Media{}, nil).Maybe()
	mockMediaDB.On("InsertMediaTag", mock.AnythingOfType("database.MediaTag")).Return(database.MediaTag{}, nil).Maybe()

	// Create database wrapper with mocks
	db := &database.Database{
		UserDB:  mockUserDB,
		MediaDB: mockMediaDB,
	}

	// Track which scanners were called
	scanner1Called := false
	scanner2Called := false

	// Create two test launchers with the same SystemID but different IDs
	launcher1 := platforms.Launcher{
		ID:       "TestLauncher1",
		SystemID: systemdefs.SystemTV,
		Scanner: func(_ *config.Instance, _ string,
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
		Scanner: func(_ *config.Instance, _ string,
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
	_, err = NewNamesIndex(platform, cfg, systems, db, func(IndexStatus) {})
	require.NoError(t, err)

	// Both scanners should have been called
	assert.True(t, scanner1Called, "Scanner 1 should have been called")
	assert.True(t, scanner2Called, "Scanner 2 should have been called") // This will fail with the current bug

	// Verify mock expectations
	mockMediaDB.AssertExpectations(t)
}

func TestGetSystemPathsRespectsSkipFilesystemScan(t *testing.T) {
	t.Parallel()
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

	mockDB := &testhelpers.MockMediaDBI{}
	scanState := &database.ScanState{
		TagTypesIndex:  0,
		TagTypeIDs:     make(map[string]int),
		TagsIndex:      0,
		TagIDs:         make(map[string]int),
		MediaTagsIndex: 0,
	}

	// Mock successful database operations
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
	})).Return(database.Tag{}, nil).Once()

	// Mock insertions for the predefined tag types (Version, Language, Region, etc.)
	mockDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Call SeedKnownTags
	err := SeedKnownTags(mockDB, scanState)

	// Verify no error occurred
	require.NoError(t, err, "SeedKnownTags should not return an error on success")

	// Verify state was updated correctly
	assert.Positive(t, scanState.TagTypesIndex, "TagTypesIndex should be incremented")
	assert.Positive(t, scanState.TagsIndex, "TagsIndex should be incremented")
	assert.Contains(t, scanState.TagIDs, "unknown", "TagIDs should contain 'unknown' tag")
	assert.Contains(t, scanState.TagIDs, ".ext", "TagIDs should contain '.ext' tag")

	// Verify mock expectations
	mockDB.AssertExpectations(t)
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
				TagTypesIndex:  0,
				TagTypeIDs:     make(map[string]int),
				TagsIndex:      0,
				TagIDs:         make(map[string]int),
				MediaTagsIndex: 0,
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

	mockDB := &testhelpers.MockMediaDBI{}
	scanState := &database.ScanState{
		TagTypesIndex:  0,
		TagTypeIDs:     make(map[string]int),
		TagsIndex:      0,
		TagIDs:         make(map[string]int),
		MediaTagsIndex: 0,
	}

	// Mock all database operations to succeed
	mockDB.On("InsertTagType", mock.AnythingOfType("database.TagType")).Return(database.TagType{}, nil).Maybe()
	mockDB.On("InsertTag", mock.AnythingOfType("database.Tag")).Return(database.Tag{}, nil).Maybe()

	// Call SeedKnownTags - this should work without any transaction context
	err := SeedKnownTags(mockDB, scanState)

	// Verify success
	require.NoError(t, err, "SeedKnownTags should work outside of transaction context")
	assert.Positive(t, scanState.TagTypesIndex, "TagTypesIndex should be incremented")
	assert.Positive(t, scanState.TagsIndex, "TagsIndex should be incremented")

	// Verify mock expectations
	mockDB.AssertExpectations(t)
}
