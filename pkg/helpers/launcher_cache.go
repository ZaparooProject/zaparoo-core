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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// LauncherCache provides fast O(1) launcher lookups by system ID.
// This replaces the expensive O(n*m) pl.Launchers() calls in hot paths.
type LauncherCache struct {
	bySystemID          map[string][]platforms.Launcher
	availableBySystemID map[string][]platforms.Launcher
	allLaunchers        []platforms.Launcher
	launchableSystems   []launchables.VirtualSystem
	extraLaunchers      []platforms.Launcher
	mu                  syncutil.RWMutex
}

// GlobalLauncherCache is the singleton instance used throughout the application.
var GlobalLauncherCache = &LauncherCache{}

// Initialize builds the launcher cache from platform launchers.
// Optional extra launchers (e.g. the native-audio launcher) are appended after
// deduplication. This should be called once at startup after custom launchers are loaded.
// The extras are retained so Refresh can reapply them.
func (lc *LauncherCache) Initialize(pl platforms.Platform, cfg *config.Instance, extra ...platforms.Launcher) {
	lc.setExtraLaunchers(extra)
	lc.rebuild(pl, cfg, extra)
}

func (lc *LauncherCache) rebuild(pl platforms.Platform, cfg *config.Instance, extra []platforms.Launcher) {
	launchableSystems, launchableMedia := launchables.Available(cfg, pl)
	all := pl.Launchers(cfg)
	all = append(all, launchables.LaunchersFor(launchableSystems, launchableMedia)...)
	for i := range extra {
		if !launcherInSlice(all, extra[i].ID) {
			all = append(all, extra[i])
		}
	}
	for i := range all {
		all[i].Available = true
		all[i].AvailabilityReason = ""
		if all[i].Availability == nil {
			continue
		}
		if err := all[i].Availability(cfg); err != nil {
			all[i].Available = false
			all[i].AvailabilityReason = err.Error()
		}
	}
	lc.rebuildFromSlice(all)
	lc.setLaunchableSystems(launchableSystems)

	lc.mu.RLock()
	defer lc.mu.RUnlock()

	for sysID, sysLaunchers := range lc.bySystemID {
		log.Debug().Str("systemID", sysID).Int("launchers", len(sysLaunchers)).
			Msg("launcher cache system entry")
	}

	log.Info().Int("totalLaunchers", len(lc.allLaunchers)).Int("systemIDs", len(lc.bySystemID)).
		Msg("launcher cache initialized")
}

func launcherInSlice(launchers []platforms.Launcher, id string) bool {
	for i := range launchers {
		if strings.EqualFold(launchers[i].ID, id) {
			return true
		}
	}
	return false
}

// GetLaunchersBySystem returns all launchers for a specific system ID.
// Returns nil if no launchers found for the system.
func (lc *LauncherCache) GetLaunchersBySystem(systemID string) []platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	return lc.bySystemID[systemID]
}

// GetAllLaunchers returns all cached launchers.
// GetAvailableLaunchersBySystem returns cached launchers whose runtime dependencies are available.
func (lc *LauncherCache) GetAvailableLaunchersBySystem(systemID string) []platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	return lc.availableBySystemID[systemID]
}

func (lc *LauncherCache) GetAllLaunchers() []platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	result := make([]platforms.Launcher, len(lc.allLaunchers))
	copy(result, lc.allLaunchers)
	return result
}

// GetLaunchableSystems returns available virtual systems cached during launcher initialization.
func (lc *LauncherCache) GetLaunchableSystems() []launchables.VirtualSystem {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	result := make([]launchables.VirtualSystem, len(lc.launchableSystems))
	copy(result, lc.launchableSystems)
	return result
}

// InitializeFromSlice builds the launcher cache from a pre-built slice of launchers.
// This is useful for testing or when launchers are already available.
func (lc *LauncherCache) InitializeFromSlice(launchers []platforms.Launcher) {
	lc.rebuildFromSlice(launchers)
}

// applySystemIDFolders lets a folder named after the system ID be scanned even
// when no launcher declares it.
//
// System IDs are the names Zaparoo already uses in its API, in the scraper's
// custom gamelist bundles and in published metadata packs, so a library
// organised that way is indexed without the user writing per-launcher folder
// lists. MiSTer's launchers hand-list these names already; doing it here gives
// every platform and every launcher set the same behaviour.
//
// This belongs on the launcher rather than in path discovery because the folder
// has to be equally visible to LauncherMatcher, which decides which launcher
// owns a scanned file. A path discovered but owned by nobody indexes nothing.
//
// A folder another system already scans by name stays with that system. Only
// MSX1 and MSX2 hit that today: folders named msx1 and msx2 are declared by MSX.
func applySystemIDFolders(launchers []platforms.Launcher) {
	declared := make(map[string]map[string]struct{})
	for i := range launchers {
		launcher := &launchers[i]
		if launcher.SkipFilesystemScan {
			continue
		}
		for _, folder := range launcher.Folders {
			if folder == "" || filepath.IsAbs(folder) {
				continue
			}
			key := strings.ToLower(folder)
			if declared[key] == nil {
				declared[key] = make(map[string]struct{})
			}
			declared[key][launcher.SystemID] = struct{}{}
		}
	}

	for i := range launchers {
		launcher := &launchers[i]
		if launcher.SystemID == "" || launcher.SkipFilesystemScan {
			continue
		}
		// Only extend launchers that already scan a root-relative folder.
		// A launcher with no folders matches by other means, such as a Test
		// function or an absolute path, and giving it one would widen what it
		// claims rather than just renaming where it looks.
		if !hasRelativeFolder(launcher.Folders) {
			continue
		}
		if !wantsSystemIDFolder(launcher.SystemID, launcher.Folders, declared) {
			continue
		}
		// Folders can share a backing array with the caller's slice, so grow a
		// copy rather than appending in place.
		folders := make([]string, 0, len(launcher.Folders)+1)
		folders = append(folders, launcher.Folders...)
		folders = append(folders, launcher.SystemID)
		launcher.Folders = folders
	}
}

// hasRelativeFolder reports whether any folder is resolved against the index
// roots, as opposed to being an absolute path.
func hasRelativeFolder(folders []string) bool {
	for _, folder := range folders {
		if folder != "" && !filepath.IsAbs(folder) {
			return true
		}
	}
	return false
}

// wantsSystemIDFolder reports whether systemID should be added to folders.
func wantsSystemIDFolder(
	systemID string, folders []string, declared map[string]map[string]struct{},
) bool {
	for _, folder := range folders {
		if strings.EqualFold(folder, systemID) {
			return false
		}
	}
	for owner := range declared[strings.ToLower(systemID)] {
		if !strings.EqualFold(owner, systemID) {
			return false
		}
	}
	return true
}

func (lc *LauncherCache) rebuildFromSlice(launchers []platforms.Launcher) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.allLaunchers = make([]platforms.Launcher, len(launchers))
	copy(lc.allLaunchers, launchers)
	applySystemIDFolders(lc.allLaunchers)
	lc.launchableSystems = nil

	lc.bySystemID = make(map[string][]platforms.Launcher)
	lc.availableBySystemID = make(map[string][]platforms.Launcher)
	for i := range lc.allLaunchers {
		launcher := lc.allLaunchers[i]
		if launcher.Availability == nil {
			launcher.Available = true
			launcher.AvailabilityReason = ""
		}
		lc.allLaunchers[i] = launcher
		if launcher.SystemID == "" {
			continue
		}
		lc.bySystemID[launcher.SystemID] = append(lc.bySystemID[launcher.SystemID], launcher)
		if launcher.Available {
			lc.availableBySystemID[launcher.SystemID] = append(lc.availableBySystemID[launcher.SystemID], launcher)
		}
	}
}

func (lc *LauncherCache) setLaunchableSystems(systems []launchables.VirtualSystem) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.launchableSystems = append([]launchables.VirtualSystem(nil), systems...)
}

func (lc *LauncherCache) setExtraLaunchers(extra []platforms.Launcher) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.extraLaunchers = append([]platforms.Launcher(nil), extra...)
}

func (lc *LauncherCache) getExtraLaunchers() []platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return append([]platforms.Launcher(nil), lc.extraLaunchers...)
}

// GetLauncherByID finds a launcher by its case-insensitive unique ID.
// Returns nil if no launcher is found or IDs differing only by case make the lookup ambiguous.
func (lc *LauncherCache) GetLauncherByID(id string) *platforms.Launcher {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	var match *platforms.Launcher
	for i := range lc.allLaunchers {
		candidate := &lc.allLaunchers[i]
		if !strings.EqualFold(candidate.ID, id) {
			continue
		}
		if match != nil {
			if candidate.ID != match.ID {
				log.Error().Str("launcherID", id).Str("firstID", match.ID).Str("conflictingID", candidate.ID).
					Msg("ambiguous case-insensitive launcher ID")
				return nil
			}
			continue
		}
		match = candidate
	}
	return match
}

// Refresh rebuilds the cache with updated launcher data, reapplying the extra
// launchers registered by Initialize. Without that, a refresh would drop every
// launcher the platform cannot build itself and break control actions for it.
// This can be called via API to refresh the cache without restarting.
// During refresh, concurrent GetLaunchableSystems calls may briefly see no
// virtual systems while the cache is rebuilt; they reappear after initialization.
func (lc *LauncherCache) Refresh(pl platforms.Platform, cfg *config.Instance) {
	lc.rebuild(pl, cfg, lc.getExtraLaunchers())
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
