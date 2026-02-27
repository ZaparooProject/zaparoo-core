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
	"fmt"
	"runtime"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestCache(entries []struct {
	slug       string
	secSlug    string
	titleDBID  int64
	systemDBID int64
}, systems map[int64]string,
) *SlugSearchCache {
	cache := &SlugSearchCache{
		slugData:       make([]byte, 0),
		slugOffsets:    make([]uint32, 0),
		secSlugData:    make([]byte, 0),
		secSlugOffsets: make([]uint32, 0),
		titleDBIDs:     make([]int64, 0),
		systemDBIDs:    make([]int64, 0),
		systemDBIDToID: systems,
		systemIDToDBID: make(map[string]int64),
	}
	for dbid, id := range systems {
		cache.systemIDToDBID[id] = dbid
	}
	for _, e := range entries {
		//nolint:gosec // Safe: test data won't overflow uint32
		cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
		cache.slugData = append(cache.slugData, e.slug...)
		cache.slugData = append(cache.slugData, 0)

		//nolint:gosec // Safe: test data won't overflow uint32
		cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))
		if e.secSlug != "" {
			cache.secSlugData = append(cache.secSlugData, e.secSlug...)
			cache.secSlugData = append(cache.secSlugData, 0)
		}

		cache.titleDBIDs = append(cache.titleDBIDs, e.titleDBID)
		cache.systemDBIDs = append(cache.systemDBIDs, e.systemDBID)
	}
	// Sentinel offsets
	//nolint:gosec // Safe: test data won't overflow uint32
	cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
	//nolint:gosec // Safe: test data won't overflow uint32
	cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))
	cache.entryCount = len(entries)
	return cache
}

func TestBuildSlugSearchCache(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	systemRows := sqlmock.NewRows([]string{"DBID", "SystemID"}).
		AddRow(1, "NES").
		AddRow(2, "SNES")
	mock.ExpectQuery("SELECT DBID, SystemID FROM Systems").WillReturnRows(systemRows)

	titleRows := sqlmock.NewRows([]string{"DBID", "SystemDBID", "Slug", "SecondarySlug"}).
		AddRow(10, 1, "super-mario-bros", nil).
		AddRow(20, 2, "zelda-link-to-the-past", "zelda-3")
	mock.ExpectQuery("SELECT DBID, SystemDBID, Slug, SecondarySlug FROM MediaTitles").
		WillReturnRows(titleRows)

	cache, err := buildSlugSearchCache(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 2, cache.entryCount)
	assert.Equal(t, "NES", cache.systemDBIDToID[1])
	assert.Equal(t, "SNES", cache.systemDBIDToID[2])
	assert.Equal(t, int64(1), cache.systemIDToDBID["NES"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildSlugSearchCache_EmptyDB(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT DBID, SystemID FROM Systems").
		WillReturnRows(sqlmock.NewRows([]string{"DBID", "SystemID"}))
	mock.ExpectQuery("SELECT DBID, SystemDBID, Slug, SecondarySlug FROM MediaTitles").
		WillReturnRows(sqlmock.NewRows([]string{"DBID", "SystemDBID", "Slug", "SecondarySlug"}))

	cache, err := buildSlugSearchCache(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 0, cache.entryCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildSlugSearchCache_ContextCancelled(t *testing.T) {
	t.Parallel()
	db, mock, err := testsqlmock.NewSQLMock()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT DBID, SystemID FROM Systems").
		WillReturnRows(sqlmock.NewRows([]string{"DBID", "SystemID"}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// The cancelled context should cause the title query to fail
	mock.ExpectQuery("SELECT DBID, SystemDBID, Slug, SecondarySlug FROM MediaTitles").
		WillReturnError(context.Canceled)

	_, err = buildSlugSearchCache(ctx, db)
	require.Error(t, err)
}

func TestSlugSearchCache_Search_SingleWord(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"super-mario-bros-3", "", 2, 1},
		{"metroid", "", 3, 1},
	}, map[int64]string{1: "NES"})

	results := cache.Search(nil, [][][]byte{{[]byte("mario")}})
	assert.Equal(t, []int64{1, 2}, results)
}

func TestSlugSearchCache_Search_MultiWord(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"super-mario-bros-3", "", 2, 1},
		{"mario-kart", "", 3, 1},
	}, map[int64]string{1: "NES"})

	// AND: must contain both "mario" and "bros"
	results := cache.Search(nil, [][][]byte{
		{[]byte("mario")},
		{[]byte("bros")},
	})
	assert.Equal(t, []int64{1, 2}, results)
}

func TestSlugSearchCache_Search_VariantOR(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"castlevania", "", 1, 1},
		{"castlevania-ii", "", 2, 1},
		{"metroid", "", 3, 1},
	}, map[int64]string{1: "NES"})

	// OR within group: "castle" OR "castel"
	results := cache.Search(nil, [][][]byte{
		{[]byte("castle"), []byte("castel")},
	})
	assert.Equal(t, []int64{1, 2}, results)
}

func TestSlugSearchCache_Search_SystemFiltering(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"super-mario-world", "", 2, 2},
		{"zelda", "", 3, 1},
	}, map[int64]string{1: "NES", 2: "SNES"})

	results := cache.Search([]int64{2}, [][][]byte{{[]byte("mario")}})
	assert.Equal(t, []int64{2}, results)
}

func TestSlugSearchCache_Search_SecondarySlugFallback(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"zelda-link-to-the-past", "zelda-3", 1, 1},
		{"zelda-ocarina-of-time", "", 2, 1},
	}, map[int64]string{1: "NES"})

	// "3" only appears in the secondary slug of entry 1
	results := cache.Search(nil, [][][]byte{
		{[]byte("zelda")},
		{[]byte("3")},
	})
	assert.Equal(t, []int64{1}, results)
}

func TestSlugSearchCache_Search_EmptyVariantGroups(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"zelda", "", 2, 1},
		{"metroid", "", 3, 2},
	}, map[int64]string{1: "NES", 2: "SNES"})

	// Empty variantGroups = browse all
	results := cache.Search(nil, nil)
	assert.Equal(t, []int64{1, 2, 3}, results)

	// With system filter: returns all entries for that system
	results = cache.Search([]int64{1}, nil)
	assert.Equal(t, []int64{1, 2}, results)
}

func TestSlugSearchCache_Search_NoMatches(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
	}, map[int64]string{1: "NES"})

	results := cache.Search(nil, [][][]byte{{[]byte("nonexistent")}})
	assert.Empty(t, results)
}

func TestSlugSearchCache_Search_NilCache(t *testing.T) {
	t.Parallel()
	var cache *SlugSearchCache
	results := cache.Search(nil, [][][]byte{{[]byte("test")}})
	assert.Nil(t, results)
}

func TestSlugSearchCache_ResolveSystemDBIDs(t *testing.T) {
	t.Parallel()
	cache := buildTestCache(nil, map[int64]string{1: "NES", 2: "SNES", 3: "Genesis"})

	result := cache.ResolveSystemDBIDs([]string{"NES", "Genesis"})
	assert.ElementsMatch(t, []int64{1, 3}, result)
}

func TestSlugSearchCache_ResolveSystemDBIDs_Unknown(t *testing.T) {
	t.Parallel()
	cache := buildTestCache(nil, map[int64]string{1: "NES"})

	result := cache.ResolveSystemDBIDs([]string{"NES", "UNKNOWN"})
	assert.Equal(t, []int64{1}, result)
}

func TestSlugSearchCache_ResolveSystemDBIDs_NilCache(t *testing.T) {
	t.Parallel()
	var cache *SlugSearchCache
	result := cache.ResolveSystemDBIDs([]string{"NES"})
	assert.Nil(t, result)
}

// buildSyntheticCache creates a cache with n entries for benchmarking.
// Each entry gets a ~25 byte slug and ~20% have a secondary slug.
func buildSyntheticCache(n int) *SlugSearchCache {
	systems := map[int64]string{
		1: "NES", 2: "SNES", 3: "Genesis", 4: "PSX", 5: "N64",
	}
	cache := &SlugSearchCache{
		slugData:       make([]byte, 0, n*26),
		slugOffsets:    make([]uint32, 0, n+1),
		secSlugData:    make([]byte, 0, n*6),
		secSlugOffsets: make([]uint32, 0, n+1),
		titleDBIDs:     make([]int64, 0, n),
		systemDBIDs:    make([]int64, 0, n),
		systemDBIDToID: systems,
		systemIDToDBID: make(map[string]int64, len(systems)),
		entryCount:     n,
	}
	for dbid, id := range systems {
		cache.systemIDToDBID[id] = dbid
	}

	// Deterministic pseudo-random using simple LCG
	seed := uint32(42)
	nextRand := func() uint32 {
		seed = seed*1103515245 + 12345
		return seed >> 16
	}

	words := []string{
		"super", "mario", "zelda", "sonic", "metroid", "castlevania",
		"mega", "man", "final", "fantasy", "dragon", "quest", "street",
		"fighter", "mortal", "kombat", "donkey", "kong", "kirby", "star",
		"fox", "fire", "emblem", "pokemon", "contra", "ninja", "gaiden",
	}

	numWords := uint32(len(words)) //nolint:gosec // Safe: word list is small
	for i := range n {
		// Build slug from 2-4 random words
		wordCount := 2 + int(nextRand()%3)
		//nolint:gosec // Safe: benchmark data won't overflow uint32
		cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
		for w := range wordCount {
			if w > 0 {
				cache.slugData = append(cache.slugData, '-')
			}
			word := words[nextRand()%numWords]
			cache.slugData = append(cache.slugData, word...)
		}
		// Append unique suffix to avoid too many duplicates
		suffix := fmt.Sprintf("-%d", i)
		cache.slugData = append(cache.slugData, suffix...)
		cache.slugData = append(cache.slugData, 0)

		// 20% have secondary slugs
		//nolint:gosec // Safe: benchmark data won't overflow uint32
		cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))
		if nextRand()%5 == 0 {
			word := words[nextRand()%numWords]
			cache.secSlugData = append(cache.secSlugData, word...)
			cache.secSlugData = append(cache.secSlugData, fmt.Sprintf("-%d", i)...)
			cache.secSlugData = append(cache.secSlugData, 0)
		}

		cache.titleDBIDs = append(cache.titleDBIDs, int64(i+1))
		cache.systemDBIDs = append(cache.systemDBIDs, int64(1+nextRand()%5))
	}

	// Sentinel offsets
	//nolint:gosec // Safe: test data won't overflow uint32
	cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
	//nolint:gosec // Safe: test data won't overflow uint32
	cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))

	return cache
}

func BenchmarkSlugSearchCacheSearch_50k(b *testing.B) {
	cache := buildSyntheticCache(50_000)
	query := [][][]byte{{[]byte("mario")}, {[]byte("super")}}
	b.ResetTimer()
	for b.Loop() {
		cache.Search(nil, query)
	}
}

func BenchmarkSlugSearchCacheSearch_250k(b *testing.B) {
	cache := buildSyntheticCache(250_000)
	query := [][][]byte{{[]byte("mario")}, {[]byte("super")}}
	b.ResetTimer()
	for b.Loop() {
		cache.Search(nil, query)
	}
}

func BenchmarkSlugSearchCacheSearch_1M(b *testing.B) {
	cache := buildSyntheticCache(1_000_000)
	query := [][][]byte{{[]byte("mario")}, {[]byte("super")}}
	b.ResetTimer()
	for b.Loop() {
		cache.Search(nil, query)
	}
}

// --- ExactSlugMatch tests ---

func TestSlugSearchCache_ExactSlugMatch(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"super-mario-bros-3", "", 2, 1},
		{"metroid", "", 3, 2},
	}, map[int64]string{1: "NES", 2: "SNES"})

	// Exact match
	results := cache.ExactSlugMatch(nil, []byte("super-mario-bros"))
	assert.Equal(t, []int64{1}, results)

	// No match
	results = cache.ExactSlugMatch(nil, []byte("super-mario"))
	assert.Nil(t, results)

	// System filter
	results = cache.ExactSlugMatch([]int64{2}, []byte("metroid"))
	assert.Equal(t, []int64{3}, results)

	// System filter excludes match
	results = cache.ExactSlugMatch([]int64{2}, []byte("super-mario-bros"))
	assert.Nil(t, results)
}

func TestSlugSearchCache_ExactSlugMatch_NilCache(t *testing.T) {
	t.Parallel()
	var cache *SlugSearchCache
	results := cache.ExactSlugMatch(nil, []byte("test"))
	assert.Nil(t, results)
}

// --- ExactSecondarySlugMatch tests ---

func TestSlugSearchCache_ExactSecondarySlugMatch(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"zelda-link-to-the-past", "zelda-3", 1, 1},
		{"zelda-ocarina-of-time", "zelda-5", 2, 1},
		{"metroid", "", 3, 1},
	}, map[int64]string{1: "NES"})

	// Exact secondary slug match
	results := cache.ExactSecondarySlugMatch(nil, []byte("zelda-3"))
	assert.Equal(t, []int64{1}, results)

	// No match (entry has no secondary slug)
	results = cache.ExactSecondarySlugMatch(nil, []byte("metroid"))
	assert.Nil(t, results)

	// Partial match should not work
	results = cache.ExactSecondarySlugMatch(nil, []byte("zelda"))
	assert.Nil(t, results)

	// System filter
	results = cache.ExactSecondarySlugMatch([]int64{1}, []byte("zelda-5"))
	assert.Equal(t, []int64{2}, results)
}

func TestSlugSearchCache_ExactSecondarySlugMatch_NilCache(t *testing.T) {
	t.Parallel()
	var cache *SlugSearchCache
	results := cache.ExactSecondarySlugMatch(nil, []byte("test"))
	assert.Nil(t, results)
}

// --- PrefixSlugMatch tests ---

func TestSlugSearchCache_PrefixSlugMatch(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"super-mario-bros-3", "", 2, 1},
		{"metroid", "", 3, 2},
	}, map[int64]string{1: "NES", 2: "SNES"})

	// Prefix match
	results := cache.PrefixSlugMatch(nil, []byte("super-mario-bros"))
	assert.Equal(t, []int64{1, 2}, results)

	// Shorter prefix
	results = cache.PrefixSlugMatch(nil, []byte("super"))
	assert.Equal(t, []int64{1, 2}, results)

	// No match
	results = cache.PrefixSlugMatch(nil, []byte("zelda"))
	assert.Nil(t, results)

	// System filter
	results = cache.PrefixSlugMatch([]int64{2}, []byte("met"))
	assert.Equal(t, []int64{3}, results)
}

func TestSlugSearchCache_PrefixSlugMatch_NilCache(t *testing.T) {
	t.Parallel()
	var cache *SlugSearchCache
	results := cache.PrefixSlugMatch(nil, []byte("test"))
	assert.Nil(t, results)
}

// --- ExactSlugMatchAny tests ---

func TestSlugSearchCache_ExactSlugMatchAny(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"metroid", "", 2, 1},
		{"zelda", "", 3, 2},
	}, map[int64]string{1: "NES", 2: "SNES"})

	// Multi-slug match
	results := cache.ExactSlugMatchAny(nil, [][]byte{
		[]byte("super-mario-bros"),
		[]byte("zelda"),
	})
	assert.Equal(t, []int64{1, 3}, results)

	// Single match
	results = cache.ExactSlugMatchAny(nil, [][]byte{[]byte("metroid")})
	assert.Equal(t, []int64{2}, results)

	// No match
	results = cache.ExactSlugMatchAny(nil, [][]byte{[]byte("nonexistent")})
	assert.Nil(t, results)

	// Empty slug list
	results = cache.ExactSlugMatchAny(nil, nil)
	assert.Nil(t, results)

	// System filter
	results = cache.ExactSlugMatchAny([]int64{1}, [][]byte{
		[]byte("super-mario-bros"),
		[]byte("zelda"),
	})
	assert.Equal(t, []int64{1}, results)
}

func TestSlugSearchCache_ExactSlugMatchAny_NilCache(t *testing.T) {
	t.Parallel()
	var cache *SlugSearchCache
	results := cache.ExactSlugMatchAny(nil, [][]byte{[]byte("test")})
	assert.Nil(t, results)
}

// --- RandomEntry tests ---

func TestSlugSearchCache_RandomEntry(t *testing.T) {
	t.Parallel()
	cache := buildTestCache([]struct {
		slug       string
		secSlug    string
		titleDBID  int64
		systemDBID int64
	}{
		{"super-mario-bros", "", 1, 1},
		{"metroid", "", 2, 1},
		{"zelda", "", 3, 2},
	}, map[int64]string{1: "NES", 2: "SNES"})

	// Random from all entries
	titleDBID, ok := cache.RandomEntry(nil)
	assert.True(t, ok)
	assert.Contains(t, []int64{1, 2, 3}, titleDBID)

	// Random with system filter
	titleDBID, ok = cache.RandomEntry([]int64{2})
	assert.True(t, ok)
	assert.Equal(t, int64(3), titleDBID)

	// Empty result
	_, ok = cache.RandomEntry([]int64{99})
	assert.False(t, ok)
}

func TestSlugSearchCache_RandomEntry_EmptyCache(t *testing.T) {
	t.Parallel()
	cache := buildTestCache(nil, map[int64]string{1: "NES"})
	_, ok := cache.RandomEntry(nil)
	assert.False(t, ok)
}

func TestSlugSearchCache_RandomEntry_NilCache(t *testing.T) {
	t.Parallel()
	var cache *SlugSearchCache
	_, ok := cache.RandomEntry(nil)
	assert.False(t, ok)
}

func BenchmarkSlugSearchCacheBuild(b *testing.B) {
	// Benchmark cache struct population speed (excludes SQL)
	for b.Loop() {
		buildSyntheticCache(250_000)
	}
}

func BenchmarkSlugSearchCacheMemory(b *testing.B) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	cache := buildSyntheticCache(250_000)
	runtime.GC()
	runtime.ReadMemStats(&after)
	_ = cache
	b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/(1024*1024), "MB")
}
