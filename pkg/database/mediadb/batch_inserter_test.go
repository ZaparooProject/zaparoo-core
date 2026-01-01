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

// TestBatchInserter_MediaTitlesWithMetadataColumns tests that the batch inserter
// correctly handles all MediaTitles columns including SlugLength and SlugWordCount.
// This test catches schema mismatches between batch inserter column lists and actual SQL.
func TestBatchInserter_MediaTitlesWithMetadataColumns(t *testing.T) {
	t.Parallel()

	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create MediaTitles table with all production columns including metadata
	_, err = db.ExecContext(ctx, `
		CREATE TABLE MediaTitles (
			DBID INTEGER PRIMARY KEY,
			SystemDBID INTEGER NOT NULL,
			Slug TEXT NOT NULL,
			Name TEXT NOT NULL,
			SlugLength INTEGER DEFAULT 0,
			SlugWordCount INTEGER DEFAULT 0
		)`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserter with ALL 6 columns (this is what production code must match)
	bi, err := NewBatchInserter(ctx, tx, "MediaTitles",
		[]string{"DBID", "SystemDBID", "Slug", "Name", "SlugLength", "SlugWordCount"}, 10)
	require.NoError(t, err)

	// Test data with explicit metadata values
	testTitles := []struct {
		slug          string
		name          string
		dbid          int64
		systemDBID    int64
		slugLength    int
		slugWordCount int
	}{
		{dbid: 1, systemDBID: 100, slug: "supermetroid", name: "Super Metroid", slugLength: 12, slugWordCount: 2},
		{dbid: 2, systemDBID: 100, slug: "chronomtrigger", name: "Chrono Trigger", slugLength: 14, slugWordCount: 2},
		{dbid: 3, systemDBID: 200, slug: "dragonquestv", name: "Dragon Quest V", slugLength: 12, slugWordCount: 3},
		{dbid: 4, systemDBID: 200, slug: "finalfantasyvi", name: "Final Fantasy VI", slugLength: 14, slugWordCount: 3},
	}

	// Add all test titles to batch
	for _, tt := range testTitles {
		err = bi.Add(tt.dbid, tt.systemDBID, tt.slug, tt.name, tt.slugLength, tt.slugWordCount)
		require.NoError(t, err, "Should accept 6 values matching the column count")
	}

	// Flush and commit
	err = bi.Close()
	require.NoError(t, err)
	err = tx.Commit()
	require.NoError(t, err)

	// Verify all rows were inserted
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaTitles").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, len(testTitles), count)

	// Verify metadata columns were correctly inserted (not default values)
	for _, tt := range testTitles {
		var slug, name string
		var slugLength, slugWordCount int
		err = db.QueryRowContext(ctx,
			"SELECT Slug, Name, SlugLength, SlugWordCount FROM MediaTitles WHERE DBID = ?",
			tt.dbid).Scan(&slug, &name, &slugLength, &slugWordCount)
		require.NoError(t, err)
		assert.Equal(t, tt.slug, slug, "Slug should match for DBID %d", tt.dbid)
		assert.Equal(t, tt.name, name, "Name should match for DBID %d", tt.dbid)
		assert.Equal(t, tt.slugLength, slugLength,
			"SlugLength should be %d (not 0) for DBID %d", tt.slugLength, tt.dbid)
		assert.Equal(t, tt.slugWordCount, slugWordCount,
			"SlugWordCount should be %d (not 0) for DBID %d", tt.slugWordCount, tt.dbid)
	}
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
			SlugLength INTEGER DEFAULT 0,
			SlugWordCount INTEGER DEFAULT 0,
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
		[]string{"DBID", "SystemDBID", "Slug", "Name", "SlugLength", "SlugWordCount"}, titleRows)
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
		slugLen := 10 + (i % 20)                  // Variable slug lengths
		wordCount := 2 + (i % 5)                  // Variable word counts
		err = titleBI.Add(int64(i), systemDBID, fmt.Sprintf("slug_%d", i), fmt.Sprintf("Title %d", i),
			slugLen, wordCount)
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

func TestBatchInserter_Dependencies(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create parent and child tables with foreign key
	_, err = db.ExecContext(ctx,
		`CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, value TEXT,
		 FOREIGN KEY(parent_id) REFERENCES parent(id))`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserters with small batch size to trigger flushes
	parentBatch, err := NewBatchInserter(ctx, tx, "parent", []string{"id", "name"}, 3)
	require.NoError(t, err)

	childBatch, err := NewBatchInserter(ctx, tx, "child", []string{"id", "parent_id", "value"}, 3)
	require.NoError(t, err)

	// Set dependency: child depends on parent
	childBatch.SetDependencies(parentBatch)

	// Add rows to both batches
	// Add 2 parents (won't flush yet, batch size is 3)
	err = parentBatch.Add(int64(1), "Parent 1")
	require.NoError(t, err)
	err = parentBatch.Add(int64(2), "Parent 2")
	require.NoError(t, err)

	// Add 3 children - this will trigger a flush
	// The dependency mechanism should flush parent batch first
	err = childBatch.Add(int64(1), int64(1), "Child 1")
	require.NoError(t, err)
	err = childBatch.Add(int64(2), int64(1), "Child 2")
	require.NoError(t, err)
	err = childBatch.Add(int64(3), int64(2), "Child 3") // This triggers flush
	require.NoError(t, err)

	// Flush remaining data
	err = parentBatch.Close()
	require.NoError(t, err)
	err = childBatch.Close()
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify all rows were inserted correctly
	var parentCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM parent").Scan(&parentCount)
	require.NoError(t, err)
	assert.Equal(t, 2, parentCount, "All parent rows should be inserted")

	var childCount int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM child").Scan(&childCount)
	require.NoError(t, err)
	assert.Equal(t, 3, childCount, "All child rows should be inserted")

	// Verify foreign key relationships are intact
	var childParentID int
	err = db.QueryRowContext(ctx, "SELECT parent_id FROM child WHERE id = 3").Scan(&childParentID)
	require.NoError(t, err)
	assert.Equal(t, 2, childParentID, "Child should reference correct parent")
}

func TestBatchInserter_MultipleDependencies(t *testing.T) {
	// Setup in-memory database
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()

	// Create tables simulating Systems -> MediaTitles -> Media -> MediaTags <- Tags
	_, err = db.ExecContext(ctx,
		`CREATE TABLE systems (id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`CREATE TABLE titles (id INTEGER PRIMARY KEY, system_id INTEGER, name TEXT,
		 FOREIGN KEY(system_id) REFERENCES systems(id))`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`CREATE TABLE media (id INTEGER PRIMARY KEY, title_id INTEGER, path TEXT,
		 FOREIGN KEY(title_id) REFERENCES titles(id))`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`CREATE TABLE media_tags (media_id INTEGER, tag_id INTEGER,
		 FOREIGN KEY(media_id) REFERENCES media(id),
		 FOREIGN KEY(tag_id) REFERENCES tags(id))`)
	require.NoError(t, err)

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Create batch inserters with small batch size
	systemsBatch, err := NewBatchInserter(ctx, tx, "systems", []string{"id", "name"}, 2)
	require.NoError(t, err)

	titlesBatch, err := NewBatchInserter(ctx, tx, "titles", []string{"id", "system_id", "name"}, 2)
	require.NoError(t, err)

	mediaBatch, err := NewBatchInserter(ctx, tx, "media", []string{"id", "title_id", "path"}, 2)
	require.NoError(t, err)

	tagsBatch, err := NewBatchInserter(ctx, tx, "tags", []string{"id", "name"}, 2)
	require.NoError(t, err)

	mediaTagsBatch, err := NewBatchInserter(ctx, tx, "media_tags", []string{"media_id", "tag_id"}, 2)
	require.NoError(t, err)

	// Set up dependency chain
	titlesBatch.SetDependencies(systemsBatch)
	mediaBatch.SetDependencies(titlesBatch)
	mediaTagsBatch.SetDependencies(mediaBatch, tagsBatch)

	// Add data - this tests transitive dependencies
	// Add 1 system (won't flush, batch size is 2)
	err = systemsBatch.Add(int64(1), "NES")
	require.NoError(t, err)

	// Add 1 title (won't flush)
	err = titlesBatch.Add(int64(1), int64(1), "Super Mario Bros")
	require.NoError(t, err)

	// Add 1 media (won't flush)
	err = mediaBatch.Add(int64(1), int64(1), "/path/to/game.rom")
	require.NoError(t, err)

	// Add 1 tag (won't flush)
	err = tagsBatch.Add(int64(1), "platformer")
	require.NoError(t, err)

	// Add 2 media_tags - this should trigger flush of all dependencies
	err = mediaTagsBatch.Add(int64(1), int64(1))
	require.NoError(t, err)
	err = mediaTagsBatch.Add(int64(1), int64(1)) // Triggers flush
	require.NoError(t, err)

	// Flush remaining data
	err = systemsBatch.Close()
	require.NoError(t, err)
	err = titlesBatch.Close()
	require.NoError(t, err)
	err = mediaBatch.Close()
	require.NoError(t, err)
	err = tagsBatch.Close()
	require.NoError(t, err)
	err = mediaTagsBatch.Close()
	require.NoError(t, err)

	// Commit transaction
	err = tx.Commit()
	require.NoError(t, err)

	// Verify all data was inserted
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM systems").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM titles").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM media").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tags").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_tags").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}
