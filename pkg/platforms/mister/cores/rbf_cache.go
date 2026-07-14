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
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/rs/zerolog/log"
)

// RBFCache provides lookups of RBF paths by system ID, short name, or launcher ID.
//
// On-disk persistence: when SetPersistPath has been called, the first
// Refresh of the process tries to load `<path>` instead of scanning the
// SD. If its shallow RBF manifest matches the live filesystem, no scan runs.
// If the manifest differs, the loaded data is installed for serving and
// NeedsRescan returns true so the caller can schedule a background rescan.
// Subsequent Refresh calls use directory mtimes as a cheap runtime check.
type RBFCache struct {
	persistPath   string
	sdRoot        string
	bySystemID    map[string]RBFInfo
	byShortName   map[string][]RBFInfo
	byLauncherID  map[string][]string
	lastDirMtimes map[string]int64
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
// persisted cache whose RBF manifest did not match the live filesystem.
// The caller (typically a platform's StartPost) is expected to schedule a
// background Refresh when this is true. The flag resets after a fresh scan.
func (c *RBFCache) NeedsRescan() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.needsRescan
}

// Refresh ensures the cache reflects the current filesystem. Behaviour:
//
//   - First call after process start, with a configured persist path: try
//     to load the persisted cache. If loaded and its shallow RBF manifest
//     matches the filesystem, install the data and return without scanning.
//     If the manifest differs, install the data, set NeedsRescan, and return.
//     Missing, corrupt, or version-mismatched files fall through to a scan.
//   - Subsequent calls: stat snapshot directories and compare root RBF names;
//     if all still match, noop. Otherwise, scan and persist.
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
	} else if !c.needsRescan && dirMtimesMatch(c.lastDirMtimes) && rootRBFsMatch(c.lastRootRBFs) {
		return
	}

	c.scanLocked()
}

// tryLoadFromDiskLocked attempts to populate the cache from the persisted
// file. Returns true if a usable file was loaded. The cache is populated
// even if its manifest drifted; needsRescan tracks that case.
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

	beforeManifest, beforeErr := c.snapshotRBFManifest()
	c.BuildFromRBFs(stored.Files)
	afterManifest, afterErr := c.snapshotRBFManifest()
	c.lastDirMtimes, _ = c.snapshotDirMtimes()
	c.lastRootRBFs, _ = c.snapshotRootRBFs()
	manifestStable := beforeErr == nil && afterErr == nil &&
		rbfManifestsMatch(beforeManifest, afterManifest)
	if manifestStable && rbfManifestsMatch(stored.Manifest, beforeManifest) {
		log.Info().
			Int("rbf_files", len(stored.Files)).
			Int("systems_mapped", len(c.bySystemID)).
			Str("path", c.persistPath).
			Msg("RBF cache loaded from disk")
		c.needsRescan = false
	} else {
		event := log.Info().
			Str("path", c.persistPath).
			Int("cached_rbf_files", len(stored.Manifest)).
			Int("current_rbf_files", len(afterManifest))
		if beforeErr != nil {
			event = event.Err(beforeErr)
		} else if afterErr != nil {
			event = event.Err(afterErr)
		}
		event.Msg("RBF cache loaded but manifest check failed or drifted; rescan needed")
		c.needsRescan = true
	}
	return true
}

// scanLocked runs the synchronous SD scan, rebuilds the in-memory maps,
// and (when persistence is configured) writes the result to disk. Caller
// must hold c.mu.
func (c *RBFCache) scanLocked() {
	const maxScanAttempts = 2
	var rbfFiles []RBFInfo
	var manifest []string
	stable := false
	for range maxScanAttempts {
		beforeManifest, beforeErr := c.snapshotRBFManifest()
		files, err := shallowScanRBF()
		if err != nil {
			log.Warn().Err(err).Msg("RBF cache: scan failed, using empty cache")
			c.BuildFromRBFs(nil)
			c.needsRescan = true
			return
		}
		rbfFiles = files
		afterManifest, afterErr := c.snapshotRBFManifest()
		if beforeErr == nil && afterErr == nil && rbfManifestsMatch(beforeManifest, afterManifest) {
			manifest = afterManifest
			stable = true
			break
		}
	}
	c.BuildFromRBFs(rbfFiles)
	c.needsRescan = !stable
	log.Info().
		Int("rbf_files", len(rbfFiles)).
		Int("systems_mapped", len(c.bySystemID)).
		Msg("RBF cache initialized")

	if c.persistPath == "" || !stable {
		return
	}
	snapshot, snapErr := c.snapshotDirMtimes()
	if snapErr != nil {
		log.Warn().Err(snapErr).Msg("RBF cache: failed to snapshot directory mtimes, skipping persist")
		return
	}
	rootRBFs, rootErr := c.snapshotRootRBFs()
	if rootErr != nil {
		log.Warn().Err(rootErr).Msg("RBF cache: failed to snapshot root RBFs, skipping persist")
		return
	}
	c.lastDirMtimes = snapshot
	c.lastRootRBFs = rootRBFs
	if writeErr := writePersistedRBFCache(c.persistPath, rbfFiles, manifest); writeErr != nil {
		log.Warn().Err(writeErr).Str("path", c.persistPath).Msg("RBF cache: failed to persist")
		return
	}
	log.Debug().
		Int("rbf_files", len(rbfFiles)).
		Str("path", c.persistPath).
		Msg("RBF cache persisted to disk")
}

func (c *RBFCache) root() string {
	if c.sdRoot != "" {
		return c.sdRoot
	}
	return misterconfig.SDRootDir
}

func (c *RBFCache) snapshotRBFManifest() ([]string, error) {
	return snapshotRBFManifestAt(c.root())
}

func (c *RBFCache) snapshotDirMtimes() (map[string]int64, error) {
	return snapshotDirMtimesAt(c.root())
}

func (c *RBFCache) snapshotRootRBFs() ([]string, error) {
	return snapshotRootRBFsAt(c.root())
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
// The short name may include a glob, used for versioned alt cores whose full
// RBF basename changes with each release.
func (c *RBFCache) GetByMglPath(mglPath string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.getByMglPathLocked(mglPath)
}

func (c *RBFCache) getByMglPathLocked(mglPath string) (RBFInfo, bool) {
	canonicalDir, shortName := splitRBFPath(mglPath)
	if strings.ContainsAny(shortName, "*?[") || strings.Contains(shortName, "<date>") ||
		strings.Contains(shortName, "<hash>") {
		return c.getByMglGlobLocked(canonicalDir, shortName)
	}

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

func (c *RBFCache) getByMglGlobLocked(canonicalDir, shortNamePattern string) (RBFInfo, bool) {
	matches := make([]RBFInfo, 0)
	for _, candidates := range c.byShortName {
		for _, candidate := range candidates {
			dir, candidateShortName := splitRBFPath(candidate.MglName)
			if canonicalDir != "" && !strings.EqualFold(dir, canonicalDir) {
				continue
			}
			if !matchMglPattern(shortNamePattern, candidateShortName) {
				continue
			}
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return RBFInfo{}, false
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].MglName < matches[j].MglName
	})
	return matches[len(matches)-1], true
}

func matchMglPattern(pattern, name string) bool {
	if strings.Contains(pattern, "<date>") || strings.Contains(pattern, "<hash>") {
		return matchMglTokenPattern(pattern, name)
	}
	matched, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(name))
	return err == nil && matched
}

func matchMglTokenPattern(pattern, name string) bool {
	patternParts := strings.Split(pattern, "_")
	nameParts := strings.Split(name, "_")
	if len(patternParts) != len(nameParts) {
		return false
	}
	for i, patternPart := range patternParts {
		namePart := nameParts[i]
		switch patternPart {
		case "<date>":
			if !isNDigits(namePart, 8) {
				return false
			}
		case "<hash>":
			if !isHexish(namePart) {
				return false
			}
		default:
			if !strings.EqualFold(patternPart, namePart) {
				return false
			}
		}
	}
	return true
}

func isNDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isHexish(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// RegisterAltCore registers an alt core's expected RBF path(s).
// Called during launcher creation. When multiple paths are given, they are
// tried in order at resolution time and the first that matches a scanned RBF
// wins, allowing a launcher to support more than one core location/naming
// convention (e.g. Sinden cores in "Light Gun/" or legacy "_Sinden/").
func (c *RBFCache) RegisterAltCore(launcherID string, rbfPaths ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byLauncherID == nil {
		c.byLauncherID = make(map[string][]string)
	}
	c.byLauncherID[launcherID] = rbfPaths
}

// GetByLauncherID returns the resolved RBF path for an alt core launcher.
// Registered paths are tried in order; the first that resolves wins. When a
// registered path includes a directory, the match is strict: a directory
// mismatch is skipped rather than silently falling back to a different
// directory's core.
func (c *RBFCache) GetByLauncherID(launcherID string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rbfPaths, ok := c.byLauncherID[launcherID]
	if !ok {
		return RBFInfo{}, false
	}

	for _, rbfPath := range rbfPaths {
		if rbfInfo, found := c.getByMglPathLocked(rbfPath); found {
			return rbfInfo, true
		}
	}
	return RBFInfo{}, false
}

func (c *RBFCache) relatedCandidates(rbfPath string) []string {
	canonicalDir, shortName := splitRBFPath(rbfPath)
	if shortName == "" {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	prefix := strings.ToLower(shortName) + "_"
	candidates := make([]string, 0)
	seen := make(map[string]struct{})
	for key, rbfs := range c.byShortName {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for _, rbf := range rbfs {
			dir, _ := splitRBFPath(rbf.MglName)
			if canonicalDir != "" && !strings.EqualFold(dir, canonicalDir) {
				continue
			}
			if _, ok := seen[rbf.MglName]; ok {
				continue
			}
			seen[rbf.MglName] = struct{}{}
			candidates = append(candidates, rbf.MglName)
		}
	}
	sort.Strings(candidates)
	return candidates
}

func missingCoreError(core *Core, key string, candidates []string) error {
	if len(candidates) == 0 {
		return fmt.Errorf(
			"no core found for system %s (launcher %s, not in cache)", core.ID, key,
		)
	}
	return fmt.Errorf(
		"no original core found for system %s (launcher %s, expected %s; not in cache); "+
			"available fork candidates: %s; set launchers.default load_path to use one explicitly",
		core.ID, key, core.RBF, strings.Join(candidates, ", "),
	)
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
		return RBFInfo{}, missingCoreError(core, key, c.relatedCandidates(core.RBF))
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
