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

// Package helpers provides testing utilities for database operations.
//
// This package includes mock implementations of database interfaces and helper
// functions for setting up test databases with sqlmock. It enables testing
// database operations without requiring a real SQLite database.
//
// Example usage:
//
//	func TestDatabaseOperations(t *testing.T) {
//		// Create mock database interfaces
//		userDB := helpers.NewMockUserDBI()
//		mediaDB := helpers.NewMockMediaDBI()
//
//		// Set up expectations
//		userDB.On("AddHistory", helpers.HistoryEntryMatcher()).Return(nil)
//
//		// Use in your code
//		err := MyFunction(userDB)
//
//		// Verify expectations were met
//		require.NoError(t, err)
//		userDB.AssertExpectations(t)
//	}
//
// For complete examples, see pkg/testing/examples/database_example_test.go
package helpers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/mock"
)

// MockUserDBI is a mock implementation of the UserDBI interface using testify/mock
type MockUserDBI struct {
	mock.Mock
}

// GenericDBI methods
func (m *MockUserDBI) Open() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI open failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) UnsafeGetSQLDb() *sql.DB {
	args := m.Called()
	if db, ok := args.Get(0).(*sql.DB); ok {
		return db
	}
	return nil
}

func (m *MockUserDBI) Truncate() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI truncate failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) Allocate() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI allocate failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) MigrateUp() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI migrate up failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) Vacuum() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI vacuum failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) Close() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI close failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetDBPath() string {
	args := m.Called()
	return args.String(0)
}

// UserDBI specific methods
func (m *MockUserDBI) AddHistory(entry *database.HistoryEntry) error {
	args := m.Called(entry)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI add history failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetHistory(lastID int) ([]database.HistoryEntry, error) {
	args := m.Called(lastID)
	if history, ok := args.Get(0).([]database.HistoryEntry); ok {
		if err := args.Error(1); err != nil {
			return history, fmt.Errorf("mock UserDBI get history failed: %w", err)
		}
		return history, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI get history failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) CleanupHistory(retentionDays int) (int64, error) {
	args := m.Called(retentionDays)
	rowsDeleted, ok := args.Get(0).(int64)
	if !ok {
		rowsDeleted = 0
	}
	if err := args.Error(1); err != nil {
		return rowsDeleted, fmt.Errorf("mock UserDBI cleanup history failed: %w", err)
	}
	return rowsDeleted, nil
}

func (m *MockUserDBI) AddMapping(mapping *database.Mapping) error {
	args := m.Called(mapping)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI add mapping failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetMapping(id int64) (database.Mapping, error) {
	args := m.Called(id)
	if mapping, ok := args.Get(0).(database.Mapping); ok {
		if err := args.Error(1); err != nil {
			return mapping, fmt.Errorf("mock UserDBI get mapping failed: %w", err)
		}
		return mapping, nil
	}
	if err := args.Error(1); err != nil {
		return database.Mapping{}, fmt.Errorf("mock UserDBI get mapping failed: %w", err)
	}
	return database.Mapping{}, nil
}

func (m *MockUserDBI) DeleteMapping(id int64) error {
	args := m.Called(id)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI delete mapping failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) UpdateMapping(id int64, mapping *database.Mapping) error {
	args := m.Called(id, mapping)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI update mapping failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetAllMappings() ([]database.Mapping, error) {
	args := m.Called()
	if mappings, ok := args.Get(0).([]database.Mapping); ok {
		if err := args.Error(1); err != nil {
			return mappings, fmt.Errorf("mock UserDBI get all mappings failed: %w", err)
		}
		return mappings, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI get all mappings failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) GetEnabledMappings() ([]database.Mapping, error) {
	args := m.Called()
	if mappings, ok := args.Get(0).([]database.Mapping); ok {
		if err := args.Error(1); err != nil {
			return mappings, fmt.Errorf("mock UserDBI get enabled mappings failed: %w", err)
		}
		return mappings, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI get enabled mappings failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) UpdateZapLinkHost(host string, zapscript int) error {
	args := m.Called(host, zapscript)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetZapLinkHost(host string) (found, zapScript bool, err error) {
	args := m.Called(host)
	return args.Bool(0), args.Bool(1), args.Error(2)
}

func (m *MockUserDBI) UpdateZapLinkCache(url, zapscript string) error {
	args := m.Called(url, zapscript)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetZapLinkCache(url string) (string, error) {
	args := m.Called(url)
	return args.String(0), args.Error(1)
}

func (m *MockUserDBI) AddMediaHistory(entry *database.MediaHistoryEntry) (int64, error) {
	args := m.Called(entry)
	dbid, ok := args.Get(0).(int64)
	if !ok {
		dbid = 0
	}
	if err := args.Error(1); err != nil {
		return dbid, fmt.Errorf("mock UserDBI add media history failed: %w", err)
	}
	return dbid, nil
}

func (m *MockUserDBI) UpdateMediaHistoryTime(dbid int64, playTime int) error {
	args := m.Called(dbid, playTime)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI update media history time failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) CloseMediaHistory(dbid int64, endTime time.Time, playTime int) error {
	args := m.Called(dbid, endTime, playTime)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI close media history failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetMediaHistory(lastID, limit int) ([]database.MediaHistoryEntry, error) {
	args := m.Called(lastID, limit)
	history, ok := args.Get(0).([]database.MediaHistoryEntry)
	if !ok {
		history = []database.MediaHistoryEntry{}
	}
	if err := args.Error(1); err != nil {
		return history, fmt.Errorf("mock UserDBI get media history failed: %w", err)
	}
	return history, nil
}

func (m *MockUserDBI) CloseHangingMediaHistory() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI close hanging media history failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) CleanupMediaHistory(retentionDays int) (int64, error) {
	args := m.Called(retentionDays)
	rowsDeleted, ok := args.Get(0).(int64)
	if !ok {
		rowsDeleted = 0
	}
	if err := args.Error(1); err != nil {
		return rowsDeleted, fmt.Errorf("mock UserDBI cleanup media history failed: %w", err)
	}
	return rowsDeleted, nil
}

func (m *MockUserDBI) HealTimestamps(bootUUID string, trueBootTime time.Time) (int64, error) {
	args := m.Called(bootUUID, trueBootTime)
	rowsHealed, ok := args.Get(0).(int64)
	if !ok {
		rowsHealed = 0
	}
	if err := args.Error(1); err != nil {
		return rowsHealed, fmt.Errorf("mock UserDBI heal timestamps failed: %w", err)
	}
	return rowsHealed, nil
}

// MockMediaDBI is a mock implementation of the MediaDBI interface using testify/mock
type MockMediaDBI struct {
	mock.Mock

	// Transaction tracking for tests
	TransactionCount     int  // Total number of transactions begun
	ActiveTransaction    bool // Whether a transaction is currently active
	OperationsOutsideTxn int  // Count of operations performed outside transactions
}

// trackDatabaseOperation tracks whether operations happen inside or outside transactions
func (m *MockMediaDBI) trackDatabaseOperation() {
	if !m.ActiveTransaction {
		m.OperationsOutsideTxn++
	}
}

// GenericDBI methods
func (m *MockMediaDBI) Open() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) UnsafeGetSQLDb() *sql.DB {
	args := m.Called()
	if db, ok := args.Get(0).(*sql.DB); ok {
		return db
	}
	return nil
}

func (m *MockMediaDBI) Truncate() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) Allocate() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) MigrateUp() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) Vacuum() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) Close() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// GetMediaByText is a convenience method for testing - wraps SearchMediaPathExact
func (m *MockMediaDBI) GetMediaByText(query string) (database.Media, error) {
	args := m.Called(query)
	if media, ok := args.Get(0).(database.Media); ok {
		if err := args.Error(1); err != nil {
			return media, fmt.Errorf("mock operation failed: %w", err)
		}
		return media, nil
	}
	if err := args.Error(1); err != nil {
		return database.Media{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.Media{}, nil
}

func (m *MockMediaDBI) GetDBPath() string {
	args := m.Called()
	return args.String(0)
}

// MediaDBI specific methods - Transaction handling
func (m *MockMediaDBI) BeginTransaction(batchEnabled bool) error {
	args := m.Called(batchEnabled)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	// Track transaction state for tests
	m.TransactionCount++
	m.ActiveTransaction = true
	return nil
}

func (m *MockMediaDBI) CommitTransaction() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	// Track transaction state for tests
	m.ActiveTransaction = false
	return nil
}

func (m *MockMediaDBI) RollbackTransaction() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	// Track transaction state for tests
	m.ActiveTransaction = false
	return nil
}

func (m *MockMediaDBI) Exists() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockMediaDBI) UpdateLastGenerated() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetLastGenerated() (time.Time, error) {
	args := m.Called()
	if t, ok := args.Get(0).(time.Time); ok {
		if err := args.Error(1); err != nil {
			return t, fmt.Errorf("mock operation failed: %w", err)
		}
		return t, nil
	}
	if err := args.Error(1); err != nil {
		return time.Time{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return time.Time{}, nil
}

// Search methods
func (m *MockMediaDBI) SearchMediaPathExact(
	systems []systemdefs.System, query string,
) ([]database.SearchResult, error) {
	args := m.Called(systems, query)
	if results, ok := args.Get(0).([]database.SearchResult); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) SearchMediaWithFilters(
	ctx context.Context,
	filters *database.SearchFilters,
) ([]database.SearchResultWithCursor, error) {
	args := m.Called(ctx, filters)
	if results, ok := args.Get(0).([]database.SearchResultWithCursor); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) SearchMediaBySlug(
	ctx context.Context, systemID string, slug string, tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	args := m.Called(ctx, systemID, slug, tags)
	if results, ok := args.Get(0).([]database.SearchResultWithCursor); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) SearchMediaBySecondarySlug(
	ctx context.Context, systemID string, secondarySlug string, tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	args := m.Called(ctx, systemID, secondarySlug, tags)
	if results, ok := args.Get(0).([]database.SearchResultWithCursor); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) SearchMediaBySlugPrefix(
	ctx context.Context, systemID string, slugPrefix string, tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	args := m.Called(ctx, systemID, slugPrefix, tags)
	if results, ok := args.Get(0).([]database.SearchResultWithCursor); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) SearchMediaBySlugIn(
	ctx context.Context, systemID string, slugs []string, tags []database.TagFilter,
) ([]database.SearchResultWithCursor, error) {
	args := m.Called(ctx, systemID, slugs, tags)
	if results, ok := args.Get(0).([]database.SearchResultWithCursor); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) GetTitlesWithPreFilter(
	ctx context.Context, systemID string, minLength, maxLength, minWordCount, maxWordCount int,
) ([]database.MediaTitle, error) {
	args := m.Called(ctx, systemID, minLength, maxLength, minWordCount, maxWordCount)
	if results, ok := args.Get(0).([]database.MediaTitle); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) GetLaunchCommandForMedia(ctx context.Context, systemID, path string) (string, error) {
	args := m.Called(ctx, systemID, path)
	return args.String(0), args.Error(1)
}

func (m *MockMediaDBI) GetTags(
	ctx context.Context,
	systems []systemdefs.System,
) ([]database.TagInfo, error) {
	args := m.Called(ctx, systems)
	if results, ok := args.Get(0).([]database.TagInfo); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) GetAllUsedTags(ctx context.Context) ([]database.TagInfo, error) {
	args := m.Called(ctx)
	if results, ok := args.Get(0).([]database.TagInfo); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) PopulateSystemTagsCache(ctx context.Context) error {
	args := m.Called(ctx)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetSystemTagsCached(
	ctx context.Context,
	systems []systemdefs.System,
) ([]database.TagInfo, error) {
	args := m.Called(ctx, systems)
	if results, ok := args.Get(0).([]database.TagInfo); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) InvalidateSystemTagsCache(
	ctx context.Context,
	systems []systemdefs.System,
) error {
	args := m.Called(ctx, systems)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) SearchMediaPathGlob(systems []systemdefs.System, query string) ([]database.SearchResult, error) {
	args := m.Called(systems, query)
	if results, ok := args.Get(0).([]database.SearchResult); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) IndexedSystems() ([]string, error) {
	args := m.Called()
	if systems, ok := args.Get(0).([]string); ok {
		if err := args.Error(1); err != nil {
			return systems, fmt.Errorf("mock operation failed: %w", err)
		}
		return systems, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) SystemIndexed(system *systemdefs.System) bool {
	args := m.Called(system)
	return args.Bool(0)
}

func (m *MockMediaDBI) RandomGame(systems []systemdefs.System) (database.SearchResult, error) {
	args := m.Called(systems)
	if result, ok := args.Get(0).(database.SearchResult); ok {
		if err := args.Error(1); err != nil {
			return result, fmt.Errorf("mock operation failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return database.SearchResult{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.SearchResult{}, nil
}

// System CRUD methods
func (m *MockMediaDBI) FindSystem(row database.System) (database.System, error) {
	args := m.Called(row)
	if system, ok := args.Get(0).(database.System); ok {
		if err := args.Error(1); err != nil {
			return system, fmt.Errorf("mock operation failed: %w", err)
		}
		return system, nil
	}
	if err := args.Error(1); err != nil {
		return database.System{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.System{}, nil
}

func (m *MockMediaDBI) FindSystemBySystemID(systemID string) (database.System, error) {
	args := m.Called(systemID)
	if system, ok := args.Get(0).(database.System); ok {
		if err := args.Error(1); err != nil {
			return system, fmt.Errorf("mock operation failed: %w", err)
		}
		return system, nil
	}
	if err := args.Error(1); err != nil {
		return database.System{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.System{}, nil
}

func (m *MockMediaDBI) InsertSystem(row database.System) (database.System, error) {
	m.trackDatabaseOperation() // Track if called outside transaction
	args := m.Called(row)
	if system, ok := args.Get(0).(database.System); ok {
		if err := args.Error(1); err != nil {
			return system, fmt.Errorf("mock operation failed: %w", err)
		}
		return system, nil
	}
	if err := args.Error(1); err != nil {
		return database.System{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.System{}, nil
}

func (m *MockMediaDBI) FindOrInsertSystem(row database.System) (database.System, error) {
	args := m.Called(row)
	if system, ok := args.Get(0).(database.System); ok {
		if err := args.Error(1); err != nil {
			return system, fmt.Errorf("mock operation failed: %w", err)
		}
		return system, nil
	}
	if err := args.Error(1); err != nil {
		return database.System{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.System{}, nil
}

// MediaTitle CRUD methods
func (m *MockMediaDBI) FindMediaTitle(row *database.MediaTitle) (database.MediaTitle, error) {
	args := m.Called(row)
	if mediaTitle, ok := args.Get(0).(database.MediaTitle); ok {
		if err := args.Error(1); err != nil {
			return mediaTitle, fmt.Errorf("mock operation failed: %w", err)
		}
		return mediaTitle, nil
	}
	if err := args.Error(1); err != nil {
		return database.MediaTitle{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.MediaTitle{}, nil
}

func (m *MockMediaDBI) InsertMediaTitle(row *database.MediaTitle) (database.MediaTitle, error) {
	m.trackDatabaseOperation() // Track if called outside transaction
	args := m.Called(row)
	if mediaTitle, ok := args.Get(0).(database.MediaTitle); ok {
		if err := args.Error(1); err != nil {
			return mediaTitle, fmt.Errorf("mock operation failed: %w", err)
		}
		return mediaTitle, nil
	}
	if err := args.Error(1); err != nil {
		return database.MediaTitle{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.MediaTitle{}, nil
}

func (m *MockMediaDBI) FindOrInsertMediaTitle(row *database.MediaTitle) (database.MediaTitle, error) {
	args := m.Called(row)
	if mediaTitle, ok := args.Get(0).(database.MediaTitle); ok {
		if err := args.Error(1); err != nil {
			return mediaTitle, fmt.Errorf("mock operation failed: %w", err)
		}
		return mediaTitle, nil
	}
	if err := args.Error(1); err != nil {
		return database.MediaTitle{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.MediaTitle{}, nil
}

// Media CRUD methods
func (m *MockMediaDBI) FindMedia(row database.Media) (database.Media, error) {
	args := m.Called(row)
	if media, ok := args.Get(0).(database.Media); ok {
		if err := args.Error(1); err != nil {
			return media, fmt.Errorf("mock operation failed: %w", err)
		}
		return media, nil
	}
	if err := args.Error(1); err != nil {
		return database.Media{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.Media{}, nil
}

func (m *MockMediaDBI) InsertMedia(row database.Media) (database.Media, error) {
	m.trackDatabaseOperation() // Track if called outside transaction
	args := m.Called(row)
	if media, ok := args.Get(0).(database.Media); ok {
		if err := args.Error(1); err != nil {
			return media, fmt.Errorf("mock operation failed: %w", err)
		}
		return media, nil
	}
	if err := args.Error(1); err != nil {
		return database.Media{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.Media{}, nil
}

func (m *MockMediaDBI) FindOrInsertMedia(row database.Media) (database.Media, error) {
	args := m.Called(row)
	if media, ok := args.Get(0).(database.Media); ok {
		if err := args.Error(1); err != nil {
			return media, fmt.Errorf("mock operation failed: %w", err)
		}
		return media, nil
	}
	if err := args.Error(1); err != nil {
		return database.Media{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.Media{}, nil
}

// TagType CRUD methods
func (m *MockMediaDBI) FindTagType(row database.TagType) (database.TagType, error) {
	args := m.Called(row)
	if tagType, ok := args.Get(0).(database.TagType); ok {
		if err := args.Error(1); err != nil {
			return tagType, fmt.Errorf("mock operation failed: %w", err)
		}
		return tagType, nil
	}
	if err := args.Error(1); err != nil {
		return database.TagType{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.TagType{}, nil
}

func (m *MockMediaDBI) InsertTagType(row database.TagType) (database.TagType, error) {
	args := m.Called(row)
	if tagType, ok := args.Get(0).(database.TagType); ok {
		if err := args.Error(1); err != nil {
			return tagType, fmt.Errorf("mock operation failed: %w", err)
		}
		return tagType, nil
	}
	if err := args.Error(1); err != nil {
		return database.TagType{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.TagType{}, nil
}

func (m *MockMediaDBI) FindOrInsertTagType(row database.TagType) (database.TagType, error) {
	args := m.Called(row)
	if tagType, ok := args.Get(0).(database.TagType); ok {
		if err := args.Error(1); err != nil {
			return tagType, fmt.Errorf("mock operation failed: %w", err)
		}
		return tagType, nil
	}
	if err := args.Error(1); err != nil {
		return database.TagType{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.TagType{}, nil
}

// Tag CRUD methods
func (m *MockMediaDBI) FindTag(row database.Tag) (database.Tag, error) {
	args := m.Called(row)
	if tag, ok := args.Get(0).(database.Tag); ok {
		if err := args.Error(1); err != nil {
			return tag, fmt.Errorf("mock operation failed: %w", err)
		}
		return tag, nil
	}
	if err := args.Error(1); err != nil {
		return database.Tag{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.Tag{}, nil
}

func (m *MockMediaDBI) InsertTag(row database.Tag) (database.Tag, error) {
	m.trackDatabaseOperation() // Track if called outside transaction
	args := m.Called(row)
	if tag, ok := args.Get(0).(database.Tag); ok {
		if err := args.Error(1); err != nil {
			return tag, fmt.Errorf("mock operation failed: %w", err)
		}
		return tag, nil
	}
	if err := args.Error(1); err != nil {
		return database.Tag{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.Tag{}, nil
}

func (m *MockMediaDBI) FindOrInsertTag(row database.Tag) (database.Tag, error) {
	args := m.Called(row)
	if tag, ok := args.Get(0).(database.Tag); ok {
		if err := args.Error(1); err != nil {
			return tag, fmt.Errorf("mock operation failed: %w", err)
		}
		return tag, nil
	}
	if err := args.Error(1); err != nil {
		return database.Tag{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.Tag{}, nil
}

// MediaTag CRUD methods
func (m *MockMediaDBI) FindMediaTag(row database.MediaTag) (database.MediaTag, error) {
	args := m.Called(row)
	if mediaTag, ok := args.Get(0).(database.MediaTag); ok {
		if err := args.Error(1); err != nil {
			return mediaTag, fmt.Errorf("mock operation failed: %w", err)
		}
		return mediaTag, nil
	}
	if err := args.Error(1); err != nil {
		return database.MediaTag{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.MediaTag{}, nil
}

func (m *MockMediaDBI) InsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	m.trackDatabaseOperation() // Track if called outside transaction
	args := m.Called(row)
	if mediaTag, ok := args.Get(0).(database.MediaTag); ok {
		if err := args.Error(1); err != nil {
			return mediaTag, fmt.Errorf("mock operation failed: %w", err)
		}
		return mediaTag, nil
	}
	if err := args.Error(1); err != nil {
		return database.MediaTag{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.MediaTag{}, nil
}

func (m *MockMediaDBI) FindOrInsertMediaTag(row database.MediaTag) (database.MediaTag, error) {
	args := m.Called(row)
	if mediaTag, ok := args.Get(0).(database.MediaTag); ok {
		if err := args.Error(1); err != nil {
			return mediaTag, fmt.Errorf("mock operation failed: %w", err)
		}
		return mediaTag, nil
	}
	if err := args.Error(1); err != nil {
		return database.MediaTag{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.MediaTag{}, nil
}

func (m *MockMediaDBI) SetOptimizationStatus(status string) error {
	args := m.Called(status)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetOptimizationStatus() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockMediaDBI) SetOptimizationStep(step string) error {
	args := m.Called(step)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetOptimizationStep() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockMediaDBI) RunBackgroundOptimization(statusCallback func(optimizing bool)) {
	m.Called(statusCallback)
}

func (m *MockMediaDBI) WaitForBackgroundOperations() {
	m.Called()
}

func (m *MockMediaDBI) SetIndexingStatus(status string) error {
	args := m.Called(status)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetIndexingStatus() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockMediaDBI) SetLastIndexedSystem(systemID string) error {
	args := m.Called(systemID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetLastIndexedSystem() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockMediaDBI) SetIndexingSystems(systemIDs []string) error {
	args := m.Called(systemIDs)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetIndexingSystems() ([]string, error) {
	args := m.Called()
	if systems, ok := args.Get(0).([]string); ok {
		if err := args.Error(1); err != nil {
			return systems, fmt.Errorf("mock operation failed: %w", err)
		}
		return systems, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) TruncateSystems(systemIDs []string) error {
	args := m.Called(systemIDs)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// Batch insert control methods
func (m *MockMediaDBI) EnableBatchInserts(enable bool) {
	m.Called(enable)
}

func (m *MockMediaDBI) SetBatchSize(size int) {
	m.Called(size)
}

// GetMax*ID methods for resume functionality
func (m *MockMediaDBI) GetMaxSystemID() (int64, error) {
	args := m.Called()
	if id, ok := args.Get(0).(int64); ok {
		if err := args.Error(1); err != nil {
			return id, fmt.Errorf("mock operation failed: %w", err)
		}
		return id, nil
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return 0, nil
}

func (m *MockMediaDBI) GetMaxTitleID() (int64, error) {
	args := m.Called()
	if id, ok := args.Get(0).(int64); ok {
		if err := args.Error(1); err != nil {
			return id, fmt.Errorf("mock operation failed: %w", err)
		}
		return id, nil
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return 0, nil
}

func (m *MockMediaDBI) GetMaxMediaID() (int64, error) {
	args := m.Called()
	if id, ok := args.Get(0).(int64); ok {
		if err := args.Error(1); err != nil {
			return id, fmt.Errorf("mock operation failed: %w", err)
		}
		return id, nil
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return 0, nil
}

func (m *MockMediaDBI) GetMaxTagTypeID() (int64, error) {
	args := m.Called()
	if id, ok := args.Get(0).(int64); ok {
		if err := args.Error(1); err != nil {
			return id, fmt.Errorf("mock operation failed: %w", err)
		}
		return id, nil
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return 0, nil
}

func (m *MockMediaDBI) GetMaxTagID() (int64, error) {
	args := m.Called()
	if id, ok := args.Get(0).(int64); ok {
		if err := args.Error(1); err != nil {
			return id, fmt.Errorf("mock operation failed: %w", err)
		}
		return id, nil
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return 0, nil
}

func (m *MockMediaDBI) GetMaxMediaTagID() (int64, error) {
	args := m.Called()
	if id, ok := args.Get(0).(int64); ok {
		if err := args.Error(1); err != nil {
			return id, fmt.Errorf("mock operation failed: %w", err)
		}
		return id, nil
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return 0, nil
}

// GetAll* methods for populating scan state maps
func (m *MockMediaDBI) GetAllSystems() ([]database.System, error) {
	args := m.Called()
	if systems, ok := args.Get(0).([]database.System); ok {
		if err := args.Error(1); err != nil {
			return systems, fmt.Errorf("mock operation failed: %w", err)
		}
		return systems, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.System{}, nil
}

func (m *MockMediaDBI) GetAllMediaTitles() ([]database.MediaTitle, error) {
	args := m.Called()
	if titles, ok := args.Get(0).([]database.MediaTitle); ok {
		if err := args.Error(1); err != nil {
			return titles, fmt.Errorf("mock operation failed: %w", err)
		}
		return titles, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.MediaTitle{}, nil
}

func (m *MockMediaDBI) GetAllMedia() ([]database.Media, error) {
	args := m.Called()
	if media, ok := args.Get(0).([]database.Media); ok {
		if err := args.Error(1); err != nil {
			return media, fmt.Errorf("mock operation failed: %w", err)
		}
		return media, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.Media{}, nil
}

func (m *MockMediaDBI) GetAllTags() ([]database.Tag, error) {
	args := m.Called()
	if tags, ok := args.Get(0).([]database.Tag); ok {
		if err := args.Error(1); err != nil {
			return tags, fmt.Errorf("mock operation failed: %w", err)
		}
		return tags, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.Tag{}, nil
}

func (m *MockMediaDBI) GetAllTagTypes() ([]database.TagType, error) {
	args := m.Called()
	if tagTypes, ok := args.Get(0).([]database.TagType); ok {
		if err := args.Error(1); err != nil {
			return tagTypes, fmt.Errorf("mock operation failed: %w", err)
		}
		return tagTypes, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.TagType{}, nil
}

// GetTitlesWithSystems mock method for optimized JOIN query
func (m *MockMediaDBI) GetTitlesWithSystems() ([]database.TitleWithSystem, error) {
	args := m.Called()
	if titles, ok := args.Get(0).([]database.TitleWithSystem); ok {
		if err := args.Error(1); err != nil {
			return titles, fmt.Errorf("mock operation failed: %w", err)
		}
		return titles, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.TitleWithSystem{}, nil
}

// GetMediaWithFullPath mock method for optimized JOIN query
func (m *MockMediaDBI) GetMediaWithFullPath() ([]database.MediaWithFullPath, error) {
	args := m.Called()
	if media, ok := args.Get(0).([]database.MediaWithFullPath); ok {
		if err := args.Error(1); err != nil {
			return media, fmt.Errorf("mock operation failed: %w", err)
		}
		return media, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.MediaWithFullPath{}, nil
}

// GetSystemsExcluding mock method for optimized selective indexing
func (m *MockMediaDBI) GetSystemsExcluding(excludeSystemIDs []string) ([]database.System, error) {
	// Try to get mock expectations, but don't fail if none are set
	if len(m.ExpectedCalls) > 0 {
		for _, call := range m.ExpectedCalls {
			if call.Method == "GetSystemsExcluding" {
				args := m.Called(excludeSystemIDs)
				if systems, ok := args.Get(0).([]database.System); ok {
					if err := args.Error(1); err != nil {
						return systems, fmt.Errorf("mock operation failed: %w", err)
					}
					return systems, nil
				}
				if err := args.Error(1); err != nil {
					return nil, fmt.Errorf("mock operation failed: %w", err)
				}
				return []database.System{}, nil
			}
		}
	}
	// Default behavior when no expectations are set - return empty slice
	return []database.System{}, nil
}

// GetTitlesWithSystemsExcluding mock method for optimized selective indexing
func (m *MockMediaDBI) GetTitlesWithSystemsExcluding(excludeSystemIDs []string) ([]database.TitleWithSystem, error) {
	// Try to get mock expectations, but don't fail if none are set
	if len(m.ExpectedCalls) > 0 {
		for _, call := range m.ExpectedCalls {
			if call.Method == "GetTitlesWithSystemsExcluding" {
				args := m.Called(excludeSystemIDs)
				if titles, ok := args.Get(0).([]database.TitleWithSystem); ok {
					if err := args.Error(1); err != nil {
						return titles, fmt.Errorf("mock operation failed: %w", err)
					}
					return titles, nil
				}
				if err := args.Error(1); err != nil {
					return nil, fmt.Errorf("mock operation failed: %w", err)
				}
				return []database.TitleWithSystem{}, nil
			}
		}
	}
	// Default behavior when no expectations are set - return empty slice
	return []database.TitleWithSystem{}, nil
}

// GetMediaWithFullPathExcluding mock method for optimized selective indexing
func (m *MockMediaDBI) GetMediaWithFullPathExcluding(excludeSystemIDs []string) ([]database.MediaWithFullPath, error) {
	// Try to get mock expectations, but don't fail if none are set
	if len(m.ExpectedCalls) > 0 {
		for _, call := range m.ExpectedCalls {
			if call.Method == "GetMediaWithFullPathExcluding" {
				args := m.Called(excludeSystemIDs)
				if media, ok := args.Get(0).([]database.MediaWithFullPath); ok {
					if err := args.Error(1); err != nil {
						return media, fmt.Errorf("mock operation failed: %w", err)
					}
					return media, nil
				}
				if err := args.Error(1); err != nil {
					return nil, fmt.Errorf("mock operation failed: %w", err)
				}
				return []database.MediaWithFullPath{}, nil
			}
		}
	}
	// Default behavior when no expectations are set - return empty slice
	return []database.MediaWithFullPath{}, nil
}

func (m *MockMediaDBI) GetTotalMediaCount() (int, error) {
	args := m.Called()
	if count, ok := args.Get(0).(int); ok {
		if err := args.Error(1); err != nil {
			return count, fmt.Errorf("mock operation failed: %w", err)
		}
		return count, nil
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return 0, nil
}

func (m *MockMediaDBI) InvalidateCountCache() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

// Slug resolution cache methods
func (m *MockMediaDBI) GetCachedSlugResolution(
	ctx context.Context, systemID, slug string, tagFilters []database.TagFilter,
) (mediaDBID int64, strategy string, found bool) {
	args := m.Called(ctx, systemID, slug, tagFilters)
	if mediaID, ok := args.Get(0).(int64); ok {
		if strat, ok := args.Get(1).(string); ok {
			strategy = strat
		}
		hit := args.Bool(2)
		return mediaID, strategy, hit
	}
	return 0, "", false
}

func (m *MockMediaDBI) SetCachedSlugResolution(
	ctx context.Context, systemID, slug string, tagFilters []database.TagFilter, mediaDBID int64, strategy string,
) error {
	args := m.Called(ctx, systemID, slug, tagFilters, mediaDBID, strategy)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) InvalidateSlugCache(ctx context.Context) error {
	args := m.Called(ctx)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) InvalidateSlugCacheForSystems(ctx context.Context, systemIDs []string) error {
	args := m.Called(ctx, systemIDs)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetMediaByDBID(ctx context.Context, mediaDBID int64) (database.SearchResultWithCursor, error) {
	args := m.Called(ctx, mediaDBID)
	if result, ok := args.Get(0).(database.SearchResultWithCursor); ok {
		if err := args.Error(1); err != nil {
			return result, fmt.Errorf("mock operation failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return database.SearchResultWithCursor{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.SearchResultWithCursor{}, nil
}

func (m *MockMediaDBI) RandomGameWithQuery(query *database.MediaQuery) (database.SearchResult, error) {
	args := m.Called(query)
	if result, ok := args.Get(0).(database.SearchResult); ok {
		if err := args.Error(1); err != nil {
			return result, fmt.Errorf("mock operation failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return database.SearchResult{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.SearchResult{}, nil
}

// Helper functions for sqlmock setup - MOVED TO pkg/testing/sqlmock
// These functions have been moved to avoid import cycles.
// Use github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock instead.
//
// SQL Mock functions moved:
// - SetupSQLMock() -> moved to pkg/testing/sqlmock
// - SetupSQLMockWithExpectations() -> moved to pkg/testing/sqlmock
// - NewSQLMock() -> moved to pkg/testing/sqlmock

// ExpectHistoryInsert sets up expectations for history insertion
func ExpectHistoryInsert(mockDB sqlmock.Sqlmock, entry *database.HistoryEntry) {
	mockDB.ExpectExec(regexp.QuoteMeta("INSERT INTO history")).
		WithArgs(entry.Time, entry.Type, entry.TokenID, entry.TokenValue, entry.TokenData, entry.Success).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

// ExpectMappingQuery sets up expectations for mapping query
func ExpectMappingQuery(mockDB sqlmock.Sqlmock, id int64, mapping *database.Mapping) {
	rows := sqlmock.NewRows([]string{"label", "type", "match", "pattern", "override", "DBID", "added", "enabled"}).
		AddRow(mapping.Label, mapping.Type, mapping.Match, mapping.Pattern, mapping.Override,
			mapping.DBID, mapping.Added, mapping.Enabled)

	mockDB.ExpectQuery(regexp.QuoteMeta("SELECT * FROM mappings WHERE DBID = ?")).
		WithArgs(id).
		WillReturnRows(rows)
}

// ExpectMappingInsert sets up expectations for mapping insertion
func ExpectMappingInsert(mockDB sqlmock.Sqlmock, mapping *database.Mapping) {
	mockDB.ExpectExec(regexp.QuoteMeta("INSERT INTO mappings")).
		WithArgs(mapping.Label, mapping.Type, mapping.Match, mapping.Pattern, mapping.Override,
			mapping.Added, mapping.Enabled).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

// ExpectMediaSearch sets up expectations for media search queries
func ExpectMediaSearch(mockDB sqlmock.Sqlmock, results []database.SearchResult) {
	rows := sqlmock.NewRows([]string{"systemid", "name", "path"})
	for _, result := range results {
		rows.AddRow(result.SystemID, result.Name, result.Path)
	}

	mockDB.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WillReturnRows(rows)
}

// ExpectTransactionBegin sets up expectations for transaction begin
func ExpectTransactionBegin(mockDB sqlmock.Sqlmock) {
	mockDB.ExpectBegin()
}

// ExpectTransactionCommit sets up expectations for transaction commit
func ExpectTransactionCommit(mockDB sqlmock.Sqlmock) {
	mockDB.ExpectCommit()
}

// ExpectTransactionRollback sets up expectations for transaction rollback
func ExpectTransactionRollback(mockDB sqlmock.Sqlmock) {
	mockDB.ExpectRollback()
}

// Constructor functions for mock database interfaces

// NewMockUserDBI creates a new mock UserDBI interface for testing.
//
// Example usage:
//
//	func TestUserOperations(t *testing.T) {
//		userDB := helpers.NewMockUserDBI()
//		userDB.On("AddHistory", mock.MatchedBy(func(he database.HistoryEntry) bool {
//			return he.TokenID != ""
//		})).Return(nil)
//
//		// Use userDB in your test
//		err := MyFunction(userDB)
//		require.NoError(t, err)
//		userDB.AssertExpectations(t)
//	}
func NewMockUserDBI() *MockUserDBI {
	return &MockUserDBI{}
}

// NewMockMediaDBI creates a new mock MediaDBI interface for testing.
//
// Example usage:
//
//	func TestMediaOperations(t *testing.T) {
//		mediaDB := helpers.NewMockMediaDBI()
//		mediaDB.On("GetMediaByText", "Game Name").Return(fixtures.SampleMedia()[0], nil)
//
//		// Use mediaDB in your test
//		media, err := mediaDB.GetMediaByText("Game Name")
//		require.NoError(t, err)
//		assert.Equal(t, "Game Name", media.Name)
//		mediaDB.AssertExpectations(t)
//	}
func NewMockMediaDBI() *MockMediaDBI {
	mockMediaDB := &MockMediaDBI{}
	// Set default expectation for PopulateSystemTagsCache to return success
	// This is called during media indexing completion and should succeed by default
	mockMediaDB.On("PopulateSystemTagsCache", mock.Anything).Return(nil).Maybe()
	// Set default expectation for InvalidateSystemTagsCache to return success
	// This is called during media inserts and should succeed by default
	mockMediaDB.On("InvalidateSystemTagsCache", mock.Anything, mock.Anything).Return(nil).Maybe()
	// Set default expectation for GetLaunchCommandForMedia to return empty string
	// This is called during media search and should succeed by default
	mockMediaDB.On("GetLaunchCommandForMedia", mock.Anything, mock.Anything, mock.Anything).Return("", nil).Maybe()
	return mockMediaDB
}

// NewSQLMock creates a new sqlmock database and mock for raw SQL testing.
// This is an alias for SetupSQLMock for consistency with other constructor functions.
//
// Example usage:
//
//	func TestRawSQL(t *testing.T) {
//		db, mock, err := helpers.NewSQLMock()
//		require.NoError(t, err)
//		defer db.Close()
//
//		mock.ExpectQuery("SELECT (.+) FROM users").
//			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "Test"))
//
//		// Test your SQL code
//		rows, err := db.Query("SELECT id, name FROM users")
//		require.NoError(t, err)
//		defer rows.Close()
//
//		assert.NoError(t, mock.ExpectationsWereMet())
//	}
func NewSQLMock() (*sql.DB, sqlmock.Sqlmock, error) {
	return nil, nil, errors.New("NewSQLMock has been moved to pkg/testing/sqlmock package " +
		"to avoid import cycles - use testsqlmock.NewSQLMock() instead")
}

// Matcher functions for common database types

// HistoryEntryMatcher returns a testify matcher for database.HistoryEntry.
// This matcher can be used to verify that AddHistory is called with appropriate data.
//
// Example usage:
//
//	userDB.On("AddHistory", helpers.HistoryEntryMatcher()).Return(nil)
func HistoryEntryMatcher() any {
	return mock.MatchedBy(func(he *database.HistoryEntry) bool {
		if he == nil {
			return false
		}
		// Basic validation - entry has required fields
		return !he.Time.IsZero() && he.TokenID != ""
	})
}

// MappingMatcher returns a testify matcher for database.Mapping.
//
// Example usage:
//
//	userDB.On("AddMapping", helpers.MappingMatcher()).Return(nil)
func MappingMatcher() any {
	return mock.MatchedBy(func(m database.Mapping) bool {
		// Basic validation - mapping has required fields
		return m.Label != "" && m.Type != ""
	})
}

// MediaMatcher returns a testify matcher for database.Media.
//
// Example usage:
//
//	mediaDB.On("InsertMedia", helpers.MediaMatcher()).Return(fixtures.SampleMedia()[0], nil)
func MediaMatcher() any {
	return mock.MatchedBy(func(m database.Media) bool {
		// Basic validation - media has required fields
		return m.Path != ""
	})
}

// SystemMatcher returns a testify matcher for database.System.
//
// Example usage:
//
//	platform.On("LaunchMedia", helpers.MediaMatcher(), helpers.SystemMatcher()).Return(nil)
func SystemMatcher() any {
	return mock.MatchedBy(func(s database.System) bool {
		// Basic validation - system has required fields
		return s.Name != ""
	})
}

// TextMatcher returns a testify matcher for string text matching.
// Useful for matching media names, token text, etc.
//
// Example usage:
//
//	mediaDB.On("GetMediaByText", helpers.TextMatcher()).Return(fixtures.SampleMedia()[0], nil)
func TextMatcher() any {
	return mock.MatchedBy(func(text string) bool {
		// Accept any non-empty string
		return text != ""
	})
}

// SearchResultMatcher returns a testify matcher for database.SearchResult.
//
// Example usage:
//
//	mediaDB.On("SearchMedia", helpers.TextMatcher()).
//		Return([]database.SearchResult{fixtures.SampleSearchResults()[0]}, nil)
func SearchResultMatcher() any {
	return mock.MatchedBy(func(sr database.SearchResult) bool {
		// Basic validation - search result has required fields
		return sr.Name != ""
	})
}
