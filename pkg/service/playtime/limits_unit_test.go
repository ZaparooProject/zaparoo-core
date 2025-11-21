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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
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

			tm := NewLimitsManager(nil, nil, cfg, nil)

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

			tm := NewLimitsManager(nil, nil, cfg, nil)
			tm.enabled = tt.initial

			tm.SetEnabled(tt.set)

			got := tm.IsEnabled()
			assert.Equal(t, tt.want, got)
		})
	}
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

			tm := NewLimitsManager(nil, nil, cfg, nil)

			if !tt.sessionStartZero {
				tm.sessionStart = tm.clock.Now()
			}

			got := tm.isSessionActive()
			assert.Equal(t, tt.want, got)
		})
	}
}
