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
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/recommend"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/require"
)

const (
	similarBenchTitles  = 129_000
	similarBenchSystems = 25
	// Tag row ranges of the benchmark library, one per tag type.
	similarBenchGenres     = 40
	similarBenchDevelopers = 3000
	similarBenchSeries     = 800
	similarBenchDevBase    = 1000
	similarBenchSeriesBase = 5000
	similarBenchYearBase   = 6000
	similarBenchPlayerBase = 7000
)

// setupSimilarBenchDB builds a library the size of a full MiSTer index with
// scraped tags spread the way real ones are: a few genres and developers hold
// much of the library and most hold little, every title has a year and a
// player count, and one title in twenty belongs to a series of about eight.
func setupSimilarBenchDB(b *testing.B) (mediaDB *MediaDB, cleanup func()) {
	b.Helper()
	tempDir, err := os.MkdirTemp("", "zaparoo-bench-similar-*")
	require.NoError(b, err)
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: tempDir})
	mediaDB, err = OpenMediaDB(context.Background(), mockPlatform)
	require.NoError(b, err)
	cleanup = func() {
		_ = mediaDB.Close()
		_ = os.RemoveAll(tempDir)
	}

	ctx := context.Background()
	conn := mediaDB.sql.Load()
	seq := func(n int) string {
		return fmt.Sprintf(`WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM seq WHERE i < %d) `, n)
	}
	statements := []string{
		seq(similarBenchSystems) + `INSERT INTO Systems (DBID, SystemID, Name)
			SELECT i, 'Bench' || i, 'Bench ' || i FROM seq`,
		`INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES
			(1, 'genre', 0), (2, 'developer', 0), (3, 'search', 0), (4, 'year', 0), (5, 'players', 0)`,
		seq(similarBenchGenres) + `INSERT INTO Tags (DBID, TypeDBID, Tag)
			SELECT i, 1, 'family' || (i % 8) || ':g' || i FROM seq`,
		seq(similarBenchDevelopers) + fmt.Sprintf(
			`INSERT INTO Tags (DBID, TypeDBID, Tag) SELECT %d + i, 2, 'dev' || i FROM seq`, similarBenchDevBase),
		seq(similarBenchSeries) + fmt.Sprintf(
			`INSERT INTO Tags (DBID, TypeDBID, Tag) SELECT %d + i, 3, 'franchise:s' || i FROM seq`,
			similarBenchSeriesBase),
		seq(40) + fmt.Sprintf(
			`INSERT INTO Tags (DBID, TypeDBID, Tag) SELECT %d + i, 4, '' || (1979 + i) FROM seq`, similarBenchYearBase),
		seq(4) + fmt.Sprintf(
			`INSERT INTO Tags (DBID, TypeDBID, Tag) SELECT %d + i, 5, '' || i FROM seq`, similarBenchPlayerBase),
		seq(similarBenchTitles) + fmt.Sprintf(`INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name)
			SELECT i, ((i-1) %% %d) + 1, 'game-' || i, 'Game ' || i FROM seq`, similarBenchSystems),
		// Squaring a uniform value piles titles onto the low tag rows.
		seq(similarBenchTitles) + fmt.Sprintf(`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID)
			SELECT i, 1 + (((i * 7919) %% %[1]d) * ((i * 7919) %% %[1]d)) / %[1]d FROM seq`, similarBenchGenres),
		seq(similarBenchTitles) + fmt.Sprintf(`INSERT OR IGNORE INTO MediaTitleTags (MediaTitleDBID, TagDBID)
			SELECT i, 1 + (((i * 104729) %% %[1]d) * ((i * 104729) %% %[1]d)) / %[1]d FROM seq WHERE i %% 3 = 0`,
			similarBenchGenres),
		seq(similarBenchTitles) + fmt.Sprintf(`INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID)
			SELECT i, %[2]d + 1 + (((i * 15485863) %% %[1]d) * ((i * 15485863) %% %[1]d)) / %[1]d FROM seq`,
			similarBenchDevelopers, similarBenchDevBase),
		seq(similarBenchTitles) + fmt.Sprintf(`INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID)
			SELECT i, %d + 1 + ((i / 20) %% %d) FROM seq WHERE i %% 20 = 0`,
			similarBenchSeriesBase, similarBenchSeries),
		seq(similarBenchTitles) + fmt.Sprintf(`INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID)
			SELECT i, %d + 1 + ((i * 31) %% 40) FROM seq`, similarBenchYearBase),
		seq(similarBenchTitles) + fmt.Sprintf(`INSERT INTO MediaTitleTags (MediaTitleDBID, TagDBID)
			SELECT i, %d + 1 + ((i * 17) %% 4) FROM seq`, similarBenchPlayerBase),
		`ANALYZE`,
	}
	for _, statement := range statements {
		_, err = conn.ExecContext(ctx, statement)
		require.NoError(b, err, statement)
	}
	return mediaDB, cleanup
}

// similarBenchWorstTitle is a title of the most common genre and developer,
// in a series: the most index entries one lookup can be asked to read.
func similarBenchWorstTitle(b *testing.B, mediaDB *MediaDB) int64 {
	b.Helper()
	var title int64
	err := mediaDB.sql.Load().QueryRowContext(context.Background(), `
		SELECT a.MediaTitleDBID FROM MediaTitleTags a
		JOIN MediaTitleTags d ON d.MediaTitleDBID = a.MediaTitleDBID AND d.TagDBID = ?
		JOIN MediaTitleTags s ON s.MediaTitleDBID = a.MediaTitleDBID AND s.TagDBID > ? AND s.TagDBID <= ?
		WHERE a.TagDBID = 2 ORDER BY a.MediaTitleDBID LIMIT 1`,
		similarBenchDevBase+2, similarBenchSeriesBase, similarBenchSeriesBase+similarBenchSeries).Scan(&title)
	if err != nil {
		// No title holds all three: fall back to one of the common developer.
		err = mediaDB.sql.Load().QueryRowContext(context.Background(),
			`SELECT MediaTitleDBID FROM MediaTitleTags WHERE TagDBID = ? ORDER BY MediaTitleDBID LIMIT 1`,
			similarBenchDevBase+2).Scan(&title)
	}
	require.NoError(b, err)
	return title
}

// BenchmarkSimilar measures media.similar's work on the index for one title:
// reading its tags, counting how common each is, finding the titles that
// share them and scoring those. It leaves out reading the media rows of the
// results.
func BenchmarkSimilar(b *testing.B) {
	mediaDB, cleanup := setupSimilarBenchDB(b)
	defer cleanup()
	ctx := context.Background()
	cases := map[string]int64{
		"series_title":  20_000,
		"typical_title": 20_001,
		"common_tags":   similarBenchWorstTitle(b, mediaDB),
	}
	for name, title := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				picks, err := recommend.Similar(ctx, mediaDB, title, recommend.SimilarOptions{Limit: 30})
				if err != nil {
					b.Fatal(err)
				}
				if len(picks) == 0 {
					b.Fatal("no picks")
				}
			}
		})
	}
}

// BenchmarkFindTitlesSharingTags measures the candidate read alone at the
// postings budget media.similar allows.
func BenchmarkFindTitlesSharingTags(b *testing.B) {
	mediaDB, cleanup := setupSimilarBenchDB(b)
	defer cleanup()
	ctx := context.Background()
	b.Run("one_series", func(b *testing.B) {
		weighted := []database.WeightedTag{{TagDBID: similarBenchSeriesBase + 1, Weight: 3}}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := mediaDB.FindTitlesSharingTags(ctx, weighted, nil, nil, 150); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("at_postings_budget", func(b *testing.B) {
		// Mid-frequency genres and developers adding up to about 6000 titles.
		weighted := []database.WeightedTag{
			{TagDBID: 30, Weight: 1},
			{TagDBID: 35, Weight: 1},
			{TagDBID: similarBenchDevBase + 2, Weight: 0.4},
			{TagDBID: similarBenchSeriesBase + 1, Weight: 3},
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := mediaDB.FindTitlesSharingTags(ctx, weighted, []int64{1}, nil, 150); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkTitleTokens measures reading a title's tags and counting how many
// titles carry each, the first half of every lookup.
func BenchmarkTitleTokens(b *testing.B) {
	mediaDB, cleanup := setupSimilarBenchDB(b)
	defer cleanup()
	ctx := context.Background()
	types := []string{"genre", "developer", "search", "year", "players"}
	b.ReportAllocs()
	for b.Loop() {
		facets, err := mediaDB.GetTitleFacetTags(ctx, []int64{20_000}, types)
		if err != nil {
			b.Fatal(err)
		}
		ids := make([]int64, len(facets))
		for i := range facets {
			ids[i] = facets[i].TagDBID
		}
		if _, err := mediaDB.CountTitlesByTag(ctx, ids, 2581); err != nil {
			b.Fatal(err)
		}
	}
}

// benchPlays is a profile's history for the plan benchmark: the title each
// play total belongs to is its path.
type benchPlays struct {
	totals []database.MediaPlayTotal
}

func (p benchPlays) GetMediaPlayTotals(
	context.Context, string, time.Time, int, int,
) ([]database.MediaPlayTotal, error) {
	return p.totals, nil
}

func (benchPlays) ListDecks() ([]database.Deck, error) {
	return nil, nil
}

// BenchmarkRecommendPlan measures building media.recommended's plan from ten
// played games, without the plan cache and without reading media rows.
func BenchmarkRecommendPlan(b *testing.B) {
	mediaDB, cleanup := setupSimilarBenchDB(b)
	defer cleanup()
	ctx := context.Background()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	var plays benchPlays
	for i := range 10 {
		title := int64(10_000 + i*7_001)
		if i%2 == 0 {
			title = int64(20_000 + i*20_000)
		}
		plays.totals = append(plays.totals, database.MediaPlayTotal{
			SystemID: "Bench1", MediaPath: strconv.FormatInt(title, 10),
			PlayTime: (i + 1) * 3600, SessionCount: 2, LastPlayedAt: now.Add(-time.Duration(i) * 24 * time.Hour),
		})
	}
	deps := &recommend.Deps{
		MediaDB: mediaDB,
		UserDB:  plays,
		ResolvePlays: func(_ context.Context, totals []database.MediaPlayTotal) (map[int]int64, error) {
			resolved := make(map[int]int64, len(totals))
			for i := range totals {
				title, err := strconv.ParseInt(totals[i].MediaPath, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parse bench title: %w", err)
				}
				resolved[i] = title
			}
			return resolved, nil
		},
	}
	b.ReportAllocs()
	for b.Loop() {
		plan, err := recommend.BuildPlan(ctx, deps, "", now, recommend.Options{})
		if err != nil {
			b.Fatal(err)
		}
		if len(plan.Sections) == 0 {
			b.Fatal("no sections")
		}
	}
}
