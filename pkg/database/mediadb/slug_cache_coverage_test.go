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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cacheCoverageEntry keeps the buildTestCache literal readable.
type cacheCoverageEntry = struct {
	slug       string
	secSlug    string
	titleDBID  int64
	systemDBID int64
}

// buildCoverageCache returns a complete cache holding entries for withData
// systems, mirroring a finished index.
func buildCoverageCache(withData int) *SlugSearchCache {
	entries := make([]cacheCoverageEntry, 0, withData)
	systems := make(map[int64]string, withData)
	for i := 1; i <= withData; i++ {
		id := fmt.Sprintf("System%03d", i)
		systems[int64(i)] = id
		entries = append(entries, cacheCoverageEntry{
			slug:       fmt.Sprintf("game-%03d", i),
			secSlug:    fmt.Sprintf("alt-%03d", i),
			titleDBID:  int64(i),
			systemDBID: int64(i),
		})
	}
	cache := buildTestCache(entries, systems)
	cache.complete = true
	return cache
}

// requestedSystems mirrors a library-wide client search: every system Zaparoo
// knows about, most of which hold no media on any given device.
func requestedSystems(total int) []string {
	ids := make([]string, 0, total)
	for i := 1; i <= total; i++ {
		ids = append(ids, fmt.Sprintf("System%03d", i))
	}
	return ids
}

// TestCanServeSystems_LibraryWideSearchDuringReindex reproduces the search
// failure from #1279 at the shape the device actually has: a client asks for
// 293 systems, the database holds rows for 106, and one of those is being
// re-indexed.
//
// Before the fix every library-wide search fell through to the grouped SQL LIKE
// path for the entire duration of an index — eight grouped queries per request,
// the slowest single query 15.2 s on device, so every search hit the 30 s API
// timeout. The systems vetoing the cache were overwhelmingly ones with no media
// at all, which cannot affect a result either way.
func TestCanServeSystems_LibraryWideSearchDuringReindex(t *testing.T) {
	t.Parallel()

	const (
		systemsWithData = 106
		systemsKnown    = 293
	)
	cache := buildCoverageCache(systemsWithData)
	all := requestedSystems(systemsKnown)

	require.True(t, cache.CanServeSystems(all),
		"a complete cache must serve a library-wide search")

	// Indexing removes the system it is about to rewrite.
	midReindex := cache.withoutSystems([]string{"System007"})
	require.False(t, midReindex.complete,
		"dropping a system must clear complete so startup still rebuilds")

	assert.True(t, midReindex.CanServeSystems([]string{"System004"}),
		"an untouched system with data must still be servable")

	// The system being re-indexed still has rows in SQL, so a search naming it
	// must fall back rather than read this cache and report nothing.
	assert.False(t, midReindex.CanServeSystems([]string{"System007"}),
		"a dropped system must fall back to SQL; its rows still exist")
	assert.False(t, midReindex.CanServeSystems(all),
		"a library-wide search names the dropped system too, so it also falls back")

	// The 187 systems with no media must no longer be what causes that: with
	// the dropped system refreshed, the same library-wide search is servable.
	refreshed := buildTestCache([]cacheCoverageEntry{
		{slug: "game-007", titleDBID: 7, systemDBID: 7},
	}, map[int64]string{7: "System007"})
	refreshed.coveredSystems = map[string]struct{}{"System007": {}}
	merged := mergeSlugSearchCaches(midReindex, refreshed)

	assert.True(t, merged.CanServeSystems(all),
		"once the in-flight system is folded back in, systems that simply have no media "+
			"must not veto a library-wide search — that veto is what forced every search "+
			"onto the grouped SQL LIKE path for a whole index")
}

// TestCanServeSystems_PartialCacheStaysStrict guards the other direction. A
// cache built as a fragment — a fresh index that has only reached a few systems
// — has no basis for assuming an unknown system is empty, so it must keep
// refusing rather than silently answer from partial data.
func TestCanServeSystems_PartialCacheStaysStrict(t *testing.T) {
	t.Parallel()

	fragment := buildTestCache([]cacheCoverageEntry{
		{slug: "game-001", titleDBID: 1, systemDBID: 1},
	}, map[int64]string{1: "System001"})
	fragment.coveredSystems = map[string]struct{}{"System001": {}}

	require.False(t, fragment.complete)
	require.False(t, fragment.derivedFromComplete,
		"a fragment never covered the library")

	assert.True(t, fragment.CanServeSystems([]string{"System001"}))
	assert.False(t, fragment.CanServeSystems([]string{"System001", "System002"}),
		"a fragment cannot assume System002 is empty; it may simply not be built yet")
}

// TestCanServeSystems_DerivedFlagSurvivesRefresh checks the flag survives the
// merge that folds a refreshed system back in, so coverage does not regress
// after the first system of an index completes.
func TestCanServeSystems_DerivedFlagSurvivesRefresh(t *testing.T) {
	t.Parallel()

	cache := buildCoverageCache(106)
	midReindex := cache.withoutSystems([]string{"System007"})
	require.True(t, midReindex.derivedFromComplete)

	refreshed := buildTestCache([]cacheCoverageEntry{
		{slug: "game-007", titleDBID: 7, systemDBID: 7},
	}, map[int64]string{7: "System007"})
	refreshed.coveredSystems = map[string]struct{}{"System007": {}}

	merged := mergeSlugSearchCaches(midReindex, refreshed)
	assert.True(t, merged.derivedFromComplete,
		"folding a refreshed system back in must not lose the library-wide basis")
	assert.True(t, merged.CanServeSystems(requestedSystems(293)))
	assert.Contains(t, merged.coveredSystems, "System007",
		"the refreshed system is covered again")
}

// TestPartitionServableSystems_SplitsCacheFromInFlight covers the split that
// keeps a library-wide search off the grouped SQL LIKE path during an index.
//
// Without it, one system being re-indexed forced the whole search to SQL across
// every system — the shape that timed out on the #1279 device. The split must
// send only the in-flight system to SQL, keep the covered ones on the cache,
// and drop systems that have no rows at all from both sides.
func TestPartitionServableSystems_SplitsCacheFromInFlight(t *testing.T) {
	t.Parallel()

	cache := buildCoverageCache(106)
	midReindex := cache.withoutSystems([]string{"System007"})

	cached, viaSQL, ok := midReindex.PartitionServableSystems(requestedSystems(293))
	require.True(t, ok, "a cache derived from a complete one must support the split")

	assert.Equal(t, []string{"System007"}, viaSQL,
		"only the system being re-indexed still needs SQL; its rows are mid-rewrite")
	assert.Len(t, cached, 105,
		"the other systems with data stay on the cache")
	assert.NotContains(t, cached, "System007")
	assert.NotContains(t, cached, "System200",
		"a system with no rows belongs on neither side")
}

// TestWithoutSystems_IgnoresSystemsTheCacheNeverHeld pins that removing a
// system the cache has no entries for does not disable it.
//
// InsertSystem outside a transaction invalidates with just the new system's ID,
// and a system that has only just been inserted has no media yet — so it is
// absent from the cache. Recording it as dropped made CanServeSystems refuse
// every library-wide search, which is the grouped SQL LIKE path this cache
// exists to avoid.
func TestWithoutSystems_IgnoresSystemsTheCacheNeverHeld(t *testing.T) {
	t.Parallel()

	cache := buildCoverageCache(106)
	all := requestedSystems(293)
	require.True(t, cache.CanServeSystems(all))

	afterInsert := cache.withoutSystems([]string{"BrandNewSystem"})

	assert.True(t, afterInsert.CanServeSystems(all),
		"inserting a system with no media must not push library-wide searches onto SQL")
	assert.NotContains(t, afterInsert.droppedSystems, "BrandNewSystem",
		"a system the cache never held has nothing stale to fall back for")

	cached, viaSQL, ok := afterInsert.PartitionServableSystems(all)
	require.True(t, ok)
	assert.Empty(t, viaSQL, "no system needs SQL: none of them were being rewritten")
	assert.Len(t, cached, 106)

	// A system the cache does hold entries for must still be dropped, so its
	// stale rows are not served while it is rewritten.
	afterReindex := cache.withoutSystems([]string{"System007"})
	assert.Contains(t, afterReindex.droppedSystems, "System007")
	assert.False(t, afterReindex.CanServeSystems([]string{"System007"}))
}

// TestPartitionServableSystems_DeclinesWhenNotApplicable keeps the split from
// being used where its reasoning does not hold. A complete cache needs no
// split, and a fragment cannot assume an unknown system is empty.
func TestPartitionServableSystems_DeclinesWhenNotApplicable(t *testing.T) {
	t.Parallel()

	complete := buildCoverageCache(10)
	_, _, ok := complete.PartitionServableSystems(requestedSystems(20))
	assert.False(t, ok, "a complete cache serves everything; no split needed")

	fragment := buildTestCache([]cacheCoverageEntry{
		{slug: "game-001", titleDBID: 1, systemDBID: 1},
	}, map[int64]string{1: "System001"})
	fragment.coveredSystems = map[string]struct{}{"System001": {}}
	_, _, ok = fragment.PartitionServableSystems([]string{"System001", "System002"})
	assert.False(t, ok, "a fragment cannot assume System002 is empty")

	var nilCache *SlugSearchCache
	_, _, ok = nilCache.PartitionServableSystems([]string{"System001"})
	assert.False(t, ok)
}
