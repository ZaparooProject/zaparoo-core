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

package profiles

import (
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
)

// LimitsResolver layers the active profile's playtime limit overrides over
// the global config. It satisfies the playtime.LimitsProvider interface.
// Reads come from the in-memory active-profile snapshot, never the
// database, so it is safe on the limit-check hot path.
//
// An explicit (non-nil) profile field wins; a nil field inherits the
// global config value. A "0" duration string means explicitly unlimited.
// Warning intervals are device UX, not per-person policy, and always come
// from global config.
type LimitsResolver struct {
	cfg *config.Instance
	st  *state.State
}

// NewLimitsResolver creates a resolver over the global config and the
// service state holding the active profile.
func NewLimitsResolver(cfg *config.Instance, st *state.State) *LimitsResolver {
	return &LimitsResolver{cfg: cfg, st: st}
}

// PlaytimeLimitsEnabled returns the active profile's enabled override, or
// the global config value when no profile is active or it has no override.
func (r *LimitsResolver) PlaytimeLimitsEnabled() bool {
	if p := r.st.ActiveProfile(); p != nil && p.LimitsEnabled != nil {
		return *p.LimitsEnabled
	}
	return r.cfg.PlaytimeLimitsEnabled()
}

// DailyLimit returns the active profile's daily limit override, or the
// global config value. Returns 0 for "no limit".
func (r *LimitsResolver) DailyLimit() time.Duration {
	if p := r.st.ActiveProfile(); p != nil && p.DailyLimit != nil {
		return parseLimit(*p.DailyLimit)
	}
	return r.cfg.DailyLimit()
}

// SessionLimit returns the active profile's session limit override, or the
// global config value. Returns 0 for "no limit".
func (r *LimitsResolver) SessionLimit() time.Duration {
	if p := r.st.ActiveProfile(); p != nil && p.SessionLimit != nil {
		return parseLimit(*p.SessionLimit)
	}
	return r.cfg.SessionLimit()
}

// WarningIntervals always returns the global config warning intervals.
func (r *LimitsResolver) WarningIntervals() []time.Duration {
	return r.cfg.WarningIntervals()
}

// ActiveProfileID returns the active profile's ID, or "" when no profile
// is active. The playtime limits manager uses this to scope daily usage
// accounting to the active profile's attributed history.
func (r *LimitsResolver) ActiveProfileID() string {
	if p := r.st.ActiveProfile(); p != nil {
		return p.ProfileID
	}
	return ""
}

// parseLimit parses a stored limit duration string. Strings are validated
// at write time; an unparseable value degrades to 0 (no limit), matching
// the config accessors' behavior.
func parseLimit(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}
