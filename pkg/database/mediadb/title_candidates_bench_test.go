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
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/matcher"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

// prepareCandidateBenchmark optionally uses an exported synthetic fixture, so
// native ARM query benchmarks do not spend their time budget building indexes.
// ZAPAROO_CANDIDATE_BENCH_EXPORT exports and skips timing; ZAPAROO_CANDIDATE_BENCH_INPUT
// loads the same fixtures on another machine. Neither mode opens a live database.
func prepareCandidateBenchmark(b *testing.B, db *MediaDB, size int) {
	b.Helper()
	name := fmt.Sprintf("titles_%d.db", size)
	if root := os.Getenv("ZAPAROO_CANDIDATE_BENCH_INPUT"); root != "" {
		loadCandidateFixtureDB(b, db, filepath.Join(root, name))
	} else {
		seedCandidateTitles(b, db, "NES", "Mario", "Series: Planet Zebes", "Metroid")
		_, err := db.sql.Load().ExecContext(b.Context(), `WITH RECURSIVE n(x) AS (
			VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x < ?)
			INSERT INTO MediaTitles (SystemDBID, Name, Slug, SecondarySlug, SlugLength, SlugWordCount)
			SELECT 1, printf('Library Adventure %06d', x), printf('libraryadventure%06d', x), '', 22, 3
			FROM n`, size)
		require.NoError(b, err)
		_, err = db.sql.Load().ExecContext(b.Context(),
			`INSERT INTO Media (SystemDBID, MediaTitleDBID, Path)
			SELECT 1, DBID, ? || DBID FROM MediaTitles WHERE DBID > 3`,
			filepath.Join("roms", "NES", "bench-"))
		require.NoError(b, err)
		_, err = db.sql.Load().ExecContext(b.Context(), "ANALYZE")
		require.NoError(b, err)
	}
	var titles, media int
	require.NoError(b, db.sql.Load().QueryRowContext(b.Context(),
		"SELECT (SELECT COUNT(*) FROM MediaTitles), (SELECT COUNT(*) FROM Media)").Scan(&titles, &media))
	require.Equal(b, size+3, titles)
	require.Equal(b, size+3, media)
	if root := os.Getenv("ZAPAROO_CANDIDATE_BENCH_EXPORT"); root != "" {
		//nolint:gosec // Export destination is explicitly supplied by the local benchmark operator.
		require.NoError(b, os.MkdirAll(root, 0o700))
		path, err := filepath.Abs(filepath.Join(root, name))
		require.NoError(b, err)
		_, err = db.sql.Load().ExecContext(b.Context(), "VACUUM INTO ?", path)
		require.NoError(b, err)
		b.Skipf("exported synthetic fixture %s; timing skipped", path)
	}
}

// loadCandidateFixtureDB imports only an explicitly selected synthetic fixture
// into the caller's disposable database; it never opens a live device database.
func loadCandidateFixtureDB(t testing.TB, db *MediaDB, path string) {
	t.Helper()
	require.NoError(t, db.Close())
	_ = database.RemoveSidecars(db.dbPath)
	//nolint:gosec // Explicit operator-selected test fixture input.
	source, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Close()) }()
	target, err := os.OpenFile(db.dbPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	_, copyErr := io.Copy(target, source)
	require.NoError(t, target.Close())
	require.NoError(t, copyErr)
	require.NoError(t, db.Open())
}

// BenchmarkTitleCandidateBlocksBuild isolates added cache-build work from SQL
// loading and the existing trigram index construction.
func BenchmarkTitleCandidateBlocksBuild(b *testing.B) {
	for _, size := range []int{50000, 500000} {
		b.Run(fmt.Sprintf("titles_%d", size), func(b *testing.B) {
			cache, _ := buildBenchSweepCache(1, size)
			b.ReportAllocs()
			for b.Loop() {
				cache.buildCandidateBlocks()
			}
			b.ReportMetric(float64(cache.candidateBlocksSize()), "bounds-B")
		})
	}
}

// BenchmarkTitleCandidatesTraversal compares visit order with identical bounds
// and fixtures. SQL eligibility stays enabled; no result memoization is involved.
func BenchmarkTitleCandidatesTraversal(b *testing.B) {
	db, cleanup := setupBrowseBenchMediaDB(b)
	defer cleanup()
	prepareCandidateBenchmark(b, db, 500000)
	require.NoError(b, db.RebuildSlugSearchCache())
	source, err := db.readConn()
	require.NoError(b, err)
	conn, err := source.Conn(b.Context())
	require.NoError(b, err)
	defer func() { require.NoError(b, conn.Close()) }()
	system, err := systemdefs.GetSystem("NES")
	require.NoError(b, err)
	for _, query := range []string{"Library Adventuer 001234", "Library Adventuer 412340"} {
		b.Run(query, func(b *testing.B) {
			metadata := GenerateSlugWithMetadata(slugs.MediaTypeGame, query)
			signature := matcher.GenerateTokenSignature(slugs.MediaTypeGame, query)
			var expected []rankedTitle
			for _, prioritize := range []bool{false, true} {
				b.Run(fmt.Sprintf("prioritize_%t", prioritize), func(b *testing.B) {
					b.ReportAllocs()
					var top []rankedTitle
					for b.Loop() {
						r := titleRanker{
							system: *system, query: metadata, name: query,
							signature: signature, allowFuzzy: true, limit: 5,
						}
						scanErr := r.scanCachedFuzzy(b.Context(), conn, db.slugSearchCache.Load(), 1, prioritize)
						if scanErr != nil {
							b.Fatal(scanErr)
						}
						top = r.top
					}
					require.Len(b, top, 5)
					if !prioritize {
						expected = top
					} else if expected != nil {
						require.Equal(b, expected, top)
					}
				})
			}
		})
	}
}

// Cold means the shared slug cache is absent, not an empty SQLite page cache.
// Cancelable contexts exercise SQLite's API-request cancellation path.
// The largest-system tier deliberately concentrates the whole 500k-title
// memory-budget workload in one system; no-match passes its SQL prefilter.
func BenchmarkTitleCandidates(b *testing.B) {
	for _, size := range []int{1000, 50000, 500000} {
		b.Run(fmt.Sprintf("titles_%d", size), func(b *testing.B) {
			db, cleanup := setupBrowseBenchMediaDB(b)
			defer cleanup()
			prepareCandidateBenchmark(b, db, size)
			for _, warm := range []bool{false, true} {
				b.Run(fmt.Sprintf("warm_%t", warm), func(b *testing.B) {
					if warm {
						require.NoError(b, db.RebuildSlugSearchCache())
					} else {
						db.slugSearchCache.Store(nil)
					}
					for _, tc := range []struct{ name, query string }{
						{"exact", "Mario"},
						{"secondary", "Planet Zebes"},
						{"typo", "Metriod"},
						{"no-match", "zzzzzzz qqqqqqqqq vvvvvv"},
						{"largest-system", "Library Adventuer 001234"},
					} {
						b.Run(tc.name, func(b *testing.B) {
							b.ReportAllocs()
							for b.Loop() {
								results, lookupErr := db.TitleCandidates(b.Context(), "NES", tc.query, 5)
								if lookupErr != nil {
									b.Fatal(lookupErr)
								}
								if (len(results) == 0) != (tc.name == "no-match") || len(results) > 5 {
									b.Fatalf("unexpected candidate count for %s: %d", tc.name, len(results))
								}
							}
							if cache := db.slugSearchCache.Load(); cache != nil {
								b.ReportMetric(float64(cache.Size()), "cache-B")
								b.ReportMetric(float64(cache.candidateBlocksSize()), "bounds-B")
							}
						})
					}
					b.Run("concurrent", func(b *testing.B) {
						b.ReportAllocs()
						b.RunParallel(func(pb *testing.PB) {
							for pb.Next() {
								_, lookupErr := db.TitleCandidates(b.Context(), "NES", "Metriod", 5)
								if lookupErr != nil {
									b.Error(lookupErr)
									return
								}
							}
						})
					})
				})
			}
		})
	}
}
