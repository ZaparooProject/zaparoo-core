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

// stubProvider is a fixed-value LimitsProvider for tests.
type stubProvider struct {
	profileID string
	warnings  []time.Duration
	daily     time.Duration
	session   time.Duration
	enabled   bool
}

func (s stubProvider) PlaytimeLimitsEnabled() bool       { return s.enabled }
func (s stubProvider) DailyLimit() time.Duration         { return s.daily }
func (s stubProvider) SessionLimit() time.Duration       { return s.session }
func (s stubProvider) WarningIntervals() []time.Duration { return s.warnings }
func (s stubProvider) ActiveProfileID() string           { return s.profileID }

func TestCalculateDailyUsage_ProfileScoped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	todayStart := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	startTime := now.Add(-2 * time.Hour)
	endTime := now.Add(-1 * time.Hour)
	profileID := "kid-a"

	mockDB := testhelpers.NewMockUserDBI()
	// Only the profile-scoped query may be used; an unscoped GetMediaHistory
	// call would fail the mock expectations.
	mockDB.On("GetMediaHistoryByProfile", profileID, int64(0), 100).
		Return([]database.MediaHistoryEntry{
			{
				DBID:      1,
				StartTime: startTime,
				EndTime:   &endTime,
				PlayTime:  3600,
				ProfileID: &profileID,
			},
		}, nil)

	tm := NewLimitsManager(
		&database.Database{UserDB: mockDB}, nil, &config.Instance{},
		clockwork.NewFakeClockAt(now), newNoOpMockPlayer(),
	)
	tm.SetLimitsProvider(stubProvider{enabled: true, daily: 2 * time.Hour, profileID: profileID})

	usage, err := tm.calculateDailyUsage(todayStart, 0)
	require.NoError(t, err)
	assert.Equal(t, time.Hour, usage)
	mockDB.AssertExpectations(t)
}

func TestCalculateDailyUsage_NoProfile_SumsAllHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	todayStart := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	startTime := now.Add(-2 * time.Hour)
	endTime := now.Add(-1 * time.Hour)

	mockDB := testhelpers.NewMockUserDBI()
	// Without an active profile, the unscoped query is used — device-level
	// accounting is byte-identical to pre-profile behavior.
	mockDB.On("GetMediaHistory", []string(nil), int64(0), 100).
		Return([]database.MediaHistoryEntry{
			{DBID: 1, StartTime: startTime, EndTime: &endTime, PlayTime: 1800},
		}, nil)

	tm := NewLimitsManager(
		&database.Database{UserDB: mockDB}, nil, &config.Instance{},
		clockwork.NewFakeClockAt(now), newNoOpMockPlayer(),
	)

	usage, err := tm.calculateDailyUsage(todayStart, 0)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, usage)
	mockDB.AssertExpectations(t)
}

func TestCheckBeforeLaunch_ProfileLimitsWithGlobalDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	startTime := now.Add(-3 * time.Hour)
	endTime := now.Add(-1 * time.Hour)
	profileID := "kid-a"

	mockDB := testhelpers.NewMockUserDBI()
	mockDB.On("GetMediaHistoryByProfile", profileID, int64(0), 100).
		Return([]database.MediaHistoryEntry{
			{
				DBID:      1,
				StartTime: startTime,
				EndTime:   &endTime,
				PlayTime:  7200, // 2 hours played today by this profile
				ProfileID: &profileID,
			},
		}, nil)

	// Global config has limits disabled — the profile override alone must
	// enforce its 1 hour daily limit (the issue #883 use case).
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetPlaytimeLimitsEnabled(false)

	tm := NewLimitsManager(
		&database.Database{UserDB: mockDB}, nil, cfg,
		clockwork.NewFakeClockAt(now), newNoOpMockPlayer(),
	)
	tm.SetLimitsProvider(stubProvider{enabled: true, daily: time.Hour, profileID: profileID})

	err = tm.CheckBeforeLaunch()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daily playtime limit reached")
}

func TestCheckBeforeLaunch_ProviderDisabledSkipsChecks(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockUserDBI()
	tm := NewLimitsManager(
		&database.Database{UserDB: mockDB}, nil, &config.Instance{},
		clockwork.NewFakeClockAt(time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)),
		newNoOpMockPlayer(),
	)
	tm.SetLimitsProvider(stubProvider{enabled: false, daily: time.Nanosecond})

	require.NoError(t, tm.CheckBeforeLaunch())
	mockDB.AssertExpectations(t)
}

func TestResetSession_ActiveGameRestartsTracking(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	clock := clockwork.NewFakeClockAt(now)
	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{}, clock, newNoOpMockPlayer())

	// Simulate an active session with accumulated time.
	tm.mu.Lock()
	tm.state = StateActive
	tm.sessionStart = now.Add(-time.Hour)
	tm.sessionStartMono = time.Now().Add(-time.Hour)
	tm.sessionCumulativeTime = 45 * time.Minute
	tm.warningsGiven[5*time.Minute] = true
	tm.mu.Unlock()

	tm.ResetSession()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	assert.Equal(t, StateActive, tm.state, "running game keeps tracking under the new profile")
	assert.True(t, tm.sessionStart.Equal(now), "session restarts from the switch moment")
	assert.Equal(t, time.Duration(0), tm.sessionCumulativeTime)
	assert.Empty(t, tm.warningsGiven)
}

func TestResetSession_CooldownResets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	clock := clockwork.NewFakeClockAt(now)
	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{}, clock, newNoOpMockPlayer())

	tm.mu.Lock()
	tm.state = StateCooldown
	tm.sessionCumulativeTime = 30 * time.Minute
	tm.lastStopTime = now.Add(-time.Minute)
	tm.cooldownTimer = clock.NewTimer(20 * time.Minute)
	tm.mu.Unlock()

	tm.ResetSession()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	assert.Equal(t, StateReset, tm.state)
	assert.Equal(t, time.Duration(0), tm.sessionCumulativeTime)
	assert.Nil(t, tm.cooldownTimer)
	assert.True(t, tm.lastStopTime.IsZero())
}
