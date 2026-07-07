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
	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
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

func (m *MockUserDBI) GetHistory(lastID int64) ([]database.HistoryEntry, error) {
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

func (m *MockUserDBI) GetMediaUserData(systemID, path string) (database.MediaUserData, bool, error) {
	args := m.Called(systemID, path)
	data, ok := args.Get(0).(database.MediaUserData)
	if !ok {
		data = database.MediaUserData{}
	}
	found := args.Bool(1)
	if err := args.Error(2); err != nil {
		return data, found, fmt.Errorf("mock UserDBI get media user data failed: %w", err)
	}
	return data, found, nil
}

func (m *MockUserDBI) SetMediaUserFavorite(systemID, path string, favorite bool) error {
	args := m.Called(systemID, path, favorite)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI set media user favorite failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) SetMediaUserLauncherOverride(systemID, path, launcherID string) error {
	args := m.Called(systemID, path, launcherID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI set media user launcher override failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) UpsertMediaUserData(data *database.MediaUserData) error {
	args := m.Called(data)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI upsert media user data failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) DeleteMediaUserData(systemID, path string) error {
	args := m.Called(systemID, path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI delete media user data failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) ListMediaUserData() ([]database.MediaUserData, error) {
	args := m.Called()
	if data, ok := args.Get(0).([]database.MediaUserData); ok {
		if err := args.Error(1); err != nil {
			return data, fmt.Errorf("mock UserDBI list media user data failed: %w", err)
		}
		return data, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI list media user data failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) UpdateZapLinkHost(host string, isZapScript int) error {
	args := m.Called(host, isZapScript)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetZapLinkHost(host string) (found, zapScript bool, err error) {
	args := m.Called(host)
	return args.Bool(0), args.Bool(1), args.Error(2)
}

func (m *MockUserDBI) GetSupportedZapLinkHosts() ([]string, error) {
	args := m.Called()
	if hosts, ok := args.Get(0).([]string); ok {
		if err := args.Error(1); err != nil {
			return hosts, fmt.Errorf("mock operation failed: %w", err)
		}
		return hosts, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) PruneExpiredZapLinkHosts(olderThan time.Duration) (int64, error) {
	args := m.Called(olderThan)
	rowsDeleted, ok := args.Get(0).(int64)
	if !ok {
		rowsDeleted = 0
	}
	if err := args.Error(1); err != nil {
		return rowsDeleted, fmt.Errorf("mock operation failed: %w", err)
	}
	return rowsDeleted, nil
}

func (m *MockUserDBI) UpdateZapLinkCache(url, zs string) error {
	args := m.Called(url, zs)
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

func (m *MockUserDBI) GetMediaHistory(
	systemIDs []string, lastID int64, limit int,
) ([]database.MediaHistoryEntry, error) {
	args := m.Called(systemIDs, lastID, limit)
	history, ok := args.Get(0).([]database.MediaHistoryEntry)
	if !ok {
		history = []database.MediaHistoryEntry{}
	}
	if err := args.Error(1); err != nil {
		return history, fmt.Errorf("mock UserDBI get media history failed: %w", err)
	}
	return history, nil
}

func (m *MockUserDBI) GetLatestMediaHistory() (database.MediaHistoryEntry, bool, error) {
	args := m.Called()
	entry, ok := args.Get(0).(database.MediaHistoryEntry)
	if !ok {
		entry = database.MediaHistoryEntry{}
	}
	found := args.Bool(1)
	if err := args.Error(2); err != nil {
		return entry, found, fmt.Errorf("mock UserDBI get latest media history failed: %w", err)
	}
	return entry, found, nil
}

func (m *MockUserDBI) GetMediaHistoryTop(
	systemIDs []string, since *time.Time, limit int,
) ([]database.MediaHistoryTopEntry, error) {
	args := m.Called(systemIDs, since, limit)
	entries, ok := args.Get(0).([]database.MediaHistoryTopEntry)
	if !ok {
		entries = []database.MediaHistoryTopEntry{}
	}
	if err := args.Error(1); err != nil {
		return entries, fmt.Errorf("mock UserDBI get media history top failed: %w", err)
	}
	return entries, nil
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

func (m *MockUserDBI) SumMediaPlayTimeForDay(dayStart time.Time) (int64, error) {
	args := m.Called(dayStart)
	total, ok := args.Get(0).(int64)
	if !ok {
		total = 0
	}
	if err := args.Error(1); err != nil {
		return total, fmt.Errorf("mock UserDBI sum media play time for day failed: %w", err)
	}
	return total, nil
}

func (m *MockUserDBI) AddInboxMessage(msg *database.InboxMessage) (*database.InboxMessage, error) {
	args := m.Called(msg)
	if result, ok := args.Get(0).(*database.InboxMessage); ok {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock UserDBI add inbox message failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI add inbox message failed: %w", err)
	}
	return nil, nil //nolint:nilnil // mock returns nil when no message is configured
}

func (m *MockUserDBI) GetInboxMessages() ([]database.InboxMessage, error) {
	args := m.Called()
	if messages, ok := args.Get(0).([]database.InboxMessage); ok {
		if err := args.Error(1); err != nil {
			return messages, fmt.Errorf("mock UserDBI get inbox messages failed: %w", err)
		}
		return messages, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI get inbox messages failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) DeleteInboxMessage(id int64) error {
	args := m.Called(id)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI delete inbox message failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) DeleteAllInboxMessages() (int64, error) {
	args := m.Called()
	rowsDeleted, ok := args.Get(0).(int64)
	if !ok {
		rowsDeleted = 0
	}
	if err := args.Error(1); err != nil {
		return rowsDeleted, fmt.Errorf("mock UserDBI delete all inbox messages failed: %w", err)
	}
	return rowsDeleted, nil
}

func (m *MockUserDBI) CreateClient(c *database.Client) error {
	args := m.Called(c)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI create client failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) GetClientByToken(authToken string) (*database.Client, error) {
	args := m.Called(authToken)
	if result, ok := args.Get(0).(*database.Client); ok {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock UserDBI get client by token failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI get client by token failed: %w", err)
	}
	return nil, nil //nolint:nilnil // mock returns nil when no client is configured
}

func (m *MockUserDBI) ListClients() ([]database.Client, error) {
	args := m.Called()
	if clients, ok := args.Get(0).([]database.Client); ok {
		if err := args.Error(1); err != nil {
			return clients, fmt.Errorf("mock UserDBI list clients failed: %w", err)
		}
		return clients, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI list clients failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) DeleteClient(clientID string) error {
	args := m.Called(clientID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI delete client failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) UpdateClientLastSeen(authToken string, lastSeenAt int64) error {
	args := m.Called(authToken, lastSeenAt)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI update client last seen failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) CountClients() (int, error) {
	args := m.Called()
	count, ok := args.Get(0).(int)
	if !ok {
		count = 0
	}
	if err := args.Error(1); err != nil {
		return count, fmt.Errorf("mock UserDBI count clients failed: %w", err)
	}
	return count, nil
}

func (m *MockUserDBI) Backup(reason string, manual bool) (database.BackupInfo, error) {
	args := m.Called(reason, manual)
	info, ok := args.Get(0).(database.BackupInfo)
	if !ok {
		return database.BackupInfo{}, errors.New("mock UserDBI backup returned invalid backup info")
	}
	if err := args.Error(1); err != nil {
		return info, fmt.Errorf("mock UserDBI backup failed: %w", err)
	}
	return info, nil
}

func (m *MockUserDBI) EnsureRecentBackup(maxAge time.Duration) (database.BackupInfo, bool, error) {
	args := m.Called(maxAge)
	info, ok := args.Get(0).(database.BackupInfo)
	if !ok {
		return database.BackupInfo{}, false, errors.New(
			"mock UserDBI ensure recent backup returned invalid backup info",
		)
	}
	created, ok := args.Get(1).(bool)
	if !ok {
		return info, false, errors.New("mock UserDBI ensure recent backup returned invalid created flag")
	}
	if err := args.Error(2); err != nil {
		return info, created, fmt.Errorf("mock UserDBI ensure recent backup failed: %w", err)
	}
	return info, created, nil
}

func (m *MockUserDBI) ListBackups() ([]database.BackupInfo, error) {
	args := m.Called()
	if backups, ok := args.Get(0).([]database.BackupInfo); ok {
		if err := args.Error(1); err != nil {
			return backups, fmt.Errorf("mock UserDBI list backups failed: %w", err)
		}
		return backups, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock UserDBI list backups failed: %w", err)
	}
	return nil, nil
}

func (m *MockUserDBI) RestoreBackup(name string) (database.RestoreInfo, error) {
	args := m.Called(name)
	info, ok := args.Get(0).(database.RestoreInfo)
	if !ok {
		return database.RestoreInfo{}, errors.New("mock UserDBI restore backup returned invalid restore info")
	}
	if err := args.Error(1); err != nil {
		return info, fmt.Errorf("mock UserDBI restore backup failed: %w", err)
	}
	return info, nil
}

func (m *MockUserDBI) IntegrityReport() []string {
	args := m.Called()
	if report, ok := args.Get(0).([]string); ok {
		return report
	}
	return nil
}

func (m *MockUserDBI) MarkCorrupt(reason string) {
	m.Called(reason)
}

func (m *MockUserDBI) IsMarkedCorrupt() bool {
	args := m.Called()
	marked, ok := args.Get(0).(bool)
	return ok && marked
}

func (m *MockUserDBI) ClearCorruptMarker() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock UserDBI clear corrupt marker failed: %w", err)
	}
	return nil
}

func (m *MockUserDBI) NoteCorruption(err error) bool {
	args := m.Called(err)
	marked, ok := args.Get(0).(bool)
	return ok && marked
}

func (m *MockUserDBI) RecoverFromCorruption() (database.RestoreInfo, error) {
	args := m.Called()
	info, ok := args.Get(0).(database.RestoreInfo)
	if !ok {
		return database.RestoreInfo{}, errors.New("mock UserDBI recover returned invalid restore info")
	}
	if err := args.Error(1); err != nil {
		return info, fmt.Errorf("mock UserDBI recover from corruption failed: %w", err)
	}
	return info, nil
}

// MockMediaDBI is a mock implementation of the MediaDBI interface using testify/mock
type MockMediaDBI struct {
	mock.Mock
	TransactionCount      int
	OperationsOutsideTxn  int
	ActiveTransaction     bool
	Optimizing            bool
	BrowseCacheRebuilding bool
}

// trackDatabaseOperation tracks whether operations happen inside or outside transactions
func (m *MockMediaDBI) trackDatabaseOperation() {
	if !m.ActiveTransaction {
		m.OperationsOutsideTxn++
	}
}

func (m *MockMediaDBI) hasExpectedCall(method string) bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == method {
			return true
		}
	}
	return false
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

func (m *MockMediaDBI) CommitTransactionWithOptions(options database.TransactionOptions) error {
	args := m.Called(options)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	// Track transaction state for tests
	m.ActiveTransaction = false
	return nil
}

func (m *MockMediaDBI) FlushBatchInserters() error {
	// Infrastructure call during indexing; treat as a no-op unless a test sets
	// an explicit expectation, so callers don't have to wire it up everywhere.
	if !m.hasExpectedCall("FlushBatchInserters") {
		return nil
	}
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
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

func (m *MockMediaDBI) StageScannedMedia(media *database.ScanStagedMedia) error {
	// Per-file staging call during indexing; a no-op unless a test wires an
	// explicit expectation, mirroring FlushBatchInserters.
	if !m.hasExpectedCall("StageScannedMedia") {
		return nil
	}
	args := m.Called(media)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) ReconcileStagedSystem(
	ctx context.Context, systemID string, opts database.ScanReconcileOpts,
) (database.ScanReconcileStats, error) {
	// Default mirrors a system with no staged files and no DB rows: reconcile
	// is a no-op and the scanner marks the system complete without a commit.
	if !m.hasExpectedCall("ReconcileStagedSystem") {
		return database.ScanReconcileStats{}, nil
	}
	args := m.Called(ctx, systemID, opts)
	stats, ok := args.Get(0).(database.ScanReconcileStats)
	if !ok {
		stats = database.ScanReconcileStats{}
	}
	if err := args.Error(1); err != nil {
		return stats, fmt.Errorf("mock operation failed: %w", err)
	}
	return stats, nil
}

func (m *MockMediaDBI) ClearScanStage() error {
	if !m.hasExpectedCall("ClearScanStage") {
		return nil
	}
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) SeedCanonicalTagDefinitions(ctx context.Context) error {
	if !m.hasExpectedCall("SeedCanonicalTagDefinitions") {
		return nil
	}
	args := m.Called(ctx)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) Exists() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockMediaDBI) WALCheckpoint() error {
	if !m.hasExpectation("WALCheckpoint") {
		return nil
	}
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) QuickCheck() (bool, error) {
	if !m.hasExpectation("QuickCheck") {
		return true, nil
	}
	args := m.Called()
	if err := args.Error(1); err != nil {
		return args.Bool(0), fmt.Errorf("mock operation failed: %w", err)
	}
	return args.Bool(0), nil
}

func (m *MockMediaDBI) IntegrityReport() []string {
	if !m.hasExpectation("IntegrityReport") {
		return []string{"ok"}
	}
	args := m.Called()
	if report, ok := args.Get(0).([]string); ok {
		return report
	}
	return nil
}

func (m *MockMediaDBI) MarkCorrupt(reason string) {
	if !m.hasExpectation("MarkCorrupt") {
		return
	}
	m.Called(reason)
}

func (m *MockMediaDBI) IsMarkedCorrupt() bool {
	if !m.hasExpectation("IsMarkedCorrupt") {
		return false
	}
	args := m.Called()
	return args.Bool(0)
}

func (m *MockMediaDBI) ClearCorruptMarker() error {
	if !m.hasExpectation("ClearCorruptMarker") {
		return nil
	}
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) NoteCorruption(err error) bool {
	if !m.hasExpectation("NoteCorruption") {
		return false
	}
	args := m.Called(err)
	return args.Bool(0)
}

func (m *MockMediaDBI) Recreate(keepBackup bool) error {
	if !m.hasExpectation("Recreate") {
		return nil
	}
	args := m.Called(keepBackup)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
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
	ctx context.Context, systems []systemdefs.System, query string,
) ([]database.SearchResult, error) {
	args := m.Called(ctx, systems, query)
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
	ctx context.Context, systemID string, slug string, tags []zapscript.TagFilter,
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
	ctx context.Context, systemID string, secondarySlug string, tags []zapscript.TagFilter,
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
	ctx context.Context, systemID string, slugPrefix string, tags []zapscript.TagFilter,
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
	ctx context.Context, systemID string, slugs []string, tags []zapscript.TagFilter,
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

func (m *MockMediaDBI) SearchMediaByProperty(
	ctx context.Context,
	systemID string,
	property string,
	value string,
) ([]database.SearchResult, error) {
	args := m.Called(ctx, systemID, property, value)
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

func (m *MockMediaDBI) HasMediaPropertyForPath(ctx context.Context, systemID, path, property string) (bool, error) {
	args := m.Called(ctx, systemID, path, property)
	if err := args.Error(1); err != nil {
		return false, fmt.Errorf("mock operation failed: %w", err)
	}
	return args.Bool(0), nil
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

// AnalyzeApproximate mock method
func (m *MockMediaDBI) AnalyzeApproximate() error {
	args := m.Called()
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

func (m *MockMediaDBI) RandomGame(ctx context.Context, systems []systemdefs.System) (database.SearchResult, error) {
	args := m.Called(ctx, systems)
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
//
//nolint:gocritic // matches MediaDBI value-parameter signature
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

//nolint:gocritic // matches MediaDBI value-parameter signature
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

//nolint:gocritic // matches MediaDBI value-parameter signature
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

func (m *MockMediaDBI) DeleteMediaTag(mediaDBID, tagDBID int64) error {
	m.trackDatabaseOperation() // Track if called outside transaction
	args := m.Called(mediaDBID, tagDBID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
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

// IsOptimizing backs the fields directly (no m.Called()) so the many callers of
// the media status query don't each need to stub it. Tests that care about a
// full RunBackgroundOptimization pass set Optimizing directly; tests exercising
// the browse-cache self-heal use BeginBrowseCacheRebuild/EndBrowseCacheRebuild.
func (m *MockMediaDBI) IsOptimizing() bool {
	return m.Optimizing || m.BrowseCacheRebuilding
}

func (m *MockMediaDBI) BeginBrowseCacheRebuild() {
	m.BrowseCacheRebuilding = true
}

func (m *MockMediaDBI) EndBrowseCacheRebuild() {
	m.BrowseCacheRebuilding = false
}

func (m *MockMediaDBI) RunBackgroundOptimization(statusCallback func(optimizing bool), pauser *syncutil.Pauser) {
	m.Called(statusCallback, pauser)
}

func (m *MockMediaDBI) TemporaryRepairJobsPending(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	if err := args.Error(1); err != nil {
		return args.Bool(0), fmt.Errorf("mock operation failed: %w", err)
	}
	return args.Bool(0), nil
}

func (m *MockMediaDBI) WaitForBackgroundOperations() {
	m.Called()
}

func (m *MockMediaDBI) TrackBackgroundOperation() {
	m.Called()
}

func (m *MockMediaDBI) SetIndexingConnBoost(active bool) {
	m.Called(active)
}

func (m *MockMediaDBI) BackgroundOperationDone() {
	m.Called()
}

func (*MockMediaDBI) SetIndexingCacheSize(_ bool) {
	// No-op for mock — cache_size is a SQLite-specific optimization
}

func (m *MockMediaDBI) DropSecondaryIndexes() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock drop indexes failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) CreateSecondaryIndexes() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock create indexes failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) SetIndexingStatus(status string) error {
	args := m.Called(status)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetIndexingStatus() (string, error) {
	if !m.hasExpectation("GetIndexingStatus") {
		return "", nil
	}
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockMediaDBI) GetIndexResumeAttempts() (int, error) {
	args := m.Called()
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return args.Int(0), nil
}

func (m *MockMediaDBI) IncrementIndexResumeAttempts() (int, error) {
	args := m.Called()
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock operation failed: %w", err)
	}
	return args.Int(0), nil
}

func (m *MockMediaDBI) ResetIndexResumeAttempts() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) hasExpectation(method string) bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == method {
			return true
		}
	}
	return false
}

func (m *MockMediaDBI) SetScrapingStatus(status string) error {
	if !m.hasExpectation("SetScrapingStatus") {
		return nil
	}
	args := m.Called(status)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetScrapingStatus() (string, error) {
	if !m.hasExpectation("GetScrapingStatus") {
		return "", nil
	}
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockMediaDBI) SetScrapingOperation(operation database.ScrapingOperation) error {
	if !m.hasExpectation("SetScrapingOperation") {
		return nil
	}
	args := m.Called(operation)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) GetScrapingOperation() (database.ScrapingOperation, bool, error) {
	if !m.hasExpectation("GetScrapingOperation") {
		return database.ScrapingOperation{}, false, nil
	}
	args := m.Called()
	operation, ok := args.Get(0).(database.ScrapingOperation)
	if !ok {
		operation = database.ScrapingOperation{}
	}
	return operation, args.Bool(1), args.Error(2)
}

func (m *MockMediaDBI) ClearScrapingOperation() error {
	if !m.hasExpectation("ClearScrapingOperation") {
		return nil
	}
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
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

func (m *MockMediaDBI) CleanMediaOrphans(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
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

// Batch insert control methods
func (m *MockMediaDBI) EnableBatchInserts(enable bool) {
	m.Called(enable)
}

func (m *MockMediaDBI) SetBatchSize(size int) {
	m.Called(size)
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

func (m *MockMediaDBI) GetExistingMediaUserData(ctx context.Context) ([]database.MediaUserData, error) {
	args := m.Called(ctx)
	if data, ok := args.Get(0).([]database.MediaUserData); ok {
		if err := args.Error(1); err != nil {
			return data, fmt.Errorf("mock operation failed: %w", err)
		}
		return data, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.MediaUserData{}, nil
}

// GetTitlesBySystemID mock method for per-system lazy loading during resume
func (m *MockMediaDBI) GetTitlesBySystemID(systemID string) ([]database.TitleWithSystem, error) {
	// Try to get mock expectations, but don't fail if none are set
	if len(m.ExpectedCalls) > 0 {
		for _, call := range m.ExpectedCalls {
			if call.Method == "GetTitlesBySystemID" {
				args := m.Called(systemID)
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

// GetMediaBySystemID mock method for per-system lazy loading during resume
func (m *MockMediaDBI) GetMediaBySystemID(systemID string) ([]database.MediaWithFullPath, error) {
	// Try to get mock expectations, but don't fail if none are set
	if len(m.ExpectedCalls) > 0 {
		for _, call := range m.ExpectedCalls {
			if call.Method == "GetMediaBySystemID" {
				args := m.Called(systemID)
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

func (m *MockMediaDBI) GetMissingMediaCount() (int, error) {
	if !m.hasExpectedCall("GetMissingMediaCount") {
		return 0, nil
	}
	args := m.Called()
	count, ok := args.Get(0).(int)
	if !ok {
		count = 0
	}
	if err := args.Error(1); err != nil {
		return count, fmt.Errorf("mock operation failed: %w", err)
	}
	return count, nil
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

func (m *MockMediaDBI) HasAnyMedia() (bool, error) {
	args := m.Called()
	if has, ok := args.Get(0).(bool); ok {
		if err := args.Error(1); err != nil {
			return has, fmt.Errorf("mock operation failed: %w", err)
		}
		return has, nil
	}
	if err := args.Error(1); err != nil {
		return false, fmt.Errorf("mock operation failed: %w", err)
	}
	return false, nil
}

func (m *MockMediaDBI) GetScrapedMediaCount(ctx context.Context, scraperID string) (int, error) {
	args := m.Called(ctx, scraperID)
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

func (m *MockMediaDBI) GetTotalScrapedMediaCount(ctx context.Context) (int, error) {
	args := m.Called(ctx)
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

func (m *MockMediaDBI) RebuildSlugSearchCache() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock RebuildSlugSearchCache: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) RebuildTagCache() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock RebuildTagCache: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) PersistTagCache() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock PersistTagCache: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) LoadCachedTagCache() (bool, error) {
	args := m.Called()
	loaded, ok := args.Get(0).(bool)
	if !ok {
		loaded = false
	}
	if err := args.Error(1); err != nil {
		return false, fmt.Errorf("mock LoadCachedTagCache: %w", err)
	}
	return loaded, nil
}

func (m *MockMediaDBI) PersistSlugSearchCache() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock PersistSlugSearchCache: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) LoadCachedSlugSearchCache() (bool, error) {
	args := m.Called()
	loaded, ok := args.Get(0).(bool)
	if !ok {
		loaded = false
	}
	if err := args.Error(1); err != nil {
		return false, fmt.Errorf("mock LoadCachedSlugSearchCache: %w", err)
	}
	return loaded, nil
}

func (m *MockMediaDBI) IndexGeneration() (int64, error) {
	args := m.Called()
	v, ok := args.Get(0).(int64)
	if !ok {
		v = 0
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock IndexGeneration: %w", err)
	}
	return v, nil
}

func (m *MockMediaDBI) BumpIndexGeneration() (int64, error) {
	args := m.Called()
	v, ok := args.Get(0).(int64)
	if !ok {
		v = 0
	}
	if err := args.Error(1); err != nil {
		return 0, fmt.Errorf("mock BumpIndexGeneration: %w", err)
	}
	return v, nil
}

func (m *MockMediaDBI) PopulateSystemTagsCacheForSystems(ctx context.Context, systems []systemdefs.System) error {
	args := m.Called(ctx, systems)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock PopulateSystemTagsCacheForSystems: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) RefreshSlugSearchCacheForSystems(ctx context.Context, systemIDs []string) error {
	args := m.Called(ctx, systemIDs)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock RefreshSlugSearchCacheForSystems: %w", err)
	}
	return nil
}

// Slug resolution cache methods
func (m *MockMediaDBI) GetCachedSlugResolution(
	ctx context.Context, systemID, slug string, tagFilters []zapscript.TagFilter,
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
	ctx context.Context, systemID, slug string, tagFilters []zapscript.TagFilter, mediaDBID int64, strategy string,
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

func (m *MockMediaDBI) GetZapScriptTagsBySystemAndPath(
	ctx context.Context, systemID, path string,
) ([]database.TagInfo, error) {
	args := m.Called(ctx, systemID, path)
	if result, ok := args.Get(0).([]database.TagInfo); ok {
		if err := args.Error(1); err != nil {
			return result, fmt.Errorf("mock operation failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return nil, nil
}

func (m *MockMediaDBI) RandomGameWithQuery(
	ctx context.Context, query *database.MediaQuery,
) (database.SearchResult, error) {
	args := m.Called(ctx, query)
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
	m := &MockUserDBI{}
	// A reindex always lists media user data to re-materialize the media.db
	// projection. Default to an empty list so tests exercising NewNamesIndex
	// don't each need to stub it; tests can override with their own expectation.
	m.On("ListMediaUserData").Return([]database.MediaUserData{}, nil).Maybe()
	return m
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
	// Planner-statistics refresh before cache builds; succeeds by default
	mockMediaDB.On("AnalyzeApproximate").Return(nil).Maybe()
	// Per-system browse cache refresh after each system commit during indexing
	mockMediaDB.On("PopulateBrowseCacheForSystems", mock.Anything, mock.Anything).Return(nil).Maybe()
	// Connection pool widening around an index run
	mockMediaDB.On("SetIndexingConnBoost", mock.Anything).Maybe()
	// Set default expectation for InvalidateSystemTagsCache to return success
	// This is called during media inserts and should succeed by default
	mockMediaDB.On("InvalidateSystemTagsCache", mock.Anything, mock.Anything).Return(nil).Maybe()
	// Set default expectation for GetLaunchCommandForMedia to return empty string
	// This is called during media search and should succeed by default
	mockMediaDB.On("GetLaunchCommandForMedia", mock.Anything, mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockMediaDB.On("RebuildSlugSearchCache").Return(nil).Maybe()
	mockMediaDB.On("RebuildTagCache").Return(nil).Maybe()
	mockMediaDB.On("PersistTagCache").Return(nil).Maybe()
	mockMediaDB.On("LoadCachedTagCache").Return(false, nil).Maybe()
	mockMediaDB.On("PersistSlugSearchCache").Return(nil).Maybe()
	mockMediaDB.On("LoadCachedSlugSearchCache").Return(false, nil).Maybe()
	mockMediaDB.On("IndexGeneration").Return(int64(0), nil).Maybe()
	mockMediaDB.On("BumpIndexGeneration").Return(int64(1), nil).Maybe()
	mockMediaDB.On("GetIndexResumeAttempts").Return(0, nil).Maybe()
	mockMediaDB.On("IncrementIndexResumeAttempts").Return(1, nil).Maybe()
	mockMediaDB.On("ResetIndexResumeAttempts").Return(nil).Maybe()
	mockMediaDB.On("GetDBPath").Return("/tmp/mock-media.db").Maybe()
	mockMediaDB.On("HasAnyMedia").Return(false, nil).Maybe()
	mockMediaDB.On("DropSecondaryIndexes").Return(nil).Maybe()
	mockMediaDB.On("BulkSetMediaMissing", mock.Anything).Return(nil).Maybe()
	mockMediaDB.On("ResetMissingFlags", mock.Anything).Return(nil).Maybe()
	mockMediaDB.On("UpdateMediaTitle", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockMediaDB.On("UpdateMediaTitleName", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockMediaDB.On("DeleteMediaTags", mock.Anything).Return(nil).Maybe()
	// Disambiguation refresh runs per system at the end of indexing.
	mockMediaDB.On("RecomputeSystemDisambiguation", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockMediaDB.On("RecomputeTitleDisambiguation", mock.Anything, mock.Anything).Return(nil).Maybe()
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

// Browse methods

func (m *MockMediaDBI) BrowseDirectories(
	ctx context.Context, opts database.BrowseDirectoriesOptions,
) ([]database.BrowseDirectoryResult, error) {
	args := m.Called(ctx, opts)
	if results, ok := args.Get(0).([]database.BrowseDirectoryResult); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.BrowseDirectoryResult{}, nil
}

func (m *MockMediaDBI) BrowseDirCount(
	ctx context.Context, opts database.BrowseDirCountOptions,
) (int, error) {
	args := m.Called(ctx, opts)
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

func (m *MockMediaDBI) BrowseFiles(
	ctx context.Context, opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	args := m.Called(ctx, opts)
	if results, ok := args.Get(0).([]database.SearchResultWithCursor); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.SearchResultWithCursor{}, nil
}

func (m *MockMediaDBI) BrowseFileCount(
	ctx context.Context, opts database.BrowseFileCountOptions,
) (int, error) {
	args := m.Called(ctx, opts)
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

func (m *MockMediaDBI) BrowseIndex(
	ctx context.Context, opts database.BrowseIndexOptions,
) (database.BrowseIndexResult, error) {
	args := m.Called(ctx, opts)
	if result, ok := args.Get(0).(database.BrowseIndexResult); ok {
		if err := args.Error(1); err != nil {
			return result, fmt.Errorf("mock operation failed: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return database.BrowseIndexResult{}, fmt.Errorf("mock operation failed: %w", err)
	}
	return database.BrowseIndexResult{}, nil
}

func (m *MockMediaDBI) BrowseVirtualSchemes(
	ctx context.Context, opts database.BrowseVirtualSchemesOptions,
) ([]database.BrowseVirtualScheme, error) {
	args := m.Called(ctx, opts)
	if results, ok := args.Get(0).([]database.BrowseVirtualScheme); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return []database.BrowseVirtualScheme{}, nil
}

func (m *MockMediaDBI) BrowseRouteCounts(
	ctx context.Context, opts database.BrowseRouteCountsOptions,
) (map[string]database.BrowseRouteCount, error) {
	args := m.Called(ctx, opts)
	if results, ok := args.Get(0).(map[string]database.BrowseRouteCount); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return map[string]database.BrowseRouteCount{}, nil
}

func (m *MockMediaDBI) BrowseSystemRootCandidates(
	ctx context.Context, opts database.BrowseSystemRootCandidatesOptions,
) (database.BrowseSystemRootCandidates, bool, error) {
	args := m.Called(ctx, opts)
	result, ok := args.Get(0).(database.BrowseSystemRootCandidates)
	if !ok {
		result = database.BrowseSystemRootCandidates{}
	}
	cacheReady, ok := args.Get(1).(bool)
	if !ok {
		cacheReady = false
	}
	if err := args.Error(2); err != nil {
		return result, cacheReady, fmt.Errorf("mock operation failed: %w", err)
	}
	return result, cacheReady, nil
}

func (m *MockMediaDBI) BrowseRootCounts(
	ctx context.Context, rootDirs []string,
) (map[string]*int, error) {
	args := m.Called(ctx, rootDirs)
	if results, ok := args.Get(0).(map[string]*int); ok {
		if err := args.Error(1); err != nil {
			return results, fmt.Errorf("mock operation failed: %w", err)
		}
		return results, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock operation failed: %w", err)
	}
	return map[string]*int{}, nil
}

func (m *MockMediaDBI) PopulateBrowseCache(
	ctx context.Context,
) error {
	args := m.Called(ctx)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) PopulateBrowseCacheForSystems(ctx context.Context, systemIDs []string) error {
	args := m.Called(ctx, systemIDs)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) BrowseCacheNeedsRebuild(
	ctx context.Context,
) (bool, error) {
	args := m.Called(ctx)
	if err := args.Error(1); err != nil {
		return false, fmt.Errorf("mock operation failed: %w", err)
	}
	return args.Bool(0), nil
}

// --- Scraper support methods ---

func (m *MockMediaDBI) FindMediaBySystemAndPath(
	ctx context.Context, systemDBID int64, path string,
) (*database.Media, error) {
	args := m.Called(ctx, systemDBID, path)
	if result, ok := args.Get(0).(*database.Media); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) FindMediaBySystemAndPaths(
	ctx context.Context, systemDBID int64, paths []string,
) (map[string]database.Media, error) {
	args := m.Called(ctx, systemDBID, paths)
	if result, ok := args.Get(0).(map[string]database.Media); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) FindMediaIDsByPaths(
	ctx context.Context, paths []string,
) ([]database.MediaPathID, error) {
	if !m.hasExpectedCall("FindMediaIDsByPaths") {
		return nil, nil
	}
	args := m.Called(ctx, paths)
	if result, ok := args.Get(0).([]database.MediaPathID); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) FindSingleContainerLaunchMedia(
	ctx context.Context, systemDBID int64, containerPath string,
) (*database.Media, error) {
	if !m.hasExpectedCall("FindSingleContainerLaunchMedia") {
		return nil, nil //nolint:nilnil // default mock behavior for tests that do not exercise aliasing
	}
	args := m.Called(ctx, systemDBID, containerPath)
	if result, ok := args.Get(0).(*database.Media); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) ResolveSingletonContainerAliases(
	ctx context.Context, systemDBID int64, candidates []database.SingletonAliasCandidate,
) ([]database.SingletonContainerAlias, error) {
	if !m.hasExpectedCall("ResolveSingletonContainerAliases") {
		return nil, nil //nolint:nilnil // default mock behavior for tests that do not exercise batch aliasing
	}
	args := m.Called(ctx, systemDBID, candidates)
	if result, ok := args.Get(0).([]database.SingletonContainerAlias); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) FindMediaBySystemAndPathFold(
	ctx context.Context, systemDBID int64, path string,
) (*database.Media, error) {
	args := m.Called(ctx, systemDBID, path)
	if result, ok := args.Get(0).(*database.Media); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) FindMediaBySystemAndPathSuffix(
	ctx context.Context, systemDBID int64, filename string,
) ([]database.Media, error) {
	args := m.Called(ctx, systemDBID, filename)
	if result, ok := args.Get(0).([]database.Media); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) MediaHasTag(ctx context.Context, mediaDBID int64, tagValue string) (bool, error) {
	args := m.Called(ctx, mediaDBID, tagValue)
	return args.Bool(0), args.Error(1)
}

func (m *MockMediaDBI) GetScrapedMediaIDs(
	ctx context.Context, scraperID string, systemDBID int64,
) (map[int64]struct{}, error) {
	args := m.Called(ctx, scraperID, systemDBID)
	if result, ok := args.Get(0).(map[int64]struct{}); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetScrapeRunMediaIDs(
	ctx context.Context, scraperID, runID string, systemDBID int64,
) (map[int64]struct{}, error) {
	args := m.Called(ctx, scraperID, runID, systemDBID)
	if result, ok := args.Get(0).(map[int64]struct{}); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) ClearScrapeRunMarkers(ctx context.Context, scraperID, runID string) error {
	if !m.hasExpectation("ClearScrapeRunMarkers") {
		return nil
	}
	args := m.Called(ctx, scraperID, runID)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) UpsertMediaTags(ctx context.Context, mediaDBID int64, tags []database.TagInfo) error {
	args := m.Called(ctx, mediaDBID, tags)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) UpsertMediaTitleTags(ctx context.Context, mediaTitleDBID int64, tags []database.TagInfo) error {
	args := m.Called(ctx, mediaTitleDBID, tags)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) RecomputeTitleDisambiguation(ctx context.Context, titleDBIDs []int64) error {
	args := m.Called(ctx, titleDBIDs)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) RecomputeSystemDisambiguation(ctx context.Context, systemDBIDs []int64) error {
	args := m.Called(ctx, systemDBIDs)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) UpsertMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64, props []database.MediaProperty,
) error {
	args := m.Called(ctx, mediaTitleDBID, props)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) UpsertMediaProperties(
	ctx context.Context, mediaDBID int64, props []database.MediaProperty,
) error {
	args := m.Called(ctx, mediaDBID, props)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) ApplyScrapeResult(
	ctx context.Context, mediaDBID, mediaTitleDBID int64, write *database.ScrapeWrite,
) error {
	args := m.Called(ctx, mediaDBID, mediaTitleDBID, write)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock operation failed: %w", err)
	}
	return nil
}

func (m *MockMediaDBI) FindMediaTitlesWithoutSentinel(
	ctx context.Context, systemDBID int64, sentinelTag string,
) ([]database.MediaTitle, error) {
	args := m.Called(ctx, systemDBID, sentinelTag)
	if result, ok := args.Get(0).([]database.MediaTitle); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) FindMediaTitleByDBID(ctx context.Context, dbid int64) (*database.MediaTitle, error) {
	args := m.Called(ctx, dbid)
	if result, ok := args.Get(0).(*database.MediaTitle); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) FindMediaTitleBySystemAndSlug(
	ctx context.Context, systemDBID int64, slug string,
) (*database.MediaTitle, error) {
	args := m.Called(ctx, systemDBID, slug)
	if result, ok := args.Get(0).(*database.MediaTitle); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTitleProperties(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.MediaProperty, error) {
	args := m.Called(ctx, mediaTitleDBID)
	if result, ok := args.Get(0).([]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTitlePropertiesByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	args := m.Called(ctx, mediaTitleDBIDs)
	if result, ok := args.Get(0).(map[int64][]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTitlePropertyMetadata(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.MediaProperty, error) {
	if !m.hasExpectedCall("GetMediaTitlePropertyMetadata") && m.hasExpectedCall("GetMediaTitleProperties") {
		return m.GetMediaTitleProperties(ctx, mediaTitleDBID)
	}
	args := m.Called(ctx, mediaTitleDBID)
	if result, ok := args.Get(0).([]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTitlePropertyMetadataByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	if !m.hasExpectedCall("GetMediaTitlePropertyMetadataByMediaTitleDBIDs") &&
		m.hasExpectedCall("GetMediaTitlePropertiesByMediaTitleDBIDs") {
		return m.GetMediaTitlePropertiesByMediaTitleDBIDs(ctx, mediaTitleDBIDs)
	}
	args := m.Called(ctx, mediaTitleDBIDs)
	if result, ok := args.Get(0).(map[int64][]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaProperties(ctx context.Context, mediaDBID int64) ([]database.MediaProperty, error) {
	args := m.Called(ctx, mediaDBID)
	if result, ok := args.Get(0).([]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaPropertiesByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	args := m.Called(ctx, mediaDBIDs)
	if result, ok := args.Get(0).(map[int64][]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaPropertyMetadata(
	ctx context.Context, mediaDBID int64,
) ([]database.MediaProperty, error) {
	if !m.hasExpectedCall("GetMediaPropertyMetadata") && m.hasExpectedCall("GetMediaProperties") {
		return m.GetMediaProperties(ctx, mediaDBID)
	}
	args := m.Called(ctx, mediaDBID)
	if result, ok := args.Get(0).([]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaPropertyMetadataByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.MediaProperty, error) {
	if !m.hasExpectedCall("GetMediaPropertyMetadataByMediaDBIDs") &&
		m.hasExpectedCall("GetMediaPropertiesByMediaDBIDs") {
		return m.GetMediaPropertiesByMediaDBIDs(ctx, mediaDBIDs)
	}
	args := m.Called(ctx, mediaDBIDs)
	if result, ok := args.Get(0).(map[int64][]database.MediaProperty); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) DeleteMediaTitleProperty(ctx context.Context, mediaTitleDBID, typeTagDBID int64) error {
	args := m.Called(ctx, mediaTitleDBID, typeTagDBID)
	return args.Error(0) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) DeleteMediaProperty(ctx context.Context, mediaDBID, typeTagDBID int64) error {
	args := m.Called(ctx, mediaDBID, typeTagDBID)
	return args.Error(0) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) UpsertMediaBlob(ctx context.Context, contentType string, data []byte) (int64, error) {
	args := m.Called(ctx, contentType, data)
	if id, ok := args.Get(0).(int64); ok {
		return id, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return 0, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaBlob(ctx context.Context, blobDBID int64) (*database.MediaBlob, error) {
	args := m.Called(ctx, blobDBID)
	if result, ok := args.Get(0).(*database.MediaBlob); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaBlobDataCapped(
	ctx context.Context, blobDBID int64, maxBytes int64,
) (data []byte, contentType string, err error) {
	args := m.Called(ctx, blobDBID, maxBytes)
	if result, ok := args.Get(0).([]byte); ok {
		data = result
	}
	if result, ok := args.Get(1).(string); ok {
		contentType = result
	}
	return data, contentType, args.Error(2) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) PruneOrphanedBlobs(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	if n, ok := args.Get(0).(int64); ok {
		return n, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return 0, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaWithTitleAndSystem(
	ctx context.Context, mediaDBID int64,
) (*database.MediaFullRow, error) {
	args := m.Called(ctx, mediaDBID)
	if result, ok := args.Get(0).(*database.MediaFullRow); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaWithTitleAndSystemByIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64]database.MediaFullRow, error) {
	args := m.Called(ctx, mediaDBIDs)
	if result, ok := args.Get(0).(map[int64]database.MediaFullRow); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTagsByMediaDBID(
	ctx context.Context, mediaDBID int64,
) ([]database.TagInfo, error) {
	args := m.Called(ctx, mediaDBID)
	if result, ok := args.Get(0).([]database.TagInfo); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTagsByMediaDBIDs(
	ctx context.Context, mediaDBIDs []int64,
) (map[int64][]database.TagInfo, error) {
	args := m.Called(ctx, mediaDBIDs)
	if result, ok := args.Get(0).(map[int64][]database.TagInfo); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTitleTagsByMediaTitleDBID(
	ctx context.Context, mediaTitleDBID int64,
) ([]database.TagInfo, error) {
	args := m.Called(ctx, mediaTitleDBID)
	if result, ok := args.Get(0).([]database.TagInfo); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

func (m *MockMediaDBI) GetMediaTitleTagsByMediaTitleDBIDs(
	ctx context.Context, mediaTitleDBIDs []int64,
) (map[int64][]database.TagInfo, error) {
	args := m.Called(ctx, mediaTitleDBIDs)
	if result, ok := args.Get(0).(map[int64][]database.TagInfo); ok {
		return result, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
	}
	return nil, args.Error(1) //nolint:wrapcheck // mock passes testify errors through unwrapped by design
}

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
