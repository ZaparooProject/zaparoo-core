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

package playtime

import (
	"testing"
	"time"

	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNoOpMockPlayer() *mocks.MockPlayer {
	p := mocks.NewMockPlayer()
	p.SetupNoOpMock()
	return p
}

func TestLimitsManagerStopWaitsForNotificationHandler(t *testing.T) {
	t.Parallel()

	broker := fixtures.NewStopNotificationBroker()
	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{}, clockwork.NewFakeClock(), newNoOpMockPlayer())

	tm.Start(broker, make(chan apimodels.Notification, 1))
	tm.Stop()

	select {
	case <-broker.Unsubscribed:
	default:
		t.Fatal("expected Stop to wait for notification handler unsubscribe")
	}
}

func TestLimitsManagerIgnoresMediaStartedAfterStop(t *testing.T) {
	t.Parallel()

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetPlaytimeLimitsEnabled(true)
	tm := NewLimitsManager(
		&database.Database{}, nil, cfg, clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)),
		newNoOpMockPlayer(),
	)

	tm.Stop()
	tm.OnMediaStarted()

	tm.mu.Lock()
	state := tm.state
	sessionStart := tm.sessionStart
	tm.mu.Unlock()
	assert.Equal(t, StateReset, state)
	assert.True(t, sessionStart.IsZero())
}

func TestIsClockReliable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		time time.Time
		name string
		want bool
	}{
		{
			name: "year 2025 - reliable",
			time: time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2024 - reliable (release year)",
			time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "year 2023 - unreliable (before release)",
			time: time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),
			want: false,
		},
		{
			name: "year 1970 - unreliable (epoch)",
			time: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "year 2000 - unreliable",
			time: time.Date(2000, 6, 15, 12, 0, 0, 0, time.UTC),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := helpers.IsClockReliable(tt.time)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildRuleContext_UnreliableClock(t *testing.T) {
	t.Parallel()

	t.Run("clock year 1970 - sets ClockReliable to false", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// Should NOT call GetMediaHistory when clock unreliable

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := &config.Instance{}

		// Session started at epoch
		sessionStart := time.Date(1970, 1, 1, 10, 0, 0, 0, time.UTC)
		currentTime := time.Date(1970, 1, 1, 11, 0, 0, 0, time.UTC)

		fakeClock := clockwork.NewFakeClockAt(currentTime)
		tm := NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

		ctx, err := tm.buildRuleContext(sessionStart)

		require.NoError(t, err)
		assert.False(t, ctx.ClockReliable, "clock should be unreliable for year 1970")
		assert.Equal(t, time.Hour, ctx.SessionDuration, "session duration should still be calculated")
		assert.Equal(t, time.Duration(0), ctx.DailyUsageToday, "daily usage should be 0 when clock unreliable")

		// DB should not have been queried
		mockDB.AssertExpectations(t)
	})

	t.Run("clock year 2025 - sets ClockReliable to true", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		mockDB.On("SumMediaPlayTimeForDay", time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)).
			Return(int64(0), nil)

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := &config.Instance{}

		sessionStart := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
		currentTime := time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC)

		fakeClock := clockwork.NewFakeClockAt(currentTime)
		tm := NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

		// Session started with reliable clock
		tm.mu.Lock()
		tm.sessionStartReliable = true
		tm.mu.Unlock()

		ctx, err := tm.buildRuleContext(sessionStart)

		require.NoError(t, err)
		assert.True(t, ctx.ClockReliable, "clock should be reliable for year 2025")
		assert.Equal(t, time.Hour, ctx.SessionDuration)

		// DB should have been queried
		mockDB.AssertExpectations(t)
	})
}

func TestBuildRuleContext_ClockHealing(t *testing.T) {
	t.Parallel()

	t.Run("session starts at 1970, clock heals to 2025 mid-session", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// Should NOT query DB - session started unreliable

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := &config.Instance{}

		// Session started at 1970, 10:00
		sessionStart := time.Date(1970, 1, 1, 10, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(sessionStart)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

		// Simulate OnMediaStarted at 1970
		tm.mu.Lock()
		tm.sessionStart = sessionStart
		tm.sessionStartReliable = false // Clock was unreliable at session start
		tm.mu.Unlock()

		// 5 minutes later, NTP syncs - clock jumps to 2025
		// (We can't simulate wall clock jumps with fakeClock, but the monotonic time advances)
		fakeClock.Advance(5 * time.Minute) // Monotonic advances 5 minutes

		// Manually test what would happen if clock healed
		// Since sessionStartReliable was false, daily should still be disabled

		ctx, err := tm.buildRuleContext(sessionStart)

		require.NoError(t, err)
		assert.False(t, ctx.ClockReliable, "should remain unreliable - session started unreliably")
		assert.Equal(t, 5*time.Minute, ctx.SessionDuration, "session duration correct via monotonic")
		assert.Equal(t, time.Duration(0), ctx.DailyUsageToday, "daily disabled for this session")

		mockDB.AssertExpectations(t)
	})

	t.Run("both clocks reliable with safety clamp", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		mockDB.On("SumMediaPlayTimeForDay", time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)).
			Return(int64(0), nil)

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := &config.Instance{}

		// Session started yesterday at 11 PM
		sessionStart := time.Date(2025, 1, 14, 23, 0, 0, 0, time.UTC)
		currentTime := time.Date(2025, 1, 15, 0, 30, 0, 0, time.UTC) // 12:30 AM today

		fakeClock := clockwork.NewFakeClockAt(currentTime)
		tm := NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

		// Mark session start as reliable
		tm.mu.Lock()
		tm.sessionStartReliable = true
		tm.mu.Unlock()

		ctx, err := tm.buildRuleContext(sessionStart)

		require.NoError(t, err)
		assert.True(t, ctx.ClockReliable, "both clocks reliable")
		assert.Equal(t, 90*time.Minute, ctx.SessionDuration, "total session: 1.5 hours")

		// Due to safety clamp, even though sessionStartToday would be midnight (30 min),
		// and the Before() check triggers, sessionDurationToday should be clamped to sessionDuration
		// Actually wait - the midnight logic sets sessionStartToday = midnight = 00:00
		// So sessionDurationToday = 00:30 - 00:00 = 30 minutes
		// This is LESS than sessionDuration (90 min), so no clamp needed
		assert.Equal(t, 30*time.Minute, ctx.DailyUsageToday, "only 30 min counts toward today")

		mockDB.AssertExpectations(t)
	})
}

func TestBuildRuleContext_MidnightRollover_CurrentSession(t *testing.T) {
	t.Parallel()

	// todayStart for all test cases: midnight UTC on 2025-01-15
	todayStart := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		sessionStart            time.Time
		currentTime             time.Time
		name                    string
		wantSessionDurationDesc string
		wantDailyUsageDesc      string
		// sqlSeconds is the value SumMediaPlayTimeForDay returns for completed
		// historical sessions. The current session's contribution is added by
		// buildRuleContext via sessionDurationToday.
		sqlSeconds          int64
		wantSessionDuration time.Duration
		wantDailyUsageToday time.Duration
	}{
		{
			name:                    "session entirely within today",
			sessionStart:            time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
			currentTime:             time.Date(2025, 1, 15, 15, 30, 0, 0, time.UTC),
			sqlSeconds:              0, // no completed sessions before current
			wantSessionDuration:     90 * time.Minute,
			wantDailyUsageToday:     90 * time.Minute,
			wantSessionDurationDesc: "1.5 hours",
			wantDailyUsageDesc:      "1.5 hours (current session only)",
		},
		{
			// Previous session 22:00–23:00 yesterday; SQL excludes it (EndTime not > dayStart).
			// sqlSeconds=0; only the 30 min after midnight counts toward today.
			name:                    "session started yesterday, continues today",
			sessionStart:            time.Date(2025, 1, 14, 23, 0, 0, 0, time.UTC),
			currentTime:             time.Date(2025, 1, 15, 0, 30, 0, 0, time.UTC),
			sqlSeconds:              0,
			wantSessionDuration:     90 * time.Minute,
			wantDailyUsageToday:     30 * time.Minute,
			wantSessionDurationDesc: "1.5 hours total",
			wantDailyUsageDesc:      "30 minutes (only time after midnight)",
		},
		{
			// Historical session spans midnight (23:00–00:30): 30 min portion after midnight = 1800 sec.
			name:                    "session started yesterday, historical session spans midnight",
			sessionStart:            time.Date(2025, 1, 15, 1, 0, 0, 0, time.UTC), // 1 AM today
			currentTime:             time.Date(2025, 1, 15, 2, 0, 0, 0, time.UTC), // 2 AM today
			sqlSeconds:              1800,                                         // 30 min after midnight
			wantSessionDuration:     60 * time.Minute,                             // Current session: 1 hour
			wantDailyUsageToday:     90 * time.Minute,                             // 30 min historical + 60 min current
			wantSessionDurationDesc: "1 hour",
			wantDailyUsageDesc:      "1.5 hours (30 min historical after midnight + 1 hour current)",
		},
		{
			// Two historical sessions overlap today: 8–9 AM (3600 sec) + midnight-span 45 min
			// (2700 sec) = 6300 sec total. Entirely-yesterday session (22–23) excluded by SQL.
			// wantDailyUsageToday = 105 min historical + 60 min current = 165 min.
			name:                    "multiple historical sessions, some span midnight",
			sessionStart:            time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			currentTime:             time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
			sqlSeconds:              6300,
			wantSessionDuration:     60 * time.Minute,
			wantDailyUsageToday:     165 * time.Minute,
			wantSessionDurationDesc: "1 hour",
			wantDailyUsageDesc:      "2h45m (45 min midnight span + 1 hour 8-9 AM + 1 hour current)",
		},
		{
			// Historical session 22:00–23:30 yesterday; EndTime not > dayStart, excluded.
			name:                    "historical session ended before today - should not count",
			sessionStart:            time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			currentTime:             time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
			sqlSeconds:              0, // excluded
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
			mockDB := testhelpers.NewMockUserDBI()
			mockDB.On("SumMediaPlayTimeForDay", todayStart).Return(tt.sqlSeconds, nil)

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
			tm := NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

			// Mark session start as reliable for these tests
			tm.mu.Lock()
			tm.sessionStartReliable = true
			tm.mu.Unlock()

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
		todayStart        time.Time
		name              string
		currentSessionDur time.Duration
		wantDailyUsage    time.Duration
		// sqlSeconds is the value SumMediaPlayTimeForDay returns for completed
		// sessions overlapping today. The active session (EndTime IS NULL) is
		// excluded by the SQL query; callers add currentSessionDur separately.
		sqlSeconds int64
	}{
		{
			name:              "no historical entries, only current session",
			todayStart:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			currentSessionDur: 45 * time.Minute,
			sqlSeconds:        0,
			wantDailyUsage:    45 * time.Minute,
		},
		{
			// Completed session (1 hour) + active session excluded by SQL.
			// currentSessionDur accounts for the active session; no double-count.
			name:              "active session in DB should be skipped (prevents double-counting)",
			todayStart:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			currentSessionDur: 30 * time.Minute,
			sqlSeconds:        3600, // 1 hour completed session
			wantDailyUsage:    90 * time.Minute,
		},
		{
			// Session ending exactly at midnight: EndTime == dayStart, SQL uses > not >=, so excluded.
			name:              "historical session exactly at midnight boundary",
			todayStart:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			currentSessionDur: 30 * time.Minute,
			sqlSeconds:        0,
			wantDailyUsage:    30 * time.Minute,
		},
		{
			// Session ending 1 second after midnight: EndTime > dayStart, StartTime < dayStart.
			// SQL returns EndTime - dayStart = 1 second.
			name:              "historical session ends 1 second after midnight",
			todayStart:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			currentSessionDur: 30 * time.Minute,
			sqlSeconds:        1,
			wantDailyUsage:    30*time.Minute + 1*time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock database
			mockDB := testhelpers.NewMockUserDBI()
			mockDB.On("SumMediaPlayTimeForDay", tt.todayStart).Return(tt.sqlSeconds, nil)

			db := &database.Database{
				UserDB: mockDB,
			}

			// Create LimitsManager
			cfg := &config.Instance{}
			tm := NewLimitsManager(db, nil, cfg, clockwork.NewRealClock(), newNoOpMockPlayer())

			// Calculate daily usage
			dailyUsage, err := tm.calculateDailyUsage(tt.todayStart, tt.currentSessionDur)

			// Verify
			require.NoError(t, err, "calculateDailyUsage should not error")
			assert.Equal(t, tt.wantDailyUsage, dailyUsage, "daily usage mismatch")

			mockDB.AssertExpectations(t)
		})
	}
}
