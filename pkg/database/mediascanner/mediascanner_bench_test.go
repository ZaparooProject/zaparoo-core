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
	"fmt"
	"math/rand"
	"runtime"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
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
				GetPathFragments(params)
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
					GetPathFragments(PathFragmentParams{
						Config:   nil,
						Path:     fn,
						SystemID: "NES",
					})
				}
			}
		})
	}
}

func BenchmarkFlushScanStateMaps(b *testing.B) {
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
			for b.Loop() {
				b.StopTimer()
				ss := &database.ScanState{
					SystemIDs:  make(map[string]int, 100),
					TitleIDs:   make(map[string]int, sz.n),
					MediaIDs:   make(map[string]int, sz.n),
					TagTypeIDs: make(map[string]int, 50),
					TagIDs:     make(map[string]int, 500),
				}
				for i := range sz.n {
					ss.TitleIDs[fmt.Sprintf("title-%d", i)] = i
					ss.MediaIDs[fmt.Sprintf("media-%d", i)] = i
				}
				b.StartTimer()
				FlushScanStateMaps(ss)
				runtime.KeepAlive(ss)
			}
		})
	}
}

// buildSyntheticFilenamesMultiSystem distributes n filenames across multiple
// systems with a Zipf-like distribution to mimic real-world collections.
func buildSyntheticFilenamesMultiSystem(n int, systems []string) map[string][]string {
	if len(systems) == 0 {
		return nil
	}

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

	// Distribute with decreasing weight: first system gets most files
	//nolint:gosec // Deterministic seed for reproducible benchmarks
	rng := rand.New(rand.NewSource(42))
	weights := make([]float64, len(systems))
	total := 0.0
	for i := range systems {
		w := 1.0 / float64(i+1) // Inverse-rank weighted: 1, 0.5, 0.33, 0.25, ...
		weights[i] = w
		total += w
	}

	result := make(map[string][]string, len(systems))
	remaining := n
	for i, sys := range systems {
		count := int(float64(n) * weights[i] / total)
		if i == len(systems)-1 {
			count = remaining // Last system gets remainder
		}
		if count > remaining {
			count = remaining
		}
		remaining -= count

		fns := make([]string, count)
		for j := range count {
			fns[j] = fmt.Sprintf("/roms/%s/%s %s %s %d %s%s",
				sys,
				prefixes[rng.Intn(len(prefixes))],
				middles[rng.Intn(len(middles))],
				suffixes[rng.Intn(len(suffixes))],
				rng.Intn(99)+1,
				regions[rng.Intn(len(regions))],
				extensions[rng.Intn(len(extensions))],
			)
		}
		result[sys] = fns
	}
	return result
}

// newScanState creates a fresh ScanState for benchmarking.
func newScanState() *database.ScanState {
	return &database.ScanState{
		SystemIDs:  make(map[string]int),
		TitleIDs:   make(map[string]int),
		MediaIDs:   make(map[string]int),
		TagTypeIDs: make(map[string]int),
		TagIDs:     make(map[string]int),
	}
}

// setupMockMediaDB creates a MockMediaDBI with all Insert/Find methods stubbed
// for benchmarking AddMediaPath without real SQLite.
func setupMockMediaDB() *helpers.MockMediaDBI {
	mockDB := helpers.NewMockMediaDBI()
	mockDB.On("InsertSystem", mock.Anything).Return(database.System{}, nil)
	mockDB.On("InsertMediaTitle", mock.Anything).Return(database.MediaTitle{}, nil)
	mockDB.On("InsertMedia", mock.Anything).Return(database.Media{}, nil)
	mockDB.On("InsertTag", mock.Anything).Return(database.Tag{}, nil)
	mockDB.On("InsertTagType", mock.Anything).Return(database.TagType{}, nil)
	mockDB.On("InsertMediaTag", mock.Anything).Return(database.MediaTag{}, nil)
	mockDB.On("FindTagType", mock.Anything).Return(database.TagType{DBID: 1}, nil)
	mockDB.On("BeginTransaction", mock.Anything).Return(nil)
	mockDB.On("CommitTransaction").Return(nil)
	return mockDB
}

// seedMockScanState populates a ScanState with tag types and tags
// matching what SeedCanonicalTags would produce, but without hitting a real DB.
func seedMockScanState(ss *database.ScanState) {
	// Seed the tag types that AddMediaPath looks up
	ss.TagTypesIndex = 2
	ss.TagTypeIDs["extension"] = 1
	ss.TagTypeIDs["unknown"] = 2
	ss.TagsIndex = 1
	ss.TagIDs["unknown:unknown"] = 1
}

func BenchmarkAddMediaPath_MockDB(b *testing.B) {
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
			mockDB := setupMockMediaDB()
			filenames := buildSyntheticFilenames(sz.n)
			b.ResetTimer()
			for b.Loop() {
				ss := newScanState()
				seedMockScanState(ss)
				for i, fn := range filenames {
					_, _, err := AddMediaPath(mockDB, ss, "nes", fn, false, false, nil, "")
					if i == 0 && err != nil {
						b.Fatal(err)
					}
					// Match production pattern: flush every 10k files
					if sz.n > 10_000 && (i+1)%10_000 == 0 {
						FlushScanStateMaps(ss)
					}
				}
			}
		})
	}
}

func BenchmarkAddMediaPath_RealDB(b *testing.B) {
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

			// Each iteration needs a fresh DB. Setup cost is included in
			// timing but is constant (~20-50ms) and doesn't affect comparisons.
			for b.Loop() {
				db, cleanup := helpers.NewInMemoryMediaDB(b)
				ss := newScanState()
				_ = SeedCanonicalTags(db, ss)
				_ = db.BeginTransaction(true)

				// Measured: insert all files with production commit pattern
				for i, fn := range filenames {
					_, _, err := AddMediaPath(db, ss, "nes", fn, false, false, nil, "")
					if i == 0 && err != nil {
						b.Fatal(err)
					}
					if sz.n > 10_000 && (i+1)%10_000 == 0 {
						_ = db.CommitTransaction()
						FlushScanStateMaps(ss)
						_ = db.BeginTransaction(true)
					}
				}

				_ = db.CommitTransaction()
				cleanup()
			}
		})
	}
}

func BenchmarkIndexingPipeline_EndToEnd(b *testing.B) {
	systems := []string{"nes", "snes", "gba", "n64", "psx", "genesis", "megadrive", "gamegear", "mastersystem", "gb"}

	type endToEndCase struct {
		name    string
		systems []string
		n       int
	}
	sizes := []endToEndCase{
		{name: "10k_1sys", systems: systems[:1], n: 10_000},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			filesBySystem := buildSyntheticFilenamesMultiSystem(sz.n, sz.systems)
			for b.Loop() {
				db, cleanup := helpers.NewInMemoryMediaDB(b)
				ss := newScanState()
				_ = SeedCanonicalTags(db, ss)

				filesInBatch := 0
				batchStarted := false

				for _, sys := range sz.systems {
					fns := filesBySystem[sys]
					for _, fn := range fns {
						if !batchStarted {
							_ = db.BeginTransaction(true)
							batchStarted = true
						}

						_, _, err := AddMediaPath(db, ss, sys, fn, false, false, nil, "")
						if filesInBatch == 0 && err != nil {
							b.Fatal(err)
						}
						filesInBatch++

						if filesInBatch >= 10_000 {
							_ = db.CommitTransaction()
							FlushScanStateMaps(ss)
							filesInBatch = 0
							batchStarted = false
						}
					}
				}

				if batchStarted {
					_ = db.CommitTransaction()
				}

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
		results[i] = GetPathFragments(PathFragmentParams{
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
