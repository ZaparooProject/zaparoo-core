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
	"database/sql"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner/testdata"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestEndToEndResumeScenarios tests complete resume scenarios that mirror real-world usage
// These tests simulate the exact conditions that caused the original "0 games indexed" bug
func TestEndToEndResumeScenarios(t *testing.T) {
	t.Parallel()

	t.Run("Complete_Indexing_Run_Then_Resume", func(t *testing.T) {
		t.Parallel()

		// Setup database and platform
		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")
		mockPlatform.On("Settings").Return(platforms.Settings{})
		mockPlatform.On("Launchers").Return([]platforms.Launcher{})
		mockPlatform.On("RootDirs", mock.Anything).Return([]string{})

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		cfg := &config.Instance{}
		systems := []systemdefs.System{
			{ID: "NES"},
			{ID: "SNES"},
			{ID: "Genesis"},
		}

		// Generate test data
		batch := testdata.CreateReproducibleBatch([]string{"NES", "SNES", "Genesis"}, 10)

		// Mock the platform with test launchers that return our test data
		createMockLauncher := func(systemID string, entries []testdata.TestMediaEntry) platforms.Launcher {
			return platforms.Launcher{
				ID:       "TestLauncher_" + systemID,
				SystemID: systemID,
				Scanner: func(_ *config.Instance, _ string, _ []platforms.ScanResult) ([]platforms.ScanResult, error) {
					results := make([]platforms.ScanResult, len(entries))
					for i, entry := range entries {
						results[i] = platforms.ScanResult{
							Name: entry.Name,
							Path: entry.Path,
						}
					}
					return results, nil
				},
			}
		}

		launchers := []platforms.Launcher{
			createMockLauncher("NES", batch.Entries["NES"]),
			createMockLauncher("SNES", batch.Entries["SNES"]),
			createMockLauncher("Genesis", batch.Entries["Genesis"]),
		}
		mockPlatform.On("Launchers").Return(launchers)

		db := &database.Database{
			MediaDB: mediaDB,
		}

		// Phase 1: Complete fresh indexing run
		var phase1Stats IndexStatus
		updateFunc := func(status IndexStatus) {
			phase1Stats = status
		}

		indexStats, err := NewNamesIndex(mockPlatform, cfg, systems, db, updateFunc)
		require.NoError(t, err)
		require.NotNil(t, indexStats)

		// Wait for background optimization to complete to prevent "database closed" error
		for {
			status, getStatusErr := mediaDB.GetOptimizationStatus()
			if getStatusErr != nil {
				// If we can't get status, optimization might not have started or is already done
				break
			}
			if status == "completed" || status == "failed" || status == "" {
				break
			}
			time.Sleep(50 * time.Millisecond) // Small sleep to avoid busy waiting
		}

		// Verify Phase 1 completed successfully
		assert.Equal(t, 30, indexStats, "Should have indexed 30 games (10 per system)")
		assert.Equal(t, "NES", phase1Stats.SystemID, "Final system ID should be set")
		assert.Equal(t, 30, phase1Stats.Files, "Total files should be 30")

		// Record Phase 1 database state
		phase1MaxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		phase1MaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		phase1MaxMediaID, err := mediaDB.GetMaxMediaID()
		require.NoError(t, err)

		assert.Equal(t, int64(3), phase1MaxSystemID, "Phase 1: Should have 3 systems")
		assert.Equal(t, int64(30), phase1MaxTitleID, "Phase 1: Should have 30 titles")
		assert.Equal(t, int64(30), phase1MaxMediaID, "Phase 1: Should have 30 media entries")

		// Phase 2: Simulate restart and resume
		// Create new platform instance to simulate process restart
		mockPlatform2 := mocks.NewMockPlatform()
		mockPlatform2.On("ID").Return("test-platform")
		mockPlatform2.On("Settings").Return(platforms.Settings{})
		mockPlatform2.On("RootDirs", mock.Anything).Return([]string{})

		// Add one more system to test resume with new data
		systems2 := []systemdefs.System{
			{ID: "NES"},
			{ID: "SNES"},
			{ID: "Genesis"},
			{ID: "Gameboy"}, // New system
		}

		// Generate new test data including the new system
		batch2 := testdata.CreateReproducibleBatch([]string{"NES", "SNES", "Genesis", "Gameboy"}, 10)

		launchers2 := []platforms.Launcher{
			createMockLauncher("NES", batch2.Entries["NES"]),
			createMockLauncher("SNES", batch2.Entries["SNES"]),
			createMockLauncher("Genesis", batch2.Entries["Genesis"]),
			createMockLauncher("Gameboy", batch2.Entries["Gameboy"]), // New launcher
		}
		mockPlatform2.On("Launchers").Return(launchers2)

		// Simulate the conditions that caused the original bug:
		// 1. Database exists with data from Phase 1
		// 2. Indexing status indicates interruption
		err = mediaDB.SetIndexingStatus("running")
		require.NoError(t, err)
		err = mediaDB.SetLastIndexedSystem("Genesis") // Interrupted after Genesis
		require.NoError(t, err)

		// Phase 2: Resume indexing
		var phase2Stats IndexStatus
		updateFunc2 := func(status IndexStatus) {
			phase2Stats = status
		}

		// This is where the bug occurred - resume would start from 0 instead of continuing
		indexStats2, err := NewNamesIndex(mockPlatform2, cfg, systems2, db, updateFunc2)
		require.NoError(t, err)
		require.NotNil(t, indexStats2)

		// Verify Phase 2 completed successfully with resume
		assert.Equal(t, "GameboyColor", phase2Stats.SystemID, "Final system ID should be set")
		assert.Equal(t, 20, indexStats2, "Should have indexed 20 more games")

		// Verify database state after resume
		finalMaxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		finalMaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		finalMaxMediaID, err := mediaDB.GetMaxMediaID()
		require.NoError(t, err)

		// Should have added one more system and its games
		assert.Equal(t, int64(4), finalMaxSystemID, "Should have 4 systems after resume")
		assert.Equal(t, int64(40), finalMaxTitleID, "Should have 40 titles after resume")
		assert.Equal(t, int64(40), finalMaxMediaID, "Should have 40 media entries after resume")

		// Critical: Verify no constraint violations occurred
		// (The test would fail during NewNamesIndex if constraint violations happened)
		// This validates that PopulateScanStateFromDB correctly restored the scan state
	})

	t.Run("Interrupt_At_10_Percent_Then_Resume", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		// Phase 1: Partial indexing (simulate 10% completion)
		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		// Seed known tags BEFORE transaction
		err = SeedKnownTags(mediaDB, scanState)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		generator := testdata.NewTestDataGenerator(6000)
		entry1 := generator.GenerateMediaEntry("NES")
		entry2 := generator.GenerateMediaEntry("NES")

		AddMediaPath(mediaDB, scanState, "NES", entry1.Path)
		AddMediaPath(mediaDB, scanState, "NES", entry2.Path)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Set indexing status to simulate interruption
		err = mediaDB.SetIndexingStatus("running")
		require.NoError(t, err)
		err = mediaDB.SetLastIndexedSystem("NES")
		require.NoError(t, err)

		// Phase 2: Resume from 10%
		resumeState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		// This should restore state from 10% completion
		err = PopulateScanStateFromDB(mediaDB, resumeState)
		require.NoError(t, err)

		// Continue indexing
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		entry3 := generator.GenerateMediaEntry("SNES")
		entry4 := generator.GenerateMediaEntry("Genesis")

		titleIndex3, mediaIndex3 := AddMediaPath(mediaDB, resumeState, "SNES", entry3.Path)
		titleIndex4, mediaIndex4 := AddMediaPath(mediaDB, resumeState, "Genesis", entry4.Path)

		// Should continue from where we left off
		assert.Greater(t, titleIndex3, 2, "Title 3 should have ID > 2")
		assert.Greater(t, mediaIndex3, 2, "Media 3 should have ID > 2")
		assert.Greater(t, titleIndex4, titleIndex3, "Title 4 should have ID > Title 3")
		assert.Greater(t, mediaIndex4, mediaIndex3, "Media 4 should have ID > Media 3")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Clear indexing status to indicate completion
		err = mediaDB.SetIndexingStatus("completed")
		require.NoError(t, err)
		err = mediaDB.SetLastIndexedSystem("")
		require.NoError(t, err)

		// Verify final state is consistent
		finalMaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)
		finalMaxMediaID, err := mediaDB.GetMaxMediaID()
		require.NoError(t, err)

		assert.Equal(t, int64(4), finalMaxTitleID, "Should have 4 total titles")
		assert.Equal(t, int64(4), finalMaxMediaID, "Should have 4 total media entries")
	})

	t.Run("Resume_With_Changed_System_List", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		// Phase 1: Index with initial system list
		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = SeedKnownTags(mediaDB, scanState)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		generator := testdata.NewTestDataGenerator(7000)

		// Add games for NES and SNES
		for range 3 {
			entryNES := generator.GenerateMediaEntry("NES")
			entrySNES := generator.GenerateMediaEntry("SNES")
			AddMediaPath(mediaDB, scanState, "NES", entryNES.Path)
			AddMediaPath(mediaDB, scanState, "SNES", entrySNES.Path)
		}

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Set status as if interrupted
		err = mediaDB.SetIndexingStatus("running")
		require.NoError(t, err)
		err = mediaDB.SetLastIndexedSystem("SNES")
		require.NoError(t, err)

		phase1MaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)

		// Phase 2: Resume with different system list (removed NES, added Genesis and Gameboy)
		resumeState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = PopulateScanStateFromDB(mediaDB, resumeState)
		require.NoError(t, err)

		// Continue with new systems
		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		entryGenesis := generator.GenerateMediaEntry("Genesis")
		entryGameboy := generator.GenerateMediaEntry("Gameboy")

		titleIndexGenesis, _ := AddMediaPath(mediaDB, resumeState, "Genesis", entryGenesis.Path)
		titleIndexGameboy, _ := AddMediaPath(mediaDB, resumeState, "Gameboy", entryGameboy.Path)

		// Should continue from existing max IDs
		assert.Greater(t, titleIndexGenesis, int(phase1MaxTitleID), "Genesis should get next ID after existing")
		assert.Greater(t, titleIndexGameboy, titleIndexGenesis, "Gameboy should get next ID after Genesis")

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify system coexistence
		finalMaxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		finalMaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)

		assert.Equal(t, int64(4), finalMaxSystemID, "Should have 4 systems total (NES, SNES, Genesis, Gameboy)")
		assert.Equal(t, phase1MaxTitleID+2, finalMaxTitleID, "Should have 2 additional titles")
	})

	t.Run("Multiple_Resume_Cycles", func(t *testing.T) {
		t.Parallel()

		sqlDB, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer func() { _ = sqlDB.Close() }()

		ctx := context.Background()
		mockPlatform := mocks.NewMockPlatform()
		mockPlatform.On("ID").Return("test-platform")

		mediaDB := &mediadb.MediaDB{}
		err = mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform)
		require.NoError(t, err)

		generator := testdata.NewTestDataGenerator(8000)

		// Cycle 1: Initial data
		scanState := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = SeedKnownTags(mediaDB, scanState)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		entry1 := generator.GenerateMediaEntry("NES")
		AddMediaPath(mediaDB, scanState, "NES", entry1.Path)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		cycle1MaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)

		// Cycle 2: Resume and add more
		resumeState1 := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = PopulateScanStateFromDB(mediaDB, resumeState1)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		entry2 := generator.GenerateMediaEntry("SNES")
		titleIndex2, _ := AddMediaPath(mediaDB, resumeState1, "SNES", entry2.Path)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		assert.Equal(t, int(cycle1MaxTitleID+1), titleIndex2, "Cycle 2 should continue from Cycle 1")

		cycle2MaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)

		// Cycle 3: Resume again
		resumeState2 := &database.ScanState{
			SystemIDs:      make(map[string]int),
			TitleIDs:       make(map[string]int),
			MediaIDs:       make(map[string]int),
			TagTypeIDs:     make(map[string]int),
			TagIDs:         make(map[string]int),
			SystemsIndex:   0,
			TitlesIndex:    0,
			MediaIndex:     0,
			TagTypesIndex:  0,
			TagsIndex:      0,
			MediaTagsIndex: 0,
		}

		err = PopulateScanStateFromDB(mediaDB, resumeState2)
		require.NoError(t, err)

		err = mediaDB.BeginTransaction()
		require.NoError(t, err)

		entry3 := generator.GenerateMediaEntry("Genesis")
		titleIndex3, _ := AddMediaPath(mediaDB, resumeState2, "Genesis", entry3.Path)

		err = mediaDB.CommitTransaction()
		require.NoError(t, err)

		assert.Equal(t, int(cycle2MaxTitleID+1), titleIndex3, "Cycle 3 should continue from Cycle 2")

		// Verify final consistency
		finalMaxSystemID, err := mediaDB.GetMaxSystemID()
		require.NoError(t, err)
		finalMaxTitleID, err := mediaDB.GetMaxTitleID()
		require.NoError(t, err)

		assert.Equal(t, int64(3), finalMaxSystemID, "Should have 3 systems after 3 cycles")
		assert.Equal(t, int64(3), finalMaxTitleID, "Should have 3 titles after 3 cycles")
	})
}
