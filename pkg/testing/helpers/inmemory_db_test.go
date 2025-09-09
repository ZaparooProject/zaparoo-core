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

package helpers

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestHistoryEntry creates a standard history entry for testing
func createTestHistoryEntry() *database.HistoryEntry {
	return &database.HistoryEntry{
		Time:       time.Now(),
		Type:       "test",
		TokenID:    "token-123",
		TokenValue: "test-value",
		TokenData:  "",
		Success:    true,
	}
}

// createTestSystem creates a standard system for testing
func createTestSystem() database.System {
	return database.System{
		SystemID: "nes",
		Name:     "Nintendo Entertainment System",
	}
}

//nolint:paralleltest // Cannot use t.Parallel() due to goose global state race condition
func TestNewInMemoryUserDB(t *testing.T) {
	// Note: t.Parallel() removed due to goose global state race condition
	userDB, cleanup := NewInMemoryUserDB(t)
	defer cleanup()

	// Test basic operations work with real database
	entry := createTestHistoryEntry()

	err := userDB.AddHistory(entry)
	require.NoError(t, err)

	history, err := userDB.GetHistory(0)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Equal(t, "token-123", history[0].TokenID)
}

//nolint:paralleltest // Cannot use t.Parallel() due to goose global state race condition
func TestNewInMemoryMediaDB(t *testing.T) {
	// Note: t.Parallel() removed due to goose global state race condition
	mediaDB, cleanup := NewInMemoryMediaDB(t)
	defer cleanup()

	// Test basic operations work with real database
	system := createTestSystem()

	result, err := mediaDB.InsertSystem(system)
	require.NoError(t, err)
	assert.NotZero(t, result.DBID)
	assert.Equal(t, "nes", result.SystemID)
	assert.Equal(t, "Nintendo Entertainment System", result.Name)
}
