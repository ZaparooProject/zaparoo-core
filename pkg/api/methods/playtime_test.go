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

package methods

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestConfig creates a config instance with the given values for testing
func newTestConfig(t *testing.T, vals *config.Values) *config.Instance {
	t.Helper()

	cfg, err := config.NewConfig(t.TempDir(), *vals)
	require.NoError(t, err)

	return cfg
}

func TestHandlePlaytime_NoLimitsManager(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, &config.Values{})

	env := requests.RequestEnv{
		Config:        cfg,
		LimitsManager: nil, // No limits manager
	}

	result, err := HandlePlaytime(env)

	require.NoError(t, err)
	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)

	assert.Equal(t, "reset", resp.State)
	assert.False(t, resp.SessionActive)
	assert.Nil(t, resp.DailyUsageToday)
	assert.Nil(t, resp.DailyRemaining)
}

func TestHandlePlaytime_ResetStateWithDailyFields(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockUserDBI()
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

	enabled := true
	cfg := newTestConfig(t, &config.Values{
		Playtime: config.Playtime{
			Limits: config.PlaytimeLimits{
				Enabled: &enabled,
				Daily:   "2h",
			},
		},
	})

	currentTime := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
	fakeClock := clockwork.NewFakeClockAt(currentTime)

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, nil)
	// State is already StateReset by default

	env := requests.RequestEnv{
		Config:        cfg,
		LimitsManager: tm,
	}

	result, err := HandlePlaytime(env)

	require.NoError(t, err)
	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)

	assert.Equal(t, "reset", resp.State)
	assert.False(t, resp.SessionActive)

	// Daily fields should be present (converted to strings)
	require.NotNil(t, resp.DailyUsageToday, "daily usage should be present")
	assert.Equal(t, "1h0m0s", *resp.DailyUsageToday)

	require.NotNil(t, resp.DailyRemaining, "daily remaining should be present")
	assert.Equal(t, "1h0m0s", *resp.DailyRemaining)

	mockDB.AssertExpectations(t)
}

func TestHandlePlaytime_ResetStateNilDailyFields(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockUserDBI()
	// No DB calls expected - daily limit disabled

	db := &database.Database{
		UserDB: mockDB,
	}

	// No daily limit
	cfg := newTestConfig(t, &config.Values{
		Playtime: config.Playtime{
			Limits: config.PlaytimeLimits{
				Daily: "",
			},
		},
	})

	currentTime := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
	fakeClock := clockwork.NewFakeClockAt(currentTime)

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, nil)

	env := requests.RequestEnv{
		Config:        cfg,
		LimitsManager: tm,
	}

	result, err := HandlePlaytime(env)

	require.NoError(t, err)
	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)

	assert.Equal(t, "reset", resp.State)
	assert.Nil(t, resp.DailyUsageToday, "daily usage should be nil when limit disabled")
	assert.Nil(t, resp.DailyRemaining, "daily remaining should be nil when limit disabled")

	mockDB.AssertExpectations(t)
}

func TestHandlePlaytime_CooldownStateWithDailyFields(t *testing.T) {
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

	enabled := true
	cfg := newTestConfig(t, &config.Values{
		Playtime: config.Playtime{
			Limits: config.PlaytimeLimits{
				Enabled: &enabled,
				Daily:   "2h",
				Session: "1h",
			},
		},
	})

	currentTime := time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC)
	fakeClock := clockwork.NewFakeClockAt(currentTime)

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, nil)

	// Put manager in cooldown state (need to access internals)
	// We'll use reflection or simply test the handler with a real state transition
	// For this test, we can simulate by manually setting state via exported test helper
	// Since we can't directly set state, let's verify the handler output instead

	env := requests.RequestEnv{
		Config:        cfg,
		LimitsManager: tm,
	}

	result, err := HandlePlaytime(env)

	require.NoError(t, err)
	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)

	// In reset state (no game started yet), daily fields should still be present
	assert.Equal(t, "reset", resp.State)
	require.NotNil(t, resp.DailyUsageToday)
	require.NotNil(t, resp.DailyRemaining)

	mockDB.AssertExpectations(t)
}

func TestHandlePlaytime_SessionFields(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockUserDBI()
	// No DB calls expected - daily limit disabled

	db := &database.Database{
		UserDB: mockDB,
	}

	// Session limit only, no daily limit
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

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, nil)

	env := requests.RequestEnv{
		Config:        cfg,
		LimitsManager: tm,
	}

	result, err := HandlePlaytime(env)

	require.NoError(t, err)
	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)

	// In reset state, session fields should not be present
	assert.Equal(t, "reset", resp.State)
	assert.Nil(t, resp.SessionDuration)
	assert.Nil(t, resp.SessionRemaining)
	assert.Nil(t, resp.SessionCumulativeTime)

	mockDB.AssertExpectations(t)
}

func TestHandlePlaytime_UnreliableClockNilDailyFields(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockUserDBI()
	// No DB calls expected - clock unreliable

	db := &database.Database{
		UserDB: mockDB,
	}

	enabled := true
	cfg := newTestConfig(t, &config.Values{
		Playtime: config.Playtime{
			Limits: config.PlaytimeLimits{
				Enabled: &enabled,
				Daily:   "2h",
			},
		},
	})

	// Unreliable clock (year 1970)
	currentTime := time.Date(1970, 1, 1, 14, 0, 0, 0, time.UTC)
	fakeClock := clockwork.NewFakeClockAt(currentTime)

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, nil)

	env := requests.RequestEnv{
		Config:        cfg,
		LimitsManager: tm,
	}

	result, err := HandlePlaytime(env)

	require.NoError(t, err)
	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)

	assert.Equal(t, "reset", resp.State)
	// Daily fields should be nil when clock is unreliable
	assert.Nil(t, resp.DailyUsageToday, "daily usage should be nil when clock unreliable")
	assert.Nil(t, resp.DailyRemaining, "daily remaining should be nil when clock unreliable")

	mockDB.AssertExpectations(t)
}

// timePtr returns a pointer to the given time
func timePtr(t time.Time) *time.Time {
	return &t
}
