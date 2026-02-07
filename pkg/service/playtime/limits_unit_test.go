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

func TestCreateRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		playtime        config.Playtime
		wantRuleCount   int
		wantDailyRule   bool
		wantSessionRule bool
	}{
		{
			name: "both limits configured",
			playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "45m",
				},
			},
			wantRuleCount:   2,
			wantDailyRule:   true,
			wantSessionRule: true,
		},
		{
			name: "only daily limit",
			playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "2h",
					Session: "",
				},
			},
			wantRuleCount:   1,
			wantDailyRule:   true,
			wantSessionRule: false,
		},
		{
			name: "only session limit",
			playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "",
					Session: "45m",
				},
			},
			wantRuleCount:   1,
			wantDailyRule:   false,
			wantSessionRule: true,
		},
		{
			name: "no limits configured",
			playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "",
					Session: "",
				},
			},
			wantRuleCount:   0,
			wantDailyRule:   false,
			wantSessionRule: false,
		},
		{
			name: "invalid durations return no rules",
			playtime: config.Playtime{
				Limits: config.PlaytimeLimits{
					Daily:   "invalid",
					Session: "invalid",
				},
			},
			wantRuleCount:   0,
			wantDailyRule:   false,
			wantSessionRule: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := newTestConfig(t, &config.Values{ //nolint:exhaustruct // Only Playtime field needed for test
				Playtime: tt.playtime,
			})

			tm := NewLimitsManager(nil, nil, cfg, nil, nil)

			rules := tm.createRules()

			assert.Len(t, rules, tt.wantRuleCount)

			// Check rule types
			hasDaily := false
			hasSession := false
			for _, rule := range rules {
				switch rule.(type) {
				case *DailyLimitRule:
					hasDaily = true
				case *SessionLimitRule:
					hasSession = true
				}
			}

			assert.Equal(t, tt.wantDailyRule, hasDaily, "daily rule presence mismatch")
			assert.Equal(t, tt.wantSessionRule, hasSession, "session rule presence mismatch")
		})
	}
}

func TestSetEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		initial bool
		set     bool
		want    bool
	}{
		{
			name:    "enable from disabled",
			initial: false,
			set:     true,
			want:    true,
		},
		{
			name:    "disable from enabled",
			initial: true,
			set:     false,
			want:    false,
		},
		{
			name:    "enable when already enabled",
			initial: true,
			set:     true,
			want:    true,
		},
		{
			name:    "disable when already disabled",
			initial: false,
			set:     false,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := newTestConfig(t, &config.Values{}) //nolint:exhaustruct // Default config is fine

			tm := NewLimitsManager(nil, nil, cfg, nil, nil)
			tm.enabled = tt.initial

			tm.SetEnabled(tt.set)

			got := tm.IsEnabled()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetEnabled_SessionReset(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, &config.Values{}) //nolint:exhaustruct // Default config is fine
	clock := clockwork.NewFakeClock()

	tm := NewLimitsManager(nil, nil, cfg, clock, nil)
	tm.enabled = true

	// Simulate active session with cumulative time
	tm.mu.Lock()
	tm.state = StateActive
	tm.sessionStart = clock.Now()
	tm.sessionStartMono = time.Now()
	tm.sessionCumulativeTime = 15 * time.Minute
	tm.sessionStartReliable = true
	tm.mu.Unlock()

	// Verify session is active
	assert.True(t, tm.isSessionActive())
	assert.Equal(t, StateActive, tm.state)

	// Disable limits - should reset session
	tm.SetEnabled(false)

	// Verify session was reset
	tm.mu.Lock()
	assert.Equal(t, StateReset, tm.state, "state should be reset")
	assert.True(t, tm.sessionStart.IsZero(), "sessionStart should be cleared")
	assert.True(t, tm.sessionStartMono.IsZero(), "sessionStartMono should be cleared")
	assert.Equal(t, time.Duration(0), tm.sessionCumulativeTime, "cumulative time should be cleared")
	assert.True(t, tm.lastStopTime.IsZero(), "lastStopTime should be cleared")
	assert.False(t, tm.sessionStartReliable, "sessionStartReliable should be false")
	tm.mu.Unlock()

	// Verify not active anymore
	assert.False(t, tm.isSessionActive())

	// Re-enable - session should still be reset
	tm.SetEnabled(true)

	tm.mu.Lock()
	assert.Equal(t, StateReset, tm.state, "state should remain reset after re-enabling")
	assert.Equal(t, time.Duration(0), tm.sessionCumulativeTime, "cumulative time should still be 0")
	tm.mu.Unlock()
}

func TestSetEnabled_CooldownReset(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, &config.Values{}) //nolint:exhaustruct // Default config is fine
	clock := clockwork.NewFakeClock()

	tm := NewLimitsManager(nil, nil, cfg, clock, nil)
	tm.enabled = true

	// Simulate cooldown state with cumulative time
	tm.mu.Lock()
	tm.state = StateCooldown
	tm.sessionStart = clock.Now().Add(-30 * time.Minute)
	tm.sessionCumulativeTime = 30 * time.Minute
	tm.lastStopTime = clock.Now()
	tm.mu.Unlock()

	// Verify in cooldown
	assert.Equal(t, StateCooldown, tm.state)

	// Disable limits - should reset session (clearing cooldown)
	tm.SetEnabled(false)

	// Verify session was reset
	tm.mu.Lock()
	assert.Equal(t, StateReset, tm.state, "state should be reset (cooldown cleared)")
	assert.Equal(t, time.Duration(0), tm.sessionCumulativeTime, "cumulative time should be cleared")
	assert.True(t, tm.sessionStart.IsZero(), "sessionStart should be cleared")
	assert.True(t, tm.lastStopTime.IsZero(), "lastStopTime should be cleared")
	tm.mu.Unlock()
}

func TestCooldownTimer_AutomaticReset(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, &config.Values{}) //nolint:exhaustruct // Default config is fine
	clock := clockwork.NewFakeClock()

	tm := NewLimitsManager(nil, nil, cfg, clock, nil)
	defer tm.Stop() // Stop manager to clean up goroutines

	tm.enabled = true

	// Simulate entering cooldown with 20 minute timeout
	tm.mu.Lock()
	tm.state = StateCooldown
	tm.sessionCumulativeTime = 30 * time.Minute
	tm.lastStopTime = clock.Now()
	tm.sessionResetTimeout = 20 * time.Minute
	tm.cooldownTimer = clock.NewTimer(20 * time.Minute)
	tm.mu.Unlock()

	go tm.cooldownTimerLoop()

	// Verify still in cooldown
	assert.Equal(t, StateCooldown, tm.state)

	// Advance clock past timeout
	clock.Advance(21 * time.Minute)

	// Wait for state transition with timeout
	assert.Eventually(t, func() bool {
		tm.mu.Lock()
		defer tm.mu.Unlock()
		return tm.state == StateReset
	}, 100*time.Millisecond, 5*time.Millisecond, "state should be reset after timer expires")

	// Verify final state
	tm.mu.Lock()
	assert.Equal(t, StateReset, tm.state, "state should be reset after timer expires")
	assert.Equal(t, time.Duration(0), tm.sessionCumulativeTime, "cumulative time should be cleared")
	assert.Nil(t, tm.cooldownTimer, "timer should be cleared")
	tm.mu.Unlock()
}

func TestCooldownTimer_CancelledByNewGame(t *testing.T) {
	t.Parallel()

	// Enable playtime limits in config
	enabled := true
	cfg := newTestConfig(t, &config.Values{
		Playtime: config.Playtime{
			Limits: config.PlaytimeLimits{
				Enabled: &enabled,
			},
		},
	})
	clock := clockwork.NewFakeClock()

	tm := NewLimitsManager(nil, nil, cfg, clock, nil)
	defer tm.Stop() // Stop manager to clean up goroutines

	tm.enabled = true

	// Enter cooldown with timer running
	tm.mu.Lock()
	tm.state = StateCooldown
	tm.sessionCumulativeTime = 15 * time.Minute
	tm.lastStopTime = clock.Now()
	tm.sessionResetTimeout = 20 * time.Minute
	tm.cooldownTimer = clock.NewTimer(20 * time.Minute)
	originalTimer := tm.cooldownTimer
	tm.mu.Unlock()

	// Verify in cooldown
	assert.Equal(t, StateCooldown, tm.state)

	// Advance clock only 10 minutes (before timeout)
	clock.Advance(10 * time.Minute)

	// Manually cancel timer (simulating what OnMediaStarted does)
	// We can't call OnMediaStarted() because it starts checkLoop which needs a database
	tm.mu.Lock()
	if tm.cooldownTimer != nil {
		tm.cooldownTimer.Stop()
		tm.cooldownTimer = nil
	}
	// Transition to active (what OnMediaStarted would do)
	tm.transitionTo(StateActive)
	tm.sessionStart = clock.Now()
	tm.mu.Unlock()

	// Verify timer was cancelled and session resumed
	tm.mu.Lock()
	assert.Equal(t, StateActive, tm.state, "state should be active")
	assert.Equal(t, 15*time.Minute, tm.sessionCumulativeTime, "cumulative time should be preserved")
	assert.Nil(t, tm.cooldownTimer, "timer should be cancelled")
	tm.mu.Unlock()

	// Advance clock further - timer should not fire (it was cancelled)
	clock.Advance(15 * time.Minute)
	time.Sleep(10 * time.Millisecond)

	// Verify still in active state (timer didn't fire and reset)
	tm.mu.Lock()
	assert.Equal(t, StateActive, tm.state, "state should still be active (timer was cancelled)")
	assert.Equal(t, 15*time.Minute, tm.sessionCumulativeTime, "cumulative time unchanged")
	tm.mu.Unlock()

	// For test cleanup: stop the original timer to avoid leaks
	originalTimer.Stop()
}

func TestIsSessionActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		sessionStartZero bool
		want             bool
	}{
		{
			name:             "no active session",
			sessionStartZero: true,
			want:             false,
		},
		{
			name:             "active session",
			sessionStartZero: false,
			want:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := newTestConfig(t, &config.Values{}) //nolint:exhaustruct // Default config is fine

			tm := NewLimitsManager(nil, nil, cfg, nil, nil)

			if !tt.sessionStartZero {
				tm.sessionStart = tm.clock.Now()
			}

			got := tm.isSessionActive()
			assert.Equal(t, tt.want, got)
		})
	}
}
