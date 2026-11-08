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

package database

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func browseListingTestKey(path, revision string) *BrowseListingKey {
	return &BrowseListingKey{Path: path, Revision: revision, Sort: "name-asc"}
}

func TestBrowseListingCacheStoresTotalsAndIndexPerListing(t *testing.T) {
	t.Parallel()
	var cache BrowseListingCache
	ticket := cache.Begin("r1")
	key := browseListingTestKey("/a/", "r1")

	_, ok := cache.Totals(key)
	assert.False(t, ok)
	_, ok = cache.Index(key)
	assert.False(t, ok)

	cache.StoreTotals(key, ticket, BrowseListingTotals{Dirs: 2, Files: 3})
	cache.StoreIndex(key, ticket, &BrowseIndexResult{Scheme: "latin"})
	assert.Equal(t, 1, cache.Len())

	totals, ok := cache.Totals(key)
	require.True(t, ok)
	assert.Equal(t, BrowseListingTotals{Dirs: 2, Files: 3}, totals)
	index, ok := cache.Index(key)
	require.True(t, ok)
	assert.Equal(t, "latin", index.Scheme)

	other := *key
	other.IncludeHidden = true
	_, ok = cache.Totals(&other)
	assert.False(t, ok, "every key field names a different listing")
}

func TestBrowseListingCacheIndexIsACopy(t *testing.T) {
	t.Parallel()
	var cache BrowseListingCache
	ticket := cache.Begin("r1")
	key := browseListingTestKey("/a/", "r1")
	stored := &BrowseIndexResult{Buckets: []BrowseIndexBucket{{Key: "A"}}}
	cache.StoreIndex(key, ticket, stored)

	stored.Buckets[0].Key = "changed after store"
	first, ok := cache.Index(key)
	require.True(t, ok)
	assert.Equal(t, "A", first.Buckets[0].Key)

	first.Buckets[0].Key = "changed after read"
	second, ok := cache.Index(key)
	require.True(t, ok)
	assert.Equal(t, "A", second.Buckets[0].Key)
}

func TestBrowseListingCacheEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()
	var cache BrowseListingCache
	ticket := cache.Begin("r1")
	keyAt := func(i int) *BrowseListingKey { return browseListingTestKey(fmt.Sprintf("/%d/", i), "r1") }
	for i := range BrowseListingCacheLimit {
		cache.StoreTotals(keyAt(i), ticket, BrowseListingTotals{Files: i})
	}
	require.Equal(t, BrowseListingCacheLimit, cache.Len())

	// A read makes the oldest entry the newest, so the next store takes /1/.
	_, ok := cache.Totals(keyAt(0))
	require.True(t, ok)
	cache.StoreTotals(keyAt(100), ticket, BrowseListingTotals{Files: 100})
	assert.Equal(t, BrowseListingCacheLimit, cache.Len())

	_, ok = cache.Totals(keyAt(1))
	assert.False(t, ok, "the least recently used listing goes")
	_, ok = cache.Totals(keyAt(0))
	assert.True(t, ok, "the recently read listing stays")
	_, ok = cache.Totals(keyAt(100))
	assert.True(t, ok)

	// A store to a held listing is a use and evicts nothing.
	cache.StoreIndex(keyAt(2), ticket, &BrowseIndexResult{})
	assert.Equal(t, BrowseListingCacheLimit, cache.Len())
	_, ok = cache.Totals(keyAt(2))
	assert.True(t, ok)
}

func TestBrowseListingCacheClearDropsListingsAndOpenReads(t *testing.T) {
	t.Parallel()
	var cache BrowseListingCache
	key := browseListingTestKey("/a/", "r1")
	ticket := cache.Begin("r1")
	cache.StoreTotals(key, ticket, BrowseListingTotals{Files: 1})

	cache.Clear()
	assert.Zero(t, cache.Len())

	// A read that began before the change saw rows that are now gone.
	cache.StoreTotals(key, ticket, BrowseListingTotals{Files: 1})
	cache.StoreIndex(key, ticket, &BrowseIndexResult{})
	assert.Zero(t, cache.Len())

	cache.StoreTotals(key, cache.Begin("r1"), BrowseListingTotals{Files: 2})
	totals, ok := cache.Totals(key)
	require.True(t, ok)
	assert.Equal(t, 2, totals.Files)
}

func TestBrowseListingCacheClearTagFilteredKeepsUnfilteredListings(t *testing.T) {
	t.Parallel()
	var cache BrowseListingCache
	plain := browseListingTestKey("/a/", "r1")
	tagged := browseListingTestKey("/a/", "r1")
	tagged.Tags = "genre:action"
	ticket := cache.Begin("r1")
	cache.StoreTotals(plain, ticket, BrowseListingTotals{Files: 3})
	cache.StoreTotals(tagged, ticket, BrowseListingTotals{Files: 1})

	cache.ClearTagFiltered()
	assert.Equal(t, 1, cache.Len())
	totals, ok := cache.Totals(plain)
	require.True(t, ok)
	assert.Equal(t, 3, totals.Files)
	_, ok = cache.Totals(tagged)
	assert.False(t, ok)

	// A filtered read that began before the write counted the old tags.
	cache.StoreTotals(tagged, ticket, BrowseListingTotals{Files: 1})
	_, ok = cache.Totals(tagged)
	assert.False(t, ok)
}

func TestBrowseListingCacheNewRevisionEmptiesIt(t *testing.T) {
	t.Parallel()
	var cache BrowseListingCache
	old := browseListingTestKey("/a/", "r1")
	oldTicket := cache.Begin("r1")
	cache.StoreTotals(old, oldTicket, BrowseListingTotals{Files: 1})

	newTicket := cache.Begin("r2")
	assert.Zero(t, cache.Len())
	cache.StoreTotals(old, oldTicket, BrowseListingTotals{Files: 1})
	assert.Zero(t, cache.Len(), "a read under the old revision stores nothing")

	// Even a current ticket cannot file a key from another revision.
	cache.StoreTotals(old, newTicket, BrowseListingTotals{Files: 1})
	assert.Zero(t, cache.Len())

	cache.StoreTotals(browseListingTestKey("/a/", "r2"), newTicket, BrowseListingTotals{Files: 2})
	assert.Equal(t, 1, cache.Len())
}

func TestBrowseListingCacheConcurrentUse(t *testing.T) {
	t.Parallel()
	var cache BrowseListingCache
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				ticket := cache.Begin("r1")
				key := browseListingTestKey(fmt.Sprintf("/%d/", (worker*i)%64), "r1")
				cache.StoreTotals(key, ticket, BrowseListingTotals{Files: i})
				cache.StoreIndex(key, ticket, &BrowseIndexResult{Scheme: "latin"})
				_, _ = cache.Totals(key)
				_, _ = cache.Index(key)
				if i%50 == 0 {
					cache.Clear()
				}
			}
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, cache.Len(), BrowseListingCacheLimit)
}
