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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// deviceStateStore backs the DeviceState key/value calls with a real map, so
// tests can assert on what was actually persisted rather than on call counts.
type deviceStateStore struct {
	values map[string]string
}

// wireDeviceState points a mock UserDB's key/value calls at store, so tests
// can assert on what was actually persisted rather than on call counts.
func wireDeviceState(mockDB *testhelpers.MockUserDBI, store *deviceStateStore) {
	mockDB.On("SetDeviceState", mock.Anything, mock.Anything).
		Return(nil).
		Run(func(args mock.Arguments) {
			store.values[args.String(0)] = args.String(1)
		}).Maybe()
	// Reads are snapshotted at construction, which is exactly the restore
	// case: a fresh manager sees what the previous one persisted.
	for key, value := range store.values {
		mockDB.On("GetDeviceState", key).Return(value, true, nil).Maybe()
	}
	mockDB.On("GetDeviceState", mock.Anything).Return("", false, nil).Maybe()
	mockDB.On("DeleteDeviceState", mock.Anything).
		Return(nil).
		Run(func(args mock.Arguments) {
			delete(store.values, args.String(0))
		}).Maybe()
	mockDB.On("SumMediaPlayTimeForDay", mock.Anything).Return(int64(0), nil).Maybe()
	mockDB.On("SumMediaPlayTimeForDayByProfile", mock.Anything, mock.Anything).
		Return(int64(0), nil).Maybe()
}

// newExtensionManager builds a manager whose DeviceState reads and writes go
// to an in-memory store. Passing an existing store simulates a restart: a
// fresh manager over the state the previous one left behind.
func newExtensionManager(
	t *testing.T, now time.Time, provider LimitsProvider, store *deviceStateStore,
) (*LimitsManager, *deviceStateStore) {
	t.Helper()

	if store == nil {
		store = &deviceStateStore{values: make(map[string]string)}
	}
	mockDB := testhelpers.NewMockUserDBI()
	wireDeviceState(mockDB, store)

	cfg := newTestConfig(t, &config.Values{})
	tm := NewLimitsManager(
		&database.Database{UserDB: mockDB}, nil, cfg,
		clockwork.NewFakeClockAt(now), newNoOpMockPlayer(),
	)
	tm.SetLimitsProvider(provider)

	return tm, store
}

// enterCooldown puts the manager in the state it reaches after a game stops:
// the session is still alive and its accumulated time is remembered.
func (tm *LimitsManager) enterCooldown(cumulative time.Duration) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.state = StateCooldown
	tm.sessionCumulativeTime = cumulative
}

func durationGrant(d time.Duration) *GrantRequest {
	return &GrantRequest{
		Mode:                GrantModeDuration,
		Duration:            d,
		AuthorizerProfileID: "parent",
		Source:              "reader",
	}
}

func TestGrant_DurationExtendsSessionLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(30 * time.Minute)

	result, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	assert.Equal(t, GrantModeDuration, result.Mode)
	assert.Equal(t, 15*time.Minute, result.Duration)
	assert.Equal(t, 15*time.Minute, result.SessionExtension)
	assert.False(t, result.Replayed)

	assert.Equal(t, time.Hour+15*time.Minute, tm.effectiveSessionLimit(),
		"granted time should raise the session limit")
}

func TestGrant_DurationAccumulatesAcrossGrants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)
	second, err := tm.Grant(durationGrant(10 * time.Minute))
	require.NoError(t, err)

	assert.Equal(t, 10*time.Minute, second.Duration, "result reports this grant only")
	assert.Equal(t, 25*time.Minute, second.SessionExtension, "accumulated total covers both")
	assert.Equal(t, time.Hour+25*time.Minute, tm.effectiveSessionLimit())
}

func TestGrant_DurationRejectedOutsideBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		wantErr  error
		name     string
		duration time.Duration
	}{
		{name: "zero", duration: 0, wantErr: ErrGrantDurationRange},
		{name: "negative", duration: -5 * time.Minute, wantErr: ErrGrantDurationRange},
		{name: "below minimum", duration: 30 * time.Second, wantErr: ErrGrantDurationRange},
		{name: "above maximum", duration: 25 * time.Hour, wantErr: ErrGrantDurationRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
			tm.enterCooldown(0)

			_, err := tm.Grant(durationGrant(tt.duration))
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, time.Hour, tm.effectiveSessionLimit(), "a rejected grant changes nothing")
		})
	}
}

func TestGrant_DurationRejectedPastCumulativeCap(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(MaxSessionExtension))
	require.NoError(t, err)

	// Rejected rather than clamped: silently granting less than asked would
	// leave the card holder believing time was added.
	_, err = tm.Grant(durationGrant(time.Minute))
	require.ErrorIs(t, err, ErrGrantCapExceeded)
	assert.Equal(t, time.Hour+MaxSessionExtension, tm.effectiveSessionLimit())
}

func TestGrant_DurationRejectedWithNoSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, store := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.ErrorIs(t, err, ErrGrantNoSession)
	assert.Empty(t, store.values, "a refused grant persists nothing")
}

func TestGrant_RejectedWhenLimitsDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: false, session: time.Hour}, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.ErrorIs(t, err, ErrGrantLimitsDisabled)
}

// TestGrant_UnblocksRelaunchAfterLimitStop covers the flow the card exists
// for: the session limit stopped the game, a parent grants more time, and the
// next launch has to be allowed. This is what CheckBeforeLaunch reading the
// effective session limit buys.
func TestGrant_UnblocksRelaunchAfterLimitStop(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	// The limit stopped the game, so the session is over its allowance.
	tm.enterCooldown(time.Hour)

	reason, err := tm.CheckBeforeLaunch()
	require.Error(t, err, "relaunch must be blocked before any grant")
	assert.Equal(t, "session", reason)

	_, err = tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	reason, err = tm.CheckBeforeLaunch()
	require.NoError(t, err, "relaunch must be allowed once time is granted")
	assert.Empty(t, reason)
}

func TestGrant_ResetsWarningsSoTheyFireAgain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	tm.mu.Lock()
	tm.warningsGiven[5*time.Minute] = true
	tm.mu.Unlock()

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	tm.mu.Lock()
	given := tm.warningsGiven[5*time.Minute]
	tm.mu.Unlock()
	assert.False(t, given, "thresholds already spent were measured against the old allowance")
}

func TestGrant_TodayWaivesSessionLimitOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{
		enabled: true, session: time.Hour, daily: 3 * time.Hour,
	}, nil)

	result, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.NoError(t, err)

	assert.Equal(t, time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC), result.ExpiresAt)
	assert.Equal(t, time.Duration(0), tm.effectiveSessionLimit(), "session limit is waived")
	assert.Equal(t, 3*time.Hour, tm.effectiveDailyLimit(), "daily limit is untouched")

	rules := tm.createRules()
	require.Len(t, rules, 1, "only the daily rule should remain")
	assert.IsType(t, &DailyLimitRule{}, rules[0])
}

// A day grant is explicitly day-scoped, so it does not need a session to
// attach to and can be handed out before anyone starts playing.
func TestGrant_TodayAcceptedWithNoSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	_, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), tm.effectiveSessionLimit())
}

func TestGrant_TodayRepeatKeepsSameBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	first, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.NoError(t, err)
	second, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.NoError(t, err)

	assert.Equal(t, first.ExpiresAt, second.ExpiresAt,
		"rescanning must not roll the waiver into another day")
}

func TestGrant_TodayExpiresAtMidnight(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 23, 30, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	_, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), tm.effectiveSessionLimit())

	tm.clock.(*clockwork.FakeClock).Advance(31 * time.Minute)

	assert.Equal(t, time.Hour, tm.effectiveSessionLimit(),
		"the ordinary session limit returns after midnight")
}

func TestGrant_TodayRejectedWhenClockUnreliable(t *testing.T) {
	t.Parallel()

	// A day waiver is anchored to the local calendar, so an unset clock
	// makes "until midnight" meaningless.
	now := time.Date(1970, 1, 1, 12, 0, 0, 0, time.UTC)
	tm, store := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	_, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.ErrorIs(t, err, ErrGrantClockUnreliable)
	assert.Empty(t, store.values)
}

func TestGrant_RejectsUnknownMode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(&GrantRequest{Mode: "forever", AuthorizerProfileID: "parent"})
	require.ErrorIs(t, err, ErrGrantModeInvalid)
}

func TestGrant_IdempotencyKeyReplaysWithoutAddingTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	req := durationGrant(15 * time.Minute)
	req.IdempotencyKey = "retry-1"

	first, err := tm.Grant(req)
	require.NoError(t, err)
	assert.False(t, first.Replayed)

	second, err := tm.Grant(req)
	require.NoError(t, err)
	assert.True(t, second.Replayed, "a repeat must report the original grant")
	assert.Equal(t, first.SessionExtension, second.SessionExtension)
	assert.Equal(t, time.Hour+15*time.Minute, tm.effectiveSessionLimit(),
		"a replay must not add more time")
}

func TestGrant_IdempotencyWindowExpires(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	req := durationGrant(15 * time.Minute)
	req.IdempotencyKey = "tap"
	req.IdempotencyWindow = 10 * time.Second

	_, err := tm.Grant(req)
	require.NoError(t, err)

	tm.clock.(*clockwork.FakeClock).Advance(11 * time.Second)

	// A deliberate second tap later is a second grant, not a duplicate.
	again, err := tm.Grant(req)
	require.NoError(t, err)
	assert.False(t, again.Replayed)
	assert.Equal(t, 30*time.Minute, again.SessionExtension)
}

func TestGrant_DurationPinnedToRecipientProfile(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	provider := &swappableProvider{}
	provider.set(stubProvider{enabled: true, session: time.Hour, profileID: "kid-a"})

	tm, _ := newExtensionManager(t, now, provider, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)
	require.Equal(t, time.Hour+15*time.Minute, tm.effectiveSessionLimit())

	// Somebody else is playing now. The grant belonged to the previous
	// session and must not carry over.
	provider.set(stubProvider{enabled: true, session: time.Hour, profileID: "kid-b"})
	assert.Equal(t, time.Hour, tm.effectiveSessionLimit())
}

func TestGrant_ClearedWhenSessionResets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, store := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)
	require.NotEmpty(t, store.values)

	tm.ResetSession()

	assert.Equal(t, time.Hour, tm.effectiveSessionLimit())
	assert.Empty(t, store.values, "an empty snapshot deletes the stored key")
}

func TestGrant_SurvivesMediaStopIntoCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	tm.mu.Lock()
	tm.state = StateActive
	tm.sessionStart = now
	tm.sessionStartMono = time.Now()
	tm.mu.Unlock()

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	// Stopping a game ends the game, not the session: cooldown is exactly
	// when a player relaunches on the time they were just granted.
	tm.OnMediaStopped()

	assert.Equal(t, time.Hour+15*time.Minute, tm.effectiveSessionLimit())
}

func TestGrant_ClearedWhenLimitsDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	tm.SetEnabled(false)

	tm.mu.Lock()
	extension := tm.sessionExtension
	tm.mu.Unlock()
	assert.Nil(t, extension)
}

func TestGrant_FailsClosedWhenStorageUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	cfg := newTestConfig(t, &config.Values{})
	tm := NewLimitsManager(nil, nil, cfg, clockwork.NewFakeClockAt(now), newNoOpMockPlayer())
	tm.SetLimitsProvider(stubProvider{enabled: true, session: time.Hour})
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.ErrorIs(t, err, ErrGrantUnavailable)
	assert.Equal(t, time.Hour, tm.effectiveSessionLimit(),
		"a grant that cannot be stored must not apply in memory either")
}

func TestRestoreExtensions_RestoresGrantWithSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	provider := stubProvider{enabled: true, session: time.Hour, profileID: "kid-a"}
	tm, store := newExtensionManager(t, now, provider, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	// A restart: same stored state, a fresh manager, and a session that came
	// back from history.
	restored, _ := newExtensionManager(t, now, provider, store)
	restored.mu.Lock()
	restored.state = StateCooldown
	restored.mu.Unlock()

	restored.RestoreExtensions(now)

	assert.Equal(t, time.Hour+15*time.Minute, restored.effectiveSessionLimit())
}

func TestRestoreExtensions_DiscardsGrantWithoutSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	provider := stubProvider{enabled: true, session: time.Hour, profileID: "kid-a"}
	tm, store := newExtensionManager(t, now, provider, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	// The cooldown window lapsed while the service was down, so the session
	// the grant belonged to is gone.
	restored, _ := newExtensionManager(t, now, provider, store)

	restored.RestoreExtensions(now)

	assert.Equal(t, time.Hour, restored.effectiveSessionLimit())
}

func TestRestoreExtensions_DiscardsGrantForAnotherProfile(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, store := newExtensionManager(t, now,
		stubProvider{enabled: true, session: time.Hour, profileID: "kid-a"}, nil)
	tm.enterCooldown(0)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	restored, _ := newExtensionManager(t, now,
		stubProvider{enabled: true, session: time.Hour, profileID: "kid-b"}, store)
	restored.mu.Lock()
	restored.state = StateCooldown
	restored.mu.Unlock()

	restored.RestoreExtensions(now)

	assert.Equal(t, time.Hour, restored.effectiveSessionLimit())
}

func TestRestoreExtensions_RestoresWaiverWithoutSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	provider := stubProvider{enabled: true, session: time.Hour}
	tm, store := newExtensionManager(t, now, provider, nil)

	_, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.NoError(t, err)

	// A waiver is scoped to a profile and a day, not to a session, so it
	// comes back whether or not a session did.
	restored, _ := newExtensionManager(t, now, provider, store)

	restored.RestoreExtensions(now)

	assert.Equal(t, time.Duration(0), restored.effectiveSessionLimit())
}

func TestRestoreExtensions_FailsClosedOnBadState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		state string
	}{
		{name: "malformed json", state: `{"version":`},
		{name: "unknown version", state: `{"version":99,"session":{"totalSeconds":900}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &deviceStateStore{values: map[string]string{
				database.DeviceStateKeyPlaytimeExtensions: tt.state,
			}}
			tm, _ := newExtensionManager(t, now,
				stubProvider{enabled: true, session: time.Hour}, store)
			tm.enterCooldown(0)

			tm.RestoreExtensions(now)

			assert.Equal(t, time.Hour, tm.effectiveSessionLimit(),
				"state this build cannot read must not enable an extension")
			assert.Empty(t, store.values, "unreadable state is discarded rather than carried forward")
		})
	}
}

func TestGetStatus_ReportsExtension(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)
	tm.enterCooldown(10 * time.Minute)

	_, err := tm.Grant(durationGrant(15 * time.Minute))
	require.NoError(t, err)

	status := tm.GetStatus()
	assert.Equal(t, 15*time.Minute, status.SessionExtension)
	assert.True(t, status.SessionExtendedUntil.IsZero())
	assert.Equal(t, 65*time.Minute, status.SessionRemaining,
		"remaining time should count the granted extension")
}

func TestGetStatus_ReportsDayWaiver(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm, _ := newExtensionManager(t, now, stubProvider{enabled: true, session: time.Hour}, nil)

	_, err := tm.Grant(&GrantRequest{Mode: GrantModeToday, AuthorizerProfileID: "parent"})
	require.NoError(t, err)

	status := tm.GetStatus()
	assert.Equal(t, time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC), status.SessionExtendedUntil)
}

func TestNextLocalMidnight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		now  time.Time
		want time.Time
		name string
	}{
		{
			name: "midday",
			now:  time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC),
			want: time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "just before midnight",
			now:  time.Date(2026, 6, 12, 23, 59, 59, 0, time.UTC),
			want: time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "end of month rolls over",
			now:  time.Date(2026, 6, 30, 20, 0, 0, 0, time.UTC),
			want: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "end of year rolls over",
			now:  time.Date(2026, 12, 31, 20, 0, 0, 0, time.UTC),
			want: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, nextLocalMidnight(tt.now))
		})
	}
}

func TestGrantErrorsAreDistinguishable(t *testing.T) {
	t.Parallel()

	// Callers map these onto client errors, so they must not collapse into
	// one another through wrapping.
	require.NotErrorIs(t, ErrGrantCapExceeded, ErrGrantDurationRange)
	require.NotErrorIs(t, ErrGrantNoSession, ErrGrantLimitsDisabled)
}
