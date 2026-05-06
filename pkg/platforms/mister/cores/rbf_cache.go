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
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// RBFCache provides lookups of RBF paths by system ID, short name, or launcher ID.
//
// On-disk persistence: when SetPersistPath has been called, the first
// Refresh of the process tries to load `<path>` instead of scanning the
// SD. If the loaded file's directory-mtime snapshot still matches the
// live filesystem, no scan runs at all. If mtimes have drifted, the
// loaded data is still installed for serving and NeedsRescan() returns
// true so the caller can schedule a background rescan via the idle
// scheduler. Subsequent Refresh calls noop when mtimes still match
// (cheap stat-only check) and rescan when they don't.
type RBFCache struct {
	bySystemID    map[string]RBFInfo
	byShortName   map[string][]RBFInfo
	byLauncherID  map[string]string
	lastDirMtimes map[string]int64
	persistPath   string
	lastRootRBFs  []string
	mu            syncutil.RWMutex
	initialized   bool
	needsRescan   bool
}

// GlobalRBFCache is the singleton instance for the MiSTer platform.
var GlobalRBFCache = &RBFCache{}

// splitRBFPath splits an RBF path like "_Console/SNES" into ("_Console", "SNES").
// A bare short name returns ("", name).
func splitRBFPath(rbfPath string) (dir, shortName string) {
	if idx := strings.LastIndex(rbfPath, "/"); idx >= 0 {
		return rbfPath[:idx], rbfPath[idx+1:]
	}
	return "", rbfPath
}

// selectByCanonicalDir prefers the candidate whose MglName directory equals canonicalDir.
// Falls back to the first candidate when no canonical match exists. Returns false only
// when candidates is empty.
func selectByCanonicalDir(candidates []RBFInfo, canonicalDir string) (RBFInfo, bool) {
	if len(candidates) == 0 {
		return RBFInfo{}, false
	}
	for _, c := range candidates {
		dir, _ := splitRBFPath(c.MglName)
		if strings.EqualFold(dir, canonicalDir) {
			return c, true
		}
	}
	return candidates[0], true
}

// SetPersistPath configures the on-disk cache file. Pass an empty string
// to disable persistence (e.g. tests). Must be called before the first
// Refresh to take effect for the load step.
func (c *RBFCache) SetPersistPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.persistPath = path
}

// NeedsRescan reports whether the most recent first-call Refresh loaded a
// persisted cache whose directory-mtime snapshot didn't match the live
// filesystem. The caller (typically a platform's StartPost) is expected
// to schedule a background Refresh when this is true. The flag is reset
// once a fresh scan completes.
func (c *RBFCache) NeedsRescan() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.needsRescan
}

// Refresh ensures the cache reflects the current filesystem. Behaviour:
//
//   - First call after process start, with a configured persist path: try
//     to load the persisted cache. If loaded and the directory-mtime
//     snapshot matches the live filesystem, install the data and return
//     without scanning. If loaded but mtimes drifted, install the data,
//     set NeedsRescan, and return without scanning. If the file is
//     missing, corrupt, or version-mismatched, fall through to a scan.
//   - Subsequent calls: stat the snapshot directories; if all mtimes
//     still match, noop. Otherwise, scan and persist.
//
// The cheap-stat fast path means callers like Platform.Launchers (which
// is invoked from many hot paths) pay only a handful of stats per call
// instead of a full SD walk.
func (c *RBFCache) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		c.initialized = true
		if c.tryLoadFromDiskLocked() {
			return
		}
	} else if dirMtimesMatch(c.lastDirMtimes) && rootRBFsMatch(c.lastRootRBFs) {
		return
	}

	c.scanLocked()
}

// tryLoadFromDiskLocked attempts to populate the cache from the persisted
// file. Returns true if a usable file was loaded (the cache is now
// populated even if mtimes drifted; needsRescan tracks that case).
func (c *RBFCache) tryLoadFromDiskLocked() bool {
	if c.persistPath == "" {
		return false
	}
	stored, loaded, err := loadPersistedRBFCache(c.persistPath)
	if err != nil {
		log.Warn().Err(err).Str("path", c.persistPath).Msg("RBF cache: load failed, falling back to scan")
		return false
	}
	if !loaded {
		return false
	}

	c.BuildFromRBFs(stored.Files)
	c.lastDirMtimes = stored.DirMtimes
	c.lastRootRBFs = stored.RootRBFs
	mtimesOK := dirMtimesMatch(stored.DirMtimes)
	rootOK := rootRBFsMatch(stored.RootRBFs)
	if mtimesOK && rootOK {
		log.Info().
			Int("rbf_files", len(stored.Files)).
			Int("systems_mapped", len(c.bySystemID)).
			Str("path", c.persistPath).
			Msg("RBF cache loaded from disk")
		c.needsRescan = false
	} else {
		drifted := diffDirMtimes(stored.DirMtimes)
		rootDiff := diffRootRBFs(stored.RootRBFs)
		log.Info().
			Str("path", c.persistPath).
			Int("drifted_count", len(drifted)).
			Interface("drifted", drifted).
			Strs("added_root_rbfs", rootDiff.Added).
			Strs("removed_root_rbfs", rootDiff.Removed).
			Msg("RBF cache loaded but state drifted; rescan needed")
		c.needsRescan = true
	}
	return true
}

// scanLocked runs the synchronous SD scan, rebuilds the in-memory maps,
// and (when persistence is configured) writes the result to disk. Caller
// must hold c.mu.
func (c *RBFCache) scanLocked() {
	rbfFiles, err := shallowScanRBF()
	if err != nil {
		log.Warn().Err(err).Msg("RBF cache: scan failed, using empty cache")
		c.BuildFromRBFs(nil)
		c.needsRescan = false
		return
	}
	c.BuildFromRBFs(rbfFiles)
	c.needsRescan = false
	log.Info().
		Int("rbf_files", len(rbfFiles)).
		Int("systems_mapped", len(c.bySystemID)).
		Msg("RBF cache initialized")

	if c.persistPath == "" {
		return
	}
	snapshot, snapErr := snapshotDirMtimes()
	if snapErr != nil {
		log.Warn().Err(snapErr).Msg("RBF cache: failed to snapshot directory mtimes, skipping persist")
		return
	}
	rootRBFs, rootErr := snapshotRootRBFs()
	if rootErr != nil {
		log.Warn().Err(rootErr).Msg("RBF cache: failed to snapshot root RBFs, skipping persist")
		return
	}
	c.lastDirMtimes = snapshot
	c.lastRootRBFs = rootRBFs
	if writeErr := writePersistedRBFCache(c.persistPath, rbfFiles, snapshot, rootRBFs); writeErr != nil {
		log.Warn().Err(writeErr).Str("path", c.persistPath).Msg("RBF cache: failed to persist")
		return
	}
	log.Debug().
		Int("rbf_files", len(rbfFiles)).
		Str("path", c.persistPath).
		Msg("RBF cache persisted to disk")
}

// BuildFromRBFs deterministically rebuilds bySystemID and byShortName from a
// scanned RBF list, preferring each system's canonical directory when multiple
// RBFs share a short name. No filesystem access; safe to call in tests.
func (c *RBFCache) BuildFromRBFs(rbfFiles []RBFInfo) {
	c.bySystemID = make(map[string]RBFInfo)
	c.byShortName = make(map[string][]RBFInfo)

	for _, rbf := range rbfFiles {
		key := strings.ToLower(rbf.ShortName)
		c.byShortName[key] = append(c.byShortName[key], rbf)
	}

	for _, system := range Systems {
		if system.RBF == "" {
			continue
		}
		canonicalDir, shortName := splitRBFPath(system.RBF)
		candidates := c.byShortName[strings.ToLower(shortName)]
		if rbf, ok := selectByCanonicalDir(candidates, canonicalDir); ok {
			c.bySystemID[system.ID] = rbf
		}
	}
}

// GetBySystemID returns the cached RBFInfo for a system ID.
func (c *RBFCache) GetBySystemID(systemID string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rbf, ok := c.bySystemID[systemID]
	return rbf, ok
}

// GetByShortName returns the first cached RBFInfo for a short name. Case-insensitive.
// For directory-aware lookup, use GetBySystemID or GetByLauncherID.
func (c *RBFCache) GetByShortName(shortName string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	candidates := c.byShortName[strings.ToLower(shortName)]
	if len(candidates) == 0 {
		return RBFInfo{}, false
	}
	return candidates[0], true
}

// GetByMglPath resolves a user-supplied MGL path (e.g. "_Unstable/SNES") to a
// scanned RBFInfo. When the path includes a directory, the match is strict: a
// wrong directory returns (RBFInfo{}, false) instead of a silent fallback to
// another core. A bare short name (no directory) returns the first candidate.
func (c *RBFCache) GetByMglPath(mglPath string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	canonicalDir, shortName := splitRBFPath(mglPath)
	candidates := c.byShortName[strings.ToLower(shortName)]
	if len(candidates) == 0 {
		return RBFInfo{}, false
	}
	if canonicalDir == "" {
		return candidates[0], true
	}
	for _, candidate := range candidates {
		dir, _ := splitRBFPath(candidate.MglName)
		if strings.EqualFold(dir, canonicalDir) {
			return candidate, true
		}
	}
	return RBFInfo{}, false
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
// When the registered path includes a directory, the match is strict: a
// directory mismatch returns (RBFInfo{}, false) rather than silently falling
// back to a different directory's core.
func (c *RBFCache) GetByLauncherID(launcherID string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rbfPath, ok := c.byLauncherID[launcherID]
	if !ok {
		return RBFInfo{}, false
	}

	canonicalDir, shortName := splitRBFPath(rbfPath)
	candidates := c.byShortName[strings.ToLower(shortName)]
	if len(candidates) == 0 {
		return RBFInfo{}, false
	}
	if canonicalDir == "" {
		return candidates[0], true
	}
	for _, candidate := range candidates {
		dir, _ := splitRBFPath(candidate.MglName)
		if strings.EqualFold(dir, canonicalDir) {
			return candidate, true
		}
	}
	return RBFInfo{}, false
}

// Resolve returns the RBFInfo for a core, honoring config load_path override,
// then alt core LauncherID, then system ID. It errors if load_path is set but
// doesn't match a scanned RBF, or if no lookup succeeds.
func (c *RBFCache) Resolve(cfg *config.Instance, core *Core) (RBFInfo, error) {
	if core == nil {
		return RBFInfo{}, errors.New("nil core")
	}
	key := core.LauncherID
	if key == "" {
		key = core.ID
	}

	if cfg != nil {
		if lp := cfg.LookupLauncherDefaults(key, nil).LoadPath; lp != "" {
			rbfInfo, ok := c.GetByMglPath(lp)
			if !ok {
				return RBFInfo{}, fmt.Errorf(
					"configured load_path %q for %s not found in RBF cache", lp, core.ID,
				)
			}
			log.Debug().Str("system", core.ID).Str("load_path", lp).Msg("core overridden by config load_path")
			return rbfInfo, nil
		}
	}

	if core.LauncherID != "" {
		if rbfInfo, ok := c.GetByLauncherID(core.LauncherID); ok {
			return rbfInfo, nil
		}
	}

	rbfInfo, ok := c.GetBySystemID(core.ID)
	if !ok {
		return RBFInfo{}, fmt.Errorf(
			"no core found for system %s (launcher %s, not in cache)", core.ID, key,
		)
	}
	return rbfInfo, nil
}

// Count returns the number of cached entries.
func (c *RBFCache) Count() (systems, rbfs int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := 0
	for _, v := range c.byShortName {
		total += len(v)
	}
	return len(c.bySystemID), total
}
