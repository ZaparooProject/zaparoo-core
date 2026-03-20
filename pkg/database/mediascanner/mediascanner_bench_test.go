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
		{"100k", 100_000},
		{"500k", 500_000},
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
			}
		})
	}
}

func BenchmarkGetPathFragments_PeakMemory(b *testing.B) {
	filenames := buildSyntheticFilenames(500_000)

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
