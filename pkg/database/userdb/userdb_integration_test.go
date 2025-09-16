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

func setupTempUserDB(t *testing.T) (userDB *UserDB, cleanup func()) {
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
	userDB, err = OpenUserDB(ctx, mockPlatform)
	require.NoError(t, err)

	cleanup = func() {
		if userDB != nil {
			_ = userDB.Close()
		}
		_ = os.RemoveAll(tempDir)
	}

	return userDB, cleanup
}

func TestUserDB_OpenClose_Integration(t *testing.T) {
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
