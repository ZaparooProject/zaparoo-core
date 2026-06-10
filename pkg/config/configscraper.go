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

type Scraper struct {
	CustomGamelistsPath *string `toml:"custom_gamelists_path,omitempty"`
	StatGamelistImages  *bool   `toml:"stat_gamelist_images,omitempty"`
}

// ScraperCustomGamelistsPath returns the configured custom gamelists root path,
// or "" if unset. When set, the gamelistxml scraper looks for an additional
// gamelist.xml at {path}/{system_id}/gamelist.xml for each scanned system.
func (c *Instance) ScraperCustomGamelistsPath() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Scraper.CustomGamelistsPath == nil {
		return ""
	}
	return *c.vals.Scraper.CustomGamelistsPath
}

// ScraperStatGamelistImages returns whether the gamelistxml scraper should
// verify image files referenced by gamelist.xml entries exist on disk before
// including them as media metadata. Defaults to false.
func (c *Instance) ScraperStatGamelistImages() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Scraper.StatGamelistImages == nil {
		return false
	}
	return *c.vals.Scraper.StatGamelistImages
}
