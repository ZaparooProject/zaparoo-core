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
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
)

// zaparooBenchLargeEnv gates reconcile benchmark cases large enough to blow
// the 45-minute count=6 budget of task bench-baseline/bench-compare — neither
// passes -short, so testing.Short() wouldn't gate these. Added for #1279 to
// reproduce the scale (tens of thousands of files/tag links in one system)
// where the reconcile pipeline showed super-linear cost on device:
//
//	ZAPAROO_BENCH_LARGE=1 go test -bench=Reconcile -benchtime=1x -count=1 ./pkg/database/mediascanner/
const zaparooBenchLargeEnv = "ZAPAROO_BENCH_LARGE"

// largeReconcileBenchSize is the size threshold above which a bench case
// needs zaparooBenchLargeEnv set.
const largeReconcileBenchSize = 50_000

func skipUnlessLargeReconcileBench(b *testing.B) {
	b.Helper()
	if os.Getenv(zaparooBenchLargeEnv) != "1" {
		b.Skipf("set %s=1 to run this large-scale reconcile benchmark", zaparooBenchLargeEnv)
	}
}

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
		{name: "50k", n: largeReconcileBenchSize},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			if sz.n == largeReconcileBenchSize {
				skipUnlessLargeReconcileBench(b)
			}
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
				for _, fn := range filenames {
					err := StageMediaPath(&StageMediaPathParams{DB: db, SystemID: "nes", Path: fn})
					if err != nil {
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
// regression guard for the staging rearchitecture: retained memory must stay
// flat as the existing row count grows (the old pipeline preloaded every
// existing row into Go maps, so its footprint scaled with the database instead
// of the scan). The n-1k rows outside the scan flip to missing on the first
// reconcile, before the timer starts; timed iterations are steady-state.
//
// Since #1317 the steady-state iterations are skipped reconciles, and the
// skip's stored-state digest streams one row per media of the system through
// the driver (sqlScanStoredStateDigest). B/op and allocs/op therefore grow
// with the system's row count here — that is transient per-row garbage from
// the driver, not retained state, and it is why the digest is a single
// one-column query rather than a scan per table.
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
					if err := StageMediaPath(&StageMediaPathParams{DB: db, SystemID: "nes", Path: fn}); err != nil {
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
// super-linearly with the database. Since #1317 the reconcile itself is
// skipped here: the staged-set fingerprint matches the stored one, so the
// timed work is staging plus the two digests (staged set, stored state) that
// prove the skip is safe.
func BenchmarkMediaScanner_Reconcile_ExistingRows(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{name: "10k", n: 10_000},
		{name: "50k", n: largeReconcileBenchSize},
		{name: "100k", n: 100_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			if sz.n == largeReconcileBenchSize {
				skipUnlessLargeReconcileBench(b)
			}
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
					if err := StageMediaPath(&StageMediaPathParams{DB: db, SystemID: "nes", Path: fn}); err != nil {
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

// growingRunSystemCount, growingRunFilesPerSystem, and growingRunMegaFiles
// shape BenchmarkMediaScanner_GrowingRun_AnalyzeCadence: a full library's
// worth of distinct systems reconciled in sequence against one continuously
// growing database, the shape a real device index run takes and none of the
// other benchmarks in this file reproduce (they compare a fixed scan against
// a small number of discrete existing-DB sizes, not a monotonic multi-system
// growth curve). 120 systems of 500 files each, with every 20th system a
// 20,000-file mega-system, approximates a real MiSTer library (#1279 round 3
// device data: 131 systems, most under 1k files, a handful of mega-systems in
// the tens of thousands) without the runtime of a full-scale run. The mega
// systems specifically stress the risk this benchmark exists to check: a real
// re-analysis (not a skipped no-op) firing against an already-large, still
// growing Media table.
const (
	growingRunSystemCount    = 120
	growingRunFilesPerSystem = 500
	growingRunMegaEvery      = 20
	growingRunMegaFiles      = 20_000
)

func growingRunFilesForSystem(sys int) int {
	if sys%growingRunMegaEvery == 0 {
		return growingRunMegaFiles
	}
	return growingRunFilesPerSystem
}

// BenchmarkMediaScanner_GrowingRun_AnalyzeCadence measures the aggregate cost
// of calling AnalyzeApproximate (PRAGMA optimize) after every system-boundary
// commit in a long run, instead of only once after the first system (#1279
// round 4). PRAGMA optimize only re-analyzes a table whose size has changed
// >10x (or that lacks stats) since its last analysis, so the expectation is
// that most of these calls become cheap no-ops once table sizes stabilize
// relative to their own last analysis — this benchmark measures whether that
// expectation holds, rather than assuming it does.
//
//	ZAPAROO_BENCH_LARGE=1 go test -bench=GrowingRun -benchtime=1x -count=1 ./pkg/database/mediascanner/
func BenchmarkMediaScanner_GrowingRun_AnalyzeCadence(b *testing.B) {
	skipUnlessLargeReconcileBench(b)
	ctx := context.Background()

	cases := []struct {
		name         string
		analyzeEvery bool
		analyzeOnce  bool
	}{
		{name: "PerSystem", analyzeEvery: true},
		{name: "Once", analyzeOnce: true},
		{name: "Never"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				db, cleanup := helpers.NewInMemoryMediaDB(b)
				if err := SeedCanonicalTags(ctx, db); err != nil {
					b.Fatal(err)
				}

				var analyzeTotal time.Duration
				analyzeCalls := 0
				analyzed := false
				for sys := range growingRunSystemCount {
					systemID := fmt.Sprintf("synth%d", sys)
					filenames := buildSyntheticFilenames(growingRunFilesForSystem(sys))
					for i := range filenames {
						filenames[i] = fmt.Sprintf("/roms/%s/f%d.rom", systemID, i)
					}

					if err := db.BeginTransaction(true); err != nil {
						b.Fatal(err)
					}
					for _, fn := range filenames {
						if err := StageMediaPath(&StageMediaPathParams{
							DB: db, SystemID: systemID, Path: fn,
						}); err != nil {
							b.Fatal(err)
						}
					}
					if _, err := db.ReconcileStagedSystem(ctx, systemID, database.ScanReconcileOpts{}); err != nil {
						b.Fatal(err)
					}
					if err := db.CommitTransaction(); err != nil {
						b.Fatal(err)
					}

					runAnalyze := tc.analyzeEvery || (tc.analyzeOnce && !analyzed)
					if runAnalyze {
						start := time.Now()
						if err := db.AnalyzeApproximate(); err != nil {
							b.Fatal(err)
						}
						analyzeTotal += time.Since(start)
						analyzeCalls++
						analyzed = true
					}
				}

				b.ReportMetric(float64(analyzeTotal.Milliseconds()), "analyze-ms/op")
				b.ReportMetric(float64(analyzeCalls), "analyze-calls/op")
				cleanup()
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
