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
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// RBFCache provides lookups of RBF paths by system ID, short name, or launcher ID.
type RBFCache struct {
	bySystemID   map[string]RBFInfo
	byShortName  map[string][]RBFInfo // short name (lower) → all scanned RBFs with that short name
	byLauncherID map[string]string    // launcherID → rbfPath (unresolved)
	mu           syncutil.RWMutex
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

// Refresh scans for RBF files and rebuilds the cache.
func (c *RBFCache) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.populateCache()
}

func (c *RBFCache) populateCache() {
	rbfFiles, err := shallowScanRBF()
	if err != nil {
		log.Warn().Err(err).Msg("RBF cache: scan failed, using empty cache")
		c.BuildFromRBFs(nil)
		return
	}
	c.BuildFromRBFs(rbfFiles)
	log.Info().
		Int("rbf_files", len(rbfFiles)).
		Int("systems_mapped", len(c.bySystemID)).
		Msg("RBF cache initialized")
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
// scanned RBFInfo, preferring the directory embedded in the path. Returns false
// if no scanned RBF matches the short name.
func (c *RBFCache) GetByMglPath(mglPath string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	canonicalDir, shortName := splitRBFPath(mglPath)
	return selectByCanonicalDir(c.byShortName[strings.ToLower(shortName)], canonicalDir)
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

// GetByLauncherID returns the resolved RBF path for an alt core launcher,
// preferring the directory registered for that launcher.
func (c *RBFCache) GetByLauncherID(launcherID string) (RBFInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rbfPath, ok := c.byLauncherID[launcherID]
	if !ok {
		return RBFInfo{}, false
	}

	canonicalDir, shortName := splitRBFPath(rbfPath)
	return selectByCanonicalDir(c.byShortName[strings.ToLower(shortName)], canonicalDir)
}

// Resolve returns the RBFInfo for a core, honoring config load_path override,
// then alt core LauncherID, then system ID. It errors if load_path is set but
// doesn't match a scanned RBF, or if no lookup succeeds.
func (c *RBFCache) Resolve(cfg *config.Instance, core *Core) (RBFInfo, error) {
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
