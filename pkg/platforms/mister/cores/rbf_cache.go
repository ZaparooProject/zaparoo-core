//go:build linux

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

package cores

import (
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// RBFCache provides O(1) lookups of RBF paths by system ID, short name, or launcher ID.
type RBFCache struct {
	bySystemID   map[string]RBFInfo
	byShortName  map[string]RBFInfo
	byLauncherID map[string]string // launcherID → rbfPath (unresolved)
	mu           syncutil.RWMutex
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

// RegisterAltCore registers an alt core's expected RBF path.
// Called during launcher creation.
func (c *RBFCache) RegisterAltCore(launcherID, rbfPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byLauncherID == nil {
		c.byLauncherID = make(map[string]string)
	}
	c.byLauncherID[launcherID] = rbfPath
}

// GetByLauncherID returns the resolved RBF path for an alt core launcher.
func (c *RBFCache) GetByLauncherID(launcherID string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rbfPath, ok := c.byLauncherID[launcherID]
	if !ok {
		return RBFInfo{}, false
	}

	// Extract short name and look up in byShortName
	shortName := rbfPath
	if idx := strings.LastIndex(rbfPath, "/"); idx >= 0 {
		shortName = rbfPath[idx+1:]
	}

	info, ok := c.byShortName[strings.ToLower(shortName)]
	return info, ok
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

// ResolveRBFPathForLauncher resolves RBF path using launcherID if available,
// falling back to systemID lookup for main cores.
func ResolveRBFPathForLauncher(launcherID, systemID, hardcodedRBF string) string {
	// Try launcherID first (for alt cores)
	if launcherID != "" {
		if cached, ok := GlobalRBFCache.GetByLauncherID(launcherID); ok {
			log.Debug().
				Str("launcher", launcherID).
				Str("cached_path", cached.MglName).
				Msg("RBF resolved by launcher ID")
			return cached.MglName
		}
	}

	// Fall back to systemID lookup (for main cores)
	if cached, ok := GlobalRBFCache.GetBySystemID(systemID); ok {
		log.Debug().
			Str("system", systemID).
			Str("cached_path", cached.MglName).
			Msg("RBF resolved by system ID")
		return cached.MglName
	}

	// Final fallback to hardcoded path
	return hardcodedRBF
}

// Count returns the number of cached entries.
func (c *RBFCache) Count() (systems, rbfs int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.bySystemID), len(c.byShortName)
}
