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

package userdb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealTimestamps_MediaHistory(t *testing.T) {
	t.Parallel()

	db := NewInMemoryUserDB(t)
	bootUUID := uuid.New().String()

	// Simulate MiSTer boot without RTC - records created with epoch time
	epochTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	entry1 := &database.MediaHistoryEntry{
		ID:             uuid.New().String(),
		StartTime:      epochTime.Add(30 * time.Second), // 30 seconds after "boot"
		EndTime:        nil,                             // Still playing
		SystemID:       "nes",
		SystemName:     "Nintendo Entertainment System",
		MediaPath:      "/roms/game.nes",
		MediaName:      "Test Game",
		LauncherID:     "retroarch",
		PlayTime:       120, // 2 minutes of play
		BootUUID:       bootUUID,
		MonotonicStart: 30, // 30 seconds since boot
		DurationSec:    120,
		WallDuration:   0,
		TimeSkewFlag:   false,
		ClockReliable:  false,
		ClockSource:    helpers.ClockSourceEpoch,
		CreatedAt:      epochTime.Add(30 * time.Second),
		UpdatedAt:      epochTime.Add(150 * time.Second),
	}

	dbid1, err := db.AddMediaHistory(entry1)
	require.NoError(t, err)

	// Add second entry from same boot session
	entry2 := &database.MediaHistoryEntry{
		ID:             uuid.New().String(),
		StartTime:      epochTime.Add(200 * time.Second), // 200 seconds after "boot"
		EndTime:        nil,
		SystemID:       "snes",
		SystemName:     "Super Nintendo",
		MediaPath:      "/roms/game2.sfc",
		MediaName:      "Another Game",
		LauncherID:     "retroarch",
		PlayTime:       60,
		BootUUID:       bootUUID,
		MonotonicStart: 200, // 200 seconds since boot
		DurationSec:    60,
		WallDuration:   0,
		TimeSkewFlag:   false,
		ClockReliable:  false,
		ClockSource:    helpers.ClockSourceEpoch,
		CreatedAt:      epochTime.Add(200 * time.Second),
		UpdatedAt:      epochTime.Add(260 * time.Second),
	}

	dbid2, err := db.AddMediaHistory(entry2)
	require.NoError(t, err)

	// Simulate NTP sync - calculate true boot time
	// Let's say system has been running for 5 minutes (300 seconds)
	// and current time is 2025-01-22 12:05:00
	ntpSyncTime := time.Date(2025, 1, 22, 12, 5, 0, 0, time.UTC)
	systemUptime := 300 * time.Second
	trueBootTime := ntpSyncTime.Add(-systemUptime)

	// Heal timestamps
	rowsAffected, err := db.HealTimestamps(bootUUID, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rowsAffected, "should heal 2 MediaHistory records")

	// Verify entry1 was healed correctly
	history, err := db.GetMediaHistory(0, 100)
	require.NoError(t, err)
	require.Len(t, history, 2)

	// Find entry1 by DBID
	var healed1, healed2 database.MediaHistoryEntry
	for _, h := range history {
		switch h.DBID {
		case dbid1:
			healed1 = h
		case dbid2:
			healed2 = h
		}
	}

	// Entry1: Started 30 seconds after boot
	expectedStart1 := trueBootTime.Add(30 * time.Second)
	assert.Equal(t, expectedStart1.Unix(), healed1.StartTime.Unix(),
		"entry1 StartTime should be TrueBootTime + MonotonicStart")

	// Entry1: Should still be playing (EndTime nil)
	assert.Nil(t, healed1.EndTime, "entry1 EndTime should remain nil")

	// Entry1: ClockSource should be healed
	assert.Equal(t, helpers.ClockSourceHealed, healed1.ClockSource)
	assert.True(t, healed1.ClockReliable)

	// Entry2: Started 200 seconds after boot
	expectedStart2 := trueBootTime.Add(200 * time.Second)
	assert.Equal(t, expectedStart2.Unix(), healed2.StartTime.Unix(),
		"entry2 StartTime should be TrueBootTime + MonotonicStart")
}

func TestHealTimestamps_History(t *testing.T) {
	t.Parallel()

	db := NewInMemoryUserDB(t)
	bootUUID := uuid.New().String()

	// Create history entries with unreliable clock
	epochTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	entry1 := &database.HistoryEntry{
		ID:             uuid.New().String(),
		Time:           epochTime.Add(10 * time.Second),
		Type:           "nfc",
		TokenID:        "test-token-1",
		TokenValue:     "test-value-1",
		TokenData:      "test-data-1",
		Success:        true,
		ClockReliable:  false,
		BootUUID:       bootUUID,
		MonotonicStart: 10, // 10 seconds since boot
		CreatedAt:      epochTime.Add(10 * time.Second),
	}

	err := db.AddHistory(entry1)
	require.NoError(t, err)

	entry2 := &database.HistoryEntry{
		ID:             uuid.New().String(),
		Time:           epochTime.Add(50 * time.Second),
		Type:           "barcode",
		TokenID:        "test-token-2",
		TokenValue:     "test-value-2",
		TokenData:      "test-data-2",
		Success:        true,
		ClockReliable:  false,
		BootUUID:       bootUUID,
		MonotonicStart: 50, // 50 seconds since boot
		CreatedAt:      epochTime.Add(50 * time.Second),
	}

	err = db.AddHistory(entry2)
	require.NoError(t, err)

	// Heal timestamps
	ntpSyncTime := time.Date(2025, 1, 22, 12, 10, 0, 0, time.UTC)
	systemUptime := 100 * time.Second
	trueBootTime := ntpSyncTime.Add(-systemUptime)

	rowsAffected, err := db.HealTimestamps(bootUUID, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rowsAffected, "should heal 2 History records")

	// Verify healing
	history, err := db.GetHistory(0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(history), 2)

	// Find our entries
	var healed1, healed2 *database.HistoryEntry
	for i := range history {
		switch history[i].ID {
		case entry1.ID:
			healed1 = &history[i]
		case entry2.ID:
			healed2 = &history[i]
		}
	}

	require.NotNil(t, healed1, "should find entry1 in history")
	require.NotNil(t, healed2, "should find entry2 in history")

	// Verify timestamps were healed correctly
	expectedTime1 := trueBootTime.Add(10 * time.Second)
	assert.Equal(t, expectedTime1.Unix(), healed1.Time.Unix(),
		"entry1 Time should be TrueBootTime + MonotonicStart")

	expectedTime2 := trueBootTime.Add(50 * time.Second)
	assert.Equal(t, expectedTime2.Unix(), healed2.Time.Unix(),
		"entry2 Time should be TrueBootTime + MonotonicStart")

	// Verify both are now marked as reliable
	assert.True(t, healed1.ClockReliable)
	assert.True(t, healed2.ClockReliable)
}

func TestHealTimestamps_BootUUID_Isolation(t *testing.T) {
	t.Parallel()

	db := NewInMemoryUserDB(t)

	bootUUID1 := uuid.New().String()
	bootUUID2 := uuid.New().String()

	epochTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create entry for boot session 1
	entry1 := &database.MediaHistoryEntry{
		ID:             uuid.New().String(),
		StartTime:      epochTime.Add(30 * time.Second),
		SystemID:       "nes",
		SystemName:     "Nintendo Entertainment System",
		MediaPath:      "/roms/game1.nes",
		MediaName:      "Game 1",
		LauncherID:     "retroarch",
		PlayTime:       60,
		BootUUID:       bootUUID1,
		MonotonicStart: 30,
		DurationSec:    60,
		ClockReliable:  false,
		ClockSource:    helpers.ClockSourceEpoch,
		CreatedAt:      epochTime.Add(30 * time.Second),
		UpdatedAt:      epochTime.Add(90 * time.Second),
	}

	_, err := db.AddMediaHistory(entry1)
	require.NoError(t, err)

	// Create entry for boot session 2
	entry2 := &database.MediaHistoryEntry{
		ID:             uuid.New().String(),
		StartTime:      epochTime.Add(40 * time.Second),
		SystemID:       "snes",
		SystemName:     "Super Nintendo",
		MediaPath:      "/roms/game2.sfc",
		MediaName:      "Game 2",
		LauncherID:     "retroarch",
		PlayTime:       120,
		BootUUID:       bootUUID2,
		MonotonicStart: 40,
		DurationSec:    120,
		ClockReliable:  false,
		ClockSource:    helpers.ClockSourceEpoch,
		CreatedAt:      epochTime.Add(40 * time.Second),
		UpdatedAt:      epochTime.Add(160 * time.Second),
	}

	_, err = db.AddMediaHistory(entry2)
	require.NoError(t, err)

	// Heal only boot session 1
	ntpSyncTime := time.Date(2025, 1, 22, 12, 0, 0, 0, time.UTC)
	systemUptime := 100 * time.Second
	trueBootTime := ntpSyncTime.Add(-systemUptime)

	rowsAffected, err := db.HealTimestamps(bootUUID1, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rowsAffected, "should only heal boot session 1")

	// Verify boot session 1 was healed
	history, err := db.GetMediaHistory(0, 100)
	require.NoError(t, err)

	var healed1, unhealed2 *database.MediaHistoryEntry
	for i := range history {
		switch history[i].ID {
		case entry1.ID:
			healed1 = &history[i]
		case entry2.ID:
			unhealed2 = &history[i]
		}
	}

	require.NotNil(t, healed1)
	require.NotNil(t, unhealed2)

	// Session 1 should be healed
	assert.True(t, healed1.ClockReliable)
	assert.Equal(t, helpers.ClockSourceHealed, healed1.ClockSource)
	expectedStart1 := trueBootTime.Add(30 * time.Second)
	assert.Equal(t, expectedStart1.Unix(), healed1.StartTime.Unix())

	// Session 2 should NOT be healed
	assert.False(t, unhealed2.ClockReliable)
	assert.Equal(t, helpers.ClockSourceEpoch, unhealed2.ClockSource)
	assert.Equal(t, epochTime.Add(40*time.Second).Unix(), unhealed2.StartTime.Unix(),
		"boot session 2 should remain unchanged")
}

func TestHealTimestamps_Idempotent(t *testing.T) {
	t.Parallel()

	db := NewInMemoryUserDB(t)
	bootUUID := uuid.New().String()

	epochTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := &database.MediaHistoryEntry{
		ID:             uuid.New().String(),
		StartTime:      epochTime.Add(30 * time.Second),
		SystemID:       "nes",
		SystemName:     "Nintendo Entertainment System",
		MediaPath:      "/roms/game.nes",
		MediaName:      "Test Game",
		LauncherID:     "retroarch",
		PlayTime:       60,
		BootUUID:       bootUUID,
		MonotonicStart: 30,
		DurationSec:    60,
		ClockReliable:  false,
		ClockSource:    helpers.ClockSourceEpoch,
		CreatedAt:      epochTime.Add(30 * time.Second),
		UpdatedAt:      epochTime.Add(90 * time.Second),
	}

	dbid, err := db.AddMediaHistory(entry)
	require.NoError(t, err)

	// Heal once
	ntpSyncTime := time.Date(2025, 1, 22, 12, 0, 0, 0, time.UTC)
	systemUptime := 100 * time.Second
	trueBootTime := ntpSyncTime.Add(-systemUptime)

	rows1, err := db.HealTimestamps(bootUUID, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows1, "first heal should affect 1 row")

	// Get healed timestamp
	history1, err := db.GetMediaHistory(0, 100)
	require.NoError(t, err)

	var healed1 database.MediaHistoryEntry
	for _, h := range history1 {
		if h.DBID == dbid {
			healed1 = h
			break
		}
	}

	// Heal again (should be idempotent)
	rows2, err := db.HealTimestamps(bootUUID, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rows2, "second heal should affect 0 rows (already healed)")

	// Verify timestamp didn't change
	history2, err := db.GetMediaHistory(0, 100)
	require.NoError(t, err)

	var healed2 database.MediaHistoryEntry
	for _, h := range history2 {
		if h.DBID == dbid {
			healed2 = h
			break
		}
	}

	assert.Equal(t, healed1.StartTime.Unix(), healed2.StartTime.Unix(),
		"timestamp should not change on second heal")
	assert.Equal(t, healed1.ClockSource, healed2.ClockSource)
}

func TestHealTimestamps_WithEndTime(t *testing.T) {
	t.Parallel()

	db := NewInMemoryUserDB(t)
	bootUUID := uuid.New().String()

	epochTime := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := &database.MediaHistoryEntry{
		ID:             uuid.New().String(),
		StartTime:      epochTime.Add(30 * time.Second),
		SystemID:       "nes",
		SystemName:     "Nintendo Entertainment System",
		MediaPath:      "/roms/game.nes",
		MediaName:      "Test Game",
		LauncherID:     "retroarch",
		PlayTime:       0,
		BootUUID:       bootUUID,
		MonotonicStart: 30,
		DurationSec:    0,
		WallDuration:   0,
		ClockReliable:  false,
		ClockSource:    helpers.ClockSourceEpoch,
		CreatedAt:      epochTime.Add(30 * time.Second),
		UpdatedAt:      epochTime.Add(30 * time.Second),
	}

	dbid, err := db.AddMediaHistory(entry)
	require.NoError(t, err)

	// Close the media history entry (as happens in real flow)
	endTime := epochTime.Add(150 * time.Second)
	playTime := 120 // 2 minutes
	err = db.CloseMediaHistory(dbid, endTime, playTime)
	require.NoError(t, err)

	// Heal timestamps
	ntpSyncTime := time.Date(2025, 1, 22, 12, 0, 0, 0, time.UTC)
	systemUptime := 200 * time.Second
	trueBootTime := ntpSyncTime.Add(-systemUptime)

	rowsAffected, err := db.HealTimestamps(bootUUID, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rowsAffected)

	// Verify both StartTime and EndTime were healed
	history, err := db.GetMediaHistory(0, 100)
	require.NoError(t, err)

	var healed database.MediaHistoryEntry
	for _, h := range history {
		if h.DBID == dbid {
			healed = h
			break
		}
	}

	// Verify StartTime was healed using the formula: TrueBootTime + MonotonicStart
	expectedStart := trueBootTime.Add(30 * time.Second)
	assert.Equal(t, expectedStart.Unix(), healed.StartTime.Unix())

	// Verify EndTime was healed using the formula: TrueBootTime + (MonotonicStart + DurationSec)
	expectedEnd := trueBootTime.Add((30 + 120) * time.Second)
	require.NotNil(t, healed.EndTime)
	assert.Equal(t, expectedEnd.Unix(), healed.EndTime.Unix())
}

func TestHealTimestamps_NoRecordsToHeal(t *testing.T) {
	t.Parallel()

	db := NewInMemoryUserDB(t)
	bootUUID := uuid.New().String()

	// Don't add any records
	ntpSyncTime := time.Date(2025, 1, 22, 12, 0, 0, 0, time.UTC)
	systemUptime := 100 * time.Second
	trueBootTime := ntpSyncTime.Add(-systemUptime)

	rowsAffected, err := db.HealTimestamps(bootUUID, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rowsAffected, "should heal 0 rows when no records exist")
}

func TestHealTimestamps_OnlyReliableRecords(t *testing.T) {
	t.Parallel()

	db := NewInMemoryUserDB(t)
	bootUUID := uuid.New().String()

	// Create entry that was already reliable
	reliableTime := time.Date(2025, 1, 22, 11, 0, 0, 0, time.UTC)
	entry := &database.MediaHistoryEntry{
		ID:             uuid.New().String(),
		StartTime:      reliableTime,
		SystemID:       "nes",
		SystemName:     "Nintendo Entertainment System",
		MediaPath:      "/roms/game.nes",
		MediaName:      "Test Game",
		LauncherID:     "retroarch",
		PlayTime:       60,
		BootUUID:       bootUUID,
		MonotonicStart: 30,
		DurationSec:    60,
		ClockReliable:  true, // Already reliable
		ClockSource:    helpers.ClockSourceSystem,
		CreatedAt:      reliableTime,
		UpdatedAt:      reliableTime.Add(60 * time.Second),
	}

	_, err := db.AddMediaHistory(entry)
	require.NoError(t, err)

	// Try to heal
	ntpSyncTime := time.Date(2025, 1, 22, 12, 0, 0, 0, time.UTC)
	systemUptime := 100 * time.Second
	trueBootTime := ntpSyncTime.Add(-systemUptime)

	rowsAffected, err := db.HealTimestamps(bootUUID, trueBootTime)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rowsAffected,
		"should not heal records that are already reliable")
}

// NewInMemoryUserDB creates an in-memory SQLite database for testing
func NewInMemoryUserDB(t *testing.T) *UserDB {
	t.Helper()

	// Open in-memory SQLite database
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Create UserDB wrapper
	ctx := context.Background()
	db := &UserDB{
		sql: sqlDB,
		ctx: ctx,
		pl:  nil, // Not needed for tests
	}

	// Run migrations to create schema
	err = db.Allocate()
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}
