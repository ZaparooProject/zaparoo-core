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
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/hbollon/go-edlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedCandidateTitles(t testing.TB, db *MediaDB, systemID string, names ...string) []int64 {
	t.Helper()
	system, err := db.FindOrInsertSystem(database.System{SystemID: systemID, Name: systemID})
	require.NoError(t, err)
	tx, err := db.sql.Load().BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		metadata := GenerateSlugWithMetadata(slugs.MediaTypeGame, name)
		result, insertErr := tx.ExecContext(context.Background(), `INSERT INTO MediaTitles
			(SystemDBID, Name, Slug, SecondarySlug, SlugLength, SlugWordCount) VALUES (?, ?, ?, ?, ?, ?)`,
			system.DBID, name, metadata.Slug, metadata.SecondarySlug, metadata.SlugLength, metadata.SlugWordCount)
		require.NoError(t, insertErr)
		id, idErr := result.LastInsertId()
		require.NoError(t, idErr)
		path := filepath.Join("roms", systemID, fmt.Sprintf("%d.nes", id))
		result, insertErr = tx.ExecContext(context.Background(),
			`INSERT INTO Media (SystemDBID, MediaTitleDBID, Path) VALUES (?, ?, ?)`,
			system.DBID, id, path)
		require.NoError(t, insertErr)
		mediaID, mediaErr := result.LastInsertId()
		require.NoError(t, mediaErr)
		ids = append(ids, mediaID)
	}
	require.NoError(t, tx.Commit())
	return ids
}

func TestTitleCandidatesEvidence(t *testing.T) {
	t.Parallel()
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			t.Parallel()
			db, cleanup := setupTempMediaDB(t)
			t.Cleanup(cleanup)
			seedCandidateTitles(t, db, "NES", "Super Mario Bros", "Mario World", "Legend of Zelda: Ocarina of Time",
				"Metroid", "Metroid: Zero Mission", "ドラゴンクエスト", "Pokémon", "Crystal Space Quest",
				"Touhou Koumakyou: The Embodiment of Scarlet Devil")
			seedCandidateTitles(t, db, "SNES", "Super Mario Bros Two")
			if cached {
				require.NoError(t, db.RebuildSlugSearchCache())
			}
			for _, tc := range []struct{ query, name, kind string }{
				{"the super mario bros", "Super Mario Bros", "exact"},
				{"Ocarina of Time", "Legend of Zelda: Ocarina of Time", "secondary"},
				{"Unknown Series: Metroid", "Metroid", "secondary"},
				{"Metriod", "Metroid", "fuzzy"},
				{"ドラゴンクエスト", "ドラゴンクエスト", "exact"},
				{"ドラゴンクエスド", "ドラゴンクエスト", "fuzzy"},
				{"Pokemon", "Pokémon", "exact"},
				{"Quest Space Crystal", "Crystal Space Quest", "fuzzy"},
				{
					"Touhou 06: The Embodiment of Scarlet Devil",
					"Touhou Koumakyou: The Embodiment of Scarlet Devil", "secondary",
				},
			} {
				t.Run(tc.query, func(t *testing.T) {
					got, err := db.TitleCandidates(context.Background(), "NES", tc.query, 5)
					require.NoError(t, err)
					require.NotEmpty(t, got)
					assert.Equal(t, tc.name, got[0].Name)
					assert.Equal(t, tc.kind, got[0].MatchType)
					for i := range got {
						assert.Equal(t, "NES", got[i].SystemID)
						assert.Equal(t, i+1, got[i].Rank)
					}
				})
			}
			for _, query := range []string{"zzzzqqqqvvvv", "ab", "Foreign Franchise: The Embodiment of Scarlet Devil"} {
				got, err := db.TitleCandidates(context.Background(), "NES", query, 5)
				require.NoError(t, err)
				assert.Empty(t, got)
				assert.NotNil(t, got)
			}
		})
	}
}

func TestTitleCandidatesVisibilityBeforeLimit(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	names := make([]string, 0, candidateBatchSize+7)
	for i := range candidateBatchSize + 7 {
		names = append(names, fmt.Sprintf("Series %03d: Shared Subtitle", i))
	}
	ids := seedCandidateTitles(t, db, "NES", names...)
	require.NoError(t, db.RebuildSlugSearchCache())
	for _, id := range ids[:candidateBatchSize] {
		require.NoError(t, db.UpdateMediaTags(context.Background(), id, nil,
			[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	}
	_, err := db.sql.Load().ExecContext(context.Background(),
		"UPDATE Media SET IsMissing = 1 WHERE DBID = ?", ids[candidateBatchSize])
	require.NoError(t, err)
	// Reuse the same title for another present variant. A hidden variant must
	// not suppress the title while this variant remains discoverable.
	_, err = db.sql.Load().ExecContext(context.Background(), `INSERT INTO Media (SystemDBID, MediaTitleDBID, Path)
		SELECT SystemDBID, MediaTitleDBID, ? FROM Media WHERE DBID = ?`,
		filepath.Join("roms", "NES", "visible-variant.nes"), ids[0])
	require.NoError(t, err)
	for _, cached := range []bool{true, false} {
		if !cached {
			db.slugSearchCache.Store(nil)
		}
		got, lookupErr := db.TitleCandidates(context.Background(), "NES", "Shared Subtitle", 5)
		require.NoError(t, lookupErr)
		require.Len(t, got, 5)
		assert.Equal(t, names[0], got[0].Name)
		for i := 1; i < 5; i++ {
			assert.Equal(t, names[candidateBatchSize+i], got[i].Name)
		}
	}
}

func TestTitleCandidatesFuzzyEligibilityBeforeRanking(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	names := make([]string, candidateBatchSize+12)
	for i := range names {
		names[i] = fmt.Sprintf("Mario Game %03d", i)
	}
	ids := seedCandidateTitles(t, db, "NES", names...)
	for _, id := range ids[:candidateBatchSize] {
		require.NoError(t, db.UpdateMediaTags(context.Background(), id, nil,
			[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	}
	cold, err := db.TitleCandidates(context.Background(), "NES", "Mario Gmae 003", 5)
	require.NoError(t, err)
	require.Len(t, cold, 5)
	for _, candidate := range cold {
		assert.Contains(t, names[candidateBatchSize:], candidate.Name)
		assert.Equal(t, "fuzzy", candidate.MatchType)
	}
	require.NoError(t, db.RebuildSlugSearchCache())
	warm, err := db.TitleCandidates(context.Background(), "NES", "Mario Gmae 003", 5)
	require.NoError(t, err)
	assert.Equal(t, cold, warm, "ineligible high-scoring cache nominations cannot consume the top-five budget")
}

func TestTitleCandidatesRankingAndReadOnly(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	seedCandidateTitles(t, db, "NES", "Mario", "A Series: Mario", "B Series: Mario", "Mariob", "Marioa", "Marioa")
	for _, cached := range []bool{false, true} {
		if cached {
			require.NoError(t, db.RebuildSlugSearchCache())
		}
		before := db.slugSearchCache.Load()
		got, err := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
		require.NoError(t, err)
		require.Len(t, got, 3, "five is a ceiling, not a reason to pad exact matches with fuzzy results")
		assert.Equal(t, []string{"Mario", "A Series: Mario", "B Series: Mario"},
			[]string{got[0].Name, got[1].Name, got[2].Name})
		assert.Equal(t, "exact", got[0].MatchType)
		assert.Equal(t, "secondary", got[1].MatchType)
		assert.Equal(t, "secondary", got[2].MatchType)
		assert.Same(t, before, db.slugSearchCache.Load())
		var count int
		err = db.sql.Load().QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM SlugResolutionCache").Scan(&count)
		require.NoError(t, err)
		assert.Zero(t, count)
		var wg sync.WaitGroup
		for range 12 {
			wg.Go(func() {
				result, lookupErr := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
				if assert.NoError(t, lookupErr) {
					assert.Equal(t, got, result)
				}
			})
		}
		wg.Wait()
	}
}

func TestTitleCandidatesHiddenExactAllowsFuzzyFallback(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	ids := seedCandidateTitles(t, db, "NES", "Mario", "Series: Mario", "Marioa")
	require.NoError(t, db.UpdateMediaTags(context.Background(), ids[0], nil,
		[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	_, err := db.sql.Load().ExecContext(context.Background(),
		"UPDATE Media SET IsMissing = 1 WHERE DBID = ?", ids[1])
	require.NoError(t, err)
	for _, cached := range []bool{false, true} {
		if cached {
			require.NoError(t, db.RebuildSlugSearchCache())
		}
		got, lookupErr := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
		require.NoError(t, lookupErr)
		require.Len(t, got, 1)
		assert.Equal(t, "Marioa", got[0].Name)
		assert.Equal(t, "fuzzy", got[0].MatchType)
	}
}

func TestTitleCandidatesFuzzyWithoutSharedTrigrams(t *testing.T) {
	t.Parallel()
	const query, name = "abcdefghi", "badcfehgi"
	for i := range len(query) - 2 {
		require.NotContains(t, name, query[i:i+3])
	}
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	seedCandidateTitles(t, db, "NES", name)
	for _, cached := range []bool{false, true} {
		if cached {
			require.NoError(t, db.RebuildSlugSearchCache())
		}
		got, err := db.TitleCandidates(context.Background(), "NES", query, 5)
		require.NoError(t, err)
		require.Len(t, got, 1, "substring trigram pruning would discard a qualifying fuzzy match")
		assert.Equal(t, name, got[0].Name)
		assert.Equal(t, "fuzzy", got[0].MatchType)
	}
}

func TestTitleCandidatesInvalidAndCanceled(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	for _, name := range []string{
		"", " \t ", "!!!", strings.Repeat("a", 257), strings.Repeat("\u754c", 257), string([]byte{0xff}),
	} {
		_, err := db.TitleCandidates(context.Background(), "NES", name, 5)
		require.Error(t, err, name)
	}
	for _, limit := range []int{-1, 0, 6} {
		_, err := db.TitleCandidates(context.Background(), "NES", "Mario", limit)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := db.TitleCandidates(ctx, "NES", "Mario", 5)
	require.ErrorIs(t, err, context.Canceled)
	got, err := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestTitleCandidatesRejectOldCacheAfterRecreate(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	seedCandidateTitles(t, db, "NES", "Mario")
	require.NoError(t, db.RebuildSlugSearchCache())
	old := db.slugSearchCache.Load()
	require.NoError(t, db.Recreate(false))
	seedCandidateTitles(t, db, "NES", "Metroid")
	// Simulate a delayed old cache publication after the new database opens.
	db.slugSearchCache.Store(old)
	got, err := db.TitleCandidates(context.Background(), "NES", "Metroid", 5)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Metroid", got[0].Name)
	got, err = db.TitleCandidates(context.Background(), "NES", "Mario", 5)
	require.NoError(t, err)
	assert.Empty(t, got)
}

type candidateWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *candidateWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestTitleCandidatesReplacementDuringRequest(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	other, cleanupOther := setupTempMediaDB(t)
	t.Cleanup(cleanupOther)
	seedCandidateTitles(t, db, "NES", "Mario")
	old := db.sql.Load()
	old.SetMaxOpenConns(1)
	held, err := old.Conn(context.Background())
	require.NoError(t, err)
	ctx := &candidateWaitingContext{Context: context.Background(), waiting: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		_, lookupErr := db.TitleCandidates(ctx, "NES", "Mario", 5)
		result <- lookupErr
	}()
	<-ctx.waiting
	db.sql.Store(other.sql.Load())
	defer db.sql.Store(old)
	require.NoError(t, held.Close())
	require.ErrorIs(t, <-result, errCandidateDatabaseChanged)
}

func TestTitleCandidatesCacheCoverageFallback(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	seedCandidateTitles(t, db, "NES", "Mario")
	require.NoError(t, db.RebuildSlugSearchCache())
	db.slugSearchCache.Store(db.slugSearchCache.Load().withoutSystems([]string{"NES"}))
	got, err := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Mario", got[0].Name)
}

func TestTitleCandidateRankingIndependentOfInputOrder(t *testing.T) {
	t.Parallel()
	system, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	names := []string{"Marioa", "Mariob", "Marioc", "Mariod", "Marioe", "Mariof", "Marioa"}
	var want []rankedTitle
	for pass := range names {
		r := titleRanker{
			system: *system, query: GenerateSlugWithMetadata(slugs.MediaTypeGame, "Mario"),
			name: "Mario", limit: 5, allowFuzzy: true,
		}
		for i := range names {
			idx := (i + pass) % len(names)
			r.consider(int64(idx+1), names[idx], slugs.Slugify(slugs.MediaTypeGame, names[idx]), "")
		}
		if pass == 0 {
			want = slices.Clone(r.top)
			require.Len(t, want, 5)
			for i, name := range []string{"Marioa", "Mariob", "Marioc", "Mariod", "Marioe"} {
				assert.Equal(t, name, want[i].candidate.Name)
			}
			assert.Equal(t, int64(1), want[0].id, "duplicate canonical titles retain deterministic identity")
		} else {
			assert.Equal(t, want, r.top)
		}
	}
}

type candidateCancelContext struct {
	context.Context
	checks int
}

func (c *candidateCancelContext) Err() error {
	c.checks--
	if c.checks <= 0 {
		return context.Canceled
	}
	return nil
}

func TestTitleCandidatesExactSeesNewTitlesBeforeCacheRefresh(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	seedCandidateTitles(t, db, "NES", "Mariob")
	require.NoError(t, db.RebuildSlugSearchCache())
	cache := db.slugSearchCache.Load()
	seedCandidateTitles(t, db, "NES", "Mario", "Series: Mario")
	require.Same(t, cache, db.slugSearchCache.Load(), "fixture must retain pre-insert cache")
	got, err := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Mario", got[0].Name)
	assert.Equal(t, "exact", got[0].MatchType)
	assert.Equal(t, "Series: Mario", got[1].Name)
	assert.Equal(t, "secondary", got[1].MatchType)
}

func TestTitleCandidatesCancelFuzzyCacheTraversal(t *testing.T) {
	t.Parallel()
	cache, _ := buildBenchSweepCache(1, 1000)
	r := titleRanker{query: SlugMetadata{Slug: "zzzzzzzzzzzzzzzzzzzzzz"}, limit: 5}
	ctx := &candidateCancelContext{Context: context.Background(), checks: 20}
	err := r.cachedFuzzy(ctx, nil, cache, 1)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTitleCandidateDenseTiesMatchExhaustiveRanking(t *testing.T) {
	t.Parallel()
	system, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	const query = "Library Adventuer 001234"
	names := make([]string, 256)
	for i := range names {
		names[i] = fmt.Sprintf("Library Adventure %06d", 1100+i)
		a := slugs.Slugify(slugs.MediaTypeGame, query)
		b := slugs.Slugify(slugs.MediaTypeGame, names[i])
		assert.Equal(t,
			math.Float32bits(edlib.JaroWinklerSimilarity(a, b)), math.Float32bits(candidateSimilarity(a, b)))
	}
	rank := func(limit, shift int) []rankedTitle {
		r := titleRanker{
			system: *system, query: GenerateSlugWithMetadata(slugs.MediaTypeGame, query),
			name: query, limit: limit, allowFuzzy: true,
			signature: matcher.GenerateTokenSignature(slugs.MediaTypeGame, query),
		}
		for i := range names {
			index := (i + shift) % len(names)
			r.consider(int64(index+1), names[index], slugs.Slugify(slugs.MediaTypeGame, names[index]), "")
		}
		return r.top
	}
	all := rank(len(names), 0) // No top-k pruning: every qualifying distance is evaluated.
	require.GreaterOrEqual(t, len(all), 5)
	for _, shift := range []int{0, 1, 64, 127, 255} {
		assert.Equal(t, all[:5], rank(5, shift))
	}
}

func TestTitleCandidateBlocksCacheLifecycle(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	seedCandidateTitles(t, db, "NES", "Metroid")
	seedCandidateTitles(t, db, "SNES", "Mario")
	require.NoError(t, db.RebuildSlugSearchCache())
	base := db.slugSearchCache.Load()
	require.NotEmpty(t, base.candidateBlocks)
	require.NoError(t, db.PersistSlugSearchCache())
	db.slugSearchCache.Store(nil)
	loaded, err := db.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	require.True(t, loaded)
	assert.Equal(t, base.candidateBlocks, db.slugSearchCache.Load().candidateBlocks)

	nes := base.systemIDToDBID["NES"]
	original := slices.Clone(base.candidateBlocks[nes])
	seedCandidateTitles(t, db, "NES", "Zelda")
	fragment, err := buildSlugSearchCacheForSystems(context.Background(), db.sql.Load(), []string{"NES"})
	require.NoError(t, err)
	merged := mergeSlugSearchCaches(base.withoutSystems([]string{"NES"}), fragment)
	assert.Equal(t, original, base.candidateBlocks[nes], "published bounds remain immutable")
	for _, tc := range []struct{ system, query string }{{"NES", "Zedla"}, {"SNES", "Maroi"}} {
		db.slugSearchCache.Store(nil)
		cold, lookupErr := db.TitleCandidates(context.Background(), tc.system, tc.query, 5)
		require.NoError(t, lookupErr)
		require.NotEmpty(t, cold)
		db.slugSearchCache.Store(merged)
		warm, lookupErr := db.TitleCandidates(context.Background(), tc.system, tc.query, 5)
		require.NoError(t, lookupErr)
		assert.Equal(t, cold, warm)
	}
}

func TestTitleCandidateHintsNeverRequireSharedTrigrams(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	names := make([]string, 0, 642)
	names = append(names, "Abcdefghz") // A misleading but qualifying hint in the first group.
	for range 640 {
		names = append(names, "Zzzzzzzzz")
	}
	names = append(names, "Badcfehgi") // Qualifies without sharing any query trigram.
	seedCandidateTitles(t, db, "NES", names...)
	seedCandidateTitles(t, db, "SNES", "Abcdefghi")
	want, err := db.TitleCandidates(t.Context(), "NES", "Abcdefghi", 5)
	require.NoError(t, err)
	require.Len(t, want, 2)
	require.NoError(t, db.RebuildSlugSearchCache())
	base := db.slugSearchCache.Load()
	require.NoError(t, db.PersistSlugSearchCache())
	db.slugSearchCache.Store(nil)
	loaded, err := db.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	require.True(t, loaded)
	persisted := db.slugSearchCache.Load()
	fragment, err := buildSlugSearchCacheForSystems(t.Context(), db.sql.Load(), []string{"NES"})
	require.NoError(t, err)
	merged := mergeSlugSearchCaches(base.withoutSystems([]string{"NES"}), fragment)
	withoutIndex := *base
	withoutIndex.trigramOffsets, withoutIndex.trigramPostings, withoutIndex.trigramDeltas = nil, nil, nil
	for _, cache := range []*SlugSearchCache{base, persisted, merged, &withoutIndex} {
		db.slugSearchCache.Store(cache)
		got, lookupErr := db.TitleCandidates(t.Context(), "NES", "Abcdefghi", 5)
		require.NoError(t, lookupErr)
		assert.Equal(t, want, got, "hints cannot exclude matches, including after persistence or rebasing")
	}
}

func TestTitleCandidateSeedTraversalPreservesEligibility(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	names := make([]string, 640)
	for i := range names {
		names[i] = fmt.Sprintf("Library Adventure %06d", 1000+i)
	}
	seedCandidateTitles(t, db, "NES", names...)
	require.NoError(t, db.RebuildSlugSearchCache())
	cache := db.slugSearchCache.Load()
	query := "Library Adventuer 001234"
	metadata := GenerateSlugWithMetadata(slugs.MediaTypeGame, query)
	seeds, count, err := cache.seedBlocks(t.Context(), metadata.Slug, cache.systemRanges[1])
	require.NoError(t, err)
	require.Positive(t, count)
	require.LessOrEqual(t, count, candidateSeedBlocks)
	// Make every initially preferred group unavailable. No cutoff may be
	// raised from those nominations, and later groups must remain reachable.
	for _, group := range seeds[:count] {
		start := group * candidateBlockEntries
		for _, id := range cache.titleDBIDs[start:min(start+candidateBlockEntries, cache.entryCount)] {
			_, err = db.sql.Load().ExecContext(t.Context(),
				"UPDATE Media SET IsMissing = 1 WHERE MediaTitleDBID = ?", id)
			require.NoError(t, err)
		}
	}
	got, err := db.TitleCandidates(t.Context(), "NES", query, 5)
	require.NoError(t, err)
	require.Len(t, got, 5)
	db.slugSearchCache.Store(nil)
	want, err := db.TitleCandidates(t.Context(), "NES", query, 5)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestTitleCandidateBlockBounds(t *testing.T) {
	t.Parallel()
	cache, _ := buildBenchSweepCache(1, 1000)
	require.NotEmpty(t, cache.candidateBlocks)
	query := newCandidateCharacterBound("zzzzzzzzzzzzzzzzzzzzzz")
	for _, block := range cache.candidateBlocks[1] {
		assert.False(t, query.blockPossible(&block, matcher.FuzzyMatchMinSimilarity))
	}
	// Independent maxima cover every query character, but every title also
	// contains two '5' characters that cannot match this query.
	cache = buildTestCache([]benchCacheEntry{
		{slug: "libraryadventure550012", titleDBID: 1, systemDBID: 1},
		{slug: "libraryadventure550034", titleDBID: 2, systemDBID: 1},
	}, map[int64]string{1: "NES"})
	block, ok := buildCandidateBlock(cache, 0, 2)
	require.True(t, ok)
	assert.False(t, newCandidateCharacterBound("libraryadventuer001234").blockPossible(&block, 0.98))
	assert.Equal(t, byte(len("libraryadventure5500")), block.prefixLength)

	// Mandatory repeats also consume unmatched positions when the query has
	// that character, but fewer copies. Independent maxima otherwise hide this.
	cache = buildTestCache([]benchCacheEntry{
		{slug: "libraryadventure000012", titleDBID: 1, systemDBID: 1},
		{slug: "libraryadventure000034", titleDBID: 2, systemDBID: 1},
	}, map[int64]string{1: "NES"})
	block, ok = buildCandidateBlock(cache, 0, 2)
	require.True(t, ok)
	assert.False(t, newCandidateCharacterBound("libraryadventuer001234").blockPossible(&block, 0.98))

	// Token-order evidence can qualify even when its Jaro score does not.
	cache = &SlugSearchCache{
		slugData: []byte("questspacecrystal"), slugOffsets: []uint32{0, uint32(len("questspacecrystal"))},
	}
	block, ok = buildCandidateBlock(cache, 0, 1)
	require.True(t, ok)
	assert.True(t, newCandidateCharacterBound("crystalspacequest").blockPossible(&block, 1))
	cache.slugOffsets[1] = 999
	_, ok = buildCandidateBlock(cache, 0, 1)
	assert.False(t, ok, "malformed cache offsets cannot panic during derived-index construction")

	cache = &SlugSearchCache{slugData: []byte(strings.Repeat("a", 256)), slugOffsets: []uint32{0, 256}}
	block, ok = buildCandidateBlock(cache, 0, 1)
	require.True(t, ok)
	assert.True(t, block.unbounded, "byte counter overflow must disable character pruning")
	assert.True(t, newCandidateCharacterBound(strings.Repeat("a", 256)).blockPossible(&block, 1))
}

func FuzzTitleCandidateSimilarityBound(f *testing.F) {
	f.Add("Mario", "Mariob", float32(0.85))
	f.Add("", "", float32(0.85))
	f.Add("a", "b", float32(0.85))
	f.Add(strings.Repeat("a", 31)+"b", "b"+strings.Repeat("a", 31), float32(0.85))
	f.Add(strings.Repeat("a", 32)+"b", "b"+strings.Repeat("a", 32), float32(0.85))
	f.Add("abcde", "edcba", float32(0.9))
	f.Add("libraryadventuer001234", "libraryadventure001234", float32(0.98))
	f.Add("abcdefghi", "badcfehgi", float32(0.85))
	f.Add("crystalspacequest", "questspacecrystal", float32(1))
	f.Add(strings.Repeat("a", 256), strings.Repeat("a", 256), float32(1))
	f.Add("\u65e5\u672c\u8a9e", "\u65e5\u672c", float32(0.85))
	f.Fuzz(func(t *testing.T, a, b string, cutoff float32) {
		if len(a) > 256 || len(b) > 256 || cutoff < 0 || cutoff > 1 {
			return
		}
		assert.Equal(t,
			math.Float32bits(edlib.JaroWinklerSimilarity(a, b)), math.Float32bits(candidateSimilarity(a, b)),
			"fast scorer must preserve the reference float32 score exactly")
		if len(a) <= 32 && len(b) <= 32 {
			assert.LessOrEqual(t, candidateDistanceLowerBound(a, b), edlib.DamerauLevenshteinDistance(a, b))
		}
		bound := newCandidateCharacterBound(a)
		same, possible := bound.check(b, cutoff)
		if edlib.JaroWinklerSimilarity(a, b) >= cutoff && cutoff > 0 {
			assert.True(t, possible, "upper bound dropped a qualifying match")
			if len(b) >= len(a)-matcher.FuzzyMatchMaxLengthDiff &&
				len(b) <= len(a)+matcher.FuzzyMatchMaxLengthDiff {
				//nolint:gosec // Fuzz strings are bounded to 256 bytes above.
				cache := &SlugSearchCache{slugData: append([]byte(b), 0), slugOffsets: []uint32{0, uint32(len(b) + 1)}}
				block, ok := buildCandidateBlock(cache, 0, 1)
				require.True(t, ok)
				assert.True(t, bound.blockPossible(&block, cutoff), "group bound dropped a qualifying match")
				other := "0" + b
				if b != "" {
					other = "0" + b[1:]
				}
				data := append(append([]byte(b), 0), other...)
				data = append(data, 0)
				group := &SlugSearchCache{
					slugData: data,
					//nolint:gosec // Both fuzz entries are bounded to 256 bytes above.
					slugOffsets: []uint32{0, uint32(len(b) + 1), uint32(len(data))},
				}
				block, ok = buildCandidateBlock(group, 0, 2)
				require.True(t, ok)
				assert.True(t, bound.blockPossible(&block, cutoff), "mixed group dropped a qualifying member")
			}
		}
		if a != "" && b != "" {
			assert.Equal(t, sameCandidateLetters(a, b), same, "signature nomination must preserve byte multisets")
		}
		for _, length := range []int{0, len(b) / 2, len(b)} {
			prefixed := newCandidateCharacterBound(a)
			prefixed.setPrefix([]byte(b[:length]))
			prefixSame, prefixPossible := prefixed.check(b, cutoff)
			assert.Equal(t, same, prefixSame, "shared-prefix consumption changed anagram evidence")
			assert.Equal(t, possible, prefixPossible, "shared-prefix consumption changed similarity bound")
			prefixed.setPrefix(nil)
			resetSame, resetPossible := prefixed.check(b, cutoff)
			assert.Equal(t, same, resetSame)
			assert.Equal(t, possible, resetPossible)
		}
		// Exercise restoration after unrelated ASCII, Unicode and repeated-byte
		// probes; retained scratch must not depend on the previous candidate.
		for _, probe := range []string{a, "\u65e5\u672c", strings.Repeat("a", 256)} {
			bound.check(probe, cutoff)
		}
		againSame, againPossible := bound.check(b, cutoff)
		assert.Equal(t, same, againSame)
		assert.Equal(t, possible, againPossible)
	})
}

func TestTitleCandidatesSQLReadsAreBatched(t *testing.T) {
	t.Parallel()
	conn, mockDB, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	db := &MediaDB{}
	db.sql.Store(conn)
	mockDB.ExpectQuery("SELECT DBID FROM Systems").WithArgs("NES").
		WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(1))
	columns := []string{"DBID", "Name", "Slug", "SecondarySlug"}
	mockDB.ExpectQuery("SELECT t.DBID").WillReturnRows(sqlmock.NewRows(columns))
	rows := sqlmock.NewRows(columns)
	for i := range 100 {
		rows.AddRow(i+1, fmt.Sprintf("Mario%02d", i), fmt.Sprintf("mario%02d", i), "")
	}
	mockDB.ExpectQuery("SELECT t.DBID").WillReturnRows(rows)
	got, err := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
	require.NoError(t, err)
	require.Len(t, got, 5)
	require.NoError(t, mockDB.ExpectationsWereMet(), "one system read and two title queries, not per-candidate reads")
}

func TestTitleCandidatesStreamErrorDiscardsPartialResults(t *testing.T) {
	t.Parallel()
	conn, mockDB, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	db := &MediaDB{}
	db.sql.Store(conn)
	mockDB.ExpectQuery("SELECT DBID FROM Systems").WillReturnRows(sqlmock.NewRows([]string{"DBID"}).AddRow(1))
	mockDB.ExpectQuery("SELECT t.DBID").WillReturnRows(sqlmock.NewRows(
		[]string{"DBID", "Name", "Slug", "SecondarySlug"}).
		AddRow(1, "Mario", "mario", "").AddRow(2, "Mario", "mario", "").RowError(1, context.Canceled))
	got, err := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got, "an interrupted stream must not return its partial ranking")
	require.NoError(t, mockDB.ExpectationsWereMet())
}

func TestTitleCandidatesDatabaseErrorsAreNotNoMatch(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.sql.Load().Close())
	got, err := db.TitleCandidates(context.Background(), "NES", "Mario", 5)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.NotErrorIs(t, err, sql.ErrNoRows)
}
