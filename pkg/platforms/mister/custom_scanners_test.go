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

package mister

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
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

func TestAmigaScanner_IgnoresStaleListingRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stalePath := filepath.Join(root, "Amiga")
	validPath := filepath.Join(root, "games", "Amiga")
	writeAmigaListings(t, stalePath, "Stale Game")
	writeAmigaVisionInstall(t, validPath, "Valid Game")

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root, filepath.Join(root, "games")},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(cfg))

	results, err := amigaLauncher.Scanner(context.Background(), cfg, "Amiga", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, filepath.Join(validPath, "Games", "Valid Game"), results[0].Path)
	assert.Empty(t, results[0].Name)
}

func TestAmigaScanner_AddsGamesAndDemosSubfolders(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	validPath := filepath.Join(root, "games", "Amiga")
	writeAmigaVisionInstall(t, validPath, "Valid Game")
	require.NoError(t, os.WriteFile(filepath.Join(validPath, "listings", "demos.txt"), []byte("Valid Demo\n"), 0o600))

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root, filepath.Join(root, "games")},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(cfg))

	results, err := amigaLauncher.Scanner(context.Background(), cfg, "Amiga", nil)
	require.NoError(t, err)
	assert.Contains(t, results, platforms.ScanResult{
		Path:  filepath.Join(validPath, "Games", "Valid Game"),
		NoExt: true,
	})
	assert.Contains(t, results, platforms.ScanResult{
		Path:  filepath.Join(validPath, "Demos", "Valid Demo"),
		NoExt: true,
	})
}

func TestAmigaScanner_FiltersListingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	validPath := filepath.Join(root, "games", "Amiga")
	writeAmigaVisionInstall(t, validPath, "Valid Game")

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root, filepath.Join(root, "games")},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(cfg))
	initial := []platforms.ScanResult{{Path: filepath.Join(validPath, "listings", "games.txt")}}

	results, err := amigaLauncher.Scanner(context.Background(), cfg, "Amiga", initial)
	require.NoError(t, err)
	assert.NotContains(t, results, platforms.ScanResult{Path: filepath.Join(validPath, "listings", "games.txt")})
	assert.Contains(t, results, platforms.ScanResult{
		Path:  filepath.Join(validPath, "Games", "Valid Game"),
		NoExt: true,
	})
}

func TestAmigaScanner_LeavesNameEmptyForIndexerTitleCleaning(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	validPath := filepath.Join(root, "games", "Amiga")

	listingPath := filepath.Join(validPath, "listings")
	require.NoError(t, os.MkdirAll(listingPath, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(validPath, "shared"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(validPath, "AmigaVision.hdf"), []byte("test"), 0o600))
	gamesContent := "1869 (AGA)[en]\n3D Pool (OCS)[en]\n7 Colors (OCS)[en-de-fr-it-es]\n"
	demosContent := "1001 Stolen Ideas (Airwalk)(AGA)\n9 Fingers (Spaceballs)(OCS)\n"
	require.NoError(t, os.WriteFile(filepath.Join(listingPath, "games.txt"), []byte(gamesContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(listingPath, "demos.txt"), []byte(demosContent), 0o600))

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{filepath.Join(root, "games")},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(cfg))

	results, err := amigaLauncher.Scanner(context.Background(), cfg, "Amiga", nil)
	require.NoError(t, err)

	for _, r := range results {
		assert.Empty(t, r.Name, "ScanResult.Name must be empty so the indexer derives a cleaned title from the path")
	}

	// Paths still carry the raw listing line so launch validation can match exactly.
	assert.Contains(t, results, platforms.ScanResult{
		Path:  filepath.Join(validPath, "Games", "1869 (AGA)[en]"),
		NoExt: true,
	})
	assert.Contains(t, results, platforms.ScanResult{
		Path:  filepath.Join(validPath, "Demos", "1001 Stolen Ideas (Airwalk)(AGA)"),
		NoExt: true,
	})
}

func TestAmigaLauncher_DoesNotMatchListingBackupFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	validPath := filepath.Join(root, "games", "Amiga")
	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root, filepath.Join(root, "games")},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(cfg))
	assert.True(t, amigaLauncher.Test(cfg, filepath.Join(validPath, "listings", "games.txt")))
	assert.True(t, amigaLauncher.Test(cfg, filepath.Join(validPath, "listings", "demos.txt")))
	assert.False(t, amigaLauncher.Test(cfg, filepath.Join(validPath, "listings", "games.txt.bak")))
	assert.False(t, amigaLauncher.Test(cfg, filepath.Join(validPath, "listings", "demos.txt.bak")))
}

func TestAmigaScanner_AddsVirtualMGLFiles(t *testing.T) {
	t.Parallel()

	installPath := filepath.Join(t.TempDir(), "games", "Amiga")
	mglDir := t.TempDir()
	amigaMGL := filepath.Join(mglDir, "Amiga.mgl")
	amiga500MGL := filepath.Join(mglDir, "Amiga 500.mgl")
	require.NoError(t, os.WriteFile(amigaMGL, []byte("test"), 0o600))
	require.NoError(t, os.WriteFile(amiga500MGL, []byte("test"), 0o600))

	mglPaths := []string{
		amigaMGL,
		amiga500MGL,
		filepath.Join(mglDir, "Missing.mgl"),
	}
	results := amigaVisionMGLScanResults(installPath, mglPaths)

	assert.Equal(t, []platforms.ScanResult{
		{Path: filepath.Join(installPath, "Amiga.mgl"), Name: "Amiga"},
		{Path: filepath.Join(installPath, "Amiga 500.mgl"), Name: "Amiga 500"},
	}, results)
}

func TestResolveAmigaVisionVirtualMGLPath(t *testing.T) {
	mglDir := t.TempDir()
	realMGL := filepath.Join(mglDir, "Amiga.mgl")
	virtualMGL := filepath.Join(t.TempDir(), "games", "Amiga", "Amiga.mgl")
	realVirtualMGL := filepath.Join(t.TempDir(), "games", "Amiga", "Amiga.mgl")
	require.NoError(t, os.WriteFile(realMGL, []byte("test"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Dir(realVirtualMGL), 0o700))
	require.NoError(t, os.WriteFile(realVirtualMGL, []byte("test"), 0o600))

	oldPaths := amigaVisionMGLPaths
	amigaVisionMGLPaths = []string{realMGL}
	t.Cleanup(func() { amigaVisionMGLPaths = oldPaths })

	installPath := filepath.Join(t.TempDir(), "games", "Amiga")
	routedVirtualMGL := filepath.Join(installPath, "Amiga.mgl")
	require.NoError(t, os.MkdirAll(installPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(installPath, "AmigaVision.hdf"), []byte("test"), 0o600))

	missingMGL := filepath.Join(t.TempDir(), "games", "Amiga", "Amiga 500.mgl")
	nonMGL := filepath.Join(t.TempDir(), "games", "Amiga", "Readme.txt")
	mglWithoutAmigaVision := filepath.Join(t.TempDir(), "games", "Amiga", "Amiga.mgl")

	assert.Equal(t, realMGL, resolveAmigaVisionVirtualMGLPath(virtualMGL))
	assert.Equal(t, realVirtualMGL, resolveAmigaVisionVirtualMGLPath(realVirtualMGL))
	assert.Equal(t, missingMGL, resolveAmigaVisionVirtualMGLPath(missingMGL))
	assert.Equal(t, nonMGL, resolveAmigaVisionVirtualMGLPath(nonMGL))
	assert.True(t, isAmigaVisionVirtualMGLPath(routedVirtualMGL))
	assert.False(t, isAmigaVisionVirtualMGLPath(mglWithoutAmigaVision))

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(&config.Instance{}))
	assert.True(t, amigaLauncher.Test(nil, routedVirtualMGL))
}

func TestAmigaLauncher_TestRequiresAmigaVisionVirtualPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	validPath := filepath.Join(root, "games", "Amiga")
	writeAmigaVisionInstall(t, validPath, "Valid Game")
	nonAmigaVisionPath := filepath.Join(root, "other", "Amiga")
	require.NoError(t, os.MkdirAll(filepath.Join(nonAmigaVisionPath, "Games"), 0o700))

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(&config.Instance{}))

	assert.True(t, amigaLauncher.Test(nil, filepath.Join(validPath, "Games", "Valid Game")))
	assert.False(t, amigaLauncher.Test(nil, filepath.Join(nonAmigaVisionPath, "Games", "Other Game")))
}

func TestAmigaScanner_RequiresBootImage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stalePath := filepath.Join(root, "Amiga")
	writeAmigaListings(t, stalePath, "Stale Game")

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	amigaLauncher := findAmigaLauncher(t, p.Launchers(cfg))

	results, err := amigaLauncher.Scanner(context.Background(), cfg, "Amiga", nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func findAmigaLauncher(t *testing.T, launchers []platforms.Launcher) *platforms.Launcher {
	t.Helper()

	for i := range launchers {
		if launchers[i].ID == "Amiga" {
			return &launchers[i]
		}
	}
	require.FailNow(t, "Amiga launcher should exist")
	return nil
}

func findNeoGeoLauncher(t *testing.T, launchers []platforms.Launcher) *platforms.Launcher {
	t.Helper()

	for i := range launchers {
		if launchers[i].ID == "NeoGeo" {
			return &launchers[i]
		}
	}
	require.FailNow(t, "NeoGeo launcher should exist")
	return nil
}

func writeAmigaVisionInstall(t *testing.T, path, game string) {
	t.Helper()

	writeAmigaListings(t, path, game)
	err := os.WriteFile(filepath.Join(path, "AmigaVision.hdf"), []byte("test"), 0o600)
	require.NoError(t, err)
}

func writeAmigaListings(t *testing.T, path, game string) {
	t.Helper()

	listingPath := filepath.Join(path, "listings")
	err := os.MkdirAll(listingPath, 0o700)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(path, "shared"), 0o700)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(listingPath, "games.txt"), []byte(game+"\n"), 0o600)
	require.NoError(t, err)
}

func TestNeoGeoScanner_AddsNestedRomsetEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	neoGeoPath := filepath.Join(root, "NEOGEO")
	favoritesPath := filepath.Join(neoGeoPath, "Favorites")
	zipPath := filepath.Join(favoritesPath, "mslug.zip")
	folderPath := filepath.Join(favoritesPath, "kof98")
	nestedNeoPath := filepath.Join(favoritesPath, "collection", "game.neo")

	require.NoError(t, os.MkdirAll(folderPath, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(nestedNeoPath), 0o700))
	require.NoError(t, os.WriteFile(zipPath, []byte("test"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(folderPath, "crom0"), []byte("test"), 0o600))
	require.NoError(t, os.WriteFile(nestedNeoPath, []byte("test"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(neoGeoPath, "romsets.xml"), []byte(`<?xml version="1.0"?>
<romsets>
  <romset name="mslug" altname="Metal Slug"/>
  <romset name="kof98" altname="King of Fighters '98"/>
</romsets>
`), 0o600))

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	neoGeoLauncher := findNeoGeoLauncher(t, p.Launchers(cfg))
	initial := []platforms.ScanResult{
		{Path: filepath.Join(zipPath, "crom0")},
		{Path: filepath.Join(folderPath, "crom0")},
		{Path: nestedNeoPath},
	}

	results, err := neoGeoLauncher.Scanner(context.Background(), cfg, "NeoGeo", initial)
	require.NoError(t, err)

	assert.NotContains(t, results, platforms.ScanResult{Path: filepath.Join(zipPath, "crom0")})
	assert.NotContains(t, results, platforms.ScanResult{Path: filepath.Join(folderPath, "crom0")})
	assert.Contains(t, results, platforms.ScanResult{Path: nestedNeoPath})
	assert.Contains(t, results, platforms.ScanResult{Path: zipPath, Name: "Metal Slug", NoExt: true})
	assert.Contains(t, results, platforms.ScanResult{Path: folderPath, Name: "King of Fighters '98", NoExt: true})
}

func TestNeoGeoScanner_AddsRootRomsetEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	neoGeoPath := filepath.Join(root, "NEOGEO")
	folderPath := filepath.Join(neoGeoPath, "MSLUG")
	zipPath := filepath.Join(neoGeoPath, "kof98.zip")

	require.NoError(t, os.MkdirAll(folderPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(folderPath, "crom0"), []byte("test"), 0o600))
	require.NoError(t, os.WriteFile(zipPath, []byte("test"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(neoGeoPath, "romsets.xml"), []byte(`<?xml version="1.0"?>
<romsets>
  <romset name="mslug" altname="Metal Slug"/>
  <romset name="kof98" altname="King of Fighters '98"/>
</romsets>
`), 0o600))

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	neoGeoLauncher := findNeoGeoLauncher(t, p.Launchers(cfg))
	results, err := neoGeoLauncher.Scanner(context.Background(), cfg, "NeoGeo", []platforms.ScanResult{
		{Path: filepath.Join(folderPath, "crom0")},
	})
	require.NoError(t, err)

	assert.NotContains(t, results, platforms.ScanResult{Path: filepath.Join(folderPath, "crom0")})
	assert.Contains(t, results, platforms.ScanResult{Path: folderPath, Name: "Metal Slug", NoExt: true})
	assert.Contains(t, results, platforms.ScanResult{Path: zipPath, Name: "King of Fighters '98", NoExt: true})
}

func TestNeoGeoScanner_AddsRootSymlinkedRomsetFolder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	neoGeoPath := filepath.Join(root, "NEOGEO")
	targetPath := filepath.Join(root, "romsets", "mslug")
	linkPath := filepath.Join(neoGeoPath, "mslug")
	require.NoError(t, os.MkdirAll(targetPath, 0o700))
	require.NoError(t, os.MkdirAll(neoGeoPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(targetPath, "crom0"), []byte("test"), 0o600))
	require.NoError(t, os.Symlink(targetPath, linkPath))
	require.NoError(t, os.WriteFile(filepath.Join(neoGeoPath, "romsets.xml"), []byte(`<?xml version="1.0"?>
<romsets>
  <romset name="mslug" altname="Metal Slug"/>
</romsets>
`), 0o600))

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	neoGeoLauncher := findNeoGeoLauncher(t, p.Launchers(cfg))
	results, err := neoGeoLauncher.Scanner(context.Background(), cfg, "NeoGeo", nil)
	require.NoError(t, err)

	assert.Contains(t, results, platforms.ScanResult{Path: linkPath, Name: "Metal Slug", NoExt: true})
}

func TestNeoGeoScanner_HandlesMalformedRomsets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	neoGeoPath := filepath.Join(root, "NEOGEO")
	folderPath := filepath.Join(neoGeoPath, "mslug")
	contentPath := filepath.Join(folderPath, "crom0")
	require.NoError(t, os.MkdirAll(folderPath, 0o700))
	require.NoError(t, os.WriteFile(contentPath, []byte("test"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(neoGeoPath, "romsets.xml"), []byte("<romsets>"), 0o600))

	cfg, err := config.NewConfig(t.TempDir(), config.Values{
		Launchers: config.Launchers{
			IndexRoot: []string{root},
		},
	})
	require.NoError(t, err)

	p := NewPlatform()
	neoGeoLauncher := findNeoGeoLauncher(t, p.Launchers(cfg))
	results, err := neoGeoLauncher.Scanner(context.Background(), cfg, "NeoGeo", []platforms.ScanResult{
		{Path: contentPath},
	})
	require.NoError(t, err)

	assert.Equal(t, []platforms.ScanResult{{Path: contentPath}}, results)
}

func TestCollectNeoGeoRomsetEntries_DeduplicatesOverlappingRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	neoGeoPath := filepath.Join(root, "NEOGEO")
	favoritesPath := filepath.Join(neoGeoPath, "Favorites")
	zipPath := filepath.Join(favoritesPath, "mslug.zip")
	require.NoError(t, os.MkdirAll(favoritesPath, 0o700))
	require.NoError(t, os.WriteFile(zipPath, []byte("test"), 0o600))

	romsets := map[string]string{
		"mslug": "Metal Slug",
	}
	seen := make(map[string]struct{})
	fs := afero.NewOsFs()

	first, err := collectNeoGeoRomsetEntries(context.Background(), fs, neoGeoPath, romsets, seen)
	require.NoError(t, err)
	second, err := collectNeoGeoRomsetEntries(context.Background(), fs, favoritesPath, romsets, seen)
	require.NoError(t, err)

	assert.Equal(t, []platforms.ScanResult{{Path: zipPath, Name: "Metal Slug", NoExt: true}}, first)
	assert.Empty(t, second)
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

	t.Run("filters nested extracted game folder contents", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}

		nestedRomPath := filepath.Join(
			string(filepath.Separator), "media", "fat", "NEOGEO", "favorites", "mslug", "crom0",
		)
		nestedNeoPath := filepath.Join(
			string(filepath.Separator), "media", "fat", "NEOGEO", "favorites", "collection", "game.neo",
		)
		input := []platforms.ScanResult{
			{Path: nestedRomPath},
			{Path: nestedNeoPath},
		}

		result := filterNeoGeoGameContents(input, romsets, defaultNeogeoPaths)

		require.Len(t, result, 1)
		assert.Equal(t, nestedNeoPath, result[0].Path)
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

	t.Run("returns true for path inside nested game folder", func(t *testing.T) {
		t.Parallel()

		romsets := map[string]string{
			"mslug": "Metal Slug",
		}
		neogeoPaths := []string{filepath.Join(string(filepath.Separator), "media", "fat", "neogeo")}

		result := isInsideGameFolder(
			filepath.Join(string(filepath.Separator), "media", "fat", "neogeo", "favorites", "mslug", "crom0"),
			romsets,
			neogeoPaths,
		)
		assert.True(t, result)
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

func TestFilterNeoGeoZipToNeoOnly(t *testing.T) {
	t.Parallel()

	t.Run("keeps .neo files from zips and adds zips without .neo as games", func(t *testing.T) {
		t.Parallel()

		input := []platforms.ScanResult{
			// mslug.zip has no .neo files - zip itself should be added
			{Path: "/media/fat/NEOGEO/mslug.zip/crom0", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug.zip/prom", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug.zip/srom", Name: ""},
			// collection.zip has .neo files - only keep the .neo files
			{Path: "/media/fat/NEOGEO/collection.zip/game1.neo", Name: ""},
			{Path: "/media/fat/NEOGEO/collection.zip/game2.neo", Name: ""},
		}

		result := filterNeoGeoZipToNeoOnly(input)

		assert.Len(t, result, 3)
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/collection.zip/game1.neo"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/collection.zip/game2.neo"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/mslug.zip"})
	})

	t.Run("keeps all top-level files", func(t *testing.T) {
		t.Parallel()

		input := []platforms.ScanResult{
			{Path: "/media/fat/NEOGEO/game.neo", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug.zip", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug/crom0", Name: ""},
		}

		result := filterNeoGeoZipToNeoOnly(input)

		assert.Len(t, result, 3)
		assert.Equal(t, "/media/fat/NEOGEO/game.neo", result[0].Path)
		assert.Equal(t, "/media/fat/NEOGEO/mslug.zip", result[1].Path)
		assert.Equal(t, "/media/fat/NEOGEO/mslug/crom0", result[2].Path)
	})

	t.Run("handles case-insensitive path matching", func(t *testing.T) {
		t.Parallel()

		input := []platforms.ScanResult{
			// MSLUG.ZIP has no .neo - zip itself added
			{Path: "/media/fat/NEOGEO/MSLUG.ZIP/crom0", Name: ""},
			// collection zips have .neo - keep the .neo files
			{Path: "/media/fat/NEOGEO/collection.ZIP/GAME.NEO", Name: ""},
			{Path: "/media/fat/NEOGEO/collection.zip/Game.Neo", Name: ""},
		}

		result := filterNeoGeoZipToNeoOnly(input)

		assert.Len(t, result, 3)
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/collection.ZIP/GAME.NEO"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/collection.zip/Game.Neo"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/MSLUG.ZIP"})
	})

	t.Run("handles mixed paths correctly", func(t *testing.T) {
		t.Parallel()

		input := []platforms.ScanResult{
			// mslug.zip has no .neo - zip itself added
			{Path: "/media/fat/NEOGEO/mslug.zip/mslug.rom", Name: ""},
			// collection.zip has .neo - keep the .neo
			{Path: "/media/fat/NEOGEO/collection.zip/game.neo", Name: ""},
			// Top-level files - kept
			{Path: "/media/fat/NEOGEO/standalone.neo", Name: ""},
			{Path: "/media/fat/NEOGEO/kof98.zip", Name: ""},
			{Path: "/media/fat/NEOGEO/mslug/crom0", Name: ""},
		}

		result := filterNeoGeoZipToNeoOnly(input)

		assert.Len(t, result, 5)
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/collection.zip/game.neo"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/standalone.neo"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/kof98.zip"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/mslug/crom0"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/mslug.zip"})
	})

	t.Run("handles empty input", func(t *testing.T) {
		t.Parallel()

		input := []platforms.ScanResult{}
		result := filterNeoGeoZipToNeoOnly(input)
		assert.Empty(t, result)
	})

	t.Run("adds zips without .neo files as launchable games", func(t *testing.T) {
		t.Parallel()

		input := []platforms.ScanResult{
			// Both zips have no .neo - both should be added as games
			{Path: "/media/fat/NEOGEO/mslug.zip/crom0", Name: ""},
			{Path: "/media/fat/NEOGEO/kof98.zip/prom", Name: ""},
		}

		result := filterNeoGeoZipToNeoOnly(input)

		assert.Len(t, result, 2)
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/mslug.zip"})
		assert.Contains(t, result, platforms.ScanResult{Path: "/media/fat/NEOGEO/kof98.zip"})
	})
}
