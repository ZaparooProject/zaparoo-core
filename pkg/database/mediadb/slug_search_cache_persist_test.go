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
	"encoding/gob"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSlugCache returns a small but fully populated SlugSearchCache for
// round-trip serialization tests.
func fakeSlugCache() *SlugSearchCache {
	return &SlugSearchCache{
		systemDBIDToID:  map[int64]string{1: "nes"},
		systemIDToDBID:  map[string]int64{"nes": 1},
		systemRanges:    map[int64][2]int{1: {0, 1}},
		coveredSystems:  map[string]struct{}{"nes": {}},
		secSlugOffsets:  []uint32{0, 5},
		slugOffsets:     []uint32{0, 6},
		secSlugData:     []byte("alpha"),
		slugData:        []byte("bravo!"),
		titleDBIDs:      []int64{42},
		systemDBIDs:     []int64{1},
		trigramOffsets:  []uint32{0, 1, 1},
		trigramPostings: []uint32{0},
		trigramCapped:   []bool{false, true},
		entryCount:      1,
		complete:        true,
	}
}

func TestLoadCachedSlugSearchCache_FileMissing(t *testing.T) {
	t.Parallel()
	mediaDB, mock, _ := newPersistTestDB(t)
	expectIndexGenerationReadOnce(t, mock, 5)

	loaded, err := mediaDB.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.slugSearchCache.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPersistAndLoadSlugSearchCache_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, mock, _ := newPersistTestDB(t)

	original := fakeSlugCache()
	mediaDB.slugSearchCache.Store(original)

	expectIndexGenerationReadOnce(t, mock, 42)
	require.NoError(t, mediaDB.PersistSlugSearchCache())

	mediaDB.slugSearchCache.Store(nil)

	expectIndexGenerationReadOnce(t, mock, 42)
	loadedOK, err := mediaDB.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	assert.True(t, loadedOK)

	loaded := mediaDB.slugSearchCache.Load()
	require.NotNil(t, loaded)
	assert.Equal(t, original.systemDBIDToID, loaded.systemDBIDToID)
	assert.Equal(t, original.systemIDToDBID, loaded.systemIDToDBID)
	assert.Equal(t, original.systemRanges, loaded.systemRanges)
	assert.Equal(t, original.coveredSystems, loaded.coveredSystems)
	assert.Equal(t, original.secSlugOffsets, loaded.secSlugOffsets)
	assert.Equal(t, original.slugOffsets, loaded.slugOffsets)
	assert.Equal(t, original.secSlugData, loaded.secSlugData)
	assert.Equal(t, original.slugData, loaded.slugData)
	assert.Equal(t, original.titleDBIDs, loaded.titleDBIDs)
	assert.Equal(t, original.systemDBIDs, loaded.systemDBIDs)
	assert.Equal(t, original.trigramOffsets, loaded.trigramOffsets)
	assert.Equal(t, original.trigramPostings, loaded.trigramPostings)
	assert.Equal(t, original.trigramCapped, loaded.trigramCapped)
	assert.Equal(t, original.entryCount, loaded.entryCount)
	assert.Equal(t, original.complete, loaded.complete)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCachedSlugSearchCache_GenerationMismatch(t *testing.T) {
	t.Parallel()
	mediaDB, mock, _ := newPersistTestDB(t)

	mediaDB.slugSearchCache.Store(fakeSlugCache())

	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistSlugSearchCache())

	mediaDB.slugSearchCache.Store(nil)

	// DB now reports a newer generation than the cache file.
	expectIndexGenerationReadOnce(t, mock, 2)
	loaded, err := mediaDB.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.slugSearchCache.Load(),
		"stale cache file should not be loaded")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCachedSlugSearchCache_BadMagic(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)

	cachePath := dbPath + SlugSearchCacheFileSuffix
	f, err := os.Create(cachePath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(&persistedSlugSearchCache{
		Magic:           "WRONG",
		Version:         slugSearchCacheFileVersion,
		IndexGeneration: 1,
	}))
	require.NoError(t, f.Close())

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.slugSearchCache.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCachedSlugSearchCache_VersionMismatch(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)

	cachePath := dbPath + SlugSearchCacheFileSuffix
	f, err := os.Create(cachePath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(&persistedSlugSearchCache{
		Magic:           slugSearchCacheFileMagic,
		Version:         slugSearchCacheFileVersion + 99,
		IndexGeneration: 1,
		EntryCount:      1,
	}))
	require.NoError(t, f.Close())

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.slugSearchCache.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLoadCachedSlugSearchCache_TruncatedFile simulates a crash mid-write:
// the cache file exists but is shorter than a full gob payload.
// LoadCached* should fall back to SQL by returning nil with no cache loaded.
func TestLoadCachedSlugSearchCache_TruncatedFile(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)

	mediaDB.slugSearchCache.Store(fakeSlugCache())
	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistSlugSearchCache())

	cachePath := dbPath + SlugSearchCacheFileSuffix
	info, err := os.Stat(cachePath)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(8), "need a non-trivial file to truncate")
	require.NoError(t, os.Truncate(cachePath, 8))

	mediaDB.slugSearchCache.Store(nil)

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedSlugSearchCache()
	require.NoError(t, err, "truncated file should be a graceful fallback, not an error")
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.slugSearchCache.Load(),
		"truncated file should not be loaded as a cache")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLoadCachedSlugSearchCache_OversizedFile validates the io.LimitReader
// cap by temporarily lowering slugSearchCacheMaxBytes below the size of a
// real persisted cache. Decode hits the limit and returns ErrUnexpectedEOF,
// which the loader treats as a corrupted file and falls back to SQL.
func TestLoadCachedSlugSearchCache_OversizedFile(t *testing.T) {
	mediaDB, mock, _ := newPersistTestDB(t)

	mediaDB.slugSearchCache.Store(fakeSlugCache())
	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistSlugSearchCache())

	mediaDB.slugSearchCache.Store(nil)

	originalCap := slugSearchCacheMaxBytes
	slugSearchCacheMaxBytes = 16
	t.Cleanup(func() { slugSearchCacheMaxBytes = originalCap })

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedSlugSearchCache()
	require.NoError(t, err, "oversized file should fall back gracefully")
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.slugSearchCache.Load(),
		"oversized file must not be loaded")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPersistSlugSearchCache_RemovesStaleFileWhenEmpty(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)
	cachePath := dbPath + SlugSearchCacheFileSuffix

	mediaDB.slugSearchCache.Store(fakeSlugCache())
	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistSlugSearchCache())
	_, err := os.Stat(cachePath)
	require.NoError(t, err, "cache file should exist after persist")

	mediaDB.slugSearchCache.Store(nil)
	require.NoError(t, mediaDB.PersistSlugSearchCache())
	_, err = os.Stat(cachePath)
	assert.True(t, os.IsNotExist(err), "cache file should be removed when nothing to persist")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPersistedSlugSearchCacheMirrorsCacheStruct guards against drift
// between SlugSearchCache and persistedSlugSearchCache. The persisted
// struct must hold every in-memory field plus the three header fields;
// if a field is added to SlugSearchCache but not mirrored here, the
// round-trip silently drops data.
func TestPersistedSlugSearchCacheMirrorsCacheStruct(t *testing.T) {
	t.Parallel()
	cacheT := reflect.TypeOf(SlugSearchCache{})
	persistedT := reflect.TypeOf(persistedSlugSearchCache{})
	headerT := reflect.TypeOf(persistedHeader{})
	require.Equal(t,
		cacheT.NumField()+headerT.NumField(),
		persistedT.NumField(),
		"persistedSlugSearchCache field count drifted from SlugSearchCache; "+
			"update the persisted struct, the load->cache copy in LoadCachedSlugSearchCache, "+
			"and the cache->persisted copy in PersistSlugSearchCache",
	)
}
