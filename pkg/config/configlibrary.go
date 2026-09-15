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

// Library configures Library sync: uploading which games this device holds
// and keeping favorites, play-later, reactions and decks converged with the
// linked Zaparoo Online account.
type Library struct {
	Sync *bool `toml:"sync,omitempty"`
}

// LibrarySyncEnabled reports whether the user explicitly consented to Library
// sync. Unset defaults to false: linking an account never grants it.
func (c *Instance) LibrarySyncEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Library.Sync != nil && *c.vals.Library.Sync
}

// SetLibrarySync enables or disables Library sync.
func (c *Instance) SetLibrarySync(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Library.Sync = &enabled
}
