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
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The browse caches are keyed on the handle each statement runs against, and
// they are cleared with the pool handle. beginBrowse hands every sqlBrowse call
// a fresh *sql.Conn, so a cache keyed on that handle never survives the call
// that filled it: the entry is written under a connection that is returned to
// the pool microseconds later, and the next page starts from nothing.
//
// These tests drive the caches through the public browse entry points rather
// than calling the cache helpers directly, which is what the existing coverage
// in sql_browse_cache_test.go does and why the defect was invisible to it.

func browseCacheEntryCount() int {
	prefixPolicyCacheMu.RLock()
	defer prefixPolicyCacheMu.RUnlock()
	return len(prefixPolicyCacheMap)
}

func utilityTagCacheEntryCount() int {
	utilityTagCacheMu.RLock()
	defer utilityTagCacheMu.RUnlock()
	return len(utilityTagCacheMap)
}

func imagePropertyTagCacheEntryCount() int {
	imagePropertyTagCacheMu.RLock()
	defer imagePropertyTagCacheMu.RUnlock()
	return len(imagePropertyTagCacheMap)
}

func TestBrowseFilesReusesPrefixPolicyCacheAcrossPages(t *testing.T) {
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := seedBrowsePlanTestDB(t, mediaDB, 2500)

	clearPrefixPolicyCache()
	t.Cleanup(clearPrefixPolicyCache)

	ctx := context.Background()
	const pages = 5
	for range pages {
		_, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			PathPrefix: parentDir,
			Limit:      10,
			Sort:       "name-asc",
		})
		require.NoError(t, err)
	}

	assert.Equal(t, 1, browseCacheEntryCount(),
		"browsing one scope %d times must leave one prefix-policy entry; more means the "+
			"cache is keyed on the per-call connection and every page re-detects", pages)

	_, ok := cachedPrefixPolicy(mediaDB.sql.Load(), prefixPolicyCacheKey(parentDir, nil))
	assert.True(t, ok,
		"the detected policy must be reachable from the pool handle, which is the handle "+
			"clearPrefixPolicyCacheFor and invalidateCaches use")
}

func TestBrowseFilesReusesTagCachesAcrossPages(t *testing.T) {
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	defer cleanup()
	parentDir := seedBrowsePlanTestDB(t, mediaDB, 200)
	// resolveImagePropertyTagDBIDs declines to cache an empty result, so without
	// the image-* property tags indexing seeds there would be nothing to measure.
	seedImagePropertyTags(t, mediaDB)

	clearUtilityTagCache()
	clearImagePropertyTagCache()
	t.Cleanup(clearUtilityTagCache)
	t.Cleanup(clearImagePropertyTagCache)

	ctx := context.Background()
	const pages = 5
	for range pages {
		_, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
			PathPrefix: parentDir,
			Limit:      10,
			Sort:       "name-asc",
		})
		require.NoError(t, err)
	}

	assert.Equal(t, 1, utilityTagCacheEntryCount(),
		"utility tag DBIDs must resolve once per database, not once per browse page")
	assert.Equal(t, 1, imagePropertyTagCacheEntryCount(),
		"image property tag DBIDs must resolve once per database, not once per browse page")
}
