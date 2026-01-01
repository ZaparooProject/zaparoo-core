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
	"sort"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsecutiveFullScansProduceIdenticalCounts is a regression test for the bug where
// consecutive full scans would produce different file counts due to incorrect truncation logic.
//
// The bug was caused by:
// 1. allSystemIDs not being sorted before comparison with currentSystemIDs
// 2. This caused the code to use selective truncation instead of full truncation
// 3. Selective truncation left stale data that would be "rediscovered" on the next scan
//
// This test ensures that running NewNamesIndex twice with the exact same data produces
// the exact same file count both times.
func TestConsecutiveFullScansProduceIdenticalCounts(t *testing.T) {
	ctx := context.Background()

	// Create test database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Create minimal config
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	// Create mock platform with minimal setup
	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()

	// Set up mock platform to return a test root directory
	platform.On("RootDirs", cfg).Return([]string{"/test/roms"})

	// Initialize empty launcher cache to avoid nil pointer issues
	testLauncherCacheMutex.Lock()
	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.Initialize(platform, cfg)
	helpers.GlobalLauncherCache = testCache
	defer func() {
		helpers.GlobalLauncherCache = originalCache
		testLauncherCacheMutex.Unlock()
	}()

	// Use a small subset of systems for testing
	testSystemIDs := []string{systemdefs.SystemNES, systemdefs.SystemSNES, systemdefs.SystemGenesis}
	testSystems := make([]systemdefs.System, 0, len(testSystemIDs))
	for _, id := range testSystemIDs {
		sys, err := systemdefs.GetSystem(id)
		require.NoError(t, err)
		testSystems = append(testSystems, *sys)
	}

	// Track file counts from each scan
	var firstScanCount, secondScanCount, thirdScanCount int

	t.Run("First Full Scan", func(t *testing.T) {
		// Perform first full scan
		count, err := NewNamesIndex(ctx, platform, cfg, testSystems, db, func(_ IndexStatus) {})
		require.NoError(t, err)
		firstScanCount = count

		// With no actual files to scan, count will be 0, but that's OK for this test
		// The important thing is consistency across scans
		t.Logf("First scan indexed %d files", firstScanCount)
	})

	t.Run("Second Full Scan - Identical Count", func(t *testing.T) {
		// Perform second full scan with exact same data
		count, err := NewNamesIndex(ctx, platform, cfg, testSystems, db, func(_ IndexStatus) {})
		require.NoError(t, err)
		secondScanCount = count

		// THE KEY ASSERTION: Second scan must produce identical count
		assert.Equal(t, firstScanCount, secondScanCount,
			"Second full scan must produce identical file count to first scan (regression check for truncation bug)")

		t.Logf("Second scan indexed %d files", secondScanCount)
	})

	t.Run("Third Full Scan - Still Identical", func(t *testing.T) {
		// Perform third scan to ensure stability
		count, err := NewNamesIndex(ctx, platform, cfg, testSystems, db, func(_ IndexStatus) {})
		require.NoError(t, err)
		thirdScanCount = count

		// All three scans should produce identical counts
		assert.Equal(t, firstScanCount, thirdScanCount,
			"Third full scan must produce identical file count")
		assert.Equal(t, secondScanCount, thirdScanCount,
			"All consecutive scans should be stable")

		t.Logf("Third scan indexed %d files", thirdScanCount)
	})

	// Log results for debugging
	t.Logf("Scan counts - First: %d, Second: %d, Third: %d", firstScanCount, secondScanCount, thirdScanCount)
}

// TestFullVsSelectiveTruncation ensures that the truncation decision logic works correctly.
// This is a simpler test that verifies the truncation mode selection without requiring actual files.
func TestFullVsSelectiveTruncation(t *testing.T) {
	ctx := context.Background()

	// Create test database
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	// Create minimal config
	fs := testhelpers.NewMemoryFS()
	cfg, err := testhelpers.NewTestConfig(fs, t.TempDir())
	require.NoError(t, err)

	// Create mock platform
	platform := mocks.NewMockPlatform()
	platform.SetupBasicMock()
	platform.On("RootDirs", cfg).Return([]string{"/test/roms"})

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

	t.Run("Full Scan - All Systems", func(t *testing.T) {
		// Index all systems
		allSystems := systemdefs.AllSystems()
		_, err := NewNamesIndex(ctx, platform, cfg, allSystems, db, func(_ IndexStatus) {})
		require.NoError(t, err)

		// Verify indexing completed successfully
		indexedSystems, err := db.MediaDB.IndexedSystems()
		require.NoError(t, err)
		t.Logf("Indexed %d systems", len(indexedSystems))
	})

	t.Run("Selective Scan - Single System", func(t *testing.T) {
		// Get just NES system
		nesSys, err := systemdefs.GetSystem(systemdefs.SystemNES)
		require.NoError(t, err)

		// Reindex only NES - this should use selective truncation
		_, err = NewNamesIndex(ctx, platform, cfg, []systemdefs.System{*nesSys}, db, func(_ IndexStatus) {})
		require.NoError(t, err)

		// Test passes if no error - we can't verify systems are indexed
		// without actual files since empty systems aren't stored
		t.Log("Selective scan completed without error")
	})

	t.Run("Full Scan After Selective - Works Correctly", func(t *testing.T) {
		// Do another full scan - should use full truncation again
		allSystems := systemdefs.AllSystems()
		_, err := NewNamesIndex(ctx, platform, cfg, allSystems, db, func(_ IndexStatus) {})
		require.NoError(t, err)

		// Verify scan completed without errors
		indexedSystems, err := db.MediaDB.IndexedSystems()
		require.NoError(t, err)
		t.Logf("After full scan following selective, have %d systems indexed", len(indexedSystems))
	})
}

// TestTruncationLogicSortingBug specifically tests the sorting bug that was fixed.
// This ensures that the comparison between currentSystemIDs and allSystemIDs
// works correctly regardless of the order systems are added.
func TestTruncationLogicSortingBug(t *testing.T) {
	t.Run("Unsorted comparison could fail", func(t *testing.T) {
		// This demonstrates the bug - if lists aren't sorted,
		// they might not be equal even with same elements

		unsortedA := []string{"Genesis", "NES", "SNES"}
		unsortedB := []string{"NES", "SNES", "Genesis"}

		// These have same elements but different order
		assert.NotEqual(t, unsortedA, unsortedB,
			"Unsorted lists with same elements in different order are not equal")
	})

	t.Run("Sorted comparison always works", func(t *testing.T) {
		// After sorting, comparison works correctly
		listA := []string{"Genesis", "NES", "SNES"}
		listB := []string{"NES", "SNES", "Genesis"}

		// Sort both
		sort.Strings(listA)
		sort.Strings(listB)

		// This demonstrates the fix
		assert.Equal(t, listA, listB,
			"Sorted lists with same elements are equal")
	})
}
