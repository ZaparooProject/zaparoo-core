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

	"pgregory.net/rapid"
)

// ============================================================================
// Generators
// ============================================================================

// durationGen generates realistic time durations (0 to 24 hours).
func durationGen() *rapid.Generator[time.Duration] {
	return rapid.Custom(func(t *rapid.T) time.Duration {
		// Generate in seconds, then convert (0 to 24 hours)
		seconds := rapid.Int64Range(0, 24*60*60).Draw(t, "seconds")
		return time.Duration(seconds) * time.Second
	})
}

// limitDurationGen generates limit durations (0 to 8 hours, where 0 means disabled).
func limitDurationGen() *rapid.Generator[time.Duration] {
	return rapid.Custom(func(t *rapid.T) time.Duration {
		// Common limits: 0 (disabled), or 15min to 8 hours
		seconds := rapid.Int64Range(0, 8*60*60).Draw(t, "limitSeconds")
		return time.Duration(seconds) * time.Second
	})
}

// ruleContextGen generates valid RuleContext values.
func ruleContextGen() *rapid.Generator[RuleContext] {
	return rapid.Custom(func(t *rapid.T) RuleContext {
		return RuleContext{
			CurrentTime:     time.Now(),
			SessionDuration: durationGen().Draw(t, "sessionDuration"),
			DailyUsageToday: durationGen().Draw(t, "dailyUsageToday"),
			ClockReliable:   rapid.Bool().Draw(t, "clockReliable"),
		}
	})
}

// ============================================================================
// SessionState Property Tests
// ============================================================================

// TestPropertySessionStateStringNeverPanics verifies String() never panics for valid states.
func TestPropertySessionStateStringNeverPanics(t *testing.T) {
	t.Parallel()

	// Test all valid states
	states := []SessionState{StateReset, StateActive, StateCooldown}
	for _, state := range states {
		str := state.String()
		if str == "" {
			t.Fatalf("String() returned empty for state %d", state)
		}
	}
}

// TestPropertySessionStateStringDeterministic verifies same state produces same string.
func TestPropertySessionStateStringDeterministic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		state := SessionState(rapid.IntRange(0, 2).Draw(t, "state"))
		str1 := state.String()
		str2 := state.String()

		if str1 != str2 {
			t.Fatalf("String() not deterministic: %q vs %q for state %d", str1, str2, state)
		}
	})
}

// ============================================================================
// SessionLimitRule Property Tests
// ============================================================================

// TestPropertySessionLimitRuleZeroMeansDisabled verifies limit=0 always allows.
func TestPropertySessionLimitRuleZeroMeansDisabled(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ctx := ruleContextGen().Draw(t, "ctx")
		rule := &SessionLimitRule{Limit: 0}

		allowed, remaining, reason := rule.Evaluate(ctx)

		if !allowed {
			t.Fatal("Zero limit should always allow")
		}
		if remaining != 0 {
			t.Fatalf("Zero limit should return 0 remaining, got %v", remaining)
		}
		if reason != "" {
			t.Fatalf("Zero limit should return empty reason, got %q", reason)
		}
	})
}

// TestPropertySessionLimitRuleRemainingNeverNegative verifies remaining >= 0.
func TestPropertySessionLimitRuleRemainingNeverNegative(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		limit := limitDurationGen().Draw(t, "limit")
		ctx := ruleContextGen().Draw(t, "ctx")
		rule := &SessionLimitRule{Limit: limit}

		_, remaining, _ := rule.Evaluate(ctx)

		if remaining < 0 {
			t.Fatalf("Remaining time should never be negative: %v", remaining)
		}
	})
}

// TestPropertySessionLimitRuleExceededBlocksPlay verifies limit exceeded blocks play.
func TestPropertySessionLimitRuleExceededBlocksPlay(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a positive limit
		limitSeconds := rapid.Int64Range(1, 8*60*60).Draw(t, "limitSeconds")
		limit := time.Duration(limitSeconds) * time.Second

		// Generate session duration that exceeds limit
		excessSeconds := rapid.Int64Range(1, 60*60).Draw(t, "excessSeconds")
		sessionDuration := limit + time.Duration(excessSeconds)*time.Second

		ctx := RuleContext{
			CurrentTime:     time.Now(),
			SessionDuration: sessionDuration,
			DailyUsageToday: 0,
			ClockReliable:   true,
		}
		rule := &SessionLimitRule{Limit: limit}

		allowed, remaining, reason := rule.Evaluate(ctx)

		if allowed {
			t.Fatalf("Should block when session (%v) exceeds limit (%v)", sessionDuration, limit)
		}
		if remaining != 0 {
			t.Fatalf("Remaining should be 0 when exceeded, got %v", remaining)
		}
		if reason != "session" {
			t.Fatalf("Reason should be 'session', got %q", reason)
		}
	})
}

// TestPropertySessionLimitRuleWithinLimitAllows verifies within limit allows play.
func TestPropertySessionLimitRuleWithinLimitAllows(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a positive limit
		limitSeconds := rapid.Int64Range(60, 8*60*60).Draw(t, "limitSeconds")
		limit := time.Duration(limitSeconds) * time.Second

		// Generate session duration within limit
		sessionSeconds := rapid.Int64Range(0, limitSeconds).Draw(t, "sessionSeconds")
		sessionDuration := time.Duration(sessionSeconds) * time.Second

		ctx := RuleContext{
			CurrentTime:     time.Now(),
			SessionDuration: sessionDuration,
			DailyUsageToday: 0,
			ClockReliable:   true,
		}
		rule := &SessionLimitRule{Limit: limit}

		allowed, remaining, reason := rule.Evaluate(ctx)

		if !allowed {
			t.Fatalf("Should allow when session (%v) is within limit (%v)", sessionDuration, limit)
		}
		expectedRemaining := limit - sessionDuration
		if remaining != expectedRemaining {
			t.Fatalf("Remaining mismatch: got %v, expected %v", remaining, expectedRemaining)
		}
		if reason != "" {
			t.Fatalf("Reason should be empty when allowed, got %q", reason)
		}
	})
}

// ============================================================================
// DailyLimitRule Property Tests
// ============================================================================

// TestPropertyDailyLimitRuleZeroMeansDisabled verifies limit=0 always allows.
func TestPropertyDailyLimitRuleZeroMeansDisabled(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ctx := ruleContextGen().Draw(t, "ctx")
		rule := &DailyLimitRule{Limit: 0}

		allowed, remaining, reason := rule.Evaluate(ctx)

		if !allowed {
			t.Fatal("Zero limit should always allow")
		}
		if remaining != 0 {
			t.Fatalf("Zero limit should return 0 remaining, got %v", remaining)
		}
		if reason != "" {
			t.Fatalf("Zero limit should return empty reason, got %q", reason)
		}
	})
}

// TestPropertyDailyLimitRuleUnreliableClockBypass verifies unreliable clock bypasses limit.
func TestPropertyDailyLimitRuleUnreliableClockBypass(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate any positive limit
		limitSeconds := rapid.Int64Range(1, 8*60*60).Draw(t, "limitSeconds")
		limit := time.Duration(limitSeconds) * time.Second

		// Generate any usage amount (even exceeding limit)
		usageSeconds := rapid.Int64Range(0, 24*60*60).Draw(t, "usageSeconds")
		dailyUsage := time.Duration(usageSeconds) * time.Second

		ctx := RuleContext{
			CurrentTime:     time.Now(),
			SessionDuration: 0,
			DailyUsageToday: dailyUsage,
			ClockReliable:   false, // Unreliable clock
		}
		rule := &DailyLimitRule{Limit: limit}

		allowed, _, reason := rule.Evaluate(ctx)

		if !allowed {
			t.Fatal("Unreliable clock should bypass daily limit (graceful degradation)")
		}
		if reason != "" {
			t.Fatalf("Reason should be empty when clock unreliable, got %q", reason)
		}
	})
}

// TestPropertyDailyLimitRuleRemainingNeverNegative verifies remaining >= 0.
func TestPropertyDailyLimitRuleRemainingNeverNegative(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		limit := limitDurationGen().Draw(t, "limit")
		ctx := ruleContextGen().Draw(t, "ctx")
		rule := &DailyLimitRule{Limit: limit}

		_, remaining, _ := rule.Evaluate(ctx)

		if remaining < 0 {
			t.Fatalf("Remaining time should never be negative: %v", remaining)
		}
	})
}

// TestPropertyDailyLimitRuleExceededBlocksPlay verifies limit exceeded blocks with reliable clock.
func TestPropertyDailyLimitRuleExceededBlocksPlay(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a positive limit
		limitSeconds := rapid.Int64Range(1, 8*60*60).Draw(t, "limitSeconds")
		limit := time.Duration(limitSeconds) * time.Second

		// Generate usage that exceeds limit
		excessSeconds := rapid.Int64Range(1, 60*60).Draw(t, "excessSeconds")
		dailyUsage := limit + time.Duration(excessSeconds)*time.Second

		ctx := RuleContext{
			CurrentTime:     time.Now(),
			SessionDuration: 0,
			DailyUsageToday: dailyUsage,
			ClockReliable:   true, // Reliable clock
		}
		rule := &DailyLimitRule{Limit: limit}

		allowed, remaining, reason := rule.Evaluate(ctx)

		if allowed {
			t.Fatalf("Should block when usage (%v) exceeds limit (%v)", dailyUsage, limit)
		}
		if remaining != 0 {
			t.Fatalf("Remaining should be 0 when exceeded, got %v", remaining)
		}
		if reason != "daily" {
			t.Fatalf("Reason should be 'daily', got %q", reason)
		}
	})
}

// ============================================================================
// RuleContext Property Tests
// ============================================================================

// TestPropertyRuleContextDurationsNonNegative verifies generated durations are valid.
func TestPropertyRuleContextDurationsNonNegative(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ctx := ruleContextGen().Draw(t, "ctx")

		if ctx.SessionDuration < 0 {
			t.Fatalf("SessionDuration should be non-negative: %v", ctx.SessionDuration)
		}
		if ctx.DailyUsageToday < 0 {
			t.Fatalf("DailyUsageToday should be non-negative: %v", ctx.DailyUsageToday)
		}
	})
}

// ============================================================================
// MinimumViableSession Property Tests
// ============================================================================

// TestPropertyMinimumViableSessionIsPositive verifies the constant is positive.
func TestPropertyMinimumViableSessionIsPositive(t *testing.T) {
	t.Parallel()

	if MinimumViableSession <= 0 {
		t.Fatalf("MinimumViableSession should be positive: %v", MinimumViableSession)
	}
}

// TestPropertyMinimumViableSessionIsReasonable verifies the constant is reasonable.
func TestPropertyMinimumViableSessionIsReasonable(t *testing.T) {
	t.Parallel()

	// Should be at least 30 seconds
	if MinimumViableSession < 30*time.Second {
		t.Fatalf("MinimumViableSession too short: %v", MinimumViableSession)
	}

	// Should be at most 5 minutes (don't block too aggressively)
	if MinimumViableSession > 5*time.Minute {
		t.Fatalf("MinimumViableSession too long: %v", MinimumViableSession)
	}
}

// ============================================================================
// DefaultSessionResetTimeout Property Tests
// ============================================================================

// TestPropertyDefaultSessionResetTimeoutIsPositive verifies the constant is positive.
func TestPropertyDefaultSessionResetTimeoutIsPositive(t *testing.T) {
	t.Parallel()

	if DefaultSessionResetTimeout <= 0 {
		t.Fatalf("DefaultSessionResetTimeout should be positive: %v", DefaultSessionResetTimeout)
	}
}

// TestPropertyDefaultSessionResetTimeoutIsReasonable verifies the constant is reasonable.
func TestPropertyDefaultSessionResetTimeoutIsReasonable(t *testing.T) {
	t.Parallel()

	// Should be at least 5 minutes (allow bathroom breaks)
	if DefaultSessionResetTimeout < 5*time.Minute {
		t.Fatalf("DefaultSessionResetTimeout too short: %v", DefaultSessionResetTimeout)
	}

	// Should be at most 2 hours (don't carry session forever)
	if DefaultSessionResetTimeout > 2*time.Hour {
		t.Fatalf("DefaultSessionResetTimeout too long: %v", DefaultSessionResetTimeout)
	}
}

// ============================================================================
// Combined Rule Property Tests
// ============================================================================

// TestPropertyBothRulesCanBeEvaluated verifies both rules can evaluate the same context.
func TestPropertyBothRulesCanBeEvaluated(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		ctx := ruleContextGen().Draw(t, "ctx")
		sessionLimit := limitDurationGen().Draw(t, "sessionLimit")
		dailyLimit := limitDurationGen().Draw(t, "dailyLimit")

		sessionRule := &SessionLimitRule{Limit: sessionLimit}
		dailyRule := &DailyLimitRule{Limit: dailyLimit}

		// Both should evaluate without panicking
		sessionAllowed, sessionRemaining, _ := sessionRule.Evaluate(ctx)
		dailyAllowed, dailyRemaining, _ := dailyRule.Evaluate(ctx)

		// Results should be consistent
		if sessionRemaining < 0 {
			t.Fatalf("Session remaining negative: %v", sessionRemaining)
		}
		if dailyRemaining < 0 {
			t.Fatalf("Daily remaining negative: %v", dailyRemaining)
		}

		// If allowed, remaining should match calculation
		if sessionAllowed && sessionLimit > 0 && ctx.SessionDuration < sessionLimit {
			expected := sessionLimit - ctx.SessionDuration
			if sessionRemaining != expected {
				t.Fatalf("Session remaining mismatch: got %v, expected %v", sessionRemaining, expected)
			}
		}
		if dailyAllowed && dailyLimit > 0 && ctx.ClockReliable && ctx.DailyUsageToday < dailyLimit {
			expected := dailyLimit - ctx.DailyUsageToday
			if dailyRemaining != expected {
				t.Fatalf("Daily remaining mismatch: got %v, expected %v", dailyRemaining, expected)
			}
		}
	})
}

// TestPropertyMinimumRemainingCalculation verifies minimum remaining time calculation.
func TestPropertyMinimumRemainingCalculation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate limits where both are active
		sessionLimitSec := rapid.Int64Range(60, 4*60*60).Draw(t, "sessionLimitSec")
		dailyLimitSec := rapid.Int64Range(60, 4*60*60).Draw(t, "dailyLimitSec")
		sessionLimit := time.Duration(sessionLimitSec) * time.Second
		dailyLimit := time.Duration(dailyLimitSec) * time.Second

		// Generate usage within both limits
		sessionSec := rapid.Int64Range(0, sessionLimitSec-1).Draw(t, "sessionSec")
		dailySec := rapid.Int64Range(0, dailyLimitSec-1).Draw(t, "dailySec")
		sessionDuration := time.Duration(sessionSec) * time.Second
		dailyUsage := time.Duration(dailySec) * time.Second

		ctx := RuleContext{
			CurrentTime:     time.Now(),
			SessionDuration: sessionDuration,
			DailyUsageToday: dailyUsage,
			ClockReliable:   true,
		}

		sessionRule := &SessionLimitRule{Limit: sessionLimit}
		dailyRule := &DailyLimitRule{Limit: dailyLimit}

		_, sessionRemaining, _ := sessionRule.Evaluate(ctx)
		_, dailyRemaining, _ := dailyRule.Evaluate(ctx)

		// Calculate minimum
		minRemaining := sessionRemaining
		if dailyRemaining > 0 && dailyRemaining < minRemaining {
			minRemaining = dailyRemaining
		}

		// Minimum should be positive (both are within limits)
		if minRemaining <= 0 {
			t.Fatalf("Minimum remaining should be positive: %v (session: %v, daily: %v)",
				minRemaining, sessionRemaining, dailyRemaining)
		}
	})
}
