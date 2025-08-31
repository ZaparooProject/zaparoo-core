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

package examples

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/fixtures"
	"github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatabaseMockUsage demonstrates how to use database mocks and fixtures
func TestDatabaseMockUsage(t *testing.T) {
	t.Parallel()
	t.Run("UserDBI Mock Usage", func(t *testing.T) {
		t.Parallel()
		// Create a mock UserDBI
		mockUserDB := &helpers.MockUserDBI{}

		// Use fixtures for test data
		expectedMapping := fixtures.Mappings.SimplePattern
		expectedHistory := fixtures.HistoryEntries.Collection

		// Set up expectations
		mockUserDB.On("GetAllMappings").Return([]database.Mapping{expectedMapping}, nil)
		mockUserDB.On("GetHistory", 0).Return(expectedHistory, nil)
		mockUserDB.On("AddHistory", &expectedHistory[0]).Return(nil)

		// Test the mock
		mappings, err := mockUserDB.GetAllMappings()
		require.NoError(t, err)
		assert.Len(t, mappings, 1)
		assert.Equal(t, expectedMapping.Label, mappings[0].Label)

		history, err := mockUserDB.GetHistory(0)
		require.NoError(t, err)
		assert.Len(t, history, 3)
		assert.Equal(t, expectedHistory[0].TokenValue, history[0].TokenValue)

		err = mockUserDB.AddHistory(&expectedHistory[0])
		require.NoError(t, err)

		// Verify all expectations were met
		mockUserDB.AssertExpectations(t)
	})

	t.Run("MediaDBI Mock Usage", func(t *testing.T) {
		t.Parallel()
		// Create a mock MediaDBI
		mockMediaDB := &helpers.MockMediaDBI{}

		// Use fixtures for test data
		expectedResults := fixtures.SearchResults.Collection
		testSystemDefs := fixtures.GetTestSystemDefs()

		// Set up expectations
		mockMediaDB.On("IndexedSystems").Return([]string{"atari2600", "gb", "snes"}, nil)
		mockMediaDB.On("SearchMediaPathExact", testSystemDefs, "Tetris").
			Return([]database.SearchResult{expectedResults[1]}, nil)
		mockMediaDB.On("FindSystem", fixtures.Systems.Atari2600).Return(fixtures.Systems.Atari2600, nil)

		// Test the mock
		systems, err := mockMediaDB.IndexedSystems()
		require.NoError(t, err)
		assert.Len(t, systems, 3)
		assert.Contains(t, systems, "atari2600")

		results, err := mockMediaDB.SearchMediaPathExact(testSystemDefs, "Tetris")
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Tetris", results[0].Name)

		system, err := mockMediaDB.FindSystem(fixtures.Systems.Atari2600)
		require.NoError(t, err)
		assert.Equal(t, "atari2600", system.SystemID)

		// Verify all expectations were met
		mockMediaDB.AssertExpectations(t)
	})

	t.Run("Transaction Mock Usage", func(t *testing.T) {
		t.Parallel()
		// Create a mock MediaDBI for transaction testing
		mockMediaDB := &helpers.MockMediaDBI{}

		// Set up transaction expectations
		mockMediaDB.On("BeginTransaction").Return(nil)
		mockMediaDB.On("InsertSystem", fixtures.Systems.Atari2600).Return(fixtures.Systems.Atari2600, nil)
		mockMediaDB.On("CommitTransaction").Return(nil)

		// Test transaction workflow
		err := mockMediaDB.BeginTransaction()
		require.NoError(t, err)

		system, err := mockMediaDB.InsertSystem(fixtures.Systems.Atari2600)
		require.NoError(t, err)
		assert.Equal(t, "atari2600", system.SystemID)

		err = mockMediaDB.CommitTransaction()
		require.NoError(t, err)

		// Verify all expectations were met
		mockMediaDB.AssertExpectations(t)
	})
}

// TestSQLMockUsage demonstrates how to use sqlmock for direct database testing
func TestSQLMockUsage(t *testing.T) {
	t.Parallel()
	t.Run("SQLMock Setup and Usage", func(t *testing.T) {
		t.Parallel()
		// Create sqlmock database
		db, mock, err := helpers.SetupSQLMock()
		require.NoError(t, err)
		defer func() {
			// Note: SQLMock doesn't expect Close() calls unless explicitly set up
			// For real database connections, you would close them here
			_ = db.Close()
		}()

		// Use fixtures for expected data
		expectedMapping := fixtures.Mappings.SimplePattern

		// Set up expectations using helper functions
		helpers.ExpectMappingQuery(mock, expectedMapping.DBID, &expectedMapping)

		// Execute test query
		var mapping database.Mapping
		row := db.QueryRow("SELECT * FROM mappings WHERE DBID = ?", expectedMapping.DBID)
		err = row.Scan(&mapping.Label, &mapping.Type, &mapping.Match, &mapping.Pattern,
			&mapping.Override, &mapping.DBID, &mapping.Added, &mapping.Enabled)
		require.NoError(t, err)

		// Verify results
		assert.Equal(t, expectedMapping.Label, mapping.Label)
		assert.Equal(t, expectedMapping.Type, mapping.Type)

		// Verify all expectations were met
		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("SQLMock Insert Operations", func(t *testing.T) {
		t.Parallel()
		// Create sqlmock database with expectations
		db, mock, err := helpers.SetupSQLMockWithExpectations()
		require.NoError(t, err)
		defer func() {
			// Note: SQLMock doesn't expect Close() calls unless explicitly set up
			// For real database connections, you would close them here
			_ = db.Close()
		}()

		// Use fixtures for test data
		historyEntry := fixtures.HistoryEntries.Successful

		// Set up insert expectations
		helpers.ExpectHistoryInsert(mock, &historyEntry)

		// Execute insert
		_, err = db.Exec(
			"INSERT INTO history (time, type, token_id, token_value, token_data, success) VALUES (?, ?, ?, ?, ?, ?)",
			historyEntry.Time, historyEntry.Type, historyEntry.TokenID,
			historyEntry.TokenValue, historyEntry.TokenData, historyEntry.Success)
		require.NoError(t, err)

		// Verify all expectations were met
		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("SQLMock Transaction Testing", func(t *testing.T) {
		t.Parallel()
		// Create sqlmock database
		db, mock, err := helpers.SetupSQLMock()
		require.NoError(t, err)
		defer func() {
			// Note: SQLMock doesn't expect Close() calls unless explicitly set up
			// For real database connections, you would close them here
			_ = db.Close()
		}()

		// Set up transaction expectations
		helpers.ExpectTransactionBegin(mock)

		// Set up insert expectations using fixtures
		system := fixtures.Systems.GameBoy
		mock.ExpectExec("INSERT INTO systems").
			WithArgs(system.SystemID, system.Name).
			WillReturnResult(sqlmock.NewResult(1, 1))

		helpers.ExpectTransactionCommit(mock)

		// Execute transaction
		tx, err := db.Begin()
		require.NoError(t, err)

		_, err = tx.Exec("INSERT INTO systems (system_id, name) VALUES (?, ?)",
			system.SystemID, system.Name)
		require.NoError(t, err)

		err = tx.Commit()
		require.NoError(t, err)

		// Verify all expectations were met
		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})
}

// TestFixtureUsage demonstrates how to use database fixtures
func TestFixtureUsage(t *testing.T) {
	t.Parallel()
	t.Run("History Fixtures", func(t *testing.T) {
		t.Parallel()
		// Test individual fixtures
		assert.True(t, fixtures.HistoryEntries.Successful.Success)
		assert.False(t, fixtures.HistoryEntries.Failed.Success)
		assert.Equal(t, "api", fixtures.HistoryEntries.APIToken.Type)

		// Test collection fixtures
		assert.Len(t, fixtures.HistoryEntries.Collection, 3)
		assert.Equal(t, "zelda:botw", fixtures.HistoryEntries.Collection[0].TokenValue)
	})

	t.Run("Mapping Fixtures", func(t *testing.T) {
		t.Parallel()
		// Test pattern types
		assert.Equal(t, "exact", fixtures.Mappings.SimplePattern.Match)
		assert.Equal(t, "regex", fixtures.Mappings.RegexPattern.Match)
		assert.Equal(t, "system", fixtures.Mappings.SystemMapping.Type)

		// Test enabled/disabled states
		assert.True(t, fixtures.Mappings.SimplePattern.Enabled)
		assert.False(t, fixtures.Mappings.DisabledMapping.Enabled)

		// Test collection
		assert.Len(t, fixtures.Mappings.Collection, 3)
	})

	t.Run("Media Database Fixtures", func(t *testing.T) {
		t.Parallel()
		// Test systems
		assert.Equal(t, "atari2600", fixtures.Systems.Atari2600.SystemID)
		assert.Len(t, fixtures.Systems.Collection, 4)

		// Test media titles
		assert.Equal(t, "pitfall", fixtures.MediaTitles.Pitfall.Slug)
		assert.Equal(t, int64(1), fixtures.MediaTitles.Pitfall.SystemDBID)

		// Test media files
		assert.Contains(t, fixtures.Media.TetrisROM.Path, "Tetris")
		assert.Equal(t, int64(2), fixtures.Media.TetrisROM.MediaTitleDBID)

		// Test search results
		assert.Equal(t, "gb", fixtures.SearchResults.TetrisResult.SystemID)
		assert.Len(t, fixtures.SearchResults.Collection, 3)
	})

	t.Run("Helper Functions", func(t *testing.T) {
		t.Parallel()
		// Test custom history entry creation
		entry := fixtures.NewHistoryEntry("ntag213", "04:AA:BB:CC:DD:EE:FF", "test:game", true)
		assert.Equal(t, "test:game", entry.TokenValue)
		assert.True(t, entry.Success)
		assert.WithinDuration(t, time.Now(), entry.Time, time.Second)

		// Test custom mapping creation
		mapping := fixtures.NewMapping("Test Mapping", "exact", "test:*", true)
		assert.Equal(t, "Test Mapping", mapping.Label)
		assert.Equal(t, "exact", mapping.Match)
		assert.True(t, mapping.Enabled)

		// Test system definitions
		systemDefs := fixtures.GetTestSystemDefs()
		assert.Len(t, systemDefs, 4)
		assert.Equal(t, "atari2600", systemDefs[0].ID)
	})
}

// TestIntegrationExample demonstrates how to combine mocks, sqlmock, and fixtures
func TestIntegrationExample(t *testing.T) {
	t.Parallel()
	t.Run("Complete Database Workflow", func(t *testing.T) {
		t.Parallel()
		// Create both mocks
		mockUserDB := &helpers.MockUserDBI{}
		mockMediaDB := &helpers.MockMediaDBI{}

		// Set up sqlmock for direct database operations
		db, mock, err := helpers.SetupSQLMock()
		require.NoError(t, err)
		defer func() {
			// Note: SQLMock doesn't expect Close() calls unless explicitly set up
			// For real database connections, you would close them here
			_ = db.Close()
		}()

		// Use fixtures for consistent test data
		testMapping := fixtures.Mappings.SimplePattern
		testHistory := fixtures.HistoryEntries.Successful
		testResults := []database.SearchResult{fixtures.SearchResults.PitfallResult}

		// Set up mock expectations
		mockUserDB.On("GetMapping", testMapping.DBID).Return(testMapping, nil)
		mockUserDB.On("AddHistory", &testHistory).Return(nil)

		mockMediaDB.On("SearchMediaPathExact", fixtures.GetTestSystemDefs(), "Pitfall").
			Return(testResults, nil)
		mockMediaDB.On("SystemIndexed", fixtures.GetTestSystemDefs()[0]).Return(true)

		// Set up sqlmock expectations for direct database access
		helpers.ExpectMappingQuery(mock, testMapping.DBID, &testMapping)

		// Simulate a complete workflow

		// 1. Get mapping from UserDB
		mapping, err := mockUserDB.GetMapping(testMapping.DBID)
		require.NoError(t, err)
		assert.Equal(t, "zelda:*", mapping.Pattern)

		// 2. Search for media in MediaDB
		results, err := mockMediaDB.SearchMediaPathExact(fixtures.GetTestSystemDefs(), "Pitfall")
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Pitfall!", results[0].Name)

		// 3. Check if system is indexed
		isIndexed := mockMediaDB.SystemIndexed(fixtures.GetTestSystemDefs()[0])
		assert.True(t, isIndexed)

		// 4. Add history entry
		err = mockUserDB.AddHistory(&testHistory)
		require.NoError(t, err)

		// 5. Direct database query with sqlmock
		var dbMapping database.Mapping
		row := db.QueryRow("SELECT * FROM mappings WHERE DBID = ?", testMapping.DBID)
		err = row.Scan(&dbMapping.Label, &dbMapping.Type, &dbMapping.Match, &dbMapping.Pattern,
			&dbMapping.Override, &dbMapping.DBID, &dbMapping.Added, &dbMapping.Enabled)
		require.NoError(t, err)
		assert.Equal(t, testMapping.Label, dbMapping.Label)

		// Verify all expectations were met
		mockUserDB.AssertExpectations(t)
		mockMediaDB.AssertExpectations(t)
		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})
}
