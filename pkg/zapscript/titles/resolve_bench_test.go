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

package titles

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
)

// benchLaunchers provides test launchers for the NES system.
var benchLaunchers = []platforms.Launcher{{
	ID:         "nes-launcher",
	SystemID:   "nes",
	Extensions: []string{".nes"},
	Folders:    []string{"/roms/system/"},
}}

// setupResolveBenchDB creates a real MediaDB populated with n titles and returns
// the DB, a list of inserted game names (for query construction), and a cleanup
// function.
func setupResolveBenchDB(b *testing.B, n int) (
	db *mediadb.MediaDB, gameNames []string, cleanup func(),
) {
	b.Helper()

	db, cleanup = testhelpers.NewInMemoryMediaDB(b)

	// Seed canonical tags (required before AddMediaPath)
	ss := &database.ScanState{
		SystemIDs:  make(map[string]int),
		TitleIDs:   make(map[string]int),
		MediaIDs:   make(map[string]int),
		TagTypeIDs: make(map[string]int),
		TagIDs:     make(map[string]int),
	}
	if err := mediascanner.SeedCanonicalTags(db, ss); err != nil {
		b.Fatal(err)
	}

	// Generate deterministic filenames
	filenames := fixtures.BuildBenchFilenames(n)

	// Populate DB with titles via real production AddMediaPath
	if err := db.BeginTransaction(true); err != nil {
		b.Fatal(err)
	}
	for i, fn := range filenames {
		_, _, err := mediascanner.AddMediaPath(db, ss, "nes", fn, false, false, nil)
		if i == 0 && err != nil {
			b.Fatal(err)
		}
		if (i+1)%10_000 == 0 {
			if err := db.CommitTransaction(); err != nil {
				b.Fatal(err)
			}
			mediascanner.FlushScanStateMaps(ss)
			if err := db.BeginTransaction(true); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := db.CommitTransaction(); err != nil {
		b.Fatal(err)
	}

	// Build slug search cache (required for search operations)
	if err := db.RebuildSlugSearchCache(); err != nil {
		b.Fatal(err)
	}

	// Initialize global launcher cache
	helpers.GlobalLauncherCache.InitializeFromSlice(benchLaunchers)

	// Extract game names from filenames for query construction
	const pathPrefix = "/roms/system/"
	const ext = ".nes"
	gameNames = make([]string, n)
	for i, fn := range filenames {
		gameNames[i] = fn[len(pathPrefix) : len(fn)-len(ext)]
	}

	return db, gameNames, cleanup
}

func BenchmarkResolveTitle_CacheHit(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "10k", n: 10_000},
		{name: "50k", n: 50_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			db, gameNames, cleanup := setupResolveBenchDB(b, sz.n)
			defer cleanup()

			ctx := context.Background()
			gameName := gameNames[0]
			params := &ResolveParams{
				MediaDB:   db,
				Cfg:       nil,
				SystemID:  "nes",
				GameName:  gameName,
				MediaType: slugs.MediaTypeGame,
				Launchers: benchLaunchers,
			}

			// Seed the resolution cache with a first call
			_, err := ResolveTitle(ctx, params)
			if err != nil {
				b.Fatalf("initial resolve failed: %v", err)
			}

			b.ResetTimer()
			for b.Loop() {
				_, _ = ResolveTitle(ctx, params)
			}
		})
	}
}

func BenchmarkResolveTitle_ExactMatch(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "10k", n: 10_000},
		{name: "50k", n: 50_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			db, gameNames, cleanup := setupResolveBenchDB(b, sz.n)
			defer cleanup()

			ctx := context.Background()

			// Cycle through different game names to avoid resolution cache hits
			b.ResetTimer()
			idx := 0
			for b.Loop() {
				gameName := gameNames[idx%len(gameNames)]
				idx++
				params := &ResolveParams{
					MediaDB:   db,
					Cfg:       nil,
					SystemID:  "nes",
					GameName:  gameName,
					MediaType: slugs.MediaTypeGame,
					Launchers: benchLaunchers,
				}
				// Clear the resolution cache (not the slug search cache) so each
				// iteration exercises the full slug match strategy instead of
				// hitting the cached resolution from a previous iteration
				_ = db.InvalidateSlugCache(ctx)
				_, _ = ResolveTitle(ctx, params)
			}
		})
	}
}

func BenchmarkResolveTitle_FuzzyFallback(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "10k", n: 10_000},
		{name: "50k", n: 50_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			db, _, cleanup := setupResolveBenchDB(b, sz.n)
			defer cleanup()

			ctx := context.Background()

			// Use a misspelled name that won't match exactly but should
			// find a fuzzy match
			params := &ResolveParams{
				MediaDB:   db,
				Cfg:       nil,
				SystemID:  "nes",
				GameName:  "Supr Maro Brothrs Advnture",
				MediaType: slugs.MediaTypeGame,
				Launchers: benchLaunchers,
			}

			b.ResetTimer()
			for b.Loop() {
				// Clear resolution cache to force full strategy cascade
				_ = db.InvalidateSlugCache(ctx)
				// Fuzzy match may or may not succeed; we're measuring the
				// cost of falling through strategies
				_, _ = ResolveTitle(ctx, params)
			}
		})
	}
}
