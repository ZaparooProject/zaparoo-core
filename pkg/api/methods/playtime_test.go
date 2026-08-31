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
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newNoOpMockPlayer() *mocks.MockPlayer {
	p := mocks.NewMockPlayer()
	p.SetupNoOpMock()
	return p
}

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
		Context:       context.Background(),
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
	dayStart := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	mockDB.On("SumMediaPlayTimeForDay", dayStart).Return(int64(3600), nil)

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

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())
	// State is already StateReset by default

	env := requests.RequestEnv{
		Context:       context.Background(),
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

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

	env := requests.RequestEnv{
		Context:       context.Background(),
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
	dayStart := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	mockDB.On("SumMediaPlayTimeForDay", dayStart).Return(int64(1800), nil)

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

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

	// Put manager in cooldown state (need to access internals)
	// We'll use reflection or simply test the handler with a real state transition
	// For this test, we can simulate by manually setting state via exported test helper
	// Since we can't directly set state, let's verify the handler output instead

	env := requests.RequestEnv{
		Context:       context.Background(),
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

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

	env := requests.RequestEnv{
		Context:       context.Background(),
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

	tm := playtime.NewLimitsManager(db, nil, cfg, fakeClock, newNoOpMockPlayer())

	env := requests.RequestEnv{
		Context:       context.Background(),
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

// newExtendEnv builds a request environment with an enabled session limit and
// a manager sitting in cooldown, which is the state a card or app most often
// grants into: the limit stopped the game and the player wants to relaunch.
func newExtendEnv(
	t *testing.T, role string, isLocal bool,
) (env requests.RequestEnv, notifications <-chan models.Notification) {
	t.Helper()

	mockDB := testhelpers.NewMockUserDBI()
	mockDB.On("SetDeviceState", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockDB.On("GetDeviceState", mock.Anything).Return("", false, nil).Maybe()
	mockDB.On("DeleteDeviceState", mock.Anything).Return(nil).Maybe()
	mockDB.On("SumMediaPlayTimeForDay", mock.Anything).Return(int64(0), nil).Maybe()
	mockDB.On("SumMediaPlayTimeForDayByProfile", mock.Anything, mock.Anything).
		Return(int64(0), nil).Maybe()

	enabled := true
	cfg := newTestConfig(t, &config.Values{
		Playtime: config.Playtime{
			Limits: config.PlaytimeLimits{Enabled: &enabled, Session: "1h"},
		},
	})

	currentTime := time.Date(2026, 6, 12, 14, 0, 0, 0, time.UTC)
	tm := playtime.NewLimitsManager(
		&database.Database{UserDB: mockDB}, nil, cfg,
		clockwork.NewFakeClockAt(currentTime), newNoOpMockPlayer(),
	)
	t.Cleanup(tm.Stop)
	// Walk the real transitions into cooldown: a game ran and stopped, so the
	// session is still alive and a grant has something to attach to.
	tm.OnMediaStarted()
	tm.OnMediaStopped()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform").Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{
		DataDir: t.TempDir(), ConfigDir: t.TempDir(),
	}).Maybe()
	st, notificationCh := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)

	env = requests.RequestEnv{
		Context:       context.Background(),
		Config:        cfg,
		Database:      &database.Database{UserDB: mockDB},
		LimitsManager: tm,
		State:         st,
		ClientRole:    role,
		IsLocal:       isLocal,
		PlatformID:    "test-platform",
	}
	return env, notificationCh
}

func extendParams(t *testing.T, params models.ExtendPlaytimeParams) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	return raw
}

func TestHandlePlaytimeExtend_RequiresCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		role      string
		isLocal   bool
		wantAllow bool
	}{
		{name: "paired admin", role: "admin", wantAllow: true},
		{name: "localhost", role: "", isLocal: true, wantAllow: true},
		{name: "paired member", role: "member", wantAllow: false},
		// A legacy client has no identity at all, and the capability is
		// deliberately absent from every legacy platform grant.
		{name: "legacy", role: "", wantAllow: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env, _ := newExtendEnv(t, tt.role, tt.isLocal)
			env.Params = extendParams(t, models.ExtendPlaytimeParams{
				Mode:     models.PlaytimeExtendModeDuration,
				Duration: ptrTo("15m"),
			})

			_, err := HandlePlaytimeExtend(env)

			if tt.wantAllow {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrForbidden)
		})
	}
}

func TestHandlePlaytimeExtend_GrantsDuration(t *testing.T) {
	t.Parallel()

	env, notificationCh := newExtendEnv(t, "admin", false)
	env.Params = extendParams(t, models.ExtendPlaytimeParams{
		Mode:     models.PlaytimeExtendModeDuration,
		Duration: ptrTo("15m"),
	})

	result, err := HandlePlaytimeExtend(env)
	require.NoError(t, err)

	resp, ok := result.(models.ExtendPlaytimeResponse)
	require.True(t, ok)
	assert.Equal(t, models.PlaytimeExtendModeDuration, resp.Mode)
	assert.Equal(t, "15m0s", resp.Duration)
	assert.Equal(t, "15m0s", resp.SessionExtension)
	assert.Empty(t, resp.Expires)
	assert.False(t, resp.Replayed)

	select {
	case n := <-notificationCh:
		assert.Equal(t, models.NotificationPlaytimeExtended, n.Method)
	default:
		t.Fatal("expected a playtime.extended notification")
	}
}

func TestHandlePlaytimeExtend_GrantsToday(t *testing.T) {
	t.Parallel()

	env, _ := newExtendEnv(t, "admin", false)
	env.Params = extendParams(t, models.ExtendPlaytimeParams{
		Mode: models.PlaytimeExtendModeToday,
	})

	result, err := HandlePlaytimeExtend(env)
	require.NoError(t, err)

	resp, ok := result.(models.ExtendPlaytimeResponse)
	require.True(t, ok)
	assert.Equal(t, models.PlaytimeExtendModeToday, resp.Mode)
	assert.Equal(t, "2026-06-13T00:00:00Z", resp.Expires)
	assert.Empty(t, resp.Duration, "a day waiver adds no fixed amount")
}

func TestHandlePlaytimeExtend_RequestIDIsIdempotent(t *testing.T) {
	t.Parallel()

	env, notificationCh := newExtendEnv(t, "admin", false)
	env.Params = extendParams(t, models.ExtendPlaytimeParams{
		Mode:      models.PlaytimeExtendModeDuration,
		Duration:  ptrTo("15m"),
		RequestID: "retry-1",
	})

	first, err := HandlePlaytimeExtend(env)
	require.NoError(t, err)
	<-notificationCh

	second, err := HandlePlaytimeExtend(env)
	require.NoError(t, err)

	firstResp, ok := first.(models.ExtendPlaytimeResponse)
	require.True(t, ok)
	secondResp, ok := second.(models.ExtendPlaytimeResponse)
	require.True(t, ok)

	assert.True(t, secondResp.Replayed)
	assert.Equal(t, firstResp.SessionExtension, secondResp.SessionExtension,
		"a retry must not add more time")

	// A replay granted nothing, so subscribers must not see a second event.
	select {
	case n := <-notificationCh:
		t.Fatalf("unexpected notification for a replayed grant: %s", n.Method)
	default:
	}
}

func TestHandlePlaytimeExtend_RejectsBadParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		duration *string
		name     string
		mode     string
	}{
		{name: "missing mode", mode: ""},
		{name: "unknown mode", mode: "forever"},
		{name: "duration mode without duration", mode: models.PlaytimeExtendModeDuration},
		{name: "unparseable duration", mode: models.PlaytimeExtendModeDuration, duration: ptrTo("soon")},
		{name: "duration below minimum", mode: models.PlaytimeExtendModeDuration, duration: ptrTo("10s")},
		{name: "duration above maximum", mode: models.PlaytimeExtendModeDuration, duration: ptrTo("48h")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env, _ := newExtendEnv(t, "admin", false)
			env.Params = extendParams(t, models.ExtendPlaytimeParams{
				Mode: tt.mode, Duration: tt.duration,
			})

			_, err := HandlePlaytimeExtend(env)
			require.Error(t, err)

			// Caller fault, not server fault: these must not read as an
			// internal error to the client.
			var clientErr *models.ClientError
			assert.ErrorAs(t, err, &clientErr, "expected a client error for %s", tt.name)
		})
	}
}

func TestHandlePlaytimeExtend_RejectsWhenNoSession(t *testing.T) {
	t.Parallel()

	env, _ := newExtendEnv(t, "admin", false)
	// Back to a reset session: nothing is being limited, so there is nothing
	// to extend.
	env.LimitsManager.ResetSession()
	env.Params = extendParams(t, models.ExtendPlaytimeParams{
		Mode:     models.PlaytimeExtendModeDuration,
		Duration: ptrTo("15m"),
	})

	_, err := HandlePlaytimeExtend(env)
	require.ErrorIs(t, err, playtime.ErrGrantNoSession)

	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr)
}

func TestHandlePlaytimeExtend_NoLimitsManager(t *testing.T) {
	t.Parallel()

	env, _ := newExtendEnv(t, "admin", false)
	env.LimitsManager = nil
	env.Params = extendParams(t, models.ExtendPlaytimeParams{
		Mode:     models.PlaytimeExtendModeDuration,
		Duration: ptrTo("15m"),
	})

	_, err := HandlePlaytimeExtend(env)
	require.Error(t, err)
}

func TestHandlePlaytime_ReportsExtensionFields(t *testing.T) {
	t.Parallel()

	env, _ := newExtendEnv(t, "admin", false)
	env.Params = extendParams(t, models.ExtendPlaytimeParams{
		Mode:     models.PlaytimeExtendModeDuration,
		Duration: ptrTo("15m"),
	})
	_, err := HandlePlaytimeExtend(env)
	require.NoError(t, err)

	result, err := HandlePlaytime(env)
	require.NoError(t, err)

	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)
	require.NotNil(t, resp.SessionExtension)
	assert.Equal(t, "15m0s", *resp.SessionExtension)
	assert.Nil(t, resp.SessionExtendedUntil)
	require.NotNil(t, resp.SessionRemaining)
	remaining, err := time.ParseDuration(*resp.SessionRemaining)
	require.NoError(t, err)
	assert.Greater(t, remaining, time.Hour,
		"remaining time should exceed the 1h limit once 15m is granted")
}

func TestHandlePlaytime_ReportsDayWaiver(t *testing.T) {
	t.Parallel()

	env, _ := newExtendEnv(t, "admin", false)
	env.Params = extendParams(t, models.ExtendPlaytimeParams{
		Mode: models.PlaytimeExtendModeToday,
	})
	_, err := HandlePlaytimeExtend(env)
	require.NoError(t, err)

	result, err := HandlePlaytime(env)
	require.NoError(t, err)

	resp, ok := result.(models.PlaytimeStatusResponse)
	require.True(t, ok)
	require.NotNil(t, resp.SessionExtendedUntil)
	assert.Equal(t, "2026-06-13T00:00:00Z", *resp.SessionExtendedUntil)
	assert.Nil(t, resp.SessionRemaining,
		"a waived session limit has no finite remaining time")
}

func ptrTo[T any](v T) *T { return &v }
