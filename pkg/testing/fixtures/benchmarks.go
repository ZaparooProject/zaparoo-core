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

package fixtures

import (
	"fmt"
	"math/rand"
)

// BuildBenchFilenames generates n deterministic ROM-like filenames for
// benchmarking. Uses a fixed seed (42) for reproducibility. No region
// tags are included so that title resolution confidence stays high.
func BuildBenchFilenames(n int) []string {
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

	rng := rand.New(rand.NewSource(42)) //nolint:gosec // Deterministic seed for reproducible benchmarks
	filenames := make([]string, n)
	for i := range filenames {
		filenames[i] = fmt.Sprintf("/roms/system/%s %s %s %d.nes",
			prefixes[rng.Intn(len(prefixes))],
			middles[rng.Intn(len(middles))],
			suffixes[rng.Intn(len(suffixes))],
			rng.Intn(99)+1,
		)
	}
	return filenames
}
