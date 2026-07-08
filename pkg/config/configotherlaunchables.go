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

	"github.com/rs/zerolog/log"
)

// OtherLaunchable is a user-configured MiSTer _Other launchable entry. The
// mister platform merges these with its built-in list at runtime (see
// pkg/platforms/mister/launchables.go).
type OtherLaunchable struct {
	ID       string `toml:"id"`
	Name     string `toml:"name"`
	Category string `toml:"category,omitempty"`
	CorePath string `toml:"core_path"`
}

const defaultOtherLaunchableCategory = "Other"

var validOtherLaunchableCategories = map[string]struct{}{
	"Other":    {},
	"Console":  {},
	"Computer": {},
	"Handheld": {},
	"Arcade":   {},
}

// validateOtherLaunchables filters raw other_launchables entries parsed from
// config.toml, dropping invalid ones with a warning and defaulting an empty
// category to "Other". The first occurrence of a duplicate id (case
// insensitive) wins; later duplicates are dropped.
func validateOtherLaunchables(raw []OtherLaunchable) []OtherLaunchable {
	valid := make([]OtherLaunchable, 0, len(raw))
	seenIDs := make(map[string]struct{}, len(raw))

	for _, entry := range raw {
		if entry.ID == "" || entry.Name == "" || entry.CorePath == "" {
			log.Warn().Msgf("other_launchables entry missing required id/name/core_path, ignoring: %+v", entry)
			continue
		}

		if strings.ContainsAny(entry.CorePath, `/\`) || strings.Contains(entry.CorePath, "..") {
			log.Warn().Msgf(
				"other_launchables entry %q has invalid core_path %q (must be a bare filename prefix), ignoring",
				entry.ID, entry.CorePath,
			)
			continue
		}

		id := strings.ToLower(entry.ID)
		if _, ok := seenIDs[id]; ok {
			log.Warn().Msgf("other_launchables entry %q is a duplicate id, ignoring", entry.ID)
			continue
		}

		if entry.Category == "" {
			entry.Category = defaultOtherLaunchableCategory
		} else if _, ok := validOtherLaunchableCategories[entry.Category]; !ok {
			log.Warn().Msgf(
				"other_launchables entry %q has unknown category %q, ignoring",
				entry.ID, entry.Category,
			)
			continue
		}

		seenIDs[id] = struct{}{}
		valid = append(valid, entry)
	}

	return valid
}

func cloneOtherLaunchables(entries []OtherLaunchable) []OtherLaunchable {
	owned := make([]OtherLaunchable, len(entries))
	copy(owned, entries)
	return owned
}

// OtherLaunchables returns validated user-configured MiSTer _Other launchable
// entries. Invalid entries are already filtered out at config load time.
func (c *Instance) OtherLaunchables() []OtherLaunchable {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneOtherLaunchables(c.vals.otherLaunchablesValid)
}
