//go:build linux

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

package cores

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// RBFCache provides O(1) lookups of RBF paths by system ID or short name.
type RBFCache struct {
	bySystemID  map[string]RBFInfo
	byShortName map[string]RBFInfo
	mu          syncutil.RWMutex
}

// GlobalRBFCache is the singleton instance for the MiSTer platform.
var GlobalRBFCache = &RBFCache{}

// Refresh scans for RBF files and rebuilds the cache.
func (c *RBFCache) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.populateCache()
}

func (c *RBFCache) populateCache() {
	c.bySystemID = make(map[string]RBFInfo)
	c.byShortName = make(map[string]RBFInfo)

	rbfFiles, err := shallowScanRBF()
	if err != nil {
		log.Warn().Err(err).Msg("RBF cache: scan failed, using empty cache")
		return
	}

	for _, rbf := range rbfFiles {
		key := strings.ToLower(rbf.ShortName)
		c.byShortName[key] = rbf
	}

	for _, system := range Systems {
		if system.RBF == "" {
			continue
		}

		shortName := system.RBF
		if idx := strings.LastIndex(shortName, "/"); idx >= 0 {
			shortName = shortName[idx+1:]
		}

		if rbf, ok := c.byShortName[strings.ToLower(shortName)]; ok {
			c.bySystemID[system.ID] = rbf
		}
	}

	log.Info().
		Int("rbf_files", len(rbfFiles)).
		Int("systems_mapped", len(c.bySystemID)).
		Msg("RBF cache initialized")
}

// GetBySystemID returns the cached RBFInfo for a system ID.
func (c *RBFCache) GetBySystemID(systemID string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rbf, ok := c.bySystemID[systemID]
	return rbf, ok
}

// GetByShortName returns the cached RBFInfo for a short name. Case-insensitive.
func (c *RBFCache) GetByShortName(shortName string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rbf, ok := c.byShortName[strings.ToLower(shortName)]
	return rbf, ok
}

// ResolveRBFPath returns the cached RBF path for a system, or falls back to
// the hardcoded path if not cached.
func ResolveRBFPath(systemID, hardcodedRBF string) string {
	if cached, ok := GlobalRBFCache.GetBySystemID(systemID); ok {
		log.Debug().
			Str("system", systemID).
			Str("cached_path", cached.MglName).
			Str("hardcoded_path", hardcodedRBF).
			Msg("RBF resolved from cache")
		return cached.MglName
	}

	return hardcodedRBF
}

// Count returns the number of cached entries.
func (c *RBFCache) Count() (systems, rbfs int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.bySystemID), len(c.byShortName)
}
