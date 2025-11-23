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

type Media struct {
	FilenameTags   *bool    `toml:"filename_tags,omitempty"`
	DefaultRegions []string `toml:"default_regions,omitempty,multiline"`
	DefaultLangs   []string `toml:"default_langs,omitempty,multiline"`
}

// FilenameTags returns whether filename tag parsing is enabled.
func (c *Instance) FilenameTags() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Media.FilenameTags == nil {
		return true
	}
	return *c.vals.Media.FilenameTags
}

// SetFilenameTags sets whether filename tag parsing is enabled.
func (c *Instance) SetFilenameTags(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Media.FilenameTags = &enabled
}

// DefaultRegions returns the list of default regions for media matching.
func (c *Instance) DefaultRegions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.vals.Media.DefaultRegions) == 0 {
		// TODO: raw strings for now to avoid import cycle
		// TODO: should this auto-detect the locale?
		return []string{"us", "world"}
	}
	return c.vals.Media.DefaultRegions
}

// DefaultLangs returns the list of default languages for media matching.
func (c *Instance) DefaultLangs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.vals.Media.DefaultLangs) == 0 {
		// TODO: raw strings for now to avoid import cycle
		// TODO: should this auto-detect the locale?
		return []string{"en"}
	}
	return c.vals.Media.DefaultLangs
}
