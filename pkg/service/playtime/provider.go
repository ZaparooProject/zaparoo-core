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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
)

// LimitsProvider is the source of playtime limit values for the
// LimitsManager. The default implementation reads global config directly;
// the profiles service provides an implementation that layers the active
// profile's overrides over global config (see
// pkg/service/profiles.LimitsResolver).
type LimitsProvider interface {
	// PlaytimeLimitsEnabled reports whether limits are enforced.
	PlaytimeLimitsEnabled() bool
	// DailyLimit returns the daily limit, or 0 for no limit.
	DailyLimit() time.Duration
	// SessionLimit returns the per-session limit, or 0 for no limit.
	SessionLimit() time.Duration
	// WarningIntervals returns the remaining-time warning thresholds.
	WarningIntervals() []time.Duration
	// ActiveProfileID returns the active profile's ID, or "" when no
	// profile is active. Daily usage accounting is scoped to this
	// profile's attributed history; "" sums all history (device-level).
	ActiveProfileID() string
}

// globalProvider is the default LimitsProvider: global config values, no
// profile scoping.
type globalProvider struct {
	cfg *config.Instance
}

func (g globalProvider) PlaytimeLimitsEnabled() bool       { return g.cfg.PlaytimeLimitsEnabled() }
func (g globalProvider) DailyLimit() time.Duration         { return g.cfg.DailyLimit() }
func (g globalProvider) SessionLimit() time.Duration       { return g.cfg.SessionLimit() }
func (g globalProvider) WarningIntervals() []time.Duration { return g.cfg.WarningIntervals() }
func (globalProvider) ActiveProfileID() string             { return "" }
