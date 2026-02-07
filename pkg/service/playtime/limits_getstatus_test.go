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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatus_StateReset(t *testing.T) {
	t.Parallel()

	t.Run("reset state with no daily limit returns nil daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// No DB calls expected - daily limit is disabled

		db := &database.Database{
			UserDB: mockDB,
		}

		// No daily limit configured
		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "",
					Session: "1h",
				},
			},
		})

		currentTime := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)
		// State is already StateReset by default

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "reset", status.State)
		assert.False(t, status.SessionActive)
		assert.Nil(t, status.DailyUsageToday, "daily usage should be nil when limit disabled")
		assert.Nil(t, status.DailyRemaining, "daily remaining should be nil when limit disabled")

		mockDB.AssertExpectations(t)
	})

	t.Run("reset state with daily limit and reliable clock returns daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// Expect DB call to calculate daily usage
		mockDB.On("GetMediaHistory", 0, 100).Return([]database.MediaHistoryEntry{
			{
				DBID:      1,
				StartTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				EndTime:   timePtr(time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC)),
				PlayTime:  3600, // 1 hour
			},
		}, nil)

		db := &database.Database{
			UserDB: mockDB,
		}

		// Daily limit configured
		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "",
				},
			},
		})

		currentTime := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)
		// State is already StateReset by default

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "reset", status.State)
		assert.False(t, status.SessionActive)

		// Daily fields should be populated
		require.NotNil(t, status.DailyUsageToday, "daily usage should be calculated")
		assert.Equal(t, time.Hour, *status.DailyUsageToday, "should show 1 hour usage from history")

		require.NotNil(t, status.DailyRemaining, "daily remaining should be calculated")
		assert.Equal(t, time.Hour, *status.DailyRemaining, "should show 1 hour remaining (2h limit - 1h used)")

		mockDB.AssertExpectations(t)
	})

	t.Run("reset state with unreliable clock returns nil daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// No DB calls expected - clock is unreliable

		db := &database.Database{
			UserDB: mockDB,
		}

		// Daily limit configured
		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "",
				},
			},
		})

		// Unreliable clock (year 1970)
		currentTime := time.Date(1970, 1, 1, 14, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "reset", status.State)
		assert.False(t, status.SessionActive)
		assert.Nil(t, status.DailyUsageToday, "daily usage should be nil when clock unreliable")
		assert.Nil(t, status.DailyRemaining, "daily remaining should be nil when clock unreliable")

		mockDB.AssertExpectations(t)
	})

	t.Run("reset state daily remaining clamped to zero when over limit", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// Return 3 hours of usage (over the 2 hour limit)
		mockDB.On("GetMediaHistory", 0, 100).Return([]database.MediaHistoryEntry{
			{
				DBID:      1,
				StartTime: time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC),
				EndTime:   timePtr(time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC)),
				PlayTime:  10800, // 3 hours
			},
		}, nil)

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily: "2h",
				},
			},
		})

		currentTime := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		status := tm.GetStatus()

		require.NotNil(t, status)
		require.NotNil(t, status.DailyUsageToday)
		assert.Equal(t, 3*time.Hour, *status.DailyUsageToday)

		require.NotNil(t, status.DailyRemaining)
		assert.Equal(t, time.Duration(0), *status.DailyRemaining, "remaining should be clamped to 0")

		mockDB.AssertExpectations(t)
	})
}

func TestGetStatus_StateCooldown(t *testing.T) {
	t.Parallel()

	t.Run("cooldown state with daily limit returns daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		mockDB.On("GetMediaHistory", 0, 100).Return([]database.MediaHistoryEntry{
			{
				DBID:      1,
				StartTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				EndTime:   timePtr(time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)),
				PlayTime:  1800, // 30 minutes
			},
		}, nil)

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "1h",
				},
			},
		})

		currentTime := time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		// Set up cooldown state
		tm.mu.Lock()
		tm.state = StateCooldown
		tm.sessionCumulativeTime = 30 * time.Minute
		tm.lastStopTime = time.Date(2025, 1, 15, 10, 55, 0, 0, time.UTC)
		tm.sessionResetTimeout = 20 * time.Minute
		tm.mu.Unlock()

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "cooldown", status.State)
		assert.False(t, status.SessionActive)

		// Session fields
		assert.Equal(t, 30*time.Minute, status.SessionDuration)
		assert.Equal(t, 30*time.Minute, status.SessionCumulativeTime)
		assert.Equal(t, 30*time.Minute, status.SessionRemaining) // 1h limit - 30m used

		// Daily fields should be populated
		require.NotNil(t, status.DailyUsageToday)
		assert.Equal(t, 30*time.Minute, *status.DailyUsageToday)

		require.NotNil(t, status.DailyRemaining)
		assert.Equal(t, 90*time.Minute, *status.DailyRemaining) // 2h - 30m

		mockDB.AssertExpectations(t)
	})

	t.Run("cooldown state with unreliable clock returns nil daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// No DB calls - clock unreliable

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "1h",
				},
			},
		})

		// Unreliable clock
		currentTime := time.Date(1970, 1, 1, 11, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		// Set up cooldown state
		tm.mu.Lock()
		tm.state = StateCooldown
		tm.sessionCumulativeTime = 30 * time.Minute
		tm.lastStopTime = time.Date(1970, 1, 1, 10, 55, 0, 0, time.UTC)
		tm.sessionResetTimeout = 20 * time.Minute
		tm.mu.Unlock()

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "cooldown", status.State)
		assert.False(t, status.SessionActive)

		// Session fields should still work
		assert.Equal(t, 30*time.Minute, status.SessionDuration)
		assert.Equal(t, 30*time.Minute, status.SessionRemaining)

		// Daily fields should be nil
		assert.Nil(t, status.DailyUsageToday, "daily usage should be nil when clock unreliable")
		assert.Nil(t, status.DailyRemaining, "daily remaining should be nil when clock unreliable")

		mockDB.AssertExpectations(t)
	})

	t.Run("cooldown state without daily limit returns nil daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// No DB calls - no daily limit

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "", // No daily limit
					Session: "1h",
				},
			},
		})

		currentTime := time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		// Set up cooldown state
		tm.mu.Lock()
		tm.state = StateCooldown
		tm.sessionCumulativeTime = 30 * time.Minute
		tm.lastStopTime = time.Date(2025, 1, 15, 10, 55, 0, 0, time.UTC)
		tm.sessionResetTimeout = 20 * time.Minute
		tm.mu.Unlock()

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "cooldown", status.State)
		assert.Nil(t, status.DailyUsageToday)
		assert.Nil(t, status.DailyRemaining)

		mockDB.AssertExpectations(t)
	})
}

func TestGetStatus_StateActive(t *testing.T) {
	t.Parallel()

	t.Run("active state with daily limit and reliable clock returns daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		mockDB.On("GetMediaHistory", 0, 100).Return([]database.MediaHistoryEntry{
			// Previous session today
			{
				DBID:      1,
				StartTime: time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC),
				EndTime:   timePtr(time.Date(2025, 1, 15, 9, 30, 0, 0, time.UTC)),
				PlayTime:  1800, // 30 minutes
			},
		}, nil)

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "1h",
				},
			},
		})

		sessionStart := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
		currentTime := time.Date(2025, 1, 15, 10, 15, 0, 0, time.UTC) // 15 min into session
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		// Set up active state
		tm.mu.Lock()
		tm.state = StateActive
		tm.sessionStart = sessionStart
		tm.sessionStartMono = time.Now().Add(-15 * time.Minute)
		tm.sessionStartReliable = true
		tm.sessionCumulativeTime = 0
		tm.mu.Unlock()

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "active", status.State)
		assert.True(t, status.SessionActive)
		assert.Equal(t, sessionStart, status.SessionStarted)

		// Session duration = 15 minutes (current game)
		assert.Equal(t, 15*time.Minute, status.SessionDuration)
		// Session remaining = 1h - 15m = 45m
		assert.Equal(t, 45*time.Minute, status.SessionRemaining)

		// Daily fields
		require.NotNil(t, status.DailyUsageToday)
		// 30m historical + 15m current = 45m
		assert.Equal(t, 45*time.Minute, *status.DailyUsageToday)

		require.NotNil(t, status.DailyRemaining)
		// 2h - 45m = 1h15m
		assert.Equal(t, 75*time.Minute, *status.DailyRemaining)

		mockDB.AssertExpectations(t)
	})

	t.Run("active state with unreliable session start returns nil daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// No DB calls - session started unreliably

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "1h",
				},
			},
		})

		sessionStart := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
		currentTime := time.Date(2025, 1, 15, 10, 15, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		// Set up active state with unreliable session start
		tm.mu.Lock()
		tm.state = StateActive
		tm.sessionStart = sessionStart
		tm.sessionStartMono = time.Now().Add(-15 * time.Minute)
		tm.sessionStartReliable = false // Session started with bad clock
		tm.sessionCumulativeTime = 0
		tm.mu.Unlock()

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "active", status.State)
		assert.True(t, status.SessionActive)

		// Session fields still work
		assert.Equal(t, 15*time.Minute, status.SessionDuration)
		assert.Equal(t, 45*time.Minute, status.SessionRemaining)

		// Daily fields nil - session started unreliably
		assert.Nil(t, status.DailyUsageToday)
		assert.Nil(t, status.DailyRemaining)

		mockDB.AssertExpectations(t)
	})

	t.Run("active state without daily limit returns nil daily fields", func(t *testing.T) {
		t.Parallel()

		mockDB := testhelpers.NewMockUserDBI()
		// DB still gets called in buildRuleContext when clock is reliable,
		// but the result is not used when daily limit is 0
		mockDB.On("GetMediaHistory", 0, 100).Return([]database.MediaHistoryEntry{}, nil)

		db := &database.Database{
			UserDB: mockDB,
		}

		cfg := newTestConfig(t, &config.Values{
			Playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "", // No daily limit
					Session: "1h",
				},
			},
		})

		sessionStart := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
		currentTime := time.Date(2025, 1, 15, 10, 15, 0, 0, time.UTC)
		fakeClock := clockwork.NewFakeClockAt(currentTime)

		tm := NewLimitsManager(db, nil, cfg, fakeClock, nil)

		// Set up active state
		tm.mu.Lock()
		tm.state = StateActive
		tm.sessionStart = sessionStart
		tm.sessionStartMono = time.Now().Add(-15 * time.Minute)
		tm.sessionStartReliable = true
		tm.sessionCumulativeTime = 0
		tm.mu.Unlock()

		status := tm.GetStatus()

		require.NotNil(t, status)
		assert.Equal(t, "active", status.State)
		assert.True(t, status.SessionActive)

		// Session fields work
		assert.Equal(t, 15*time.Minute, status.SessionDuration)
		assert.Equal(t, 45*time.Minute, status.SessionRemaining)

		// Daily fields nil - no daily limit
		assert.Nil(t, status.DailyUsageToday)
		assert.Nil(t, status.DailyRemaining)

		mockDB.AssertExpectations(t)
	})
}
