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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// midScanTestCache builds a 3-system cache: NES (mario, zeldalike), SNES
// (metroidvania), Genesis (sonicgame). Slugs are >= 3 bytes so the trigram
// path is exercised.
func midScanTestCache() *SlugSearchCache {
	return buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{slug: "supermariobros", titleDBID: 10, systemDBID: 1},
		{slug: "zeldalike", titleDBID: 11, systemDBID: 1},
		{slug: "metroidvania", titleDBID: 20, systemDBID: 2},
		{slug: "sonicgame", titleDBID: 30, systemDBID: 3},
	}, map[int64]string{1: "NES", 2: "SNES", 3: "Genesis"})
}

// nesRefreshFragment builds the selective fragment a mid-scan refresh of NES
// would produce, with one changed title set.
func nesRefreshFragment() *SlugSearchCache {
	fragment := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{slug: "supermariobros", titleDBID: 10, systemDBID: 1},
		{slug: "kirbyadventure", titleDBID: 12, systemDBID: 1},
	}, map[int64]string{1: "NES"})
	fragment.coveredSystems = map[string]struct{}{"NES": {}}
	return fragment
}

func searchAllPaths(c *SlugSearchCache, substr string) []int64 {
	return c.Search(nil, [][][]byte{{[]byte(substr)}})
}

func TestSlugCacheDropTombstonesSystemEverywhere(t *testing.T) {
	t.Parallel()

	cache := midScanTestCache()
	cache.complete = true
	dropped := cache.withoutSystems([]string{"NES"})

	// Arrays are shared, not copied.
	assert.Same(t, &cache.slugData[0], &dropped.slugData[0], "entry arrays must be shared")
	assert.Equal(t, cache.entryCount, dropped.entryCount)

	// Unfiltered trigram search: NES titles gone, others intact.
	assert.Empty(t, searchAllPaths(dropped, "mario"))
	assert.Equal(t, []int64{20}, searchAllPaths(dropped, "metroid"))

	// Unfiltered short-variant search takes the linear path.
	assert.Empty(t, dropped.Search(nil, [][][]byte{{[]byte("ma")}}))

	// Empty variant groups → collectEntries.
	all := dropped.Search(nil, nil)
	assert.ElementsMatch(t, []int64{20, 30}, all)

	// Filtered path: dropped system has no range.
	nesDBID := []int64{1}
	assert.Empty(t, dropped.Search(nesDBID, [][][]byte{{[]byte("mario")}}))

	// Exact/prefix/any (iterateEntries) paths.
	assert.Empty(t, dropped.ExactSlugMatch(nil, []byte("supermariobros")))
	assert.Empty(t, dropped.PrefixSlugMatch(nil, []byte("super")))
	assert.Empty(t, dropped.ExactSlugMatchAny(nil, [][]byte{[]byte("supermariobros")}))
	assert.Equal(t, []int64{20}, dropped.ExactSlugMatch(nil, []byte("metroidvania")))

	// RandomEntry never returns a dropped title.
	for range 50 {
		title, ok := dropped.RandomEntry(nil)
		require.True(t, ok)
		assert.NotContains(t, []int64{10, 11}, title)
	}

	// CanServeSystems is truthful: dropped system no longer served.
	assert.False(t, dropped.CanServeSystems([]string{"NES"}))
	assert.True(t, dropped.CanServeSystems([]string{"SNES", "Genesis"}))

	// The original cache is untouched.
	assert.Equal(t, []int64{10}, searchAllPaths(cache, "mario"))
}

func TestSlugCacheMergeAppendsRefreshedSystem(t *testing.T) {
	t.Parallel()

	cache := midScanTestCache()
	cache.complete = true

	// Mimic the indexing sequence: drop before staging, merge after commit.
	working := cache.withoutSystems([]string{"NES"})
	merged := mergeSlugSearchCaches(working, nesRefreshFragment())

	// New NES entries visible on the trigram path (delta layer), old title 11
	// (zeldalike) gone, changed set includes the new title 12.
	assert.Equal(t, []int64{10}, searchAllPaths(merged, "mario"))
	assert.Equal(t, []int64{12}, searchAllPaths(merged, "kirby"))
	assert.Empty(t, searchAllPaths(merged, "zelda"))

	// Filtered path serves the appended block.
	assert.ElementsMatch(t, []int64{10, 12}, merged.Search([]int64{1}, nil))

	// Other systems untouched.
	assert.Equal(t, []int64{20}, searchAllPaths(merged, "metroid"))
	assert.Equal(t, []int64{30}, searchAllPaths(merged, "sonic"))

	// Exact/prefix and collect paths see the appended entries.
	assert.Equal(t, []int64{12}, merged.ExactSlugMatch(nil, []byte("kirbyadventure")))
	assert.ElementsMatch(t, []int64{10, 12, 20, 30}, merged.Search(nil, nil))

	// Coverage restored for the refreshed system.
	assert.True(t, merged.CanServeSystems([]string{"NES", "SNES", "Genesis"}))
	assert.True(t, merged.hasMidScanState())
}

// TestSlugCacheSweepMatchesFreshBuild pins equivalence: a full drop+refresh
// sweep over every system must answer queries identically to a cache built
// fresh from the same final data.
func TestSlugCacheSweepMatchesFreshBuild(t *testing.T) {
	t.Parallel()

	const systems = 8
	const titlesPerSystem = 25
	type entry = struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}
	systemNames := make(map[int64]string, systems)
	entries := make([]entry, 0, systems*titlesPerSystem)
	for s := 1; s <= systems; s++ {
		systemNames[int64(s)] = fmt.Sprintf("Sys%d", s)
		for i := range titlesPerSystem {
			e := entry{
				slug:       fmt.Sprintf("adventure-game-%d-%d", s, i),
				titleDBID:  int64(s*1000 + i),
				systemDBID: int64(s),
			}
			if i%5 == 0 {
				e.secSlug = fmt.Sprintf("altname-%d-%d", s, i)
			}
			entries = append(entries, e)
		}
	}
	staleEntries := append([]entry(nil), entries...)
	for i := range staleEntries {
		staleEntries[i].titleDBID += 1_000_000
	}
	cache := buildTestCache(staleEntries, systemNames)
	cache.complete = true

	evolved := cache
	for s := 1; s <= systems; s++ {
		sysID := systemNames[int64(s)]
		evolved = evolved.withoutSystems([]string{sysID})

		fragment := buildTestCache(entries[(s-1)*titlesPerSystem:s*titlesPerSystem],
			map[int64]string{int64(s): sysID})
		fragment.coveredSystems = map[string]struct{}{sysID: {}}
		evolved = mergeSlugSearchCaches(evolved, fragment)
	}

	fresh := buildTestCache(entries, systemNames)
	fresh.complete = true

	queries := []string{"adventure", "game-3", "-7-1", "zzz-none", "altname"}
	for _, q := range queries {
		assert.ElementsMatch(t, searchAllPaths(fresh, q), searchAllPaths(evolved, q), "query %q", q)
	}
	assert.ElementsMatch(t, fresh.Search(nil, nil), evolved.Search(nil, nil))
	for s := 1; s <= systems; s++ {
		assert.ElementsMatch(t,
			fresh.Search([]int64{int64(s)}, nil),
			evolved.Search([]int64{int64(s)}, nil),
			"system %d", s)
	}

	// RandomEntry must skip the fully tombstoned original block and weight
	// across the appended live block exactly as a fresh cache does.
	require.Equal(t, [][2]int{{len(entries), len(entries) * 2}}, evolved.liveEntries)
	validIDs := make(map[int64]struct{}, len(entries))
	for _, id := range fresh.Search(nil, nil) {
		validIDs[id] = struct{}{}
	}
	seenSystems := make(map[int64]struct{}, systems)
	for range 500 {
		freshID, freshOK := fresh.RandomEntry(nil)
		require.True(t, freshOK)
		require.Contains(t, validIDs, freshID)

		evolvedID, evolvedOK := evolved.RandomEntry(nil)
		require.True(t, evolvedOK)
		require.Contains(t, validIDs, evolvedID)
		seenSystems[evolvedID/1000] = struct{}{}
	}
	assert.Contains(t, seenSystems, int64(1), "weighted selection must reach the appended block head")
	assert.Contains(t, seenSystems, int64(systems), "weighted selection must reach the appended block tail")
}

// TestSlugCacheConcurrentReadersDuringSweep runs continuous readers against
// the atomic cache pointer while a writer cycles drop/refresh, mirroring
// searches during indexing. Run with -race in CI.
func TestSlugCacheConcurrentReadersDuringSweep(t *testing.T) {
	t.Parallel()

	cache := midScanTestCache()
	cache.complete = true
	var db MediaDB
	db.slugSearchCache.Store(cache)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c := db.slugSearchCache.Load()
				_ = c.Search(nil, [][][]byte{{[]byte("mario")}})
				_ = c.Search([]int64{1, 2, 3}, [][][]byte{{[]byte("game")}})
				_ = c.Search(nil, nil)
				_, _ = c.RandomEntry(nil)
				_ = c.ExactSlugMatch(nil, []byte("metroidvania"))
			}
		}()
	}

	for range 200 {
		current := db.slugSearchCache.Load()
		working := current.withoutSystems([]string{"NES"})
		db.slugSearchCache.Store(working)
		merged := mergeSlugSearchCaches(working, nesRefreshFragment())
		db.slugSearchCache.Store(merged)
	}
	close(stop)
	wg.Wait()

	final := db.slugSearchCache.Load()
	assert.Equal(t, []int64{10}, searchAllPaths(final, "mario"))
	assert.Equal(t, []int64{12}, searchAllPaths(final, "kirby"))
	assert.Empty(t, searchAllPaths(final, "zelda"))
}
