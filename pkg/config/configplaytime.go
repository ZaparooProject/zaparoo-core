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

package config

import (
	"time"
)

// Playtime configures play time tracking and limits.
type Playtime struct {
	Retention *int           `toml:"retention,omitempty"`
	Limits    PlaytimeLimits `toml:"limits,omitempty"`
}

// PlaytimeLimits configures time limits and warnings for gameplay sessions.
type PlaytimeLimits struct {
	Enabled  *bool    `toml:"enabled,omitempty"`
	Daily    string   `toml:"daily,omitempty"`
	Session  string   `toml:"session,omitempty"`
	Warnings []string `toml:"warnings,omitempty,multiline"`
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
