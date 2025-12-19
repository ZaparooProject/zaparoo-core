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

package mister

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNeoGeoScanner_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("returns early when context is cancelled at start", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		// Find the NeoGeo launcher
		var neoGeoLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "NeoGeo" {
				neoGeoLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, neoGeoLauncher, "NeoGeo launcher should exist")
		require.NotNil(t, neoGeoLauncher.Scanner, "NeoGeo launcher should have a Scanner")

		// Create an already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Call the scanner
		results, err := neoGeoLauncher.Scanner(ctx, cfg, "NeoGeo", []platforms.ScanResult{})

		// Should return context.Canceled error
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, results)
	})

	t.Run("respects context cancellation during processing", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		var neoGeoLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "NeoGeo" {
				neoGeoLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, neoGeoLauncher)
		require.NotNil(t, neoGeoLauncher.Scanner)

		// Create a context with short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		// Wait for context to be cancelled
		<-ctx.Done()

		// Call the scanner after context is cancelled
		results, err := neoGeoLauncher.Scanner(ctx, cfg, "NeoGeo", []platforms.ScanResult{})

		// Should return context error
		require.Error(t, err)
		assert.Empty(t, results)
	})
}

func TestAmigaScanner_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("returns early when context is cancelled at start", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		// Find the Amiga launcher
		var amigaLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "Amiga" {
				amigaLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, amigaLauncher, "Amiga launcher should exist")
		require.NotNil(t, amigaLauncher.Scanner, "Amiga launcher should have a Scanner")

		// Create an already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Call the scanner
		results, err := amigaLauncher.Scanner(ctx, cfg, "Amiga", []platforms.ScanResult{})

		// Should return context.Canceled error
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, results)
	})
}

func TestNeoGeoScanner_HandlesEmptyDirectory(t *testing.T) {
	t.Parallel()

	// This test verifies the scanner handles empty directories gracefully

	t.Run("handles empty directory without error", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		var neoGeoLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "NeoGeo" {
				neoGeoLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, neoGeoLauncher)
		require.NotNil(t, neoGeoLauncher.Scanner)

		ctx := context.Background()

		// Call scanner - it should handle non-existent paths gracefully
		results, err := neoGeoLauncher.Scanner(ctx, cfg, "NeoGeo", []platforms.ScanResult{})

		// Should not error, just return empty/original results
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestAmigaScanner_HandlesNoPaths(t *testing.T) {
	t.Parallel()

	t.Run("handles no Amiga paths without error", func(t *testing.T) {
		t.Parallel()

		p := NewPlatform()
		cfg := &config.Instance{}
		launchers := p.Launchers(cfg)

		var amigaLauncher *platforms.Launcher
		for i := range launchers {
			if launchers[i].ID == "Amiga" {
				amigaLauncher = &launchers[i]
				break
			}
		}
		require.NotNil(t, amigaLauncher)
		require.NotNil(t, amigaLauncher.Scanner)

		ctx := context.Background()

		// Call scanner - it should handle non-existent paths gracefully
		results, err := amigaLauncher.Scanner(ctx, cfg, "Amiga", []platforms.ScanResult{})

		// Should not error, just return empty/original results
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestFilterNeoGeoGameContents(t *testing.T) {
	t.Parallel()

	// Default NEOGEO paths used in most tests
	defaultNeogeoPaths := []string{"/media/fat/NEOGEO"}

	t.Run("filters out paths inside zips that are games", func(t *testing.T) {
		t.Parallel()

		// Romsets map: mslug is a game, random is not
		romsets := map[string]string{
			"mslug":  "Metal Slug",
			"kof98":  "King of Fighters 98",
			"samsho": "Samurai Shodown",
		}

		input := []platforms.ScanResult{
			// Regular zip files (games) - should be kept
			{Path: "/media/fat/NEOGEO/mslug.zip", Name: ""},
			{Path: "/media/fat/NEOGEO/kof98.zip", Name: ""},
			// Paths inside game zips - should be filtered out
			{Path: "/media/fat/NEOGEO/mslug.zip/mslug.rom", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug.zip/some_internal_file", Name: ""},
			{Path: "/media/fat/NEOGEO/kof98.zip/kof98.p1", Name: ""},
			// Regular file - should be kept
			{Path: "/media/fat/NEOGEO/somegame.neo", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		// Should keep: mslug.zip, kof98.zip, somegame.neo
		// Should filter: mslug.zip/*, kof98.zip/*
		assert.Len(t, result, 3)
		assert.Equal(t, "/media/fat/NEOGEO/mslug.zip", result[0].Path)
		assert.Equal(t, "/media/fat/NEOGEO/kof98.zip", result[1].Path)
		assert.Equal(t, "/media/fat/NEOGEO/somegame.neo", result[2].Path)
	})

	t.Run("filters out paths inside game folders", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug":   "Metal Slug",
			"ct2k3sa": "Crouching Tiger Hidden Dragon 2003 Super Plus",
		}

		input := []platforms.ScanResult{
			// Files inside game folders - should be filtered
			{Path: "/media/fat/NEOGEO/ct2k3sa/crom0", Name: ""},
			{Path: "/media/fat/NEOGEO/ct2k3sa/prom", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug/mslug.rom", Name: ""},
			// Regular .neo file - should be kept
			{Path: "/media/fat/NEOGEO/somegame.neo", Name: ""},
			// File inside non-game folder - should be kept
			{Path: "/media/fat/NEOGEO/collection/game1.neo", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		// Should keep: somegame.neo, collection/game1.neo
		// Should filter: ct2k3sa/*, mslug/*
		assert.Len(t, result, 2)
		assert.Equal(t, "/media/fat/NEOGEO/somegame.neo", result[0].Path)
		assert.Equal(t, "/media/fat/NEOGEO/collection/game1.neo", result[1].Path)
	})

	t.Run("filters both zip contents and folder contents", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		input := []platforms.ScanResult{
			// Inside game zip - filtered
			{Path: "/media/fat/NEOGEO/mslug.zip/mslug.rom", Name: ""},
			// Inside game folder - filtered
			{Path: "/media/fat/NEOGEO/mslug/mslug.rom", Name: ""},
			// Game zip itself - kept
			{Path: "/media/fat/NEOGEO/mslug.zip", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		assert.Len(t, result, 1)
		assert.Equal(t, "/media/fat/NEOGEO/mslug.zip", result[0].Path)
	})

	t.Run("keeps paths inside zips that are folders (not in romsets)", func(t *testing.T) {
		t.Parallel()

		// Only mslug is a game
		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		input := []platforms.ScanResult{
			// Paths inside a folder zip (not a game) - should be kept
			{Path: "/media/fat/NEOGEO/collection.zip/game1.neo", Name: ""},
			{Path: "/media/fat/NEOGEO/collection.zip/game2.neo", Name: ""},
			// Paths inside a game zip - should be filtered
			{Path: "/media/fat/NEOGEO/mslug.zip/mslug.rom", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		// Should keep the folder zip contents, filter the game zip contents
		assert.Len(t, result, 2)
		assert.Equal(t, "/media/fat/NEOGEO/collection.zip/game1.neo", result[0].Path)
		assert.Equal(t, "/media/fat/NEOGEO/collection.zip/game2.neo", result[1].Path)
	})

	t.Run("handles case insensitive matching for zips", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug", // lowercase in romsets
		}

		input := []platforms.ScanResult{
			// Mixed case zip name - should still match
			{Path: "/media/fat/NEOGEO/MSLUG.ZIP/mslug.rom", Name: ""},
			{Path: "/media/fat/NEOGEO/MsLuG.zip/internal", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		// Both should be filtered (case insensitive match)
		assert.Empty(t, result)
	})

	t.Run("handles case insensitive matching for folders", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug", // lowercase in romsets
		}

		input := []platforms.ScanResult{
			{Path: "/media/fat/NEOGEO/MSLUG/crom0", Name: ""},
			{Path: "/media/fat/NEOGEO/MsLuG/prom", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		// Both should be filtered (case insensitive match)
		assert.Empty(t, result)
	})

	t.Run("handles empty inputs", func(t *testing.T) {
		t.Parallel()

		// Empty romsets
		result1 := filterNeoGeoGameContents([]platforms.ScanResult{
			{Path: "/media/fat/NEOGEO/mslug.zip/mslug.rom", Name: ""},
		}, map[string]string{}, defaultNeogeoPaths)

		// With empty romsets, nothing matches, so all are kept
		assert.Len(t, result1, 1)

		// Empty input
		result2 := filterNeoGeoGameContents([]platforms.ScanResult{}, map[string]string{
			"mslug": "Metal Slug",
		}, defaultNeogeoPaths)
		assert.Empty(t, result2)

		// Empty NEOGEO paths - folder filtering requires paths to compare against
		result3 := filterNeoGeoGameContents([]platforms.ScanResult{
			{Path: "/media/fat/NEOGEO/mslug/crom0", Name: ""},
		}, map[string]string{
			"mslug": "Metal Slug",
		}, []string{})
		assert.Len(t, result3, 1)
	})

	t.Run("handles direct children of NEOGEO", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		input := []platforms.ScanResult{
			// Direct children - should be kept
			{Path: "/media/fat/NEOGEO/game.neo", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug.zip", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		// Both should be kept - they're direct children, not inside folders
		assert.Len(t, result, 2)
	})

	t.Run("keeps files in nested non-game folders", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		input := []platforms.ScanResult{
			// Nested inside a non-game folder - should be kept
			{Path: "/media/fat/NEOGEO/collection/subfolder/game.neo", Name: ""},
			// First level inside non-game folder - should be kept
			{Path: "/media/fat/NEOGEO/collection/game.neo", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		assert.Len(t, result, 2)
	})

	t.Run("handles multiple NEOGEO paths", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		neogeoPaths := []string{
			"/media/fat/NEOGEO",
			"/media/usb0/NEOGEO",
		}

		input := []platforms.ScanResult{
			{Path: "/media/fat/NEOGEO/mslug/crom0", Name: ""},
			{Path: "/media/usb0/NEOGEO/mslug/prom", Name: ""},
			{Path: "/media/fat/NEOGEO/other.neo", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, neogeoPaths)

		assert.Len(t, result, 1)
		assert.Equal(t, "/media/fat/NEOGEO/other.neo", result[0].Path)
	})

	t.Run("handles paths with trailing slashes", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		// Path with trailing slash
		neogeoPaths := []string{"/media/fat/NEOGEO/"}

		input := []platforms.ScanResult{
			{Path: "/media/fat/NEOGEO/mslug/crom0", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, neogeoPaths)

		assert.Empty(t, result)
	})

	t.Run("handles nested zip paths", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		input := []platforms.ScanResult{
			// Nested path inside game zip
			{Path: "/media/fat/NEOGEO/mslug.zip/subdir/file.rom", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		// Should be filtered (mslug is a game)
		assert.Empty(t, result)
	})
}

func TestIsInsideGameFolder(t *testing.T) {
	t.Parallel()

	t.Run("returns true for path inside game folder", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}
		neogeoPaths := []string{"/media/fat/neogeo"}

		result := isInsideGameFolder("/media/fat/neogeo/mslug/crom0", romsets, neogeoPaths)
		assert.True(t, result)
	})

	t.Run("returns false for direct child of NEOGEO", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}
		neogeoPaths := []string{"/media/fat/neogeo"}

		result := isInsideGameFolder("/media/fat/neogeo/mslug.zip", romsets, neogeoPaths)
		assert.False(t, result)
	})

	t.Run("returns false for path in non-game folder", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}
		neogeoPaths := []string{"/media/fat/neogeo"}

		result := isInsideGameFolder("/media/fat/neogeo/collection/game.neo", romsets, neogeoPaths)
		assert.False(t, result)
	})

	t.Run("returns false for path not under NEOGEO", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}
		neogeoPaths := []string{"/media/fat/neogeo"}

		result := isInsideGameFolder("/media/fat/other/mslug/crom0", romsets, neogeoPaths)
		assert.False(t, result)
	})
}

func TestFilterNeoGeoGameContents_CommaSeparatedRomsets(t *testing.T) {
	t.Parallel()

	t.Run("filters folder matching any alias from comma-separated name", func(t *testing.T) {
		t.Parallel()

		// Simulates how romsets would be parsed after splitting comma-separated names
		// e.g., romsets.xml has: name="samsh5spho,smsh5spo"
		// Both aliases should be in the map pointing to the same alt name
		romsets := map[string]string{
			"samsh5spho": "Samurai Shodown V Special",
			"smsh5spo":   "Samurai Shodown V Special",
		}

		neogeoPaths := []string{"/media/fat/games/NEOGEO"}

		input := []platforms.ScanResult{
			// Folder using the short alias
			{Path: "/media/fat/games/NEOGEO/smsh5spo/crom0", Name: ""},
			{Path: "/media/fat/games/NEOGEO/smsh5spo/prom", Name: ""},
			// Folder using the long alias
			{Path: "/media/fat/games/NEOGEO/samsh5spho/crom0", Name: ""},
			// Game files that should be kept
			{Path: "/media/fat/games/NEOGEO/smsh5spo.zip", Name: ""},
			{Path: "/media/fat/games/NEOGEO/game.neo", Name: ""},
		}

		result := filterNeoGeoGameContents(input, romsets, neogeoPaths)

		// Only the .zip and .neo should remain
		assert.Len(t, result, 2)
		assert.Equal(t, "/media/fat/games/NEOGEO/smsh5spo.zip", result[0].Path)
		assert.Equal(t, "/media/fat/games/NEOGEO/game.neo", result[1].Path)
	})
}
