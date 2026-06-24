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

package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCorruptionError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		name string
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unrelated", err: errors.New("syntax error"), want: false},
		{name: "sqlite corrupt code", err: sqlite3.Error{Code: sqlite3.ErrCorrupt}, want: true},
		{name: "sqlite not-a-db code", err: sqlite3.Error{Code: sqlite3.ErrNotADB}, want: true},
		{
			name: "wrapped malformed string",
			err:  fmt.Errorf("query failed: %w", errors.New("database disk image is malformed")),
			want: true,
		},
		{
			name: "wrapped not-a-database string",
			err:  fmt.Errorf("open failed: %w", errors.New("file is not a database")),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsCorruptionError(tt.err))
		})
	}
}

func TestCorruptMarkerLifecycle(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	assert.False(t, IsMarkedCorrupt(dbPath))
	assert.Equal(t, dbPath+CorruptMarkerSuffix, CorruptMarkerPath(dbPath))

	MarkCorrupt(dbPath, "torn write", time.Now())
	assert.True(t, IsMarkedCorrupt(dbPath))

	contents, err := os.ReadFile(CorruptMarkerPath(dbPath))
	require.NoError(t, err)
	assert.Contains(t, string(contents), "torn write")

	require.NoError(t, ClearCorruptMarker(dbPath))
	assert.False(t, IsMarkedCorrupt(dbPath))
	// Clearing an absent marker is a no-op.
	require.NoError(t, ClearCorruptMarker(dbPath))
}

func TestNoteCorruption(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	assert.False(t, NoteCorruption(dbPath, errors.New("not corruption"), time.Now()))
	assert.False(t, IsMarkedCorrupt(dbPath))

	assert.True(t, NoteCorruption(dbPath, sqlite3.Error{Code: sqlite3.ErrCorrupt}, time.Now()))
	assert.True(t, IsMarkedCorrupt(dbPath))
}

func TestRemoveSidecars(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		require.NoError(t, os.WriteFile(sidecar, []byte("x"), 0o600))
	}

	RemoveSidecars(dbPath)

	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		_, err := os.Stat(sidecar)
		assert.True(t, os.IsNotExist(err), "sidecar should be removed: %s", sidecar)
	}
	// Removing absent sidecars is a no-op.
	RemoveSidecars(dbPath)
}

func TestConnLoadStore(t *testing.T) {
	t.Parallel()
	var c Conn
	assert.Nil(t, c.Load())

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	c.Store(db)
	assert.Same(t, db, c.Load())

	c.Store(nil)
	assert.Nil(t, c.Load())
}
