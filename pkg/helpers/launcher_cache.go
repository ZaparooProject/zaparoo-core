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

package helpers

import (
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// LauncherCache provides fast O(1) launcher lookups by system ID.
// This replaces the expensive O(n*m) pl.Launchers() calls in hot paths.
type LauncherCache struct {
	bySystemID   map[string][]platforms.Launcher
	allLaunchers []platforms.Launcher
	mu           sync.RWMutex
}

// GlobalLauncherCache is the singleton instance used throughout the application.
var GlobalLauncherCache = &LauncherCache{}

// Initialize builds the launcher cache from platform launchers.
// This should be called once at startup after custom launchers are loaded.
func (lc *LauncherCache) Initialize(pl platforms.Platform, cfg *config.Instance) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Get all launchers from platform
	allLaunchers := pl.Launchers(cfg)
	lc.allLaunchers = make([]platforms.Launcher, len(allLaunchers))
	copy(lc.allLaunchers, allLaunchers)

	// Build system ID index
	lc.bySystemID = make(map[string][]platforms.Launcher)
	for i := range allLaunchers {
		launcher := allLaunchers[i]
		if launcher.SystemID != "" {
			lc.bySystemID[launcher.SystemID] = append(lc.bySystemID[launcher.SystemID], launcher)
		}
	}
}

// GetLaunchersBySystem returns all launchers for a specific system ID.
// Returns nil if no launchers found for the system.
func (lc *LauncherCache) GetLaunchersBySystem(systemID string) []platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	return lc.bySystemID[systemID]
}

// GetAllLaunchers returns all cached launchers.
func (lc *LauncherCache) GetAllLaunchers() []platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	result := make([]platforms.Launcher, len(lc.allLaunchers))
	copy(result, lc.allLaunchers)
	return result
}

// Refresh rebuilds the cache with updated launcher data.
// This can be called via API to refresh the cache without restarting.
func (lc *LauncherCache) Refresh(pl platforms.Platform, cfg *config.Instance) {
	lc.Initialize(pl, cfg)
}
