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

package helpers

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
)

func NewInMemoryUserDB(t *testing.T) (db *userdb.UserDB, cleanup func()) {
	t.Helper()

	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	// Create temporary directory for test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "userdb_test.db")

	// Open SQLite database using temp file (persists across connection close/reopen)
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create UserDB instance and set the sql field directly
	db = &userdb.UserDB{}
	err = db.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	if err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("Failed to close SQL database after setup error: %v", closeErr)
		}
		t.Fatalf("Failed to set up UserDB for testing: %v", err)
	}

	cleanup = func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close UserDB: %v", err)
		}
	}

	return db, cleanup
}

func NewInMemoryMediaDB(t *testing.T) (db *mediadb.MediaDB, cleanup func()) {
	t.Helper()

	// Create temporary directory for test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mediadb_test.db")

	// Open SQLite database using temp file with foreign keys enabled
	// This matches the production database configuration and ensures CASCADE deletes work
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	db = &mediadb.MediaDB{}
	ctx := context.Background()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	err = db.SetSQLForTesting(ctx, sqlDB, mockPlatform)
	if err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("Failed to close SQL database after setup error: %v", closeErr)
		}
		t.Fatalf("Failed to set up MediaDB for testing: %v", err)
	}
	db.SetDBPathForTesting(dbPath)

	cleanup = func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close MediaDB: %v", err)
		}
	}

	return db, cleanup
}

// NewTestDatabase creates both MediaDB and UserDB for comprehensive testing.
// Returns a Database wrapper and cleanup function that should be deferred.
func NewTestDatabase(t *testing.T) (db *database.Database, cleanup func()) {
	t.Helper()

	mediaDB, mediaCleanup := NewInMemoryMediaDB(t)
	userDB, userCleanup := NewInMemoryUserDB(t)

	db = &database.Database{
		MediaDB: mediaDB,
		UserDB:  userDB,
	}

	cleanup = func() {
		mediaCleanup()
		userCleanup()
	}

	return db, cleanup
}
