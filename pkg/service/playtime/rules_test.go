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

	"github.com/stretchr/testify/assert"
)

func TestSessionLimitRule_Evaluate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		wantReason      string
		limit           time.Duration
		sessionDuration time.Duration
		wantRemaining   time.Duration
		wantAllowed     bool
	}{
		{
			name:            "no limit configured",
			limit:           0,
			sessionDuration: 1 * time.Hour,
			wantAllowed:     true,
			wantRemaining:   0,
			wantReason:      "",
		},
		{
			name:            "within session limit",
			limit:           45 * time.Minute,
			sessionDuration: 30 * time.Minute,
			wantAllowed:     true,
			wantRemaining:   15 * time.Minute,
			wantReason:      "",
		},
		{
			name:            "exactly at session limit",
			limit:           45 * time.Minute,
			sessionDuration: 45 * time.Minute,
			wantAllowed:     true,
			wantRemaining:   0,
			wantReason:      "",
		},
		{
			name:            "exceeded session limit",
			limit:           45 * time.Minute,
			sessionDuration: 50 * time.Minute,
			wantAllowed:     false,
			wantRemaining:   0,
			wantReason:      "session",
		},
		{
			name:            "just started session",
			limit:           45 * time.Minute,
			sessionDuration: 0,
			wantAllowed:     true,
			wantRemaining:   45 * time.Minute,
			wantReason:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := &SessionLimitRule{Limit: tt.limit}
			ctx := RuleContext{
				CurrentTime:     time.Now(),
				SessionDuration: tt.sessionDuration,
				DailyUsageToday: 0,
			}

			allowed, remaining, reason := rule.Evaluate(ctx)

			assert.Equal(t, tt.wantAllowed, allowed, "allowed mismatch")
			assert.Equal(t, tt.wantRemaining, remaining, "remaining mismatch")
			assert.Equal(t, tt.wantReason, reason, "reason mismatch")
		})
	}
}

func TestDailyLimitRule_Evaluate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		wantReason      string
		limit           time.Duration
		dailyUsageToday time.Duration
		wantRemaining   time.Duration
		wantAllowed     bool
	}{
		{
			name:            "no limit configured",
			limit:           0,
			dailyUsageToday: 5 * time.Hour,
			wantAllowed:     true,
			wantRemaining:   0,
			wantReason:      "",
		},
		{
			name:            "within daily limit",
			limit:           2 * time.Hour,
			dailyUsageToday: 1 * time.Hour,
			wantAllowed:     true,
			wantRemaining:   1 * time.Hour,
			wantReason:      "",
		},
		{
			name:            "exactly at daily limit",
			limit:           2 * time.Hour,
			dailyUsageToday: 2 * time.Hour,
			wantAllowed:     true,
			wantRemaining:   0,
			wantReason:      "",
		},
		{
			name:            "exceeded daily limit",
			limit:           2 * time.Hour,
			dailyUsageToday: 3 * time.Hour,
			wantAllowed:     false,
			wantRemaining:   0,
			wantReason:      "daily",
		},
		{
			name:            "no usage yet today",
			limit:           2 * time.Hour,
			dailyUsageToday: 0,
			wantAllowed:     true,
			wantRemaining:   2 * time.Hour,
			wantReason:      "",
		},
		{
			name:            "just below daily limit",
			limit:           2 * time.Hour,
			dailyUsageToday: 119 * time.Minute,
			wantAllowed:     true,
			wantRemaining:   1 * time.Minute,
			wantReason:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rule := &DailyLimitRule{Limit: tt.limit}
			ctx := RuleContext{
				CurrentTime:     time.Now(),
				SessionDuration: 0,
				DailyUsageToday: tt.dailyUsageToday,
			}

			allowed, remaining, reason := rule.Evaluate(ctx)

			assert.Equal(t, tt.wantAllowed, allowed, "allowed mismatch")
			assert.Equal(t, tt.wantRemaining, remaining, "remaining mismatch")
			assert.Equal(t, tt.wantReason, reason, "reason mismatch")
		})
	}
}

func TestRuleContext_MultipleScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		wantBlockingReason string
		sessionLimit       time.Duration
		dailyLimit         time.Duration
		sessionDuration    time.Duration
		dailyUsageToday    time.Duration
		wantMinRemaining   time.Duration
		wantSessionAllowed bool
		wantDailyAllowed   bool
	}{
		{
			name:               "session limit reached first",
			sessionLimit:       45 * time.Minute,
			dailyLimit:         2 * time.Hour,
			sessionDuration:    46 * time.Minute,
			dailyUsageToday:    1 * time.Hour,
			wantSessionAllowed: false,
			wantDailyAllowed:   true,
			wantMinRemaining:   0,
			wantBlockingReason: "session",
		},
		{
			name:               "daily limit reached first",
			sessionLimit:       45 * time.Minute,
			dailyLimit:         2 * time.Hour,
			sessionDuration:    30 * time.Minute,
			dailyUsageToday:    121 * time.Minute,
			wantSessionAllowed: true,
			wantDailyAllowed:   false,
			wantMinRemaining:   0,
			wantBlockingReason: "daily",
		},
		{
			name:               "both limits OK, session is tighter",
			sessionLimit:       45 * time.Minute,
			dailyLimit:         2 * time.Hour,
			sessionDuration:    30 * time.Minute,
			dailyUsageToday:    60 * time.Minute,
			wantSessionAllowed: true,
			wantDailyAllowed:   true,
			wantMinRemaining:   15 * time.Minute,
			wantBlockingReason: "",
		},
		{
			name:               "both limits OK, daily is tighter",
			sessionLimit:       45 * time.Minute,
			dailyLimit:         90 * time.Minute,
			sessionDuration:    20 * time.Minute,
			dailyUsageToday:    80 * time.Minute,
			wantSessionAllowed: true,
			wantDailyAllowed:   true,
			wantMinRemaining:   10 * time.Minute,
			wantBlockingReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sessionRule := &SessionLimitRule{Limit: tt.sessionLimit}
			dailyRule := &DailyLimitRule{Limit: tt.dailyLimit}

			ctx := RuleContext{
				CurrentTime:     time.Now(),
				SessionDuration: tt.sessionDuration,
				DailyUsageToday: tt.dailyUsageToday,
			}

			sessionAllowed, sessionRemaining, sessionReason := sessionRule.Evaluate(ctx)
			dailyAllowed, dailyRemaining, dailyReason := dailyRule.Evaluate(ctx)

			assert.Equal(t, tt.wantSessionAllowed, sessionAllowed, "session allowed mismatch")
			assert.Equal(t, tt.wantDailyAllowed, dailyAllowed, "daily allowed mismatch")

			// Find the blocking rule if any
			if !sessionAllowed {
				assert.Equal(t, tt.wantBlockingReason, sessionReason, "blocking reason mismatch")
			} else if !dailyAllowed {
				assert.Equal(t, tt.wantBlockingReason, dailyReason, "blocking reason mismatch")
			}

			// Find minimum remaining time
			if sessionAllowed && dailyAllowed {
				minRemaining := sessionRemaining
				if dailyRemaining > 0 && dailyRemaining < minRemaining {
					minRemaining = dailyRemaining
				}
				assert.Equal(t, tt.wantMinRemaining, minRemaining, "minimum remaining time mismatch")
			}
		})
	}
}
