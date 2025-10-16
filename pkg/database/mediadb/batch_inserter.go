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
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// BatchInserter manages batched multi-row inserts for a specific table
type BatchInserter struct {
	ctx          context.Context
	tx           *sql.Tx
	tableName    string
	columns      []string
	buffer       []any
	dependencies []*BatchInserter
	batchSize    int
	columnCount  int
	currentCount int
	orIgnore     bool
}

// NewBatchInserter creates a batch inserter for the given table
func NewBatchInserter(
	ctx context.Context,
	tx *sql.Tx,
	tableName string,
	columns []string,
	batchSize int,
) (*BatchInserter, error) {
	return NewBatchInserterWithOptions(ctx, tx, tableName, columns, batchSize, false)
}

// NewBatchInserterWithOptions creates a batch inserter with OR IGNORE option
func NewBatchInserterWithOptions(
	ctx context.Context,
	tx *sql.Tx,
	tableName string,
	columns []string,
	batchSize int,
	orIgnore bool,
) (*BatchInserter, error) {
	if tx == nil {
		return nil, errors.New("transaction is nil")
	}
	if tableName == "" {
		return nil, errors.New("table name is empty")
	}
	if len(columns) == 0 {
		return nil, errors.New("columns list is empty")
	}
	if batchSize <= 0 {
		return nil, fmt.Errorf("batch size must be positive, got %d", batchSize)
	}

	return &BatchInserter{
		ctx:          ctx,
		tx:           tx,
		tableName:    tableName,
		columns:      columns,
		batchSize:    batchSize,
		columnCount:  len(columns),
		buffer:       make([]any, 0, batchSize*len(columns)),
		currentCount: 0,
		orIgnore:     orIgnore,
	}, nil
}

// SetDependencies sets the parent batch inserters that must flush before this one
func (b *BatchInserter) SetDependencies(deps ...*BatchInserter) {
	b.dependencies = deps
}

// Add appends a row to the current batch
// Note: Auto-flush removed to prevent FK constraint failures when parent batches
// have fewer items than child batches (due to deduplication). Flushing now happens
// only at transaction commit in proper dependency order.
func (b *BatchInserter) Add(values ...any) error {
	if len(values) != b.columnCount {
		return fmt.Errorf(
			"expected %d values for columns %v, got %d",
			b.columnCount,
			b.columns,
			len(values),
		)
	}

	// Add values to buffer
	b.buffer = append(b.buffer, values...)
	b.currentCount++

	// No auto-flush - let transaction commit handle flushing in dependency order
	return nil
}

// Flush executes the current batch and resets the buffer
func (b *BatchInserter) Flush() error {
	if b.currentCount == 0 {
		return nil // Nothing to flush
	}

	// Flush all dependencies first to maintain foreign key integrity
	for _, dep := range b.dependencies {
		if dep.currentCount > 0 {
			log.Debug().
				Str("parent_table", dep.tableName).
				Str("child_table", b.tableName).
				Int("parent_rows", dep.currentCount).
				Int("child_rows", b.currentCount).
				Msg("flushing dependency batch before child")
		}
		if err := dep.Flush(); err != nil {
			return fmt.Errorf("failed to flush dependency for %s: %w", b.tableName, err)
		}
	}

	// Generate statement for current batch size
	sqlStmt := b.generateMultiRowInsertSQL(b.currentCount)
	stmt, err := b.tx.PrepareContext(b.ctx, sqlStmt)
	if err != nil {
		// Check if error is due to exceeding SQLite's SQLITE_MAX_VARIABLE_NUMBER limit
		if strings.Contains(err.Error(), "too many SQL variables") {
			log.Debug().
				Str("table", b.tableName).
				Int("row_count", b.currentCount).
				Int("total_variables", b.currentCount*b.columnCount).
				Msg("batch exceeds SQLite variable limit, auto-chunking")
			return b.flushChunked()
		}
		return fmt.Errorf("failed to prepare batch insert: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close batch insert statement")
		}
	}()

	// Execute batch insert
	_, err = stmt.ExecContext(b.ctx, b.buffer[:b.currentCount*b.columnCount]...)
	if err != nil {
		// Log the error and fall back to single-row inserts for this batch
		log.Error().Err(err).
			Str("table", b.tableName).
			Int("row_count", b.currentCount).
			Bool("or_ignore", b.orIgnore).
			Msg("BATCH INSERT FAILED - falling back to single-row inserts")
		return b.flushSingleRow()
	}

	// Reset buffer
	b.buffer = b.buffer[:0]
	b.currentCount = 0
	return nil
}

// flushChunked splits the batch into smaller chunks when SQLite variable limit is exceeded
func (b *BatchInserter) flushChunked() error {
	// SQLite's default SQLITE_MAX_VARIABLE_NUMBER is 32766
	// Use 32000 to provide a safety margin
	const maxSQLiteVars = 32000

	// Calculate maximum rows per chunk based on column count
	maxRowsPerChunk := maxSQLiteVars / b.columnCount
	if maxRowsPerChunk == 0 {
		// Fallback to single-row inserts if even one row exceeds the limit
		// (This shouldn't happen in practice unless someone has 32K+ columns)
		return b.flushSingleRow()
	}

	// Split buffer into chunks and flush each one
	rowsRemaining := b.currentCount
	bufferOffset := 0

	for rowsRemaining > 0 {
		chunkSize := rowsRemaining
		if chunkSize > maxRowsPerChunk {
			chunkSize = maxRowsPerChunk
		}

		// Extract chunk from buffer
		chunkStart := bufferOffset
		chunkEnd := bufferOffset + (chunkSize * b.columnCount)
		chunkBuffer := b.buffer[chunkStart:chunkEnd]

		// Execute chunk (extracted to separate function to satisfy both sqlclosecheck and revive linters)
		if err := b.executeChunk(chunkSize, chunkBuffer); err != nil {
			return err
		}

		log.Debug().
			Str("table", b.tableName).
			Int("chunk_size", chunkSize).
			Int("remaining", rowsRemaining-chunkSize).
			Msg("flushed batch chunk")

		// Move to next chunk
		rowsRemaining -= chunkSize
		bufferOffset = chunkEnd
	}

	// Reset buffer
	b.buffer = b.buffer[:0]
	b.currentCount = 0
	return nil
}

// executeChunk executes a single chunk of the batch insert
func (b *BatchInserter) executeChunk(chunkSize int, chunkBuffer []any) error {
	sqlStmt := b.generateMultiRowInsertSQL(chunkSize)
	stmt, err := b.tx.PrepareContext(b.ctx, sqlStmt)
	if err != nil {
		return fmt.Errorf("failed to prepare chunked batch insert: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close chunked batch statement")
		}
	}()

	_, execErr := stmt.ExecContext(b.ctx, chunkBuffer...)
	if execErr != nil {
		log.Error().Err(execErr).
			Str("table", b.tableName).
			Int("chunk_size", chunkSize).
			Msg("chunked batch insert failed")
		return fmt.Errorf("failed to execute chunked batch: %w", execErr)
	}

	return nil
}

// flushSingleRow falls back to inserting each row individually
func (b *BatchInserter) flushSingleRow() error {
	// Generate single-row insert statement
	singleRowSQL := b.generateSingleRowInsertSQL()
	stmt, err := b.tx.PrepareContext(b.ctx, singleRowSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare single-row fallback insert: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close single-row insert statement")
		}
	}()

	// Insert each row individually
	for i := range b.currentCount {
		offset := i * b.columnCount
		values := b.buffer[offset : offset+b.columnCount]
		_, err := stmt.ExecContext(b.ctx, values...)
		if err != nil {
			log.Error().Err(err).
				Str("table", b.tableName).
				Int("row", i).
				Msg("failed to insert row in fallback mode")
			// Continue attempting remaining rows
		}
	}

	// Reset buffer
	b.buffer = b.buffer[:0]
	b.currentCount = 0
	return nil
}

// Close flushes remaining items and closes the statement
func (b *BatchInserter) Close() error {
	return b.Flush()
}

// generateMultiRowInsertSQL creates a multi-row INSERT statement
func (b *BatchInserter) generateMultiRowInsertSQL(rowCount int) string {
	// Example for Media table with 3 rows:
	// INSERT INTO Media (Path, MediaTitleDBID, SystemDBID) VALUES
	//     (?, ?, ?),
	//     (?, ?, ?),
	//     (?, ?, ?)

	insertKeyword := "INSERT"
	if b.orIgnore {
		insertKeyword = "INSERT OR IGNORE"
	}

	colNames := strings.Join(b.columns, ", ")
	placeholder := "(" + strings.Repeat("?, ", b.columnCount-1) + "?)"
	placeholders := strings.Repeat(placeholder+",\n    ", rowCount-1) + placeholder

	return fmt.Sprintf("%s INTO %s (%s) VALUES\n    %s", insertKeyword, b.tableName, colNames, placeholders)
}

// generateSingleRowInsertSQL creates a single-row INSERT statement
func (b *BatchInserter) generateSingleRowInsertSQL() string {
	insertKeyword := "INSERT"
	if b.orIgnore {
		insertKeyword = "INSERT OR IGNORE"
	}

	colNames := strings.Join(b.columns, ", ")
	placeholders := strings.Repeat("?, ", b.columnCount-1) + "?"
	return fmt.Sprintf("%s INTO %s (%s) VALUES (%s)", insertKeyword, b.tableName, colNames, placeholders)
}
