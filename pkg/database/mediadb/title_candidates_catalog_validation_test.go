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
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/stretchr/testify/require"
)

type candidateCatalogProbe struct {
	label string
	query string
	empty bool
}

func candidateCatalogTypo(name string) string {
	original := GenerateSlugWithMetadata(slugs.MediaTypeGame, name)
	runes := []rune(name)
	for i := range len(runes) - 1 {
		if !unicode.IsLetter(runes[i]) || !unicode.IsLetter(runes[i+1]) || runes[i] == runes[i+1] {
			continue
		}
		runes[i], runes[i+1] = runes[i+1], runes[i]
		query := string(runes)
		changed := GenerateSlugWithMetadata(slugs.MediaTypeGame, query)
		if changed.Slug != original.Slug && len(changed.Slug) >= 5 &&
			len(changed.Slug) == len(original.Slug) && changed.SlugWordCount == original.SlugWordCount {
			return query
		}
		runes[i], runes[i+1] = runes[i+1], runes[i]
	}
	return ""
}

func candidateCatalogProbes(fixture candidateCatalogFixture) []candidateCatalogProbe {
	probes := []candidateCatalogProbe{
		{label: "absent-short", query: "zzzzqqqqvvvv", empty: true},
		{label: "absent-long", query: "zzzzzzzzqqqqqqqqvvvvvvvv", empty: true},
	}
	for sample := 1; sample <= 12; sample++ {
		name := fixture.Names[len(fixture.Names)*sample/13]
		if !database.ValidTitleCandidateName(name) || slugs.Slugify(slugs.MediaTypeGame, name) == "" {
			continue
		}
		probes = append(probes, candidateCatalogProbe{label: fmt.Sprintf("%02d-exact", sample), query: name})
		if typo := candidateCatalogTypo(name); typo != "" {
			probes = append(probes, candidateCatalogProbe{label: fmt.Sprintf("%02d-typo", sample), query: typo})
		}
	}
	return probes
}

// TestTitleCandidatesCatalogValidation is opt-in and offline. It compares full
// responses against SQL fallback, not only counts, and measures each call with
// cancellation enabled. "Cold" removes the slug cache, not SQLite/OS page caches.
func TestTitleCandidatesCatalogValidation(t *testing.T) {
	fixture := loadCandidateCatalog(t)
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	prepareCandidateCatalog(t, db, fixture)
	probes := candidateCatalogProbes(fixture)
	require.GreaterOrEqual(t, len(probes), 20, "fixture must support broad query sampling")
	measure := func(query string) ([]database.TitleCandidate, time.Duration) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		start := time.Now()
		got, lookupErr := db.TitleCandidates(ctx, fixture.System, query, 5)
		elapsed := time.Since(start)
		require.NoError(t, lookupErr, "query %q", query)
		require.LessOrEqual(t, len(got), 5)
		seen := make(map[string]bool)
		for i, candidate := range got {
			require.Equal(t, fixture.System, candidate.SystemID)
			require.Equal(t, i+1, candidate.Rank)
			require.False(t, seen[candidate.Name], "canonical name repeated")
			seen[candidate.Name] = true
		}
		return got, elapsed
	}
	_, first := measure(probes[len(probes)-1].query)
	start := time.Now()
	require.NoError(t, db.RebuildSlugSearchCache())
	build := time.Since(start)
	require.NoError(t, db.PersistSlugSearchCache())
	db.slugSearchCache.Store(nil)
	start = time.Now()
	loaded, err := db.LoadCachedSlugSearchCache()
	require.NoError(t, err)
	require.True(t, loaded)
	load := time.Since(start)
	cache := db.slugSearchCache.Load()
	t.Logf("catalog=%s names=%d first_uncached_ns=%d build_ns=%d load_ns=%d cache_bytes=%d",
		fixture.System, len(fixture.Names), first, build, load, cache.Size())
	fuzzyQueries := 0
	negativeControl := false
	var slowestCold time.Duration
	slowestQuery := ""
	for _, probe := range probes {
		db.slugSearchCache.Store(cache)
		warm, elapsed := measure(probe.query)
		samples := make([]time.Duration, 1, 5)
		samples[0] = elapsed
		for range 4 {
			again, duration := measure(probe.query)
			require.Equal(t, warm, again, "unstable warm result for %q", probe.query)
			samples = append(samples, duration)
		}
		db.slugSearchCache.Store(nil)
		cold, coldDuration := measure(probe.query)
		require.Equal(t, cold, warm, "cached traversal differs from SQL fallback for %q", probe.query)
		if probe.empty {
			require.Empty(t, warm)
		}
		if strings.HasSuffix(probe.label, "-exact") {
			require.NotEmpty(t, warm, "an indexed canonical name must resolve")
			require.Equal(t, "exact", warm[0].MatchType)
		}
		kind := "none"
		if len(warm) > 0 {
			kind = warm[0].MatchType
		}
		if kind == "fuzzy" {
			fuzzyQueries++
			if coldDuration > slowestCold {
				slowestCold, slowestQuery = coldDuration, probe.query
			}
			if !negativeControl {
				broken := *cache
				broken.systemRanges = maps.Clone(cache.systemRanges)
				broken.systemRanges[cache.systemIDToDBID[fixture.System]] = [2]int{}
				db.slugSearchCache.Store(&broken)
				damaged, _ := measure(probe.query)
				require.Empty(t, damaged)
				require.NotEqual(t, cold, damaged, "parity oracle must detect dropped candidates")
				negativeControl = true
			}
		}
		t.Logf("catalog=%s case=%s query=%q kind=%s results=%d warm_ns=%v cold_ns=%d",
			fixture.System, probe.label, probe.query, kind, len(warm), durationsNS(samples), coldDuration)
	}
	require.GreaterOrEqual(t, fuzzyQueries, 6, "must exercise genuine fuzzy queries, not only exact aliases")
	require.True(t, negativeControl)
	// Exercise interruption during an observed expensive fallback, then verify
	// that the connection is usable and no partial response escaped.
	db.slugSearchCache.Store(nil)
	budget := min(100*time.Millisecond, slowestCold/4)
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	start = time.Now()
	partial, cancelErr := db.TitleCandidates(ctx, fixture.System, slowestQuery, 5)
	elapsed := time.Since(start)
	require.ErrorIs(t, cancelErr, context.DeadlineExceeded)
	require.Nil(t, partial)
	require.Less(t, elapsed, budget+time.Second, "canceled lookup must release its connection promptly")
	db.slugSearchCache.Store(cache)
	recovered, recoveryTime := measure(probes[2].query)
	require.NotEmpty(t, recovered)
	require.Equal(t, "exact", recovered[0].MatchType)
	t.Logf("catalog=%s deadline_ns=%d canceled_ns=%d recovery_ns=%d", fixture.System, budget, elapsed, recoveryTime)
}

// TestTitleCandidatesCatalogRecovery exercises real-sized rebuilding after the
// public cleanup operation, including foreground exact reads and result parity.
// All mutations belong to this disposable, name-only fixture database.
func TestTitleCandidatesCatalogRecovery(t *testing.T) {
	fixture := loadCandidateCatalog(t)
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	prepareCandidateCatalog(t, db, fixture)
	_, err := db.sql.Load().ExecContext(t.Context(),
		"UPDATE Media SET IsMissing = 1 WHERE DBID = (SELECT MAX(DBID) FROM Media)")
	require.NoError(t, err)
	require.NoError(t, db.RebuildSlugSearchCache())
	before := db.slugSearchCache.Load()
	probes := candidateCatalogProbes(fixture)
	want := make(map[string][]database.TitleCandidate, len(probes))
	for _, probe := range probes {
		want[probe.query], err = db.TitleCandidates(t.Context(), fixture.System, probe.query, 5)
		require.NoError(t, err)
	}

	started := time.Now()
	deleted, err := db.CleanMediaOrphans(t.Context())
	cleanupDuration := time.Since(started)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	for sample := range 3 {
		readStarted := time.Now()
		got, readErr := db.TitleCandidates(t.Context(), fixture.System, probes[2].query, 5)
		elapsed := time.Since(readStarted)
		require.NoError(t, readErr)
		require.Equal(t, want[probes[2].query], got)
		require.NotEmpty(t, got)
		require.Equal(t, "exact", got[0].MatchType)
		t.Logf("catalog=%s foreground_exact_sample=%d elapsed_ns=%d", fixture.System, sample, elapsed)
	}
	db.WaitForBackgroundOperations()
	recoveryDuration := time.Since(started)
	after := db.slugSearchCache.Load()
	require.NotNil(t, after, "cleanup must restore cache without an explicit rebuild or request-triggered warming")
	require.NotSame(t, before, after)
	require.True(t, after.complete)
	require.Equal(t, before.entryCount-1, after.entryCount)
	require.False(t, db.HasBackgroundOperations())
	for _, probe := range probes {
		readStarted := time.Now()
		got, readErr := db.TitleCandidates(t.Context(), fixture.System, probe.query, 5)
		elapsed := time.Since(readStarted)
		require.NoError(t, readErr)
		require.Equal(t, want[probe.query], got, "recovery changed eligible results for %q", probe.query)
		t.Logf("catalog=%s recovered_case=%s elapsed_ns=%d", fixture.System, probe.label, elapsed)
	}
	t.Logf("catalog=%s cleanup_ns=%d cleanup_and_recovery_ns=%d before_cache_bytes=%d after_cache_bytes=%d",
		fixture.System, cleanupDuration, recoveryDuration, before.Size(), after.Size())
}

func durationsNS(samples []time.Duration) []int64 {
	result := make([]int64, len(samples))
	for i, sample := range samples {
		result[i] = int64(sample)
	}
	return result
}

func TestCandidateCatalogTypoPreservesQueryShape(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"The Last Ninja", "Pokémon World", "Final Fantasy VII", "Crystal Space Quest"} {
		query := candidateCatalogTypo(name)
		require.NotEmpty(t, query)
		require.True(t, database.ValidTitleCandidateName(query))
		original := GenerateSlugWithMetadata(slugs.MediaTypeGame, name)
		changed := GenerateSlugWithMetadata(slugs.MediaTypeGame, query)
		require.NotEqual(t, original.Slug, changed.Slug)
		require.Len(t, changed.Slug, len(original.Slug))
		require.Equal(t, original.SlugWordCount, changed.SlugWordCount)
	}
	require.Empty(t, candidateCatalogTypo("1111"))
	fixture := candidateCatalogFixture{Names: []string{"Crystal Space Quest", "Metroid", "The Last Ninja"}}
	require.True(t, slices.Equal(candidateCatalogProbes(fixture), candidateCatalogProbes(fixture)))
}
