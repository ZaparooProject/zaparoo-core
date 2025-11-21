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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
)

// Rule evaluates time limit policies and determines if continued play is allowed.
type Rule interface {
	// Evaluate checks if the current context violates this rule's time limit.
	// Returns:
	//   - allowed: true if play can continue, false if limit reached
	//   - remaining: time left before limit (0 if already exceeded)
	//   - reason: reason for violation (e.g., "daily", "session")
	Evaluate(ctx RuleContext) (bool, time.Duration, string)
}

// RuleContext provides time and usage information for rule evaluation.
type RuleContext struct {
	// CurrentTime is the current time for evaluation
	CurrentTime time.Time

	// SessionDuration is how long the current session has been running
	SessionDuration time.Duration

	// DailyUsageToday is the total time used today (including current session)
	DailyUsageToday time.Duration

	// ClockReliable indicates whether the system clock is trustworthy.
	// False when clock appears to be unset (e.g., year < 2024) or has jumped suspiciously.
	// Daily limits are only enforced when ClockReliable is true.
	ClockReliable bool
}

// SessionLimitRule enforces a maximum time per gaming session.
type SessionLimitRule struct {
	Limit time.Duration
}

// Evaluate checks if the session has exceeded the session limit.
func (r *SessionLimitRule) Evaluate(ctx RuleContext) (allowed bool, remaining time.Duration, reason string) {
	if r.Limit == 0 {
		return true, 0, ""
	}

	remaining = r.Limit - ctx.SessionDuration
	if remaining < 0 {
		return false, 0, models.PlaytimeLimitReasonSession
	}

	return true, remaining, ""
}

// DailyLimitRule enforces a maximum total play time per day.
type DailyLimitRule struct {
	Limit time.Duration
}

// Evaluate checks if today's total usage has exceeded the daily limit.
func (r *DailyLimitRule) Evaluate(ctx RuleContext) (allowed bool, remaining time.Duration, reason string) {
	if r.Limit == 0 {
		return true, 0, ""
	}

	// Graceful degradation: If system clock is unreliable (e.g., year is 1970),
	// we cannot accurately enforce daily limits. Skip enforcement to avoid
	// punishing users with legitimate clock issues (offline boot, no RTC chip).
	// Session limits still provide protection in this scenario.
	if !ctx.ClockReliable {
		return true, 0, ""
	}

	remaining = r.Limit - ctx.DailyUsageToday
	if remaining < 0 {
		return false, 0, models.PlaytimeLimitReasonDaily
	}

	return true, remaining, ""
}
