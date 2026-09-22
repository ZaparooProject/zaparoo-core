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
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

type Systems struct {
	Category []SystemsCategory `toml:"category,omitempty"`
	Default  []SystemsDefault  `toml:"default,omitempty"`
}

type SystemsCategory struct {
	Name    string   `toml:"name"`
	Systems []string `toml:"systems,omitempty"`
}

type SystemsDefault struct {
	PauseOnLaunch *bool  `toml:"pause_on_launch,omitempty"`
	System        string `toml:"system"`
	Launcher      string `toml:"launcher,omitempty"`
	BeforeExit    string `toml:"before_exit,omitempty"`
}

func cloneSystemCategories(categories []SystemsCategory) []SystemsCategory {
	owned := make([]SystemsCategory, len(categories))
	copy(owned, categories)
	for i := range owned {
		owned[i].Systems = append([]string(nil), owned[i].Systems...)
	}
	return owned
}

func cloneSystemDefaults(defaults []SystemsDefault) []SystemsDefault {
	owned := make([]SystemsDefault, len(defaults))
	copy(owned, defaults)
	for i := range owned {
		if owned[i].PauseOnLaunch != nil {
			value := *owned[i].PauseOnLaunch
			owned[i].PauseOnLaunch = &value
		}
	}
	return owned
}

func (c *Instance) SystemDefaults() []SystemsDefault {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneSystemDefaults(c.vals.Systems.Default)
}

func (c *Instance) SetSystemDefaults(defaults []SystemsDefault) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Systems.Default = cloneSystemDefaults(defaults)
}

// LookupSystemDefaults returns the first entry naming systemID, or false when
// none does.
//
// Both sides are resolved to a canonical system ID first, so an alias resolves
// whichever side it is written on: system = "megadrive" matches a lookup for
// "Genesis", and system = "Genesis" matches a lookup for "MegaDrive". Comparing
// a canonical config ID against the raw argument only ever worked in the first
// direction, because a system's alias list never contains its own ID.
//
// An argument naming no known system matches nothing, the same as before: every
// entry that survives resolution carries a canonical ID, which an unknown name
// can never equal.
func (c *Instance) LookupSystemDefaults(systemID string) (SystemsDefault, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	querySystem, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return SystemsDefault{}, false
	}
	for _, defaultSystem := range c.vals.Systems.Default {
		configSystem, err := systemdefs.LookupSystem(defaultSystem.System)
		if err != nil {
			continue
		}
		if strings.EqualFold(configSystem.ID, querySystem.ID) {
			return defaultSystem, true
		}
	}
	return SystemsDefault{}, false
}

// AudioPauseOnLaunch reports whether background music should be paused when a
// game launches on the primary slot. Defaults to true when unset.
func (c *Instance) AudioPauseOnLaunch() bool {
	entry, ok := c.LookupSystemDefaults(systemdefs.SystemAudio)
	if !ok || entry.PauseOnLaunch == nil {
		return true
	}
	return *entry.PauseOnLaunch
}
