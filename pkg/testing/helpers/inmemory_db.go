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
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/dbtemplate"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	_ "github.com/mattn/go-sqlite3"
)

// NewUserDBInDataDir opens a UserDB the way the service does, at
// DataDir/user.db under a temporary directory. Unlike NewInMemoryUserDB it
// knows its own path, so the backup and restore calls that replace the file
// work against it. Use it only when a test needs that; the other helper is
// cheaper.
func NewUserDBInDataDir(t *testing.T) (db *userdb.UserDB, cleanup func()) {
	t.Helper()

	dataDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: dataDir,
		TempDir: filepath.Join(dataDir, "tmp"),
	})

	db, err := userdb.OpenUserDB(context.Background(), mockPlatform)
	if err != nil {
		t.Fatalf("Failed to open test user database: %v", err)
	}

	cleanup = func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close UserDB: %v", err)
		}
	}

	return db, cleanup
}

// NewInMemoryUserDB opens a migrated UserDB in a temporary file. Despite the
// name the database is on disk, so it survives closing and reopening the
// connection. The schema comes from a template built once per test binary.
func NewInMemoryUserDB(t *testing.T) (db *userdb.UserDB, cleanup func()) {
	t.Helper()

	tempDir := t.TempDir()
	dbtemplate.Seed(t, "userdb-plain", tempDir, buildPlainUserDB)

	db, err := openPlainUserDB(filepath.Join(tempDir, plainUserDBFile))
	if err != nil {
		t.Fatalf("Failed to set up UserDB for testing: %v", err)
	}

	cleanup = func() {
		if err := db.Close(); err != nil {
			t.Errorf("Failed to close UserDB: %v", err)
		}
	}

	return db, cleanup
}

const plainUserDBFile = "userdb_test.db"

func buildPlainUserDB(dir string) error {
	db, err := openPlainUserDB(filepath.Join(dir, plainUserDBFile))
	if err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("closing template UserDB: %w", err)
	}
	return nil
}

func openPlainUserDB(dbPath string) (*userdb.UserDB, error) {
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")

	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening test database: %w", err)
	}

	db := &userdb.UserDB{}
	if err := db.SetSQLForTesting(context.Background(), sqlDB, mockPlatform); err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, fmt.Errorf("allocating test database: %w", err)
	}
	return db, nil
}

// NewInMemoryMediaDB opens a MediaDB in a temporary data directory with the
// production opener, so tests get the same connection hooks, WAL settings and
// pool behavior as the service. Despite the name the database is on disk. The
// schema comes from a template built once per test binary.
func NewInMemoryMediaDB(tb testing.TB) (db *mediadb.MediaDB, cleanup func()) {
	tb.Helper()

	tempDir := tb.TempDir()
	dbtemplate.Seed(tb, "mediadb", tempDir, buildMediaDB)

	var err error
	db, err = openMediaDBInDataDir(tempDir)
	if err != nil {
		tb.Fatalf("Failed to set up MediaDB for testing: %v", err)
	}

	cleanup = func() {
		if err := db.Close(); err != nil {
			tb.Errorf("Failed to close MediaDB: %v", err)
		}
	}

	return db, cleanup
}

func buildMediaDB(dir string) error {
	db, err := openMediaDBInDataDir(dir)
	if err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("closing template MediaDB: %w", err)
	}
	return nil
}

func openMediaDBInDataDir(dataDir string) (*mediadb.MediaDB, error) {
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: dataDir})

	db, err := mediadb.OpenMediaDB(context.Background(), mockPlatform)
	if err != nil {
		return nil, fmt.Errorf("opening test MediaDB: %w", err)
	}
	return db, nil
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
