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

// Scraper configures metadata scraper behavior.
type Scraper struct {
	GamelistXML ScraperGamelistXML `toml:"gamelist_xml,omitempty"`
}

// ScraperGamelistXML configures the EmulationStation gamelist.xml scraper.
type ScraperGamelistXML struct {
	CustomPath string `toml:"custom_path,omitempty"`
}

// ScraperGamelistXMLCustomPath returns the optional directory containing
// per-system gamelist bundles at {custom_path}/{system_id}/gamelist.xml.
func (c *Instance) ScraperGamelistXMLCustomPath() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Scraper.GamelistXML.CustomPath
}
