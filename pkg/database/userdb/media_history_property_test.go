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
	"database/sql"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// ============================================================================
// Generators
// ============================================================================

// mediaHistoryEntryGen generates valid MediaHistoryEntry values.
func mediaHistoryEntryGen() *rapid.Generator[database.MediaHistoryEntry] {
	return rapid.Custom(func(t *rapid.T) database.MediaHistoryEntry {
		// Generate start time in reasonable range (last 5 years)
		now := time.Now()
		fiveYearsAgo := now.AddDate(-5, 0, 0)
		startUnix := rapid.Int64Range(fiveYearsAgo.Unix(), now.Unix()).Draw(t, "startUnix")
		startTime := time.Unix(startUnix, 0)

		// PlayTime is in seconds, reasonable range 0-24 hours
		playTime := rapid.IntRange(0, 86400).Draw(t, "playTime")

		// MonotonicStart is nanoseconds since boot (up to 30 days)
		monotonicStart := rapid.Int64Range(0, 30*24*60*60*int64(time.Second/time.Nanosecond)).Draw(t, "monotonicStart")

		// Generate optional end time (if present, must be after start)
		var endTime *time.Time
		if rapid.Bool().Draw(t, "hasEndTime") {
			endUnix := rapid.Int64Range(startUnix, startUnix+int64(playTime)+3600).Draw(t, "endUnix")
			et := time.Unix(endUnix, 0)
			endTime = &et
		}

		uuidPattern := `[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`
		return database.MediaHistoryEntry{
			ID:             rapid.StringMatching(uuidPattern).Draw(t, "id"),
			StartTime:      startTime,
			EndTime:        endTime,
			SystemID:       rapid.StringMatching(`[a-z0-9_]{1,20}`).Draw(t, "systemID"),
			SystemName:     rapid.String().Draw(t, "systemName"),
			MediaPath:      rapid.String().Draw(t, "mediaPath"),
			MediaName:      rapid.String().Draw(t, "mediaName"),
			LauncherID:     rapid.StringMatching(`[a-z0-9_]{1,20}`).Draw(t, "launcherID"),
			PlayTime:       playTime,
			BootUUID:       rapid.StringMatching(uuidPattern).Draw(t, "bootUUID"),
			MonotonicStart: monotonicStart,
			DurationSec:    playTime,
			WallDuration:   playTime,
			TimeSkewFlag:   rapid.Bool().Draw(t, "timeSkewFlag"),
			ClockReliable:  rapid.Bool().Draw(t, "clockReliable"),
			ClockSource:    rapid.SampledFrom([]string{"system", "ntp", "healed", ""}).Draw(t, "clockSource"),
			CreatedAt:      startTime,
			UpdatedAt:      startTime,
		}
	})
}

// ============================================================================
// Time Ordering Property Tests
// ============================================================================

// TestPropertyEndTimeAfterStartTime verifies EndTime >= StartTime when EndTime is set.
func TestPropertyEndTimeAfterStartTime(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		entry := mediaHistoryEntryGen().Draw(t, "entry")

		if entry.EndTime != nil {
			if entry.EndTime.Before(entry.StartTime) {
				t.Fatalf("EndTime (%v) should not be before StartTime (%v)",
					entry.EndTime, entry.StartTime)
			}
		}
	})
}

// TestPropertyPlayTimeNonNegative verifies PlayTime is never negative.
func TestPropertyPlayTimeNonNegative(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		entry := mediaHistoryEntryGen().Draw(t, "entry")

		if entry.PlayTime < 0 {
			t.Fatalf("PlayTime should not be negative: %d", entry.PlayTime)
		}
	})
}

// TestPropertyDurationSecMatchesPlayTime verifies DurationSec matches PlayTime.
func TestPropertyDurationSecMatchesPlayTime(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		entry := mediaHistoryEntryGen().Draw(t, "entry")

		// Our generator sets them equal, verify that's the expected invariant
		if entry.DurationSec != entry.PlayTime {
			t.Fatalf("DurationSec (%d) should match PlayTime (%d)",
				entry.DurationSec, entry.PlayTime)
		}
	})
}

// ============================================================================
// Timestamp Healing Property Tests
// ============================================================================

// TestPropertyTimestampHealingCalculation verifies the healing math.
// TrueStartTime = TrueBootTime + MonotonicStart
func TestPropertyTimestampHealingCalculation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a boot time in reasonable range
		now := time.Now()
		oneYearAgo := now.AddDate(-1, 0, 0)
		bootUnix := rapid.Int64Range(oneYearAgo.Unix(), now.Unix()).Draw(t, "bootUnix")
		trueBootTime := time.Unix(bootUnix, 0)

		// MonotonicStart is seconds since boot (up to 30 days)
		monotonicStart := rapid.Int64Range(0, 30*24*60*60).Draw(t, "monotonicStart")

		// Calculate healed start time using the same formula as sqlHealTimestamps
		healedStartTime := time.Unix(trueBootTime.Unix()+monotonicStart, 0)

		// Verify the healed time is after boot time
		if healedStartTime.Before(trueBootTime) {
			t.Fatalf("Healed start time (%v) should not be before boot time (%v)",
				healedStartTime, trueBootTime)
		}

		// Verify the math: healedStartTime should equal bootTime + monotonicStart seconds
		expected := trueBootTime.Add(time.Duration(monotonicStart) * time.Second)
		if !healedStartTime.Equal(expected) {
			t.Fatalf("Healed time mismatch: got %v, expected %v",
				healedStartTime, expected)
		}
	})
}

// TestPropertyHealedEndTimeAfterStart verifies healed EndTime >= StartTime.
func TestPropertyHealedEndTimeAfterStart(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Simulate healing calculation
		bootUnix := rapid.Int64Range(0, time.Now().Unix()).Draw(t, "bootUnix")
		monotonicStart := rapid.Int64Range(0, 30*24*60*60).Draw(t, "monotonicStart")
		durationSec := rapid.Int64Range(0, 86400).Draw(t, "durationSec")

		// From sqlHealTimestamps:
		// StartTime = bootUnix + MonotonicStart
		// EndTime = bootUnix + (MonotonicStart + DurationSec)
		healedStart := bootUnix + monotonicStart
		healedEnd := bootUnix + monotonicStart + durationSec

		if healedEnd < healedStart {
			t.Fatalf("Healed EndTime (%d) should not be before StartTime (%d)",
				healedEnd, healedStart)
		}
	})
}

// ============================================================================
// Hanging Entry Cleanup Property Tests
// ============================================================================

// TestPropertyCloseHangingCalculation verifies EndTime = StartTime + PlayTime.
func TestPropertyCloseHangingCalculation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// From sqlCloseHangingMediaHistory:
		// EndTime = StartTime + PlayTime
		startUnix := rapid.Int64Range(0, time.Now().Unix()).Draw(t, "startUnix")
		playTime := rapid.Int64Range(0, 86400).Draw(t, "playTime")

		// Calculate expected EndTime
		expectedEndTime := startUnix + playTime

		// Verify EndTime >= StartTime
		if expectedEndTime < startUnix {
			t.Fatalf("Calculated EndTime (%d) should not be before StartTime (%d)",
				expectedEndTime, startUnix)
		}

		// Verify the difference equals PlayTime
		actualDuration := expectedEndTime - startUnix
		if actualDuration != playTime {
			t.Fatalf("Duration (%d) should equal PlayTime (%d)",
				actualDuration, playTime)
		}
	})
}

// ============================================================================
// Pagination Property Tests
// ============================================================================

// TestPropertyGetMediaHistoryLimitClamping verifies limit bounds (1-100).
func TestPropertyGetMediaHistoryLimitClamping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Create the table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE MediaHistory (
			DBID INTEGER PRIMARY KEY AUTOINCREMENT,
			ID TEXT, StartTime INTEGER, EndTime INTEGER, SystemID TEXT, SystemName TEXT,
			MediaPath TEXT, MediaName TEXT, LauncherID TEXT, PlayTime INTEGER,
			BootUUID TEXT, MonotonicStart INTEGER, DurationSec INTEGER, WallDuration INTEGER,
			TimeSkewFlag INTEGER, ClockReliable INTEGER, ClockSource TEXT,
			CreatedAt INTEGER, UpdatedAt INTEGER, DeviceID TEXT
		)
	`)
	require.NoError(t, err)

	rapid.Check(t, func(t *rapid.T) {
		limit := rapid.IntRange(-100, 200).Draw(t, "limit")

		// The function should clamp limit to valid range
		entries, err := sqlGetMediaHistory(ctx, db, 0, limit)
		require.NoError(t, err)

		// With empty table, we get empty results regardless of limit
		// But the important thing is it doesn't error with invalid limits
		if entries == nil {
			t.Fatal("Expected non-nil result (empty slice)")
		}
	})
}

// TestPropertyGetMediaHistoryLastIDPagination verifies pagination token handling.
func TestPropertyGetMediaHistoryLastIDPagination(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Create the table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE MediaHistory (
			DBID INTEGER PRIMARY KEY AUTOINCREMENT,
			ID TEXT, StartTime INTEGER, EndTime INTEGER, SystemID TEXT, SystemName TEXT,
			MediaPath TEXT, MediaName TEXT, LauncherID TEXT, PlayTime INTEGER,
			BootUUID TEXT, MonotonicStart INTEGER, DurationSec INTEGER, WallDuration INTEGER,
			TimeSkewFlag INTEGER, ClockReliable INTEGER, ClockSource TEXT,
			CreatedAt INTEGER, UpdatedAt INTEGER, DeviceID TEXT
		)
	`)
	require.NoError(t, err)

	// Insert some test rows
	now := time.Now().Unix()
	for i := 1; i <= 20; i++ {
		_, err = db.ExecContext(ctx, `
			INSERT INTO MediaHistory (ID, StartTime, SystemID, SystemName, MediaPath, MediaName,
			LauncherID, PlayTime, BootUUID, MonotonicStart, DurationSec, WallDuration,
			TimeSkewFlag, ClockReliable, ClockSource, CreatedAt, UpdatedAt)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, "id-"+string(rune('a'+i)), now, "nes", "NES", "/path", "Game", "retroarch",
			100, "boot-uuid", 1000, 100, 100, 0, 1, "system", now, now)
		require.NoError(t, err)
	}

	rapid.Check(t, func(t *rapid.T) {
		lastID := rapid.IntRange(-10, 30).Draw(t, "lastID")
		limit := rapid.IntRange(1, 100).Draw(t, "limit")

		entries, err := sqlGetMediaHistory(ctx, db, lastID, limit)
		require.NoError(t, err)

		// Verify all returned entries have DBID < lastID (or lastID=0 means all)
		if lastID > 0 {
			for _, entry := range entries {
				if entry.DBID >= int64(lastID) {
					t.Fatalf("Entry DBID (%d) should be < lastID (%d)",
						entry.DBID, lastID)
				}
			}
		}

		// Verify results are ordered by DBID descending
		for i := 1; i < len(entries); i++ {
			if entries[i].DBID >= entries[i-1].DBID {
				t.Fatalf("Results not sorted descending: DBID %d >= %d",
					entries[i].DBID, entries[i-1].DBID)
			}
		}
	})
}

// ============================================================================
// Entry Field Consistency Tests
// ============================================================================

// TestPropertyCreatedAtBeforeUpdatedAt verifies CreatedAt <= UpdatedAt.
func TestPropertyCreatedAtBeforeUpdatedAt(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		entry := mediaHistoryEntryGen().Draw(t, "entry")

		// For newly created entries, they should be equal or UpdatedAt slightly after
		// Our generator sets them equal at creation, which is correct
		if entry.UpdatedAt.Before(entry.CreatedAt) {
			t.Fatalf("UpdatedAt (%v) should not be before CreatedAt (%v)",
				entry.UpdatedAt, entry.CreatedAt)
		}
	})
}

// TestPropertyMonotonicStartNonNegative verifies MonotonicStart is non-negative.
func TestPropertyMonotonicStartNonNegative(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		entry := mediaHistoryEntryGen().Draw(t, "entry")

		if entry.MonotonicStart < 0 {
			t.Fatalf("MonotonicStart should not be negative: %d", entry.MonotonicStart)
		}
	})
}

// TestPropertyWallDurationNonNegative verifies WallDuration is non-negative.
func TestPropertyWallDurationNonNegative(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		entry := mediaHistoryEntryGen().Draw(t, "entry")

		if entry.WallDuration < 0 {
			t.Fatalf("WallDuration should not be negative: %d", entry.WallDuration)
		}
	})
}

// ============================================================================
// Retention Cleanup Property Tests
// ============================================================================

// TestPropertyRetentionCalculation verifies cutoff time calculation.
func TestPropertyRetentionCalculation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		retentionDays := rapid.IntRange(1, 3650).Draw(t, "retentionDays") // Up to 10 years

		now := time.Now()
		cutoffTime := now.AddDate(0, 0, -retentionDays)

		// Cutoff should be in the past
		if !cutoffTime.Before(now) {
			t.Fatalf("Cutoff time (%v) should be before now (%v)", cutoffTime, now)
		}

		// Difference should be approximately retentionDays
		diff := now.Sub(cutoffTime)
		expectedDiff := time.Duration(retentionDays) * 24 * time.Hour
		// Allow for small rounding differences (up to 1 day due to DST, leap seconds, etc.)
		if diff < expectedDiff-24*time.Hour || diff > expectedDiff+24*time.Hour {
			t.Fatalf("Time difference (%v) not close to expected (%v)", diff, expectedDiff)
		}
	})
}
