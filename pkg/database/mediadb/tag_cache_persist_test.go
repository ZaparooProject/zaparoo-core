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
	"encoding/gob"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tagCacheTestPath(t *testing.T, dbPath string) string {
	t.Helper()
	cacheDir := filepath.Join(filepath.Dir(dbPath), config.CacheDir)
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	return filepath.Join(cacheDir, TagCacheFileName)
}

// expectIndexGenerationReadOnce queues a single sqlmock expectation for
// IndexGeneration to return the given value (or a no-rows error when value < 0).
func expectIndexGenerationReadOnce(t *testing.T, mock sqlmock.Sqlmock, value int64) {
	t.Helper()
	if value < 0 {
		mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
			WithArgs(DBConfigIndexGeneration).
			WillReturnRows(sqlmock.NewRows([]string{"Value"}))
		return
	}
	mock.ExpectQuery("SELECT Value FROM DBConfig WHERE Name = ?").
		WithArgs(DBConfigIndexGeneration).
		WillReturnRows(sqlmock.NewRows([]string{"Value"}).AddRow(strconv.FormatInt(value, 10)))
}

func newPersistTestDB(t *testing.T) (*MediaDB, sqlmock.Sqlmock, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "media.db")

	mediaDB := &MediaDB{
		ctx:    context.Background(),
		clock:  clockwork.NewFakeClock(),
		dbPath: dbPath,
	}
	mediaDB.sql.Store(db)
	return mediaDB, mock, dbPath
}

func TestLoadCachedTagCache_FileMissing(t *testing.T) {
	t.Parallel()
	mediaDB, mock, _ := newPersistTestDB(t)
	expectIndexGenerationReadOnce(t, mock, 5)

	loaded, err := mediaDB.LoadCachedTagCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.inMemoryTagCache.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPersistAndLoadTagCache_RoundTrip(t *testing.T) {
	t.Parallel()
	mediaDB, mock, _ := newPersistTestDB(t)

	mediaDB.inMemoryTagCache.Store(&tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {{Type: "genre", Tag: "Action", Count: 7}},
		},
		allTags: []database.TagInfo{{Type: "genre", Tag: "Action", Count: 7}},
	})

	expectIndexGenerationReadOnce(t, mock, 42)
	require.NoError(t, mediaDB.PersistTagCache())

	mediaDB.inMemoryTagCache.Store(nil)

	expectIndexGenerationReadOnce(t, mock, 42)
	loaded, err := mediaDB.LoadCachedTagCache()
	require.NoError(t, err)
	assert.True(t, loaded)

	cache := mediaDB.inMemoryTagCache.Load()
	require.NotNil(t, cache)
	assert.Equal(t, []database.TagInfo{{Type: "genre", Tag: "Action", Count: 7}}, cache.allTags)
	assert.Equal(t, []database.TagInfo{{Type: "genre", Tag: "Action", Count: 7}}, cache.bySystem["nes"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCachedTagCache_GenerationMismatch(t *testing.T) {
	t.Parallel()
	mediaDB, mock, _ := newPersistTestDB(t)

	mediaDB.inMemoryTagCache.Store(&tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {{Type: "genre", Tag: "Action"}},
		},
		allTags: []database.TagInfo{{Type: "genre", Tag: "Action"}},
	})

	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistTagCache())

	mediaDB.inMemoryTagCache.Store(nil)

	// DB now reports a newer generation than the cache file.
	expectIndexGenerationReadOnce(t, mock, 2)
	loaded, err := mediaDB.LoadCachedTagCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.inMemoryTagCache.Load(),
		"stale cache file should not be loaded")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCachedTagCache_BadMagic(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)

	cachePath := tagCacheTestPath(t, dbPath)
	f, err := os.Create(cachePath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(&persistedTagCache{
		Magic:           "WRONG",
		Version:         tagCacheFileVersion,
		IndexGeneration: 1,
	}))
	require.NoError(t, f.Close())

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedTagCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.inMemoryTagCache.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadCachedTagCache_VersionMismatch(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)

	cachePath := tagCacheTestPath(t, dbPath)
	f, err := os.Create(cachePath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	require.NoError(t, gob.NewEncoder(f).Encode(&persistedTagCache{
		Magic:           tagCacheFileMagic,
		Version:         tagCacheFileVersion + 99,
		IndexGeneration: 1,
		AllTags:         []database.TagInfo{{Type: "genre", Tag: "Action"}},
	}))
	require.NoError(t, f.Close())

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedTagCache()
	require.NoError(t, err)
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.inMemoryTagCache.Load())
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLoadCachedTagCache_TruncatedFile simulates a crash mid-write: the
// cache file exists but is shorter than a full gob payload. LoadCached*
// should fall back to SQL by returning nil with no cache loaded.
func TestLoadCachedTagCache_TruncatedFile(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)

	mediaDB.inMemoryTagCache.Store(&tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {{Type: "genre", Tag: "Action", Count: 7}},
		},
		allTags: []database.TagInfo{{Type: "genre", Tag: "Action", Count: 7}},
	})
	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistTagCache())

	cachePath := tagCacheTestPath(t, dbPath)
	info, err := os.Stat(cachePath)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(8), "need a non-trivial file to truncate")
	// Truncate to 8 bytes — well inside the gob length-prefix region, so
	// decode hits ErrUnexpectedEOF before producing any usable struct.
	require.NoError(t, os.Truncate(cachePath, 8))

	mediaDB.inMemoryTagCache.Store(nil)

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedTagCache()
	require.NoError(t, err, "truncated file should be a graceful fallback, not an error")
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.inMemoryTagCache.Load(),
		"truncated file should not be loaded as a cache")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestLoadCachedTagCache_OversizedFile validates the io.LimitReader cap by
// temporarily lowering tagCacheMaxBytes below the size of a real persisted
// cache. Decode hits the limit and returns ErrUnexpectedEOF, which the
// loader treats as a corrupted file and falls back to SQL.
func TestLoadCachedTagCache_OversizedFile(t *testing.T) {
	mediaDB, mock, _ := newPersistTestDB(t)

	mediaDB.inMemoryTagCache.Store(&tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {{Type: "genre", Tag: "Action", Count: 7}},
		},
		allTags: []database.TagInfo{{Type: "genre", Tag: "Action", Count: 7}},
	})
	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistTagCache())

	mediaDB.inMemoryTagCache.Store(nil)

	originalCap := tagCacheMaxBytes
	tagCacheMaxBytes = 16
	t.Cleanup(func() { tagCacheMaxBytes = originalCap })

	expectIndexGenerationReadOnce(t, mock, 1)
	loaded, err := mediaDB.LoadCachedTagCache()
	require.NoError(t, err, "oversized file should fall back gracefully")
	assert.False(t, loaded)
	assert.Nil(t, mediaDB.inMemoryTagCache.Load(),
		"oversized file must not be loaded")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPersistTagCache_RemovesStaleFileWhenEmpty(t *testing.T) {
	t.Parallel()
	mediaDB, mock, dbPath := newPersistTestDB(t)
	cachePath := tagCacheTestPath(t, dbPath)

	mediaDB.inMemoryTagCache.Store(&tagCache{
		bySystem: map[string][]database.TagInfo{
			"nes": {{Type: "genre", Tag: "Action"}},
		},
		allTags: []database.TagInfo{{Type: "genre", Tag: "Action"}},
	})
	expectIndexGenerationReadOnce(t, mock, 1)
	require.NoError(t, mediaDB.PersistTagCache())
	_, err := os.Stat(cachePath)
	require.NoError(t, err, "cache file should exist after persist")

	mediaDB.inMemoryTagCache.Store(nil)
	require.NoError(t, mediaDB.PersistTagCache())
	_, err = os.Stat(cachePath)
	assert.True(t, os.IsNotExist(err), "cache file should be removed when nothing to persist")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPersistedTagCacheMirrorsCacheStruct guards against drift between
// tagCache and persistedTagCache. The persisted struct must hold every
// in-memory field plus the three header fields; if a field is added to
// tagCache but not mirrored here, the round-trip silently drops data.
func TestPersistedTagCacheMirrorsCacheStruct(t *testing.T) {
	t.Parallel()
	cacheT := reflect.TypeOf(tagCache{})
	persistedT := reflect.TypeOf(persistedTagCache{})
	headerT := reflect.TypeOf(persistedHeader{})
	require.Equal(t,
		cacheT.NumField()+headerT.NumField(),
		persistedT.NumField(),
		"persistedTagCache field count drifted from tagCache; "+
			"update the persisted struct, the load->cache copy in LoadCachedTagCache, "+
			"and the cache->persisted copy in PersistTagCache",
	)
}
