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

import "strings"

type Systems struct {
	Default []SystemsDefault `toml:"default,omitempty"`
	Hashes  *[]string        `toml:"hashes,omitempty"`
}

type SystemsDefault struct {
	System     string `toml:"system"`
	Launcher   string `toml:"launcher,omitempty"`
	BeforeExit string `toml:"before_exit,omitempty"`
}

func (c *Instance) SystemDefaults() []SystemsDefault {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Systems.Default
}

func (c *Instance) LookupSystemDefaults(systemID string) (SystemsDefault, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, defaultSystem := range c.vals.Systems.Default {
		if strings.EqualFold(defaultSystem.System, systemID) {
			return defaultSystem, true
		}
	}
	return SystemsDefault{}, false
}

func (c *Instance) SystemHashes() *[]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Systems.Hashes
}
