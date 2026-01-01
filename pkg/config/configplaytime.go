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

package config

import (
	"fmt"
	"time"
)

// Playtime configures play time tracking and limits.
type Playtime struct {
	Retention *int           `toml:"retention,omitempty"`
	Limits    PlaytimeLimits `toml:"limits,omitempty"`
}

// PlaytimeLimits configures time limits and warnings for gameplay sessions.
type PlaytimeLimits struct {
	Enabled      *bool    `toml:"enabled,omitempty"`
	Daily        string   `toml:"daily,omitempty"`
	Session      string   `toml:"session,omitempty"`
	SessionReset *string  `toml:"session_reset,omitempty"`
	Warnings     []string `toml:"warnings,omitempty,multiline"`
}

// PlaytimeRetention returns the number of days to retain play time history.
// Returns 0 if cleanup is disabled, or 365 (1 year) by default.
func (c *Instance) PlaytimeRetention() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Playtime.Retention == nil {
		return 365 // Default: keep 365 days (1 year) of play time history
	}
	return *c.vals.Playtime.Retention
}

// PlaytimeLimitsEnabled returns true if play time limits are enabled.
func (c *Instance) PlaytimeLimitsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Playtime.Limits.Enabled == nil {
		return false
	}
	return *c.vals.Playtime.Limits.Enabled
}

// DailyLimit returns the daily time limit as a duration.
// Returns 0 if not configured or if the duration cannot be parsed.
func (c *Instance) DailyLimit() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Playtime.Limits.Daily == "" {
		return 0
	}
	d, err := time.ParseDuration(c.vals.Playtime.Limits.Daily)
	if err != nil {
		return 0
	}
	return d
}

// SessionLimit returns the per-session time limit as a duration.
// Returns 0 if not configured or if the duration cannot be parsed.
func (c *Instance) SessionLimit() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Playtime.Limits.Session == "" {
		return 0
	}
	d, err := time.ParseDuration(c.vals.Playtime.Limits.Session)
	if err != nil {
		return 0
	}
	return d
}

// WarningIntervals returns the warning intervals as durations.
// Returns default intervals [5m, 2m, 1m] if not configured.
// Skips any intervals that cannot be parsed.
func (c *Instance) WarningIntervals() []time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Default warning intervals
	if len(c.vals.Playtime.Limits.Warnings) == 0 {
		return []time.Duration{5 * time.Minute, 2 * time.Minute, 1 * time.Minute}
	}

	intervals := make([]time.Duration, 0, len(c.vals.Playtime.Limits.Warnings))
	for _, s := range c.vals.Playtime.Limits.Warnings {
		d, err := time.ParseDuration(s)
		if err == nil && d > 0 {
			intervals = append(intervals, d)
		}
	}
	return intervals
}

// SetPlaytimeLimitsEnabled enables or disables playtime limits.
func (c *Instance) SetPlaytimeLimitsEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Playtime.Limits.Enabled = &enabled
}

// SetDailyLimit sets the daily time limit from a duration string (e.g., "2h30m").
// Returns an error if the duration string is invalid.
// Pass empty string to disable daily limit.
func (c *Instance) SetDailyLimit(duration string) error {
	if duration != "" {
		_, err := time.ParseDuration(duration)
		if err != nil {
			return fmt.Errorf("invalid daily limit duration: %w", err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Playtime.Limits.Daily = duration
	return nil
}

// SetSessionLimit sets the session time limit from a duration string (e.g., "45m").
// Returns an error if the duration string is invalid.
// Pass empty string to disable session limit.
func (c *Instance) SetSessionLimit(duration string) error {
	if duration != "" {
		_, err := time.ParseDuration(duration)
		if err != nil {
			return fmt.Errorf("invalid session limit duration: %w", err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Playtime.Limits.Session = duration
	return nil
}

// SetWarningIntervals sets the warning intervals from duration strings (e.g., ["10m", "5m", "2m"]).
// Returns an error if any duration string is invalid.
// Pass empty slice to use defaults [5m, 2m, 1m].
func (c *Instance) SetWarningIntervals(intervals []string) error {
	// Validate all intervals before applying
	for _, interval := range intervals {
		if interval != "" {
			_, err := time.ParseDuration(interval)
			if err != nil {
				return fmt.Errorf("invalid warning interval: %w", err)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Playtime.Limits.Warnings = intervals
	return nil
}

// SetPlaytimeRetention sets the number of days to retain play time history.
// Pass 0 to disable cleanup.
func (c *Instance) SetPlaytimeRetention(days int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Playtime.Retention = &days
}

// SessionResetTimeout returns the idle timeout before a session resets.
// Returns 20 minutes by default if not configured (nil).
// Returns 0 if explicitly set to "0" (no timeout, session never resets).
func (c *Instance) SessionResetTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Playtime.Limits.SessionReset == nil {
		return 20 * time.Minute // Default: 20 minutes
	}
	d, err := time.ParseDuration(*c.vals.Playtime.Limits.SessionReset)
	if err != nil {
		return 20 * time.Minute // Fallback to default on parse error
	}
	return d
}

// SetSessionResetTimeout sets the idle timeout before a session resets (e.g., "20m", "1h", "0").
// Returns an error if the duration string is invalid.
// Pass nil to use default (20 minutes).
// Pass "0" to disable session reset timeout.
func (c *Instance) SetSessionResetTimeout(duration *string) error {
	if duration != nil && *duration != "" {
		_, err := time.ParseDuration(*duration)
		if err != nil {
			return fmt.Errorf("invalid session reset timeout duration: %w", err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Playtime.Limits.SessionReset = duration
	return nil
}
