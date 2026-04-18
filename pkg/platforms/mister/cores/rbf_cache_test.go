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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		byShortName: map[string][]RBFInfo{
			"nes": {{
				Path:      "/media/fat/_Console/NES_20231212.rbf",
				Filename:  "NES_20231212.rbf",
				ShortName: "NES",
				MglName:   "_Console/NES",
			}},
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
		byShortName: make(map[string][]RBFInfo),
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
		byShortName: map[string][]RBFInfo{
			"psx2xcpu": {{
				Path:      "/media/fat/_Other/PSX2XCPU_20240101.rbf",
				Filename:  "PSX2XCPU_20240101.rbf",
				ShortName: "PSX2XCPU",
				MglName:   "_Other/PSX2XCPU",
			}},
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
		byShortName: map[string][]RBFInfo{
			"n64_80mhz": {{
				Path:      "/media/fat/_Other/N64_80MHz_20240101.rbf",
				Filename:  "N64_80MHz_20240101.rbf",
				ShortName: "N64_80MHz",
				MglName:   "_Other/N64_80MHz",
			}},
		},
	}

	// Register with nested path
	cache.RegisterAltCore("80MHzNintendo64", "_Other/N64_80MHz")

	// Lookup should extract short name from path and find it
	rbf, found := cache.GetByLauncherID("80MHzNintendo64")
	assert.True(t, found, "should find RBF by extracted short name")
	assert.Equal(t, "_Other/N64_80MHz", rbf.MglName)
}

// TestRegression_Issue477_AltCoreUsesWrongRBFPath is a regression test for GitHub issue #477.
// Before the fix, alt cores like 2XPSX shared systemID ("PSX") with the main core, so the
// lookup returned the main core's path instead of the alt core's path.
func TestRegression_Issue477_AltCoreUsesWrongRBFPath(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.BuildFromRBFs([]RBFInfo{
		{Path: "/media/fat/_Console/PSX_20240101.rbf", ShortName: "PSX", MglName: "_Console/PSX"},
		{Path: "/media/fat/_Other/PSX2XCPU_20240101.rbf", ShortName: "PSX2XCPU", MglName: "_Other/PSX2XCPU"},
	})
	cache.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")

	mainCore := &Core{ID: "PSX", RBF: "_Console/PSX"}
	mainInfo, err := cache.Resolve(nil, mainCore)
	require.NoError(t, err)
	assert.Equal(t, "_Console/PSX", mainInfo.MglName, "main PSX launcher should use standard PSX core")

	altCore := &Core{ID: "PSX", LauncherID: "2XPSX", RBF: "_Console/PSX"}
	altInfo, err := cache.Resolve(nil, altCore)
	require.NoError(t, err)
	assert.Equal(t, "_Other/PSX2XCPU", altInfo.MglName, "2XPSX launcher should use PSX2XCPU core")

	assert.NotEqual(t, mainInfo.MglName, altInfo.MglName,
		"alt core should resolve to different path than main core even though they share systemID")
}

func TestRBFCache_Resolve_LoadPath(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.BuildFromRBFs([]RBFInfo{
		{Path: "/media/fat/_Console/SNES_20260311.rbf", ShortName: "SNES", MglName: "_Console/SNES"},
		{Path: "/media/fat/_Unstable/SNES_20260101.rbf", ShortName: "SNES", MglName: "_Unstable/SNES"},
	})

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "SNES"
load_path = "_Unstable/SNES"
`))

	got, err := cache.Resolve(cfg, &Core{ID: "SNES", RBF: "_Console/SNES"})
	require.NoError(t, err)
	assert.Equal(t, "/media/fat/_Unstable/SNES_20260101.rbf", got.Path)
}

func TestRBFCache_Resolve_LoadPathInvalid(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.BuildFromRBFs([]RBFInfo{
		{Path: "/media/fat/_Console/SNES_20260311.rbf", ShortName: "SNES", MglName: "_Console/SNES"},
	})

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[launchers.default]]
launcher = "SNES"
load_path = "_LLAPI/NonExistentCore"
`))

	_, err := cache.Resolve(cfg, &Core{ID: "SNES", RBF: "_Console/SNES"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_LLAPI/NonExistentCore")
}

func TestRBFCache_Resolve_AltCoreUsesLauncherID(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.BuildFromRBFs([]RBFInfo{
		{Path: "/media/fat/_Console/PSX_20240101.rbf", ShortName: "PSX", MglName: "_Console/PSX"},
		{Path: "/media/fat/_Other/PSX2XCPU_20240101.rbf", ShortName: "PSX2XCPU", MglName: "_Other/PSX2XCPU"},
	})
	cache.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")

	got, err := cache.Resolve(nil, &Core{ID: "PSX", LauncherID: "2XPSX", RBF: "_Console/PSX"})
	require.NoError(t, err)
	assert.Equal(t, "/media/fat/_Other/PSX2XCPU_20240101.rbf", got.Path)
}

func TestRBFCache_Resolve_AltCoreFallsBackToSystemID(t *testing.T) {
	t.Parallel()

	// LauncherID set but not registered — should fall back to system ID lookup.
	cache := &RBFCache{}
	cache.BuildFromRBFs([]RBFInfo{
		{Path: "/media/fat/_Console/PSX_20240101.rbf", ShortName: "PSX", MglName: "_Console/PSX"},
	})

	got, err := cache.Resolve(nil, &Core{ID: "PSX", LauncherID: "2XPSX", RBF: "_Console/PSX"})
	require.NoError(t, err)
	assert.Equal(t, "/media/fat/_Console/PSX_20240101.rbf", got.Path)
}

func TestRBFCache_Resolve_NotInCache(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.BuildFromRBFs(nil)

	_, err := cache.Resolve(nil, &Core{ID: "Nintendo64", RBF: "_Console/N64"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Nintendo64")
}

func TestSplitRBFPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input     string
		wantDir   string
		wantShort string
	}{
		{"_Console/SNES", "_Console", "SNES"},
		{"_ConsolePWM/_Turbo/PSX2XCPU_PWM", "_ConsolePWM/_Turbo", "PSX2XCPU_PWM"},
		{"SNES", "", "SNES"},
		{"", "", ""},
		{"_Console/", "_Console", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			dir, short := splitRBFPath(tc.input)
			assert.Equal(t, tc.wantDir, dir)
			assert.Equal(t, tc.wantShort, short)
		})
	}
}

func TestSelectByCanonicalDir(t *testing.T) {
	t.Parallel()

	console := RBFInfo{Path: "/media/fat/_Console/SNES_20240101.rbf", ShortName: "SNES", MglName: "_Console/SNES"}
	unstable := RBFInfo{Path: "/media/fat/_Unstable/SNES_20251001.rbf", ShortName: "SNES", MglName: "_Unstable/SNES"}
	rootLevel := RBFInfo{Path: "/media/fat/SNES_20240101.rbf", ShortName: "SNES", MglName: "SNES"}

	tests := []struct {
		name         string
		canonicalDir string
		wantMglName  string
		candidates   []RBFInfo
		wantOk       bool
	}{
		{
			name:         "empty candidates",
			candidates:   nil,
			canonicalDir: "_Console",
			wantOk:       false,
		},
		{
			name:         "single candidate any dir",
			candidates:   []RBFInfo{unstable},
			canonicalDir: "_Console",
			wantOk:       true,
			wantMglName:  "_Unstable/SNES",
		},
		{
			name:         "canonical match wins",
			candidates:   []RBFInfo{unstable, console},
			canonicalDir: "_Console",
			wantOk:       true,
			wantMglName:  "_Console/SNES",
		},
		{
			name:         "canonical match wins reversed order",
			candidates:   []RBFInfo{console, unstable},
			canonicalDir: "_Console",
			wantOk:       true,
			wantMglName:  "_Console/SNES",
		},
		{
			name:         "no canonical match falls back to first",
			candidates:   []RBFInfo{unstable, console},
			canonicalDir: "_Homebrew",
			wantOk:       true,
			wantMglName:  "_Unstable/SNES",
		},
		{
			name:         "empty canonical dir falls back to first when no root-level core",
			candidates:   []RBFInfo{unstable, console},
			canonicalDir: "",
			wantOk:       true,
			wantMglName:  "_Unstable/SNES",
		},
		{
			name:         "empty canonical dir selects root-level core",
			candidates:   []RBFInfo{console, unstable, rootLevel},
			canonicalDir: "",
			wantOk:       true,
			wantMglName:  "SNES",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := selectByCanonicalDir(tc.candidates, tc.canonicalDir)
			assert.Equal(t, tc.wantOk, ok)
			if ok {
				assert.Equal(t, tc.wantMglName, got.MglName)
			}
		})
	}
}

func TestBuildFromRBFs_PrefersCanonicalDir(t *testing.T) {
	t.Parallel()

	console := RBFInfo{
		Path:      "/media/fat/_Console/SNES_20240101.rbf",
		Filename:  "SNES_20240101.rbf",
		ShortName: "SNES",
		MglName:   "_Console/SNES",
	}
	unstable := RBFInfo{
		Path:      "/media/fat/_Unstable/SNES_20251001.rbf",
		Filename:  "SNES_20251001.rbf",
		ShortName: "SNES",
		MglName:   "_Unstable/SNES",
	}

	// Canonical dir wins regardless of iteration order
	for _, files := range [][]RBFInfo{
		{console, unstable},
		{unstable, console},
	} {
		cache := &RBFCache{}
		cache.BuildFromRBFs(files)

		rbf, ok := cache.bySystemID["SNES"]
		assert.True(t, ok, "SNES should be mapped")
		assert.Equal(t, "_Console/SNES", rbf.MglName, "canonical dir must win")
	}
}

func TestBuildFromRBFs_FallsBackToNonCanonical(t *testing.T) {
	t.Parallel()

	cache := &RBFCache{}
	cache.BuildFromRBFs([]RBFInfo{{
		Path:      "/media/fat/_Unstable/SNES_20251001.rbf",
		Filename:  "SNES_20251001.rbf",
		ShortName: "SNES",
		MglName:   "_Unstable/SNES",
	}})

	rbf, ok := cache.bySystemID["SNES"]
	assert.True(t, ok, "SNES should be mapped via fallback")
	assert.Equal(t, "_Unstable/SNES", rbf.MglName, "fallback to non-canonical when canonical absent")
}

func TestGetByLauncherID_PrefersCanonicalDir(t *testing.T) {
	t.Parallel()

	other := RBFInfo{
		Path:      "/media/fat/_Other/PSX2XCPU_20240101.rbf",
		Filename:  "PSX2XCPU_20240101.rbf",
		ShortName: "PSX2XCPU",
		MglName:   "_Other/PSX2XCPU",
	}
	unstable := RBFInfo{
		Path:      "/media/fat/_Unstable/PSX2XCPU_20251001.rbf",
		Filename:  "PSX2XCPU_20251001.rbf",
		ShortName: "PSX2XCPU",
		MglName:   "_Unstable/PSX2XCPU",
	}

	cache := &RBFCache{
		byShortName: map[string][]RBFInfo{
			"psx2xcpu": {unstable, other},
		},
	}
	cache.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")

	rbf, found := cache.GetByLauncherID("2XPSX")
	assert.True(t, found)
	assert.Equal(t, "_Other/PSX2XCPU", rbf.MglName, "registered canonical dir must win")

	// Fallback when only non-canonical dir present
	cache2 := &RBFCache{
		byShortName: map[string][]RBFInfo{
			"psx2xcpu": {unstable},
		},
	}
	cache2.RegisterAltCore("2XPSX", "_Other/PSX2XCPU")

	rbf, found = cache2.GetByLauncherID("2XPSX")
	assert.True(t, found)
	assert.Equal(t, "_Unstable/PSX2XCPU", rbf.MglName, "falls back to available dir when canonical absent")
}

func TestGetByMglPath(t *testing.T) {
	t.Parallel()

	console := RBFInfo{
		Path:      "/media/fat/_Console/SNES_20240101.rbf",
		Filename:  "SNES_20240101.rbf",
		ShortName: "SNES",
		MglName:   "_Console/SNES",
	}
	unstable := RBFInfo{
		Path:      "/media/fat/_Unstable/SNES_20251001.rbf",
		Filename:  "SNES_20251001.rbf",
		ShortName: "SNES",
		MglName:   "_Unstable/SNES",
	}

	cache := &RBFCache{
		byShortName: map[string][]RBFInfo{
			"snes": {console, unstable},
		},
	}

	// Prefers the directory embedded in the mgl path
	rbf, ok := cache.GetByMglPath("_Unstable/SNES")
	assert.True(t, ok)
	assert.Equal(t, "_Unstable/SNES", rbf.MglName)

	rbf, ok = cache.GetByMglPath("_Console/SNES")
	assert.True(t, ok)
	assert.Equal(t, "_Console/SNES", rbf.MglName)

	// Falls back to first candidate when dir not found
	rbf, ok = cache.GetByMglPath("_Homebrew/SNES")
	assert.True(t, ok)
	assert.Equal(t, "_Console/SNES", rbf.MglName)

	// Returns false when short name unknown
	_, ok = cache.GetByMglPath("_Console/Unknown12345")
	assert.False(t, ok)
}
