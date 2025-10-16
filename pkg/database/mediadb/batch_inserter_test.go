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

package mediadb

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchInserter_SingleBatch(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create table
	_, err = db.ExecContext(ctx,
		`CREATE TABLE test_table (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, value INTEGER)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserter with batch size of 10
	bi, err := NewBatchInserter(ctx, tx, "test_table", []string{"name", "value"}, 10)
	require.NoError(t, err)

	// Add 5 rows (less than batch size)
	for i := range 5 {
		err = bi.Add("test", i)
		require.NoError(t, err)
	}

	// Flush remaining
	err = bi.Close()
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify all 5 rows were inserted
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

func TestBatchInserter_MultipleBatches(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create table
	_, err = db.ExecContext(ctx,
		`CREATE TABLE test_table (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, value INTEGER)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserter with batch size of 5
	bi, err := NewBatchInserter(ctx, tx, "test_table", []string{"name", "value"}, 5)
	require.NoError(t, err)

	// Add 12 rows (will trigger 2 full batches + 1 partial)
	for i := range 12 {
		err = bi.Add("test", i)
		require.NoError(t, err)
	}

	// Flush remaining
	err = bi.Close()
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify all 12 rows were inserted
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 12, count)
}

func TestBatchInserter_EmptyFlush(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create table
	_, err = db.ExecContext(ctx,
		`CREATE TABLE test_table (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, value INTEGER)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserter
	bi, err := NewBatchInserter(ctx, tx, "test_table", []string{"name", "value"}, 10)
	require.NoError(t, err)

	// Flush without adding any rows (should be no-op)
	err = bi.Flush()
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify no rows were inserted
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestBatchInserter_InvalidColumnCount(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create table
	_, err = db.ExecContext(ctx,
		`CREATE TABLE test_table (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, value INTEGER)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// Create batch inserter with 2 columns
	bi, err := NewBatchInserter(ctx, tx, "test_table", []string{"name", "value"}, 10)
	require.NoError(t, err)

	// Try to add row with wrong number of columns
	err = bi.Add("test") // Only 1 value instead of 2
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 2 values")
}

func TestBatchInserter_ValidationErrors(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Begin transaction for valid testing
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// Test nil transaction
	_, err = NewBatchInserter(ctx, nil, "test_table", []string{"name"}, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transaction is nil")

	// Test empty table name
	_, err = NewBatchInserter(ctx, tx, "", []string{"name"}, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "table name is empty")

	// Test empty columns
	_, err = NewBatchInserter(ctx, tx, "test_table", []string{}, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "columns list is empty")

	// Test invalid batch size
	_, err = NewBatchInserter(ctx, tx, "test_table", []string{"name"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "batch size must be positive")
}

// TestBatchInserter_SQLiteVariableLimit tests that the batch inserter can handle
// batches that exceed SQLite's SQLITE_MAX_VARIABLE_NUMBER limit (default 32766).
// This reproduces the production bug where batches with >8,191 rows (4 columns each)
// fail with "too many SQL variables" error.
func TestBatchInserter_SQLiteVariableLimit(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create table with 4 columns (like MediaTitles)
	_, err = db.ExecContext(ctx,
		`CREATE TABLE test_table (
			DBID INTEGER PRIMARY KEY,
			col1 TEXT,
			col2 TEXT,
			col3 TEXT
		)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserter with a batch size that exceeds SQLite's variable limit
	// SQLite default SQLITE_MAX_VARIABLE_NUMBER = 32766
	// With 4 columns: 32766 / 4 = 8191.5, so 8500 rows * 4 columns = 34000 variables (exceeds limit)
	const numRows = 8500
	bi, err := NewBatchInserter(ctx, tx, "test_table", []string{"DBID", "col1", "col2", "col3"}, numRows)
	require.NoError(t, err)

	// Add 8500 rows - this will exceed the SQLite variable limit when flushed
	for i := range numRows {
		err = bi.Add(int64(i+1), "value1", "value2", "value3")
		require.NoError(t, err)
	}

	// Flush should handle the "too many SQL variables" error by auto-chunking
	err = bi.Close()
	require.NoError(t, err, "Batch inserter should handle SQLite variable limit by auto-chunking")

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify all rows were inserted
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, numRows, count, "All rows should be inserted despite exceeding SQLite variable limit")
}

// TestBatchInserter_OrIgnoreDuplicates tests that INSERT OR IGNORE correctly handles duplicate rows
func TestBatchInserter_OrIgnoreDuplicates(t *testing.T) {
	// Setup in-memory database with UNIQUE constraint
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create table with UNIQUE constraint (like Systems.SystemID)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE test_table (
			DBID INTEGER PRIMARY KEY,
			SystemID TEXT UNIQUE NOT NULL,
			Name TEXT NOT NULL
		)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserter with OR IGNORE
	bi, err := NewBatchInserterWithOptions(ctx, tx, "test_table",
		[]string{"DBID", "SystemID", "Name"}, 10, true)
	require.NoError(t, err)

	// Add rows including duplicates
	err = bi.Add(int64(1), "system1", "System 1")
	require.NoError(t, err)
	err = bi.Add(int64(2), "system2", "System 2")
	require.NoError(t, err)
	err = bi.Add(int64(3), "system1", "System 1 Duplicate") // Duplicate SystemID
	require.NoError(t, err)
	err = bi.Add(int64(4), "system3", "System 3")
	require.NoError(t, err)

	// Flush should succeed with OR IGNORE handling duplicates
	err = bi.Close()
	require.NoError(t, err, "OR IGNORE should handle duplicate SystemID gracefully")

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify only unique rows were inserted (3 total, duplicate was ignored)
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_table").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count, "Should insert 3 unique rows, ignoring 1 duplicate")

	// Verify first system1 was kept, duplicate was ignored
	var name string
	err = db.QueryRowContext(ctx, "SELECT Name FROM test_table WHERE SystemID = 'system1'").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "System 1", name, "First insert should be kept when duplicate is ignored")
}

// TestBatchInserter_SQLiteVariableLimitWithForeignKeys tests the production scenario
// where Systems -> MediaTitles -> Media have foreign key dependencies and MediaTitles
// exceeds the SQLite variable limit. This ensures chunking works correctly with FK constraints.
func TestBatchInserter_SQLiteVariableLimitWithForeignKeys(t *testing.T) {
	// Setup in-memory database with FK enforcement
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create tables matching production schema
	_, err = db.ExecContext(ctx, `
		CREATE TABLE Systems (
			DBID INTEGER PRIMARY KEY,
			SystemID TEXT UNIQUE NOT NULL,
			Name TEXT NOT NULL
		)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE MediaTitles (
			DBID INTEGER PRIMARY KEY,
			SystemDBID INTEGER NOT NULL,
			Slug TEXT NOT NULL,
			Name TEXT NOT NULL,
			FOREIGN KEY(SystemDBID) REFERENCES Systems(DBID)
		)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserters with large batch size
	const systemRows = 3
	const titleRows = 8500 // Exceeds SQLite variable limit with 4 columns

	systemBI, err := NewBatchInserter(ctx, tx, "Systems", []string{"DBID", "SystemID", "Name"}, 10)
	require.NoError(t, err)

	titleBI, err := NewBatchInserter(ctx, tx, "MediaTitles",
		[]string{"DBID", "SystemDBID", "Slug", "Name"}, titleRows)
	require.NoError(t, err)

	// Set up FK dependency: MediaTitles depends on Systems
	titleBI.SetDependencies(systemBI)

	// Add systems first
	for i := 1; i <= systemRows; i++ {
		err = systemBI.Add(int64(i), fmt.Sprintf("system_%d", i), fmt.Sprintf("System %d", i))
		require.NoError(t, err)
	}

	// Add many media titles (will exceed SQLite variable limit)
	for i := 1; i <= titleRows; i++ {
		systemDBID := int64((i % systemRows) + 1) // Distribute across systems
		err = titleBI.Add(int64(i), systemDBID, fmt.Sprintf("slug_%d", i), fmt.Sprintf("Title %d", i))
		require.NoError(t, err)
	}

	// Flush titles (should automatically flush Systems first due to FK dependency)
	err = titleBI.Close()
	require.NoError(t, err, "Should handle FK dependencies and SQLite variable limit")

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify all systems were inserted
	var systemCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM Systems").Scan(&systemCount)
	require.NoError(t, err)
	assert.Equal(t, systemRows, systemCount)

	// Verify all titles were inserted
	var titleCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaTitles").Scan(&titleCount)
	require.NoError(t, err)
	assert.Equal(t, titleRows, titleCount)

	// Verify FK integrity - all titles should reference valid systems
	var invalidFK int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM MediaTitles mt
		LEFT JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE s.DBID IS NULL
	`).Scan(&invalidFK)
	require.NoError(t, err)
	assert.Equal(t, 0, invalidFK, "All MediaTitles should have valid SystemDBID references")
}
