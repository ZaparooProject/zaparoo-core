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

package simfixtures

import "strings"

// BenchSeed is the seed BenchFilenames uses.
const BenchSeed = 42

// BenchFilenames returns n distinct fictional ROM paths of the form
// /roms/system/<title>.nes for benchmarks, the same for every call. Titles
// carry no region or dump tags, so title resolution stays confident, and no
// two share a slug.
func BenchFilenames(n int) []string {
	titles := syntheticTitles(systemRand(BenchSeed, "bench"), n, make(map[string]bool, n))
	out := make([]string, len(titles))
	var b strings.Builder
	for i := range titles {
		b.Reset()
		b.WriteString("/roms/system/")
		b.WriteString(titles[i].stem)
		b.WriteString(".nes")
		out[i] = b.String()
	}
	return out
}
