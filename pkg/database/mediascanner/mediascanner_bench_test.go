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

package mediascanner

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/browseprefix"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
)

func BenchmarkGetPathFragments(b *testing.B) {
	cases := []struct {
		name string
		path string
	}{
		{"Simple", "/roms/NES/Super Mario Bros.nes"},
		{"Complex", "/roms/SNES/Final Fantasy VI (USA, Europe) (Rev A) [!].sfc"},
		{
			"Long_path",
			"/media/storage/games/retro/roms/Nintendo 64/" +
				"The Legend of Zelda - Ocarina of Time (USA) (Rev 1.2) [!].z64",
		},
		{"Scene_release", "/media/movies/The.Dark.Knight.2008.1080p.BluRay.x264-GROUP.mkv"},
		{"URI_scheme", "kodi-movie://12345/Movie Title"},
		{"NoExt", "/roms/NES/Super Mario Bros"},
		{"CJK", "/roms/SNES/ゼルダの伝説 (Japan).sfc"}, //nolint:gosmopolitan // CJK benchmark
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			params := PathFragmentParams{
				Config:   nil,
				Path:     tc.path,
				SystemID: "NES",
				NoExt:    tc.name == "NoExt",
			}
			for b.Loop() {
				GetPathFragments(&params)
			}
		})
	}
}

// buildSyntheticFilenames generates deterministic ROM-like filenames for benchmarking.
func buildSyntheticFilenames(n int) []string {
	prefixes := []string{
		"Super", "Mega", "Ultra", "Final", "Grand", "Dark", "Crystal",
		"Shadow", "Iron", "Bright", "Neo", "Hyper", "Royal", "Star",
	}
	middles := []string{
		"Mario", "Fighter", "Quest", "Fantasy", "Dragon", "Knight",
		"Warrior", "Battle", "Storm", "Legend", "World", "Racer",
	}
	suffixes := []string{
		"Bros", "Adventure", "Saga", "Chronicles", "Wars", "Legacy",
		"Origins", "Legends", "Rising", "Revolution", "Arena", "Force",
	}
	regions := []string{
		"(USA)", "(Europe)", "(Japan)", "(USA, Europe)", "(World)",
	}
	extensions := []string{".nes", ".sfc", ".md", ".gba", ".z64", ".iso"}

	rng := rand.New(rand.NewSource(42)) //nolint:gosec // Deterministic seed for reproducible benchmarks
	filenames := make([]string, n)
	for i := range filenames {
		filenames[i] = fmt.Sprintf("/roms/system/%s %s %s %d %s%s",
			prefixes[rng.Intn(len(prefixes))],
			middles[rng.Intn(len(middles))],
			suffixes[rng.Intn(len(suffixes))],
			rng.Intn(99)+1,
			regions[rng.Intn(len(regions))],
			extensions[rng.Intn(len(extensions))],
		)
	}
	return filenames
}

func BenchmarkGetPathFragments_Batch(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{"10k", 10_000},
		{"50k", 50_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			filenames := buildSyntheticFilenames(sz.n)
			b.ResetTimer()
			for b.Loop() {
				for _, fn := range filenames {
					GetPathFragments(&PathFragmentParams{
						Config:   nil,
						Path:     fn,
						SystemID: "NES",
					})
				}
			}
		})
	}
}

// BenchmarkMediaScanner_StageAndReconcile_FreshDB measures a full first index
// of n files through the staging pipeline: stage every file, one set-based
// reconcile, commit.
func BenchmarkMediaScanner_StageAndReconcile_FreshDB(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "1k", n: 1_000},
		{name: "10k", n: 10_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			filenames := buildSyntheticFilenames(sz.n)
			ctx := context.Background()

			// Each iteration needs a fresh DB. Setup cost is included in
			// timing but is constant (~20-50ms) and doesn't affect comparisons.
			for b.Loop() {
				db, cleanup := helpers.NewInMemoryMediaDB(b)
				if err := SeedCanonicalTags(ctx, db); err != nil {
					b.Fatal(err)
				}
				if err := db.BeginTransaction(true); err != nil {
					b.Fatal(err)
				}
				for i, fn := range filenames {
					err := StageMediaPath(db, "nes", fn, "", false, browseprefix.Policy{}, nil, "")
					if i == 0 && err != nil {
						b.Fatal(err)
					}
				}
				if _, err := db.ReconcileStagedSystem(ctx, "nes", database.ScanReconcileOpts{}); err != nil {
					b.Fatal(err)
				}
				if err := db.CommitTransaction(); err != nil {
					b.Fatal(err)
				}
				cleanup()
			}
		})
	}
}

// BenchmarkMediaScanner_Reconcile_FixedScanGrowingDB re-indexes the same 1k
// files against databases of growing size. This is the memory-scaling
// regression guard for the staging rearchitecture: allocations must stay flat
// as the existing row count grows (the old pipeline preloaded every existing
// row into Go maps, so its footprint scaled with the database instead of the
// scan). The n-1k rows outside the scan flip to missing on the first
// reconcile, before the timer starts; timed iterations are steady-state.
func BenchmarkMediaScanner_Reconcile_FixedScanGrowingDB(b *testing.B) {
	const scanSize = 1_000
	sizes := []struct {
		name string
		n    int
	}{
		{name: "10k", n: 10_000},
		{name: "100k", n: 100_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			filenames := buildSyntheticFilenames(sz.n)
			scanFiles := filenames[:scanSize]
			ctx := context.Background()

			db, cleanup := helpers.NewInMemoryMediaDB(b)
			defer cleanup()
			if err := SeedCanonicalTags(ctx, db); err != nil {
				b.Fatal(err)
			}
			rescan := func(files []string) {
				if err := db.BeginTransaction(true); err != nil {
					b.Fatal(err)
				}
				if err := db.ClearScanStage(); err != nil {
					b.Fatal(err)
				}
				for _, fn := range files {
					if err := StageMediaPath(db, "nes", fn, "", false, browseprefix.Policy{}, nil, ""); err != nil {
						b.Fatal(err)
					}
				}
				if _, err := db.ReconcileStagedSystem(ctx, "nes", database.ScanReconcileOpts{}); err != nil {
					b.Fatal(err)
				}
				if err := db.CommitTransaction(); err != nil {
					b.Fatal(err)
				}
			}
			rescan(filenames) // seed full DB
			rescan(scanFiles) // one-time missing-state flip outside the timer

			b.ResetTimer()
			for b.Loop() {
				rescan(scanFiles)
			}
		})
	}
}

// BenchmarkMediaScanner_Reconcile_ExistingRows measures an unchanged full
// re-index against a database that already holds the same rows. Cost is
// expected to scale linearly with scan size (per-file parse + staging), never
// super-linearly with the database.
func BenchmarkMediaScanner_Reconcile_ExistingRows(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "10k", n: 10_000},
		{name: "100k", n: 100_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			filenames := buildSyntheticFilenames(sz.n)
			ctx := context.Background()

			db, cleanup := helpers.NewInMemoryMediaDB(b)
			defer cleanup()
			if err := SeedCanonicalTags(ctx, db); err != nil {
				b.Fatal(err)
			}
			seedOnce := func() {
				if err := db.BeginTransaction(true); err != nil {
					b.Fatal(err)
				}
				for _, fn := range filenames {
					if err := StageMediaPath(db, "nes", fn, "", false, browseprefix.Policy{}, nil, ""); err != nil {
						b.Fatal(err)
					}
				}
				if _, err := db.ReconcileStagedSystem(ctx, "nes", database.ScanReconcileOpts{}); err != nil {
					b.Fatal(err)
				}
				if err := db.CommitTransaction(); err != nil {
					b.Fatal(err)
				}
			}
			seedOnce()

			b.ResetTimer()
			for b.Loop() {
				seedOnce()
			}
		})
	}
}

func BenchmarkGetPathFragments_PeakMemory(b *testing.B) {
	filenames := buildSyntheticFilenames(50_000)

	// Force GC and get baseline before allocating results
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	results := make([]MediaPathFragments, len(filenames))
	for i, fn := range filenames {
		results[i] = GetPathFragments(&PathFragmentParams{
			Config:   nil,
			Path:     fn,
			SystemID: "NES",
		})
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Keep results alive for measurement
	runtime.KeepAlive(results)

	if after.TotalAlloc > before.TotalAlloc {
		b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/(1024*1024), "total-MB")
	}
}
