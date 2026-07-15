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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	profileID := "kid-a"

	mockDB := testhelpers.NewMockUserDBI()
	// Only the profile-scoped sum may be used; an unscoped
	// SumMediaPlayTimeForDay call would fail the mock expectations.
	mockDB.On("SumMediaPlayTimeForDayByProfile", todayStart, profileID).
		Return(int64(3600), nil)

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

	mockDB := testhelpers.NewMockUserDBI()
	// Without an active profile, the unscoped sum is used — device-level
	// accounting is byte-identical to pre-profile behavior.
	mockDB.On("SumMediaPlayTimeForDay", todayStart).Return(int64(1800), nil)

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
	profileID := "kid-a"

	mockDB := testhelpers.NewMockUserDBI()
	// 2 hours played today by this profile.
	mockDB.On("SumMediaPlayTimeForDayByProfile", mock.AnythingOfType("time.Time"), profileID).
		Return(int64(7200), nil)

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

	reason, err := tm.CheckBeforeLaunch()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daily playtime limit reached")
	assert.Equal(t, models.PlaytimeLimitReasonDaily, reason)
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

	reason, err := tm.CheckBeforeLaunch()
	require.NoError(t, err)
	assert.Empty(t, reason)
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

// swappableProvider is a LimitsProvider whose values tests mutate to
// simulate profile switches at runtime.
type swappableProvider struct {
	cur stubProvider
	mu  syncutil.Mutex
}

func (s *swappableProvider) set(p stubProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur = p
}

func (s *swappableProvider) get() stubProvider {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *swappableProvider) PlaytimeLimitsEnabled() bool       { return s.get().enabled }
func (s *swappableProvider) DailyLimit() time.Duration         { return s.get().daily }
func (s *swappableProvider) SessionLimit() time.Duration       { return s.get().session }
func (s *swappableProvider) WarningIntervals() []time.Duration { return s.get().warnings }
func (s *swappableProvider) ActiveProfileID() string           { return s.get().profileID }

// captureBroker records the method filter passed to Subscribe.
type captureBroker struct {
	ch      chan models.Notification
	methods []string
}

func (b *captureBroker) Subscribe(_ int, methods ...string) (notifChan <-chan models.Notification, id int) {
	b.methods = methods
	b.ch = make(chan models.Notification)
	return b.ch, 1
}

func (*captureBroker) Unsubscribe(int) {}

func TestStart_SubscribesToProfileNotifications(t *testing.T) {
	t.Parallel()

	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{},
		clockwork.NewFakeClockAt(time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)), newNoOpMockPlayer())
	b := &captureBroker{}
	tm.Start(b, nil)
	defer tm.Stop()

	// The broker filter must include profiles.active or profile switches
	// silently never reach the limits manager.
	assert.Contains(t, b.methods, models.NotificationProfilesActive)
	assert.Contains(t, b.methods, models.NotificationStarted)
	assert.Contains(t, b.methods, models.NotificationStopped)
}

func TestOnProfileChanged_SameProfileDoesNotResetSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{},
		clockwork.NewFakeClockAt(now), newNoOpMockPlayer())
	provider := &swappableProvider{}
	provider.set(stubProvider{enabled: true, session: time.Hour, profileID: "kid-a"})
	tm.SetLimitsProvider(provider)

	sessionStart := now.Add(-50 * time.Minute)
	tm.mu.Lock()
	tm.lastProfileID = "kid-a"
	tm.state = StateActive
	tm.sessionStart = sessionStart
	tm.sessionStartMono = time.Now().Add(-50 * time.Minute)
	tm.sessionCumulativeTime = 10 * time.Minute
	tm.mu.Unlock()

	// Rescanning your own profile card re-broadcasts profiles.active with
	// the same identity — the session must NOT restart, or rescanning
	// every 50 minutes defeats a 1h session limit.
	tm.onProfileChanged()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	assert.True(t, tm.sessionStart.Equal(sessionStart), "session start unchanged")
	assert.Equal(t, 10*time.Minute, tm.sessionCumulativeTime)
}

func TestOnProfileChanged_DifferentProfileResetsAndRepins(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{},
		clockwork.NewFakeClockAt(now), newNoOpMockPlayer())
	provider := &swappableProvider{}
	provider.set(stubProvider{enabled: true, session: 2 * time.Hour, profileID: "kid-b"})
	tm.SetLimitsProvider(provider)

	tm.mu.Lock()
	tm.lastProfileID = "kid-a"
	tm.state = StateActive
	tm.sessionStart = now.Add(-50 * time.Minute)
	tm.sessionStartMono = time.Now().Add(-50 * time.Minute)
	tm.sessionCumulativeTime = 10 * time.Minute
	tm.sessionLimits = &pinnedLimits{profileID: "kid-a", enabled: true, session: time.Hour}
	tm.mu.Unlock()

	tm.onProfileChanged()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	assert.Equal(t, "kid-b", tm.lastProfileID)
	assert.True(t, tm.sessionStart.Equal(now), "session restarts for the new person")
	assert.Equal(t, time.Duration(0), tm.sessionCumulativeTime)
	require.NotNil(t, tm.sessionLimits)
	assert.Equal(t, "kid-b", tm.sessionLimits.profileID, "running game re-pinned to the new profile")
	assert.Equal(t, 2*time.Hour, tm.sessionLimits.session)
}

func TestOnProfileChanged_DeactivationMidGameKeepsLaunchLimits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{},
		clockwork.NewFakeClockAt(now), newNoOpMockPlayer())
	provider := &swappableProvider{}
	provider.set(stubProvider{enabled: true, session: time.Hour, daily: 2 * time.Hour, profileID: "kid-a"})
	tm.SetLimitsProvider(provider)

	sessionStart := now.Add(-30 * time.Minute)
	tm.mu.Lock()
	tm.lastProfileID = "kid-a"
	tm.state = StateActive
	tm.sessionStart = sessionStart
	tm.sessionStartMono = time.Now().Add(-30 * time.Minute)
	tm.sessionLimits = &pinnedLimits{
		profileID: "kid-a", enabled: true, session: time.Hour, daily: 2 * time.Hour,
	}
	tm.mu.Unlock()

	// Scan a **profile.clear card mid-game: live limits drop to the shared
	// profile (disabled here), but the running game keeps its launch
	// profile's limits and identity.
	provider.set(stubProvider{enabled: false, profileID: ""})
	tm.onProfileChanged()

	tm.mu.Lock()
	assert.True(t, tm.sessionStart.Equal(sessionStart), "deactivation does not reset the session")
	assert.Empty(t, tm.lastProfileID)
	tm.mu.Unlock()

	assert.True(t, tm.effectiveEnabled(), "pinned enablement survives deactivation")
	assert.Equal(t, time.Hour, tm.effectiveSessionLimit())
	assert.Equal(t, 2*time.Hour, tm.effectiveDailyLimit())
	assert.Equal(t, "kid-a", tm.effectiveProfileID(), "daily usage stays scoped to the launch profile")

	// Once the game stops, pinning ends and live (shared) limits apply.
	tm.OnMediaStopped()
	assert.False(t, tm.effectiveEnabled())
	assert.Empty(t, tm.effectiveProfileID())
}

func TestOnProfileChanged_DeactivationInCooldownRetainsCumulative(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	clock := clockwork.NewFakeClockAt(now)
	tm := NewLimitsManager(&database.Database{}, nil, &config.Instance{}, clock, newNoOpMockPlayer())
	provider := &swappableProvider{}
	provider.set(stubProvider{enabled: false, profileID: ""})
	tm.SetLimitsProvider(provider)

	tm.mu.Lock()
	tm.lastProfileID = "kid-a"
	tm.state = StateCooldown
	tm.sessionCumulativeTime = 50 * time.Minute
	tm.lastStopTime = now.Add(-time.Minute)
	tm.mu.Unlock()

	// Stop the game, clear the profile, relaunch: cumulative session time
	// must survive the deactivation or the cooldown mechanism is
	// escapable with a **profile.clear card.
	tm.onProfileChanged()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	assert.Equal(t, StateCooldown, tm.state)
	assert.Equal(t, 50*time.Minute, tm.sessionCumulativeTime)
}
