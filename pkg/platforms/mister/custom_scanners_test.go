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

func TestFilterNeoGeoZipContents(t *testing.T) {
	t.Parallel()

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

		result := filterNeoGeoZipContents(input, romsets)

		// Should keep: mslug.zip, kof98.zip, somegame.neo
		// Should filter: mslug.zip/*, kof98.zip/*
		assert.Len(t, result, 3)
		assert.Equal(t, "/media/fat/NEOGEO/mslug.zip", result[0].Path)
		assert.Equal(t, "/media/fat/NEOGEO/kof98.zip", result[1].Path)
		assert.Equal(t, "/media/fat/NEOGEO/somegame.neo", result[2].Path)
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

		result := filterNeoGeoZipContents(input, romsets)

		// Should keep the folder zip contents, filter the game zip contents
		assert.Len(t, result, 2)
		assert.Equal(t, "/media/fat/NEOGEO/collection.zip/game1.neo", result[0].Path)
		assert.Equal(t, "/media/fat/NEOGEO/collection.zip/game2.neo", result[1].Path)
	})

	t.Run("handles case insensitive matching", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug", // lowercase in romsets
		}

		input := []platforms.ScanResult{
			// Mixed case zip name - should still match
			{Path: "/media/fat/NEOGEO/MSLUG.ZIP/mslug.rom", Name: ""},
			{Path: "/media/fat/NEOGEO/MsLuG.zip/internal", Name: ""},
		}

		result := filterNeoGeoZipContents(input, romsets)

		// Both should be filtered (case insensitive match)
		assert.Empty(t, result)
	})

	t.Run("handles empty inputs", func(t *testing.T) {
		t.Parallel()

		// Empty romsets
		result1 := filterNeoGeoZipContents([]platforms.ScanResult{
			{Path: "/media/fat/NEOGEO/mslug.zip/mslug.rom", Name: ""},
		}, map[string]string{})

		// With empty romsets, nothing matches, so all are kept
		assert.Len(t, result1, 1)

		// Empty input
		result2 := filterNeoGeoZipContents([]platforms.ScanResult{}, map[string]string{
			"mslug": "Metal Slug",
		})
		assert.Empty(t, result2)
	})

	t.Run("handles paths without .zip/ pattern", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		input := []platforms.ScanResult{
			// Regular paths - no .zip/ pattern
			{Path: "/media/fat/NEOGEO/mslug.zip", Name: ""},
			{Path: "/media/fat/NEOGEO/game.neo", Name: ""},
			{Path: "/media/fat/NEOGEO/subfolder/game.neo", Name: ""},
		}

		result := filterNeoGeoZipContents(input, romsets)

		// All should be kept (no filtering needed)
		assert.Len(t, result, 3)
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

		result := filterNeoGeoZipContents(input, romsets)

		// Should be filtered (mslug is a game)
		assert.Empty(t, result)
	})
}
