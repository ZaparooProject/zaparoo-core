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

// Profiles configures device profile behavior.
type Profiles struct {
	RequireForLaunch *bool `toml:"require_for_launch,omitempty"`
	SwapData         *bool `toml:"swap_data,omitempty"`
}

// ProfilesRequireForLaunch returns true when media launches are blocked
// while no profile is active. Defaults to false: a profile-less device
// behaves exactly as before profiles existed.
func (c *Instance) ProfilesRequireForLaunch() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Profiles.RequireForLaunch == nil {
		return false
	}
	return *c.vals.Profiles.RequireForLaunch
}

// SetProfilesRequireForLaunch enables or disables the require-profile
// launch gate.
func (c *Instance) SetProfilesRequireForLaunch(required bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Profiles.RequireForLaunch = &required
}

// ProfilesSwapData returns true when profile switches also swap
// profile-scoped data (save files, save states) on platforms that support
// it. Defaults to true: data ownership is the point of profiles.
func (c *Instance) ProfilesSwapData() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Profiles.SwapData == nil {
		return true
	}
	return *c.vals.Profiles.SwapData
}

// SetProfilesSwapData enables or disables profile data swapping.
func (c *Instance) SetProfilesSwapData(swap bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Profiles.SwapData = &swap
}
