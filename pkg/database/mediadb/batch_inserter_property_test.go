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
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// columnNameGen generates valid SQL column names.
func columnNameGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,29}`)
}

// tableNameGen generates valid SQL table names.
func tableNameGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,29}`)
}

// ============================================================================
// SQL Generation Property Tests
// ============================================================================

// TestPropertyMultiRowSQLPlaceholderCount verifies placeholder count matches rowCount * columnCount.
func TestPropertyMultiRowSQLPlaceholderCount(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		columnCount := rapid.IntRange(1, 20).Draw(t, "columnCount")
		rowCount := rapid.IntRange(1, 100).Draw(t, "rowCount")

		// Create columns
		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		// Create a minimal BatchInserter just to use generateMultiRowInsertSQL
		bi := &BatchInserter{
			tableName:   "test_table",
			columns:     columns,
			columnCount: columnCount,
			orIgnore:    rapid.Bool().Draw(t, "orIgnore"),
		}

		sqlStmt := bi.generateMultiRowInsertSQL(rowCount)

		// Count placeholders
		placeholderCount := strings.Count(sqlStmt, "?")
		expectedCount := rowCount * columnCount

		if placeholderCount != expectedCount {
			t.Fatalf("Expected %d placeholders (rows=%d * cols=%d), got %d\nSQL: %s",
				expectedCount, rowCount, columnCount, placeholderCount, sqlStmt)
		}
	})
}

// TestPropertySingleRowSQLPlaceholderCount verifies single-row SQL has correct placeholder count.
func TestPropertySingleRowSQLPlaceholderCount(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		columnCount := rapid.IntRange(1, 20).Draw(t, "columnCount")

		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		bi := &BatchInserter{
			tableName:   "test_table",
			columns:     columns,
			columnCount: columnCount,
			orIgnore:    rapid.Bool().Draw(t, "orIgnore"),
		}

		sqlStmt := bi.generateSingleRowInsertSQL()

		placeholderCount := strings.Count(sqlStmt, "?")
		if placeholderCount != columnCount {
			t.Fatalf("Expected %d placeholders for single row, got %d\nSQL: %s",
				columnCount, placeholderCount, sqlStmt)
		}
	})
}

// TestPropertySQLContainsTableName verifies generated SQL contains table name.
func TestPropertySQLContainsTableName(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tableName := tableNameGen().Draw(t, "tableName")
		columnCount := rapid.IntRange(1, 10).Draw(t, "columnCount")

		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		bi := &BatchInserter{
			tableName:   tableName,
			columns:     columns,
			columnCount: columnCount,
		}

		multiRowSQL := bi.generateMultiRowInsertSQL(5)
		singleRowSQL := bi.generateSingleRowInsertSQL()

		if !strings.Contains(multiRowSQL, tableName) {
			t.Fatalf("Multi-row SQL should contain table name %q: %s", tableName, multiRowSQL)
		}
		if !strings.Contains(singleRowSQL, tableName) {
			t.Fatalf("Single-row SQL should contain table name %q: %s", tableName, singleRowSQL)
		}
	})
}

// TestPropertySQLContainsColumns verifies generated SQL contains all column names.
func TestPropertySQLContainsColumns(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		columnCount := rapid.IntRange(1, 10).Draw(t, "columnCount")

		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		bi := &BatchInserter{
			tableName:   "test_table",
			columns:     columns,
			columnCount: columnCount,
		}

		sqlStmt := bi.generateMultiRowInsertSQL(1)

		for _, col := range columns {
			if !strings.Contains(sqlStmt, col) {
				t.Fatalf("SQL should contain column %q: %s", col, sqlStmt)
			}
		}
	})
}

// TestPropertyOrIgnoreSQL verifies OR IGNORE is included when enabled.
func TestPropertyOrIgnoreSQL(t *testing.T) {
	t.Parallel()

	columns := []string{"col1", "col2"}

	// With OR IGNORE
	biIgnore := &BatchInserter{
		tableName:   "test",
		columns:     columns,
		columnCount: 2,
		orIgnore:    true,
	}
	sqlIgnore := biIgnore.generateMultiRowInsertSQL(1)
	if !strings.Contains(sqlIgnore, "OR IGNORE") {
		t.Fatalf("SQL should contain OR IGNORE: %s", sqlIgnore)
	}

	// Without OR IGNORE
	biNormal := &BatchInserter{
		tableName:   "test",
		columns:     columns,
		columnCount: 2,
		orIgnore:    false,
	}
	sqlNormal := biNormal.generateMultiRowInsertSQL(1)
	if strings.Contains(sqlNormal, "OR IGNORE") {
		t.Fatalf("SQL should NOT contain OR IGNORE: %s", sqlNormal)
	}
}

// ============================================================================
// Validation Property Tests
// ============================================================================

// TestPropertyValidationRejectsNilTx verifies nil transaction is rejected.
func TestPropertyValidationRejectsNilTx(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tableName := tableNameGen().Draw(t, "tableName")
		columnCount := rapid.IntRange(1, 10).Draw(t, "columnCount")
		batchSize := rapid.IntRange(1, 1000).Draw(t, "batchSize")

		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		_, err := NewBatchInserter(context.Background(), nil, tableName, columns, batchSize)
		if err == nil {
			t.Fatal("Expected error for nil transaction")
		}
		if !strings.Contains(err.Error(), "transaction is nil") {
			t.Fatalf("Expected 'transaction is nil' error, got: %v", err)
		}
	})
}

// TestPropertyValidationRejectsEmptyTableName verifies empty table name is rejected.
func TestPropertyValidationRejectsEmptyTableName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	rapid.Check(t, func(t *rapid.T) {
		columnCount := rapid.IntRange(1, 10).Draw(t, "columnCount")
		batchSize := rapid.IntRange(1, 1000).Draw(t, "batchSize")

		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		_, err := NewBatchInserter(context.Background(), tx, "", columns, batchSize)
		if err == nil {
			t.Fatal("Expected error for empty table name")
		}
		if !strings.Contains(err.Error(), "table name is empty") {
			t.Fatalf("Expected 'table name is empty' error, got: %v", err)
		}
	})
}

// TestPropertyValidationRejectsEmptyColumns verifies empty columns list is rejected.
func TestPropertyValidationRejectsEmptyColumns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	rapid.Check(t, func(t *rapid.T) {
		tableName := tableNameGen().Draw(t, "tableName")
		batchSize := rapid.IntRange(1, 1000).Draw(t, "batchSize")

		_, err := NewBatchInserter(context.Background(), tx, tableName, []string{}, batchSize)
		if err == nil {
			t.Fatal("Expected error for empty columns")
		}
		if !strings.Contains(err.Error(), "columns list is empty") {
			t.Fatalf("Expected 'columns list is empty' error, got: %v", err)
		}
	})
}

// TestPropertyValidationRejectsNonPositiveBatchSize verifies non-positive batch size is rejected.
func TestPropertyValidationRejectsNonPositiveBatchSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	rapid.Check(t, func(t *rapid.T) {
		tableName := tableNameGen().Draw(t, "tableName")
		columnCount := rapid.IntRange(1, 10).Draw(t, "columnCount")
		// Generate non-positive batch size
		batchSize := rapid.IntRange(-100, 0).Draw(t, "batchSize")

		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		_, err := NewBatchInserter(context.Background(), tx, tableName, columns, batchSize)
		if err == nil {
			t.Fatalf("Expected error for non-positive batch size %d", batchSize)
		}
		if !strings.Contains(err.Error(), "batch size must be positive") {
			t.Fatalf("Expected 'batch size must be positive' error, got: %v", err)
		}
	})
}

// TestPropertyAddRejectsWrongColumnCount verifies Add rejects wrong value count.
func TestPropertyAddRejectsWrongColumnCount(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, "CREATE TABLE test (a TEXT, b TEXT, c TEXT)")
	require.NoError(t, err)

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	rapid.Check(t, func(t *rapid.T) {
		columnCount := rapid.IntRange(2, 10).Draw(t, "columnCount")
		batchSize := rapid.IntRange(1, 100).Draw(t, "batchSize")

		columns := make([]string, columnCount)
		for i := range columnCount {
			columns[i] = columnNameGen().Draw(t, "column")
		}

		bi, err := NewBatchInserter(context.Background(), tx, "test", columns, batchSize)
		require.NoError(t, err)

		// Try to add wrong number of values
		wrongCount := rapid.IntRange(1, columnCount-1).Draw(t, "wrongCount")
		values := make([]any, wrongCount)
		for i := range wrongCount {
			values[i] = "value"
		}

		err = bi.Add(values...)
		if err == nil {
			t.Fatalf("Expected error for %d values when %d expected", wrongCount, columnCount)
		}
		if !strings.Contains(err.Error(), "expected") {
			t.Fatalf("Expected column count mismatch error, got: %v", err)
		}
	})
}

// ============================================================================
// Buffer Management Property Tests
// ============================================================================

// TestPropertyBufferCountMatchesAddCalls verifies currentCount tracks Add calls.
func TestPropertyBufferCountMatchesAddCalls(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, "CREATE TABLE test (a TEXT, b TEXT)")
	require.NoError(t, err)

	rapid.Check(t, func(t *rapid.T) {
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		// Use large batch size to avoid auto-flush
		batchSize := rapid.IntRange(100, 1000).Draw(t, "batchSize")
		addCount := rapid.IntRange(1, batchSize-1).Draw(t, "addCount")

		bi, err := NewBatchInserter(ctx, tx, "test", []string{"a", "b"}, batchSize)
		require.NoError(t, err)

		for range addCount {
			err = bi.Add("val1", "val2")
			require.NoError(t, err)
		}

		if bi.currentCount != addCount {
			t.Fatalf("Expected currentCount=%d after %d Add calls, got %d",
				addCount, addCount, bi.currentCount)
		}
	})
}

// TestPropertyFlushResetsBuffer verifies Flush resets buffer state.
func TestPropertyFlushResetsBuffer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, "CREATE TABLE test (a TEXT, b TEXT)")
	require.NoError(t, err)

	rapid.Check(t, func(t *rapid.T) {
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		batchSize := rapid.IntRange(10, 100).Draw(t, "batchSize")
		addCount := rapid.IntRange(1, batchSize-1).Draw(t, "addCount")

		bi, err := NewBatchInserter(ctx, tx, "test", []string{"a", "b"}, batchSize)
		require.NoError(t, err)

		for range addCount {
			err = bi.Add("val1", "val2")
			require.NoError(t, err)
		}

		err = bi.Flush()
		require.NoError(t, err)

		if bi.currentCount != 0 {
			t.Fatalf("Expected currentCount=0 after Flush, got %d", bi.currentCount)
		}
		if len(bi.buffer) != 0 {
			t.Fatalf("Expected empty buffer after Flush, got length %d", len(bi.buffer))
		}
	})
}

// TestPropertyEmptyFlushIsNoop verifies Flush with no data is a no-op.
func TestPropertyEmptyFlushIsNoop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, "CREATE TABLE test (a TEXT)")
	require.NoError(t, err)

	rapid.Check(t, func(t *rapid.T) {
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		batchSize := rapid.IntRange(1, 100).Draw(t, "batchSize")

		bi, err := NewBatchInserter(ctx, tx, "test", []string{"a"}, batchSize)
		require.NoError(t, err)

		// Flush without adding anything - should not error
		err = bi.Flush()
		if err != nil {
			t.Fatalf("Empty Flush should not error: %v", err)
		}

		// Multiple empty flushes should also be fine
		err = bi.Flush()
		if err != nil {
			t.Fatalf("Multiple empty Flushes should not error: %v", err)
		}
	})
}
