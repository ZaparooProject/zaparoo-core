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

package mediadb

import (
	"fmt"
	"testing"
)

type benchCacheEntry = struct {
	slug       string
	secSlug    string
	titleDBID  int64
	systemDBID int64
}

// buildBenchSweepCache seeds a cache shaped like a full library: `systems`
// systems with `titlesPerSystem` entries each.
func buildBenchSweepCache(systems, titlesPerSystem int) (
	cache *SlugSearchCache, systemNames map[int64]string,
) {
	systemNames = make(map[int64]string, systems)
	entries := make([]benchCacheEntry, 0, systems*titlesPerSystem)
	for s := 1; s <= systems; s++ {
		//nolint:gosec // Safe: bench sizes are small
		sysDBID := int64(s)
		systemNames[sysDBID] = fmt.Sprintf("System%d", s)
		for t := range titlesPerSystem {
			entry := benchCacheEntry{
				slug: fmt.Sprintf("game-title-number-%d-%d", s, t),
				//nolint:gosec // Safe: bench sizes are small
				titleDBID:  int64(s*1_000_000 + t),
				systemDBID: sysDBID,
			}
			if t%10 == 0 {
				entry.secSlug = fmt.Sprintf("alt-name-%d-%d", s, t)
			}
			entries = append(entries, entry)
		}
	}
	cache = buildTestCache(entries, systemNames)
	cache.complete = true
	return cache, systemNames
}

// buildBenchFragment builds the small selective cache a mid-scan refresh
// produces for one just-committed system.
func buildBenchFragment(systemDBID int64, systemID string, titlesPerSystem int) *SlugSearchCache {
	entries := make([]benchCacheEntry, 0, titlesPerSystem)
	for t := range titlesPerSystem {
		entry := benchCacheEntry{
			slug:       fmt.Sprintf("game-title-number-%d-%d", systemDBID, t),
			titleDBID:  systemDBID*1_000_000 + int64(t),
			systemDBID: systemDBID,
		}
		if t%10 == 0 {
			entry.secSlug = fmt.Sprintf("alt-name-%d-%d", systemDBID, t)
		}
		entries = append(entries, entry)
	}
	fragment := buildTestCache(entries, map[int64]string{systemDBID: systemID})
	fragment.coveredSystems = map[string]struct{}{systemID: {}}
	return fragment
}

// BenchmarkSlugSearchCache_MidScanChurn_FullSweep measures the cache-side
// cost of one full indexing run's mid-scan maintenance: for every system,
// the pre-index drop (withoutSystems) followed by the post-commit refresh
// merge (mergeSlugSearchCaches). This is the ~18% CPU / top-allocator
// pattern observed in on-device profiles during a full index.
func BenchmarkSlugSearchCache_MidScanChurn_FullSweep(b *testing.B) {
	for _, tier := range []struct {
		systems         int
		titlesPerSystem int
	}{
		{systems: 50, titlesPerSystem: 2000},
		{systems: 100, titlesPerSystem: 2000},
	} {
		name := fmt.Sprintf("systems_%d_titles_each_%d", tier.systems, tier.titlesPerSystem)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			base, systemNames := buildBenchSweepCache(tier.systems, tier.titlesPerSystem)
			fragments := make([]*SlugSearchCache, 0, tier.systems)
			for s := 1; s <= tier.systems; s++ {
				fragments = append(fragments,
					buildBenchFragment(int64(s), systemNames[int64(s)], tier.titlesPerSystem))
			}
			b.ResetTimer()
			for b.Loop() {
				cache := base
				for s := 1; s <= tier.systems; s++ {
					cache = cache.withoutSystems([]string{systemNames[int64(s)]})
					cache = mergeSlugSearchCaches(cache, fragments[s-1])
				}
				if cache.entryCount == 0 {
					b.Fatal("sweep produced empty cache")
				}
			}
		})
	}
}
