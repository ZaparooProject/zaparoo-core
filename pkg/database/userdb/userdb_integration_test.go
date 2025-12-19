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

package userdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTempUserDB(t *testing.T) (db *UserDB, cleanup func()) {
	// Create temp directory that the mock platform will use
	tempDir, err := os.MkdirTemp("", "zaparoo-test-userdb-*")
	require.NoError(t, err)

	// Create a mock platform that returns our temp directory for Settings().DataDir
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: tempDir,
	})

	// Use OpenUserDB with context and the mock platform
	ctx := context.Background()
	db, err = OpenUserDB(ctx, mockPlatform)
	require.NoError(t, err)

	cleanup = func() {
		if db != nil {
			_ = db.Close()
		}
		_ = os.RemoveAll(tempDir)
	}

	return db, cleanup
}

func TestUserDB_OpenClose_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	// Database should be functional - test with a simple operation
	// Try truncating (which should work if DB is open)
	err := userDB.Truncate()
	require.NoError(t, err)

	// Should be able to close cleanly
	err = userDB.Close()
	require.NoError(t, err)

	// After close, operations should fail with database closed error
	err = userDB.Truncate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database is closed")
}

func TestUserDB_GetDBPath_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	dbPath := userDB.GetDBPath()

	// Path should not be empty
	assert.NotEmpty(t, dbPath)

	// Path should end with the correct database filename
	assert.Contains(t, dbPath, "user.db")

	// Database file should exist
	_, err := os.Stat(dbPath)
	assert.NoError(t, err, "Database file should exist at the returned path")
}

func TestMappingCRUD_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	// Create a test mapping
	mapping := database.Mapping{
		Added:    time.Now().Unix(),
		Label:    "Integration Test Mapping",
		Enabled:  true,
		Type:     "id", // Valid mapping type
		Match:    "exact",
		Pattern:  "test-pattern-123",
		Override: "test-override",
	}

	// Test AddMapping
	err := userDB.AddMapping(&mapping)
	require.NoError(t, err, "Should be able to add mapping")

	// Test GetAllMappings to find our mapping
	allMappings, err := userDB.GetAllMappings()
	require.NoError(t, err, "Should be able to get all mappings")
	assert.Len(t, allMappings, 1, "Should have exactly 1 mapping")

	createdMapping := allMappings[0]
	assert.Equal(t, mapping.Label, createdMapping.Label)
	assert.Equal(t, mapping.Type, createdMapping.Type)
	assert.Equal(t, mapping.Pattern, createdMapping.Pattern)
	assert.Positive(t, createdMapping.DBID, "Should have assigned a DBID")

	// Test GetMapping by ID
	retrievedMapping, err := userDB.GetMapping(createdMapping.DBID)
	require.NoError(t, err, "Should be able to get mapping by ID")
	assert.Equal(t, createdMapping.DBID, retrievedMapping.DBID)
	assert.Equal(t, mapping.Label, retrievedMapping.Label)

	// Test UpdateMapping
	updatedMapping := retrievedMapping
	updatedMapping.Label = "Updated Integration Test Mapping"
	updatedMapping.Enabled = false

	err = userDB.UpdateMapping(updatedMapping.DBID, &updatedMapping)
	require.NoError(t, err, "Should be able to update mapping")

	// Verify update worked
	verifyMapping, err := userDB.GetMapping(updatedMapping.DBID)
	require.NoError(t, err, "Should be able to get updated mapping")
	assert.Equal(t, updatedMapping.Label, verifyMapping.Label)
	assert.False(t, verifyMapping.Enabled, "Should be disabled after update")

	// Test DeleteMapping
	err = userDB.DeleteMapping(updatedMapping.DBID)
	require.NoError(t, err, "Should be able to delete mapping")

	// Confirm deletion - GetMapping should fail
	_, err = userDB.GetMapping(updatedMapping.DBID)
	require.Error(t, err, "Getting deleted mapping should fail")

	// Confirm deletion - GetAllMappings should return empty
	finalMappings, err := userDB.GetAllMappings()
	require.NoError(t, err, "Should be able to get all mappings after deletion")
	assert.Empty(t, finalMappings, "Should have 0 mappings after deletion")
}

func TestUserDB_CleanupHistory_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	// Add test history entries with different timestamps
	now := time.Now()
	oldEntry := database.HistoryEntry{
		Time:       now.AddDate(0, 0, -60), // 60 days old
		Type:       "nfc",
		TokenID:    "old-token",
		TokenValue: "old-value",
		TokenData:  "old-data",
		Success:    true,
	}
	recentEntry := database.HistoryEntry{
		Time:       now.AddDate(0, 0, -10), // 10 days old
		Type:       "nfc",
		TokenID:    "recent-token",
		TokenValue: "recent-value",
		TokenData:  "recent-data",
		Success:    true,
	}
	veryRecentEntry := database.HistoryEntry{
		Time:       now,
		Type:       "nfc",
		TokenID:    "very-recent-token",
		TokenValue: "very-recent-value",
		TokenData:  "very-recent-data",
		Success:    true,
	}

	// Add all entries
	err := userDB.AddHistory(&oldEntry)
	require.NoError(t, err)
	err = userDB.AddHistory(&recentEntry)
	require.NoError(t, err)
	err = userDB.AddHistory(&veryRecentEntry)
	require.NoError(t, err)

	// Verify all 3 entries exist
	allHistory, err := userDB.GetHistory(0)
	require.NoError(t, err)
	assert.Len(t, allHistory, 3, "Should have 3 history entries before cleanup")

	// Run cleanup with 30-day retention
	rowsDeleted, err := userDB.CleanupHistory(30)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rowsDeleted, "Should delete 1 old entry (60 days old)")

	// Verify only 2 entries remain
	remainingHistory, err := userDB.GetHistory(0)
	require.NoError(t, err)
	assert.Len(t, remainingHistory, 2, "Should have 2 history entries after cleanup")

	// Verify the old entry was deleted (remaining entries should be recent and very recent)
	for _, entry := range remainingHistory {
		assert.NotEqual(t, "old-token", entry.TokenID, "Old entry should have been deleted")
	}

	// Run cleanup again - should delete nothing
	rowsDeleted, err = userDB.CleanupHistory(30)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rowsDeleted, "Should delete 0 entries on second run")

	// Verify still 2 entries
	finalHistory, err := userDB.GetHistory(0)
	require.NoError(t, err)
	assert.Len(t, finalHistory, 2, "Should still have 2 history entries")
}

func TestUserDB_CleanupHistory_ZeroRetention_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	// Add old test entry
	oldEntry := database.HistoryEntry{
		Time:       time.Now().AddDate(0, 0, -90),
		Type:       "nfc",
		TokenID:    "test-token",
		TokenValue: "test-value",
		TokenData:  "test-data",
		Success:    true,
	}
	err := userDB.AddHistory(&oldEntry)
	require.NoError(t, err)

	// Run cleanup with 0 retention (unlimited) - should delete old entries
	rowsDeleted, err := userDB.CleanupHistory(0)
	require.NoError(t, err)

	// With 0 days retention, it should delete everything older than "now"
	// Since our entry is from the past, it should be deleted
	assert.Positive(t, rowsDeleted, "Should delete entry with 0 retention")
}

func TestInboxCRUD_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	// Test empty inbox
	messages, err := userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Empty(t, messages, "Inbox should be empty initially")

	// Test AddInboxMessage
	now := time.Now()
	msg1 := database.InboxMessage{
		Title:     "Test Notification",
		Body:      "This is the body",
		Severity:  1,
		CreatedAt: now,
	}
	inserted1, err := userDB.AddInboxMessage(&msg1)
	require.NoError(t, err, "Should be able to add inbox message")
	require.NotNil(t, inserted1)
	assert.Positive(t, inserted1.DBID, "Returned message should have DBID")
	assert.Equal(t, "Test Notification", inserted1.Title)
	assert.Equal(t, 1, inserted1.Severity)
	assert.False(t, inserted1.CreatedAt.IsZero(), "Returned message should have CreatedAt")

	msg2 := database.InboxMessage{
		Title:     "Second Notification",
		Body:      "",
		Severity:  2,
		CreatedAt: now.Add(time.Second), // Different timestamp for ordering test
	}
	inserted2, err := userDB.AddInboxMessage(&msg2)
	require.NoError(t, err, "Should be able to add second inbox message")
	require.NotNil(t, inserted2)

	// Test GetInboxMessages
	messages, err = userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Len(t, messages, 2, "Should have 2 inbox messages")

	// Messages should be ordered by CreatedAt DESC (newest first)
	assert.Equal(t, "Second Notification", messages[0].Title, "Newest message should be first")
	assert.Equal(t, "Test Notification", messages[1].Title, "Older message should be second")
	assert.Equal(t, "This is the body", messages[1].Body)
	assert.Empty(t, messages[0].Body, "Empty body should remain empty")

	// Verify DBID was assigned
	assert.Positive(t, messages[0].DBID, "Should have assigned a DBID")
	assert.Positive(t, messages[1].DBID, "Should have assigned a DBID")

	// Verify CreatedAt was set
	assert.False(t, messages[0].CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, messages[1].CreatedAt.IsZero(), "CreatedAt should be set")

	// Test DeleteInboxMessage
	err = userDB.DeleteInboxMessage(messages[1].DBID)
	require.NoError(t, err, "Should be able to delete inbox message")

	messages, err = userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Len(t, messages, 1, "Should have 1 inbox message after deletion")
	assert.Equal(t, "Second Notification", messages[0].Title)

	// Test DeleteInboxMessage on non-existent ID (should error)
	err = userDB.DeleteInboxMessage(99999)
	require.Error(t, err, "Deleting non-existent message should error")
	assert.Contains(t, err.Error(), "inbox message not found")

	// Test DeleteAllInboxMessages
	// Add another message first
	msg3 := database.InboxMessage{Title: "Third", CreatedAt: time.Now()}
	_, err = userDB.AddInboxMessage(&msg3)
	require.NoError(t, err)

	messages, err = userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Len(t, messages, 2, "Should have 2 messages before clear")

	rowsDeleted, err := userDB.DeleteAllInboxMessages()
	require.NoError(t, err, "Should be able to clear inbox")
	assert.Equal(t, int64(2), rowsDeleted, "Should report 2 rows deleted")

	messages, err = userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Empty(t, messages, "Inbox should be empty after clear")
}

func TestInbox_Limit_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	// Add more than 100 messages to test the LIMIT
	now := time.Now()
	for i := range 110 {
		msg := database.InboxMessage{
			Title:     "Message",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		_, err := userDB.AddInboxMessage(&msg)
		require.NoError(t, err)
	}

	// GetInboxMessages should return at most 100
	messages, err := userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Len(t, messages, 100, "Should return at most 100 messages due to LIMIT")
}

func TestInbox_CategoryUpsert_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	userDB, cleanup := setupTempUserDB(t)
	defer cleanup()

	now := time.Now()

	// Add a message with category
	msg1 := database.InboxMessage{
		Title:     "First Version",
		Body:      "Original body",
		Severity:  0,
		Category:  "update-check",
		ProfileID: 0,
		CreatedAt: now,
	}
	inserted1, err := userDB.AddInboxMessage(&msg1)
	require.NoError(t, err)
	originalDBID := inserted1.DBID

	// Add another message with same category - should update
	msg2 := database.InboxMessage{
		Title:     "Updated Version",
		Body:      "New body",
		Severity:  1,
		Category:  "update-check",
		ProfileID: 0,
		CreatedAt: now.Add(time.Second),
	}
	inserted2, err := userDB.AddInboxMessage(&msg2)
	require.NoError(t, err)

	// Should have same DBID (upsert updated existing)
	assert.Equal(t, originalDBID, inserted2.DBID, "UPSERT should return same DBID")

	// Verify only one message exists
	messages, err := userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Len(t, messages, 1, "Should have only 1 message after upsert")
	assert.Equal(t, "Updated Version", messages[0].Title)
	assert.Equal(t, "New body", messages[0].Body)
	assert.Equal(t, 1, messages[0].Severity)

	// Add message without category - should always create new
	msg3 := database.InboxMessage{
		Title:     "No Category",
		CreatedAt: now.Add(2 * time.Second),
	}
	_, err = userDB.AddInboxMessage(&msg3)
	require.NoError(t, err)

	messages, err = userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Len(t, messages, 2, "Should have 2 messages (1 with category, 1 without)")

	// Different profile with same category - should create separate
	msg4 := database.InboxMessage{
		Title:     "Profile Specific",
		Category:  "update-check",
		ProfileID: 1,
		CreatedAt: now.Add(3 * time.Second),
	}
	_, err = userDB.AddInboxMessage(&msg4)
	require.NoError(t, err)

	messages, err = userDB.GetInboxMessages()
	require.NoError(t, err)
	assert.Len(t, messages, 3, "Should have 3 messages (category is per-profile)")
}
