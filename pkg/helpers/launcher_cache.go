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

package helpers

import (
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// LauncherCache provides fast O(1) launcher lookups by system ID.
// This replaces the expensive O(n*m) pl.Launchers() calls in hot paths.
type LauncherCache struct {
	bySystemID   map[string][]platforms.Launcher
	allLaunchers []platforms.Launcher
	mu           syncutil.RWMutex
}

// GlobalLauncherCache is the singleton instance used throughout the application.
var GlobalLauncherCache = &LauncherCache{}

// Initialize builds the launcher cache from platform launchers.
// This should be called once at startup after custom launchers are loaded.
func (lc *LauncherCache) Initialize(pl platforms.Platform, cfg *config.Instance) {
	lc.InitializeFromSlice(pl.Launchers(cfg))

	lc.mu.RLock()
	defer lc.mu.RUnlock()

	for sysID, sysLaunchers := range lc.bySystemID {
		log.Debug().Str("systemID", sysID).Int("launchers", len(sysLaunchers)).
			Msg("launcher cache system entry")
	}

	log.Info().Int("totalLaunchers", len(lc.allLaunchers)).Int("systemIDs", len(lc.bySystemID)).
		Msg("launcher cache initialized")
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

// InitializeFromSlice builds the launcher cache from a pre-built slice of launchers.
// This is useful for testing or when launchers are already available.
func (lc *LauncherCache) InitializeFromSlice(launchers []platforms.Launcher) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.allLaunchers = make([]platforms.Launcher, len(launchers))
	copy(lc.allLaunchers, launchers)

	lc.bySystemID = make(map[string][]platforms.Launcher)
	for i := range launchers {
		launcher := launchers[i]
		if launcher.SystemID != "" {
			lc.bySystemID[launcher.SystemID] = append(lc.bySystemID[launcher.SystemID], launcher)
		}
	}
}

// GetLauncherByID finds a launcher by its unique ID.
// Returns nil if no launcher with the given ID is found.
func (lc *LauncherCache) GetLauncherByID(id string) *platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	for i := range lc.allLaunchers {
		if lc.allLaunchers[i].ID == id {
			return &lc.allLaunchers[i]
		}
	}
	return nil
}

// Refresh rebuilds the cache with updated launcher data.
// This can be called via API to refresh the cache without restarting.
func (lc *LauncherCache) Refresh(pl platforms.Platform, cfg *config.Instance) {
	lc.Initialize(pl, cfg)
}

// ToRelativePath converts an absolute media path to a relative path with the
// system ID as the first component. It strips the matching rootDir+folder
// prefix and replaces it with the systemID.
//
// Example: "/mnt/games/SNES/USA/game.sfc" with systemID "snes" becomes
// "snes/USA/game.sfc".
//
// Returns the original path unchanged if it is a URI or no prefix matches.
func (lc *LauncherCache) ToRelativePath(
	rootDirs []string,
	systemID string,
	path string,
) string {
	if ReURI.MatchString(path) {
		return path
	}

	launchers := lc.GetLaunchersBySystem(systemID)
	if len(launchers) == 0 {
		return path
	}

	// Collect unique folders from all launchers for this system.
	var relFolders, absFolders []string
	seen := make(map[string]bool)
	for i := range launchers {
		if launchers[i].SkipFilesystemScan {
			continue
		}
		for _, folder := range launchers[i].Folders {
			if seen[folder] {
				continue
			}
			seen[folder] = true
			if filepath.IsAbs(folder) {
				absFolders = append(absFolders, folder)
			} else {
				relFolders = append(relFolders, folder)
			}
		}
	}

	// Try rootDir + relative folder combinations.
	for _, root := range rootDirs {
		for _, folder := range relFolders {
			prefix := filepath.Join(root, folder)
			if PathHasPrefix(path, prefix) {
				return stripPrefixAndPrepend(systemID, path, prefix)
			}
		}
	}

	// Try absolute folders.
	for _, folder := range absFolders {
		if PathHasPrefix(path, folder) {
			return stripPrefixAndPrepend(systemID, path, folder)
		}
	}

	return path
}

// stripPrefixAndPrepend removes prefix from path and prepends systemID.
func stripPrefixAndPrepend(systemID, path, prefix string) string {
	normPath := filepath.ToSlash(filepath.Clean(path))
	normPrefix := filepath.ToSlash(filepath.Clean(prefix))
	if !strings.HasSuffix(normPrefix, "/") {
		normPrefix += "/"
	}

	// Use case-insensitive match to find where the prefix ends.
	lowerPath := strings.ToLower(normPath)
	lowerPrefix := strings.ToLower(normPrefix)
	if !strings.HasPrefix(lowerPath, lowerPrefix) {
		return path
	}

	remainder := normPath[len(normPrefix):]
	return systemID + "/" + remainder
}
