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
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
