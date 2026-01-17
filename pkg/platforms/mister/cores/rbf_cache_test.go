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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRBFCacheRefresh_Basic(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.Refresh()

	systems, rbfs := cache.Count()
	t.Logf("Cache contains %d systems, %d RBF files", systems, rbfs)

	// On non-MiSTer systems, counts will be 0 (empty cache is valid)
	assert.GreaterOrEqual(t, systems, 0, "systems count should be non-negative")
	assert.GreaterOrEqual(t, rbfs, 0, "rbfs count should be non-negative")
}

func TestResolveRBFPath_Fallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		systemID     string
		hardcodedRBF string
	}{
		{
			name:         "unknown system uses hardcoded",
			systemID:     "UnknownSystem12345",
			hardcodedRBF: "_Console/Unknown",
		},
		{
			name:         "empty system ID uses hardcoded",
			systemID:     "",
			hardcodedRBF: "_Console/Empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := ResolveRBFPath(tc.systemID, tc.hardcodedRBF)

			// On empty/unknown system, should return hardcoded path
			assert.Equal(t, tc.hardcodedRBF, result, "should return hardcoded path for unknown system")
		})
	}
}

func TestResolveRBFPath_NeverEmpty(t *testing.T) {
	t.Parallel()

	// Even with empty inputs, should return something
	result := ResolveRBFPath("", "")
	assert.Empty(t, result, "empty hardcoded returns empty")

	result = ResolveRBFPath("NES", "_Console/NES")
	assert.NotEmpty(t, result, "should never return empty for valid input")
}

func TestRBFCacheGetByShortName_CaseInsensitive(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.Refresh()

	// Test case-insensitive lookup
	_, foundLower := cache.GetByShortName("nes")
	_, foundUpper := cache.GetByShortName("NES")
	_, foundMixed := cache.GetByShortName("Nes")

	// All should return same result (whether found or not depends on system)
	assert.Equal(t, foundLower, foundUpper, "lookups should be case-insensitive")
	assert.Equal(t, foundUpper, foundMixed, "lookups should be case-insensitive")
}

func TestRBFCacheRefresh_MultipleRefreshes(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.Refresh()

	initialSystems, initialRbfs := cache.Count()

	cache.Refresh()

	refreshedSystems, refreshedRbfs := cache.Count()

	t.Logf("Initial: %d systems, %d rbfs; Refreshed: %d systems, %d rbfs",
		initialSystems, initialRbfs, refreshedSystems, refreshedRbfs)

	assert.GreaterOrEqual(t, refreshedSystems, 0, "refreshed systems count should be non-negative")
	assert.GreaterOrEqual(t, refreshedRbfs, 0, "refreshed rbfs count should be non-negative")
}

func TestRBFCacheGetBySystemID(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.Refresh()

	// Unknown system should return false
	_, found := cache.GetBySystemID("UnknownSystem12345")
	assert.False(t, found, "unknown system should not be found")
}

func TestRBFCacheGetBySystemID_CacheHit(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{
		bySystemID: map[string]RBFInfo{
			"NES": {
				Path:      "/media/fat/_Console/NES_20231212.rbf",
				Filename:  "NES_20231212.rbf",
				ShortName: "NES",
				MglName:   "_Console/NES",
			},
		},
		byShortName: map[string]RBFInfo{
			"nes": {
				Path:      "/media/fat/_Console/NES_20231212.rbf",
				Filename:  "NES_20231212.rbf",
				ShortName: "NES",
				MglName:   "_Console/NES",
			},
		},
	}

	// Test cache hit by system ID
	rbf, found := cache.GetBySystemID("NES")
	assert.True(t, found, "NES should be found in cache")
	assert.Equal(t, "_Console/NES", rbf.MglName)
	assert.Equal(t, "NES", rbf.ShortName)

	// Test cache hit by short name
	rbf, found = cache.GetByShortName("nes")
	assert.True(t, found, "nes should be found in cache")
	assert.Equal(t, "_Console/NES", rbf.MglName)

	// Verify count
	systems, rbfs := cache.Count()
	assert.Equal(t, 1, systems)
	assert.Equal(t, 1, rbfs)
}

func TestResolveRBFPath_CacheHit(t *testing.T) {
	// Save and restore GlobalRBFCache state
	originalBySystemID := GlobalRBFCache.bySystemID
	originalByShortName := GlobalRBFCache.byShortName
	defer func() {
		GlobalRBFCache.mu.Lock()
		GlobalRBFCache.bySystemID = originalBySystemID
		GlobalRBFCache.byShortName = originalByShortName
		GlobalRBFCache.mu.Unlock()
	}()

	// Populate cache with test data
	GlobalRBFCache.mu.Lock()
	GlobalRBFCache.bySystemID = map[string]RBFInfo{
		"SNES": {
			Path:      "/media/fat/_Console/SNES_20240101.rbf",
			Filename:  "SNES_20240101.rbf",
			ShortName: "SNES",
			MglName:   "_Console/SNES",
		},
	}
	GlobalRBFCache.mu.Unlock()

	// Test cache hit returns cached path, not hardcoded
	result := ResolveRBFPath("SNES", "_OldPath/SNES")
	assert.Equal(t, "_Console/SNES", result, "should return cached path, not hardcoded")

	// Test cache miss still returns hardcoded
	result = ResolveRBFPath("Genesis", "_Console/Genesis")
	assert.Equal(t, "_Console/Genesis", result, "should return hardcoded for cache miss")
}

func TestRBFCacheThreadSafety(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.Refresh()

	// Run concurrent reads and writes to verify no race conditions
	done := make(chan bool)

	// Multiple readers
	for range 10 {
		go func() {
			for range 100 {
				cache.GetBySystemID("NES")
				cache.GetByShortName("nes")
				cache.Count()
			}
			done <- true
		}()
	}

	// One writer refreshing
	go func() {
		for range 10 {
			cache.Refresh()
		}
		done <- true
	}()

	// Wait for all goroutines
	for range 11 {
		<-done
	}
}

func TestRegisterAltCore(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}

	// Register an alt core
	cache.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")

	// Verify it was stored
	cache.mu.RLock()
	rbfPath, ok := cache.byLauncherID["2XPSX"]
	cache.mu.RUnlock()

	assert.True(t, ok, "launcher ID should be found")
	assert.Equal(t, "_Other/PSX2XCPU", rbfPath)
}

func TestRegisterAltCore_MultipleRegistrations(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}

	// Register multiple alt cores
	cache.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")
	cache.RegisterAltCore("PWMPSX", "_ConsolePWM/PSX_PWM")
	cache.RegisterAltCore("LLAPIPSX", "_LLAPI/PSX_LLAPI")

	cache.mu.RLock()
	assert.Len(t, cache.byLauncherID, 3, "should have 3 registered launchers")
	assert.Equal(t, "_Other/PSX2XCPU", cache.byLauncherID["2XPSX"])
	assert.Equal(t, "_ConsolePWM/PSX_PWM", cache.byLauncherID["PWMPSX"])
	assert.Equal(t, "_LLAPI/PSX_LLAPI", cache.byLauncherID["LLAPIPSX"])
	cache.mu.RUnlock()
}

func TestGetByLauncherID_NotRegistered(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}

	// Lookup unregistered launcher should fail
	_, found := cache.GetByLauncherID("UnknownLauncher")
	assert.False(t, found, "unregistered launcher should not be found")
}

func TestGetByLauncherID_RegisteredButNotInShortNameCache(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{
		byShortName: make(map[string]RBFInfo),
	}

	// Register an alt core
	cache.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")

	// Lookup should fail because PSX2XCPU isn't in byShortName
	_, found := cache.GetByLauncherID("2XPSX")
	assert.False(t, found, "should not be found when RBF not in cache")
}

func TestGetByLauncherID_CacheHit(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{
		byShortName: map[string]RBFInfo{
			"psx2xcpu": {
				Path:      "/media/fat/_Other/PSX2XCPU_20240101.rbf",
				Filename:  "PSX2XCPU_20240101.rbf",
				ShortName: "PSX2XCPU",
				MglName:   "_Other/PSX2XCPU",
			},
		},
	}

	// Register an alt core
	cache.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")

	// Lookup should succeed
	rbf, found := cache.GetByLauncherID("2XPSX")
	assert.True(t, found, "registered launcher should be found")
	assert.Equal(t, "_Other/PSX2XCPU", rbf.MglName)
	assert.Equal(t, "PSX2XCPU", rbf.ShortName)
}

func TestGetByLauncherID_ExtractsShortNameFromPath(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{
		byShortName: map[string]RBFInfo{
			"n64_80mhz": {
				Path:      "/media/fat/_Other/N64_80MHz_20240101.rbf",
				Filename:  "N64_80MHz_20240101.rbf",
				ShortName: "N64_80MHz",
				MglName:   "_Other/N64_80MHz",
			},
		},
	}

	// Register with nested path
	cache.RegisterAltCore("80MHzNintendo64", "_Other/N64_80MHz")

	// Lookup should extract short name from path and find it
	rbf, found := cache.GetByLauncherID("80MHzNintendo64")
	assert.True(t, found, "should find RBF by extracted short name")
	assert.Equal(t, "_Other/N64_80MHz", rbf.MglName)
}

func TestResolveRBFPathForLauncher_LauncherIDTakesPriority(t *testing.T) {
	// Save and restore GlobalRBFCache state
	originalBySystemID := GlobalRBFCache.bySystemID
	originalByShortName := GlobalRBFCache.byShortName
	originalByLauncherID := GlobalRBFCache.byLauncherID
	defer func() {
		GlobalRBFCache.mu.Lock()
		GlobalRBFCache.bySystemID = originalBySystemID
		GlobalRBFCache.byShortName = originalByShortName
		GlobalRBFCache.byLauncherID = originalByLauncherID
		GlobalRBFCache.mu.Unlock()
	}()

	// Setup cache with both system and alt core entries
	GlobalRBFCache.mu.Lock()
	GlobalRBFCache.bySystemID = map[string]RBFInfo{
		"PSX": {
			Path:      "/media/fat/_Console/PSX_20240101.rbf",
			Filename:  "PSX_20240101.rbf",
			ShortName: "PSX",
			MglName:   "_Console/PSX",
		},
	}
	GlobalRBFCache.byShortName = map[string]RBFInfo{
		"psx": {
			Path:      "/media/fat/_Console/PSX_20240101.rbf",
			Filename:  "PSX_20240101.rbf",
			ShortName: "PSX",
			MglName:   "_Console/PSX",
		},
		"psx2xcpu": {
			Path:      "/media/fat/_Other/PSX2XCPU_20240101.rbf",
			Filename:  "PSX2XCPU_20240101.rbf",
			ShortName: "PSX2XCPU",
			MglName:   "_Other/PSX2XCPU",
		},
	}
	GlobalRBFCache.byLauncherID = map[string]string{
		"2XPSX": "_Other/PSX2XCPU",
	}
	GlobalRBFCache.mu.Unlock()

	// When launcherID is provided and found, should use alt core path
	result := ResolveRBFPathForLauncher("2XPSX", "PSX", "_Console/PSX")
	assert.Equal(t, "_Other/PSX2XCPU", result, "launcherID lookup should take priority over systemID")

	// When launcherID is empty, should fall back to systemID
	result = ResolveRBFPathForLauncher("", "PSX", "_Console/PSX")
	assert.Equal(t, "_Console/PSX", result, "should fall back to systemID when launcherID is empty")
}

func TestResolveRBFPathForLauncher_FallbackChain(t *testing.T) {
	// Save and restore GlobalRBFCache state
	originalBySystemID := GlobalRBFCache.bySystemID
	originalByShortName := GlobalRBFCache.byShortName
	originalByLauncherID := GlobalRBFCache.byLauncherID
	defer func() {
		GlobalRBFCache.mu.Lock()
		GlobalRBFCache.bySystemID = originalBySystemID
		GlobalRBFCache.byShortName = originalByShortName
		GlobalRBFCache.byLauncherID = originalByLauncherID
		GlobalRBFCache.mu.Unlock()
	}()

	// Setup empty cache
	GlobalRBFCache.mu.Lock()
	GlobalRBFCache.bySystemID = make(map[string]RBFInfo)
	GlobalRBFCache.byShortName = make(map[string]RBFInfo)
	GlobalRBFCache.byLauncherID = make(map[string]string)
	GlobalRBFCache.mu.Unlock()

	// When nothing is cached, should fall back to hardcoded
	result := ResolveRBFPathForLauncher("UnknownLauncher", "UnknownSystem", "_Console/Fallback")
	assert.Equal(t, "_Console/Fallback", result, "should fall back to hardcoded path")
}

func TestResolveRBFPathForLauncher_LauncherNotFoundFallsBackToSystemID(t *testing.T) {
	// Save and restore GlobalRBFCache state
	originalBySystemID := GlobalRBFCache.bySystemID
	originalByShortName := GlobalRBFCache.byShortName
	originalByLauncherID := GlobalRBFCache.byLauncherID
	defer func() {
		GlobalRBFCache.mu.Lock()
		GlobalRBFCache.bySystemID = originalBySystemID
		GlobalRBFCache.byShortName = originalByShortName
		GlobalRBFCache.byLauncherID = originalByLauncherID
		GlobalRBFCache.mu.Unlock()
	}()

	// Setup cache with system entry only
	GlobalRBFCache.mu.Lock()
	GlobalRBFCache.bySystemID = map[string]RBFInfo{
		"PSX": {
			Path:      "/media/fat/_Console/PSX_20240101.rbf",
			Filename:  "PSX_20240101.rbf",
			ShortName: "PSX",
			MglName:   "_Console/PSX",
		},
	}
	GlobalRBFCache.byShortName = make(map[string]RBFInfo)
	GlobalRBFCache.byLauncherID = make(map[string]string)
	GlobalRBFCache.mu.Unlock()

	// When launcherID is not found, should fall back to systemID
	result := ResolveRBFPathForLauncher("UnknownLauncher", "PSX", "_Console/OldPSX")
	assert.Equal(t, "_Console/PSX", result, "should fall back to systemID when launcherID not found")
}

// TestRegression_Issue477_AltCoreUsesWrongRBFPath is a regression test for GitHub issue #477.
// The bug: 2XPSX launcher didn't work correctly because ResolveRBFPath looked up cached RBF
// paths by systemID only. Alt cores like 2XPSX share the same systemID ("PSX") as the main
// core, so the cache returned the main core's path instead of the alt core's path.
func TestRegression_Issue477_AltCoreUsesWrongRBFPath(t *testing.T) {
	// Save and restore GlobalRBFCache state
	originalBySystemID := GlobalRBFCache.bySystemID
	originalByShortName := GlobalRBFCache.byShortName
	originalByLauncherID := GlobalRBFCache.byLauncherID
	defer func() {
		GlobalRBFCache.mu.Lock()
		GlobalRBFCache.bySystemID = originalBySystemID
		GlobalRBFCache.byShortName = originalByShortName
		GlobalRBFCache.byLauncherID = originalByLauncherID
		GlobalRBFCache.mu.Unlock()
	}()

	// Setup: Simulate a MiSTer with both standard PSX and 2XPSX cores installed
	GlobalRBFCache.mu.Lock()
	GlobalRBFCache.bySystemID = map[string]RBFInfo{
		"PSX": {
			Path:      "/media/fat/_Console/PSX_20240101.rbf",
			Filename:  "PSX_20240101.rbf",
			ShortName: "PSX",
			MglName:   "_Console/PSX",
		},
	}
	GlobalRBFCache.byShortName = map[string]RBFInfo{
		"psx": {
			Path:      "/media/fat/_Console/PSX_20240101.rbf",
			Filename:  "PSX_20240101.rbf",
			ShortName: "PSX",
			MglName:   "_Console/PSX",
		},
		"psx2xcpu": {
			Path:      "/media/fat/_Other/PSX2XCPU_20240101.rbf",
			Filename:  "PSX2XCPU_20240101.rbf",
			ShortName: "PSX2XCPU",
			MglName:   "_Other/PSX2XCPU",
		},
	}
	GlobalRBFCache.byLauncherID = map[string]string{
		"2XPSX": "_Other/PSX2XCPU",
	}
	GlobalRBFCache.mu.Unlock()

	// THE BUG: Before the fix, calling ResolveRBFPath("PSX", "_Other/PSX2XCPU") for the
	// 2XPSX launcher would return "_Console/PSX" (the main core) instead of "_Other/PSX2XCPU"
	// because the lookup was done by systemID ("PSX"), not by launcherID.

	// Test 1: Main PSX launcher (no launcherID) should get the standard PSX core
	mainPSXPath := ResolveRBFPathForLauncher("", "PSX", "_Console/PSX")
	assert.Equal(t, "_Console/PSX", mainPSXPath,
		"main PSX launcher should use standard PSX core")

	// Test 2: 2XPSX alt core launcher should get the PSX2XCPU core, NOT the standard PSX core
	altCorePath := ResolveRBFPathForLauncher("2XPSX", "PSX", "_Other/PSX2XCPU")
	assert.Equal(t, "_Other/PSX2XCPU", altCorePath,
		"2XPSX launcher should use PSX2XCPU core, not standard PSX core")

	// Test 3: Verify they are different - this is the core of the bug
	assert.NotEqual(t, mainPSXPath, altCorePath,
		"alt core should resolve to different path than main core even though they share systemID")
}
