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
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// launcherCacheWaitTimeout bounds how long a reader waits for a pending
// initialization before it proceeds with whatever the cache holds.
var launcherCacheWaitTimeout = 30 * time.Second

// LauncherCache provides fast O(1) launcher lookups by system ID.
// This replaces the expensive O(n*m) pl.Launchers() calls in hot paths.
//
// Startup may build the cache in the background: BeginInitialize marks it
// pending, and until Initialize finishes every reader waits for it (up to
// launcherCacheWaitTimeout) rather than seeing an empty cache.
type LauncherCache struct {
	bySystemID          map[string][]platforms.Launcher
	availableBySystemID map[string][]platforms.Launcher
	// relPrefixes holds, per system and set of root directories, the
	// folders ToRelativePath strips, already normalized. Built on first use
	// and dropped whenever the launchers are rebuilt.
	relPrefixes       map[string][]relativePrefix
	pending           chan struct{}
	allLaunchers      []platforms.Launcher
	launchableSystems []launchables.VirtualSystem
	extraLaunchers    []platforms.Launcher
	// relGen counts rebuilds, so a list built across one is not kept.
	relGen uint64
	mu     syncutil.RWMutex
}

// relativePrefix is one folder ToRelativePath can strip: as configured,
// and normalized for comparison, with and without its trailing separator.
type relativePrefix struct {
	raw       string
	norm      string
	normSlash string
}

// GlobalLauncherCache is the singleton instance used throughout the application.
var GlobalLauncherCache = &LauncherCache{}

// Initialize builds the launcher cache from platform launchers.
// Optional extra launchers (e.g. the native-audio launcher) are appended after
// deduplication. This should be called once at startup after custom launchers are loaded.
// The extras are retained so Refresh can reapply them.
func (lc *LauncherCache) Initialize(pl platforms.Platform, cfg *config.Instance, extra ...platforms.Launcher) {
	defer lc.finishInitialize()
	lc.setExtraLaunchers(extra)
	lc.rebuild(pl, cfg, extra)
}

// BeginInitialize marks the cache as being built so readers wait for the
// Initialize call that follows instead of reading an empty cache. Call it
// before starting Initialize in the background.
func (lc *LauncherCache) BeginInitialize() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.pending == nil {
		lc.pending = make(chan struct{})
	}
}

func (lc *LauncherCache) finishInitialize() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.pending != nil {
		close(lc.pending)
		lc.pending = nil
	}
}

// WaitReady blocks until a pending initialization finishes or ctx ends. It
// returns immediately when no initialization is pending.
func (lc *LauncherCache) WaitReady(ctx context.Context) error {
	lc.mu.RLock()
	pending := lc.pending
	lc.mu.RUnlock()
	if pending == nil {
		return nil
	}
	select {
	case <-pending:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // callers compare against context errors
	}
}

// awaitReady is the bounded wait every reader performs before reading.
func (lc *LauncherCache) awaitReady() {
	lc.mu.RLock()
	pending := lc.pending
	lc.mu.RUnlock()
	if pending == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), launcherCacheWaitTimeout)
	defer cancel()
	started := time.Now()
	if err := lc.WaitReady(ctx); errors.Is(err, context.DeadlineExceeded) {
		log.Warn().Dur("waited", time.Since(started)).
			Msg("launcher cache still initializing; reading it anyway")
		return
	}
	log.Debug().Dur("waited", time.Since(started)).Msg("launcher cache reader waited for initialization")
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
	lc.awaitReady()
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	return lc.bySystemID[systemID]
}

// GetAllLaunchers returns all cached launchers.
// GetAvailableLaunchersBySystem returns cached launchers whose runtime dependencies are available.
func (lc *LauncherCache) GetAvailableLaunchersBySystem(systemID string) []platforms.Launcher {
	lc.awaitReady()
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	return lc.availableBySystemID[systemID]
}

func (lc *LauncherCache) GetAllLaunchers() []platforms.Launcher {
	lc.awaitReady()
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	result := make([]platforms.Launcher, len(lc.allLaunchers))
	copy(result, lc.allLaunchers)
	return result
}

// GetLaunchableSystems returns available virtual systems cached during launcher initialization.
func (lc *LauncherCache) GetLaunchableSystems() []launchables.VirtualSystem {
	lc.awaitReady()
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	return cloneVirtualSystems(lc.launchableSystems)
}

// InitializeFromSlice builds the launcher cache from a pre-built slice of launchers.
// This is useful for testing or when launchers are already available.
func (lc *LauncherCache) InitializeFromSlice(launchers []platforms.Launcher) {
	defer lc.finishInitialize()
	lc.rebuildFromSlice(launchers)
}

func (lc *LauncherCache) rebuildFromSlice(launchers []platforms.Launcher) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.allLaunchers = make([]platforms.Launcher, len(launchers))
	copy(lc.allLaunchers, launchers)
	lc.launchableSystems = nil
	lc.relPrefixes = nil
	lc.relGen++

	lc.bySystemID = make(map[string][]platforms.Launcher)
	lc.availableBySystemID = make(map[string][]platforms.Launcher)
	for i := range launchers {
		launcher := launchers[i]
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
	lc.launchableSystems = cloneVirtualSystems(systems)
}

func cloneVirtualSystems(systems []launchables.VirtualSystem) []launchables.VirtualSystem {
	owned := make([]launchables.VirtualSystem, len(systems))
	copy(owned, systems)
	for i := range owned {
		owned[i].Categories = append([]string(nil), owned[i].Categories...)
	}
	return owned
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
	lc.awaitReady()
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

	prefixes := lc.relativePrefixes(rootDirs, systemID)
	if len(prefixes) == 0 {
		return path
	}

	// The path is normalized once; the folders were normalized when the
	// list was built. A listing calls this for every row, and normalizing
	// both sides of every comparison again was most of a browse's CPU time
	// on MiSTer.
	normPath := NormalizePathForComparison(path)
	for i := range prefixes {
		if hasNormalizedPrefix(normPath, &prefixes[i]) {
			return stripPrefixAndPrepend(systemID, path, prefixes[i].raw)
		}
	}
	return path
}

// hasNormalizedPrefix is PathHasPrefix for a path and folder that are both
// normalized already.
func hasNormalizedPrefix(normPath string, prefix *relativePrefix) bool {
	if normPath == prefix.norm {
		return true
	}
	if prefix.norm == "" {
		return false
	}
	return strings.HasPrefix(normPath, prefix.normSlash)
}

// relativePrefixes returns the folders ToRelativePath tries for systemID
// under rootDirs, in the order it tries them: every root directory joined
// with each relative launcher folder, then the absolute launcher folders.
func (lc *LauncherCache) relativePrefixes(rootDirs []string, systemID string) []relativePrefix {
	lc.awaitReady()
	key := systemID + "\x00" + strings.Join(rootDirs, "\x00")
	// Launchers, cached prefixes and the generation are read under one lock,
	// so a reload cannot slip between them and have prefixes built from old
	// launchers stored under the new generation.
	lc.mu.RLock()
	launchers := lc.bySystemID[systemID]
	cached, ok := lc.relPrefixes[key]
	gen := lc.relGen
	lc.mu.RUnlock()
	if len(launchers) == 0 {
		return nil
	}
	if ok {
		return cached
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

	prefixes := make([]relativePrefix, 0, len(rootDirs)*len(relFolders)+len(absFolders))
	add := func(raw string) {
		norm := NormalizePathForComparison(raw)
		normSlash := norm
		if !strings.HasSuffix(normSlash, "/") {
			normSlash += "/"
		}
		prefixes = append(prefixes, relativePrefix{raw: raw, norm: norm, normSlash: normSlash})
	}
	for _, root := range rootDirs {
		for _, folder := range relFolders {
			add(filepath.Join(root, folder))
		}
	}
	for _, folder := range absFolders {
		add(folder)
	}

	lc.mu.Lock()
	if lc.relGen == gen {
		if lc.relPrefixes == nil {
			lc.relPrefixes = make(map[string][]relativePrefix)
		}
		lc.relPrefixes[key] = prefixes
	}
	lc.mu.Unlock()
	return prefixes
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
