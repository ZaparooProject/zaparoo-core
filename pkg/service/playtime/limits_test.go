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

package playtime

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRuleContext_MidnightRollover_CurrentSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sessionStart            time.Time
		currentTime             time.Time
		name                    string
		wantSessionDurationDesc string
		wantDailyUsageDesc      string
		historicalEntries       []database.MediaHistoryEntry
		wantSessionDuration     time.Duration
		wantDailyUsageToday     time.Duration
	}{
		{
			name:                    "session entirely within today",
			sessionStart:            time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
			currentTime:             time.Date(2025, 1, 15, 15, 30, 0, 0, time.UTC),
			historicalEntries:       []database.MediaHistoryEntry{},
			wantSessionDuration:     90 * time.Minute,
			wantDailyUsageToday:     90 * time.Minute,
			wantSessionDurationDesc: "1.5 hours",
			wantDailyUsageDesc:      "1.5 hours (current session only)",
		},
		{
			name:         "session started yesterday, continues today",
			sessionStart: time.Date(2025, 1, 14, 23, 0, 0, 0, time.UTC), // 11 PM yesterday
			currentTime:  time.Date(2025, 1, 15, 0, 30, 0, 0, time.UTC), // 12:30 AM today
			historicalEntries: []database.MediaHistoryEntry{
				// Previous session yesterday: 10 PM - 11 PM (1 hour)
				{
					DBID:      2,
					StartTime: time.Date(2025, 1, 14, 22, 0, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 14, 23, 0, 0, 0, time.UTC)),
					PlayTime:  3600, // 1 hour in seconds
				},
			},
			wantSessionDuration:     90 * time.Minute, // Total session: 1.5 hours
			wantDailyUsageToday:     30 * time.Minute, // Only 30 minutes after midnight
			wantSessionDurationDesc: "1.5 hours total",
			wantDailyUsageDesc:      "30 minutes (only time after midnight)",
		},
		{
			name:         "session started yesterday, historical session spans midnight",
			sessionStart: time.Date(2025, 1, 15, 1, 0, 0, 0, time.UTC), // 1 AM today
			currentTime:  time.Date(2025, 1, 15, 2, 0, 0, 0, time.UTC), // 2 AM today
			historicalEntries: []database.MediaHistoryEntry{
				// Session that spans midnight: 11 PM yesterday - 12:30 AM today
				{
					DBID:      3,
					StartTime: time.Date(2025, 1, 14, 23, 0, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 15, 0, 30, 0, 0, time.UTC)),
					PlayTime:  5400, // 1.5 hours total
				},
			},
			wantSessionDuration:     60 * time.Minute, // Current session: 1 hour
			wantDailyUsageToday:     90 * time.Minute, // 30 min from historical + 60 min current
			wantSessionDurationDesc: "1 hour",
			wantDailyUsageDesc:      "1.5 hours (30 min historical after midnight + 1 hour current)",
		},
		{
			name:         "multiple historical sessions, some span midnight",
			sessionStart: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC), // 10 AM today
			currentTime:  time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC), // 11 AM today
			historicalEntries: []database.MediaHistoryEntry{
				// Most recent: Started today at 8 AM, ended at 9 AM (1 hour)
				{
					DBID:      5,
					StartTime: time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC)),
					PlayTime:  3600,
				},
				// Spans midnight: 11:30 PM yesterday - 12:45 AM today
				{
					DBID:      4,
					StartTime: time.Date(2025, 1, 14, 23, 30, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 15, 0, 45, 0, 0, time.UTC)),
					PlayTime:  4500, // 1.25 hours total
				},
				// Entirely yesterday: 10 PM - 11 PM
				{
					DBID:      3,
					StartTime: time.Date(2025, 1, 14, 22, 0, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 14, 23, 0, 0, 0, time.UTC)),
					PlayTime:  3600,
				},
			},
			wantSessionDuration:     60 * time.Minute,  // Current: 1 hour
			wantDailyUsageToday:     165 * time.Minute, // 45 min (midnight span) + 60 min (8-9 AM) + 60 min (current)
			wantSessionDurationDesc: "1 hour",
			wantDailyUsageDesc:      "2h45m (45 min from midnight span + 1 hour 8-9 AM + 1 hour current)",
		},
		{
			name:         "historical session ended before today - should not count",
			sessionStart: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			currentTime:  time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
			historicalEntries: []database.MediaHistoryEntry{
				// Ended before today
				{
					DBID:      2,
					StartTime: time.Date(2025, 1, 14, 22, 0, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 14, 23, 30, 0, 0, time.UTC)),
					PlayTime:  5400,
				},
			},
			wantSessionDuration:     60 * time.Minute,
			wantDailyUsageToday:     60 * time.Minute, // Only current session
			wantSessionDurationDesc: "1 hour",
			wantDailyUsageDesc:      "1 hour (historical session ended yesterday, doesn't count)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock database
			mockDB := helpers.NewMockUserDBI()
			mockDB.On("GetMediaHistory", 0, 100).Return(tt.historicalEntries, nil)

			db := &database.Database{
				UserDB: mockDB,
			}

			// Setup config with limits enabled
			cfg := &config.Instance{}
			*cfg = config.Instance{} // Initialize with defaults
			// Note: We don't actually need limits enabled for buildRuleContext testing,
			// just need the Instance to exist

			// Create LimitsManager with fake clock
			fakeClock := clockwork.NewFakeClockAt(tt.currentTime)
			tm := NewLimitsManager(db, nil, cfg, fakeClock)

			// Build rule context
			ctx, err := tm.buildRuleContext(tt.sessionStart)

			// Verify
			require.NoError(t, err, "buildRuleContext should not error")
			assert.Equal(t, tt.wantSessionDuration, ctx.SessionDuration,
				"session duration mismatch: expected %s", tt.wantSessionDurationDesc)
			assert.Equal(t, tt.wantDailyUsageToday, ctx.DailyUsageToday,
				"daily usage mismatch: expected %s", tt.wantDailyUsageDesc)
			assert.Equal(t, tt.currentTime, ctx.CurrentTime, "current time should match")

			mockDB.AssertExpectations(t)
		})
	}
}

func TestCalculateDailyUsage_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		todayStart            time.Time
		name                  string
		historicalEntries     []database.MediaHistoryEntry
		currentSessionDur     time.Duration
		wantDailyUsage        time.Duration
		wantDailyUsageMinutes int
	}{
		{
			name:              "no historical entries, only current session",
			todayStart:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			currentSessionDur: 45 * time.Minute,
			historicalEntries: []database.MediaHistoryEntry{},
			wantDailyUsage:    45 * time.Minute,
		},
		{
			name:              "historical session exactly at midnight boundary",
			todayStart:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			currentSessionDur: 30 * time.Minute,
			historicalEntries: []database.MediaHistoryEntry{
				{
					DBID:      1,
					StartTime: time.Date(2025, 1, 14, 23, 59, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)), // Ends exactly at midnight
					PlayTime:  60,
				},
			},
			wantDailyUsage: 30 * time.Minute, // Should not count the midnight-ending session
		},
		{
			name:              "historical session ends 1 second after midnight",
			todayStart:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			currentSessionDur: 30 * time.Minute,
			historicalEntries: []database.MediaHistoryEntry{
				{
					DBID:      1,
					StartTime: time.Date(2025, 1, 14, 23, 59, 0, 0, time.UTC),
					EndTime:   timePtr(time.Date(2025, 1, 15, 0, 0, 1, 0, time.UTC)), // 1 second after midnight
					PlayTime:  61,
				},
			},
			wantDailyUsage: 30*time.Minute + 1*time.Second, // Should count 1 second from historical
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock database
			mockDB := helpers.NewMockUserDBI()
			mockDB.On("GetMediaHistory", 0, 100).Return(tt.historicalEntries, nil)

			db := &database.Database{
				UserDB: mockDB,
			}

			// Create LimitsManager
			cfg := &config.Instance{}
			tm := NewLimitsManager(db, nil, cfg, clockwork.NewRealClock())

			// Calculate daily usage
			dailyUsage, err := tm.calculateDailyUsage(tt.todayStart, tt.currentSessionDur)

			// Verify
			require.NoError(t, err, "calculateDailyUsage should not error")
			assert.Equal(t, tt.wantDailyUsage, dailyUsage, "daily usage mismatch")

			mockDB.AssertExpectations(t)
		})
	}
}

// Helper function to create time pointers
func timePtr(t time.Time) *time.Time {
	return &t
}
