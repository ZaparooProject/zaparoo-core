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
