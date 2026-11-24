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
	"errors"
	"math/rand/v2"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/go-slugs"
	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	artBoxart     = "property:image-boxart"
	artScreenshot = "property:image-screenshot"
	artTitleshot  = "property:image-titleshot"
)

type artworkFixture struct {
	db      *MediaDB
	systems map[string]database.System
	titles  map[string]int64
	media   map[string]int64
}

type artworkRow struct {
	system string
	title  string
	path   string
}

// newArtworkFixture inserts rows in order, so DBIDs follow the slice. Rows
// sharing system and title share one MediaTitle.
func newArtworkFixture(t *testing.T, rows ...artworkRow) *artworkFixture {
	t.Helper()
	mediaDB, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	f := &artworkFixture{
		db:      mediaDB,
		systems: make(map[string]database.System),
		titles:  make(map[string]int64),
		media:   make(map[string]int64),
	}
	require.NoError(t, mediaDB.SeedCanonicalTagDefinitions(context.Background()))
	require.NoError(t, mediaDB.BeginTransaction(false))
	for _, row := range rows {
		system, ok := f.systems[row.system]
		if !ok {
			var err error
			system, err = mediaDB.FindOrInsertSystem(database.System{SystemID: row.system, Name: row.system})
			require.NoError(t, err)
			f.systems[row.system] = system
		}
		titleKey := row.system + "/" + row.title
		titleDBID, ok := f.titles[titleKey]
		if !ok {
			def, err := systemdefs.GetSystem(row.system)
			require.NoError(t, err)
			title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
				SystemDBID: system.DBID,
				Slug:       slugs.Slugify(def.GetMediaType(), row.title),
				Name:       row.title,
			})
			require.NoError(t, err)
			titleDBID = title.DBID
			f.titles[titleKey] = titleDBID
		}
		media, err := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: titleDBID,
			Path:           row.path,
			ParentDir:      filepath.ToSlash(filepath.Dir(row.path)) + "/",
			SortName:       row.title,
		})
		require.NoError(t, err)
		f.media[row.path] = media.DBID
	}
	require.NoError(t, mediaDB.CommitTransaction())
	return f
}

func (f *artworkFixture) mediaArt(t *testing.T, path string, typeTags ...string) {
	t.Helper()
	props := make([]database.MediaProperty, 0, len(typeTags))
	for _, typeTag := range typeTags {
		props = append(props, database.MediaProperty{TypeTag: typeTag, Text: path + "." + typeTag + ".png"})
	}
	require.NoError(t, f.db.UpsertMediaProperties(context.Background(), f.media[path], props))
}

func (f *artworkFixture) titleArt(t *testing.T, system, title string, typeTags ...string) {
	t.Helper()
	props := make([]database.MediaProperty, 0, len(typeTags))
	for _, typeTag := range typeTags {
		props = append(props, database.MediaProperty{TypeTag: typeTag, Text: title + "." + typeTag + ".png"})
	}
	require.NoError(t, f.db.UpsertMediaTitleProperties(
		context.Background(), f.titles[system+"/"+title], props))
}

func (f *artworkFixture) tagMedia(t *testing.T, path string, refs ...database.MediaTagRef) {
	t.Helper()
	require.NoError(t, f.db.UpdateMediaTags(context.Background(), f.media[path], nil, refs))
}

func seededRandomInt(seed uint64) func(int) (int, error) {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // deterministic test source
	return func(n int) (int, error) { return rng.IntN(n), nil }
}

func (f *artworkFixture) sample(
	t *testing.T, seed uint64, query *database.ArtworkSampleQuery,
) []database.ArtworkSample {
	t.Helper()
	samples, err := f.db.sampleArtworkMediaUsing(context.Background(), query, seededRandomInt(seed))
	require.NoError(t, err)
	require.NotNil(t, samples)
	return samples
}

func samplePaths(samples []database.ArtworkSample) []string {
	paths := make([]string, 0, len(samples))
	for i := range samples {
		paths = append(paths, samples[i].Path)
	}
	sort.Strings(paths)
	return paths
}

func nesPath(name string) string { return browseTestPath("roms", "NES", name) }

func TestSampleArtworkMedia_TypeScopes(t *testing.T) {
	t.Parallel()
	shared := nesPath("Shared (Rev 1).nes")
	sharedB := nesPath("Shared (Rev 2).nes")
	mediaOnly := nesPath("MediaOnly.nes")
	both := nesPath("Both.nes")
	bare := nesPath("Bare.nes")
	f := newArtworkFixture(t,
		artworkRow{"NES", "Shared", shared},
		artworkRow{"NES", "Shared", sharedB},
		artworkRow{"NES", "MediaOnly", mediaOnly},
		artworkRow{"NES", "Both", both},
		artworkRow{"NES", "Bare", bare},
	)
	f.titleArt(t, "NES", "Shared", artScreenshot)
	f.mediaArt(t, mediaOnly, artBoxart)
	f.mediaArt(t, both, artBoxart)
	f.titleArt(t, "NES", "Both", artScreenshot, artTitleshot)

	t.Run("title scope reaches every media of the title", func(t *testing.T) {
		t.Parallel()
		got := f.sample(t, 1, &database.ArtworkSampleQuery{ImageTypes: []string{artScreenshot}, Limit: 10})
		assert.Equal(t, []string{both, shared, sharedB}, samplePaths(got))
	})

	t.Run("media scope", func(t *testing.T) {
		t.Parallel()
		got := f.sample(t, 2, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 10})
		assert.Equal(t, []string{both, mediaOnly}, samplePaths(got))
	})

	t.Run("multiple types dedupe and keep request order", func(t *testing.T) {
		t.Parallel()
		got := f.sample(t, 3, &database.ArtworkSampleQuery{
			ImageTypes: []string{artScreenshot, artBoxart, artScreenshot},
			Limit:      10,
		})
		require.Len(t, got, 4)
		byPath := make(map[string]database.ArtworkSample, len(got))
		for _, s := range got {
			byPath[s.Path] = s
		}
		assert.Equal(t, []string{artScreenshot, artBoxart}, byPath[both].ImageTypes)
		assert.Equal(t, []string{artBoxart}, byPath[mediaOnly].ImageTypes)
		assert.Equal(t, []string{artScreenshot}, byPath[shared].ImageTypes)
		assert.Equal(t, "NES", byPath[shared].SystemID)
		assert.Equal(t, "Shared", byPath[shared].Name)
		assert.Equal(t, f.media[shared], byPath[shared].MediaID)
		assert.NotContains(t, byPath, bare)
	})

	t.Run("limit", func(t *testing.T) {
		t.Parallel()
		got := f.sample(t, 4, &database.ArtworkSampleQuery{
			ImageTypes: []string{artBoxart, artScreenshot}, Limit: 2,
		})
		assert.Len(t, got, 2)
	})

	t.Run("exclusions", func(t *testing.T) {
		t.Parallel()
		got := f.sample(t, 5, &database.ArtworkSampleQuery{
			ImageTypes:   []string{artBoxart, artScreenshot},
			ExcludeDBIDs: []int64{f.media[both], f.media[shared]},
			Limit:        10,
		})
		assert.Equal(t, []string{mediaOnly, sharedB}, samplePaths(got))
	})

	t.Run("type with no rows", func(t *testing.T) {
		t.Parallel()
		got := f.sample(t, 6, &database.ArtworkSampleQuery{
			ImageTypes: []string{"property:image-marquee", "property:image-nonexistent"}, Limit: 10,
		})
		assert.Empty(t, got)
	})

	t.Run("malformed type is an error", func(t *testing.T) {
		t.Parallel()
		_, err := f.db.SampleArtworkMedia(context.Background(), &database.ArtworkSampleQuery{
			ImageTypes: []string{"image-boxart"}, Limit: 10,
		})
		require.Error(t, err)
	})
}

func TestSampleArtworkMedia_EmptyResults(t *testing.T) {
	t.Parallel()

	t.Run("empty library", func(t *testing.T) {
		t.Parallel()
		f := newArtworkFixture(t)
		got := f.sample(t, 1, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 5})
		assert.Empty(t, got)
	})

	t.Run("library without artwork", func(t *testing.T) {
		t.Parallel()
		f := newArtworkFixture(t,
			artworkRow{"NES", "A", nesPath("A.nes")},
			artworkRow{"NES", "B", nesPath("B.nes")},
		)
		got := f.sample(t, 1, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 5})
		assert.Empty(t, got)
	})

	t.Run("missing media is skipped", func(t *testing.T) {
		t.Parallel()
		gone := nesPath("Gone.nes")
		f := newArtworkFixture(t, artworkRow{"NES", "Gone", gone})
		f.mediaArt(t, gone, artBoxart)
		_, err := f.db.sql.Load().ExecContext(context.Background(),
			"UPDATE Media SET IsMissing = 1 WHERE DBID = ?", f.media[gone])
		require.NoError(t, err)
		got := f.sample(t, 1, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 5})
		assert.Empty(t, got)
	})

	t.Run("zero limit and no types", func(t *testing.T) {
		t.Parallel()
		f := newArtworkFixture(t, artworkRow{"NES", "A", nesPath("A.nes")})
		f.mediaArt(t, nesPath("A.nes"), artBoxart)
		assert.Empty(t, f.sample(t, 1, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}}))
		assert.Empty(t, f.sample(t, 1, &database.ArtworkSampleQuery{Limit: 5}))
		assert.Empty(t, f.sample(t, 1, nil))
	})
}

func TestSampleArtworkMedia_Filters(t *testing.T) {
	t.Parallel()
	visible := nesPath("Visible.nes")
	hidden := nesPath("Hidden.nes")
	inHiddenDir := browseTestPath("roms", "NES", "Secret", "InDir.nes")
	liked := nesPath("Liked.nes")
	likedFav := nesPath("LikedFav.nes")
	snes := browseTestPath("roms", "SNES", "Super.sfc")
	f := newArtworkFixture(t,
		artworkRow{"NES", "Visible", visible},
		artworkRow{"NES", "Hidden", hidden},
		artworkRow{"NES", "InDir", inHiddenDir},
		artworkRow{"NES", "Liked", liked},
		artworkRow{"NES", "LikedFav", likedFav},
		artworkRow{"SNES", "Super", snes},
	)
	for _, path := range []string{visible, hidden, inHiddenDir, liked, likedFav, snes} {
		f.mediaArt(t, path, artBoxart)
	}
	hideMediaPaths(t, f.db, hidden)
	hideDirs(t, f.db, browseTestDir("roms", "NES", "Secret"))
	f.tagMedia(t, liked, database.MediaTagRef{Type: "user", Tag: "liked"})
	f.tagMedia(t, likedFav,
		database.MediaTagRef{Type: "user", Tag: "liked"},
		database.MediaTagRef{Type: "user", Tag: "favorite"})

	query := func(mod func(q *database.ArtworkSampleQuery)) *database.ArtworkSampleQuery {
		q := &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 50, ExcludeHidden: true}
		mod(q)
		return q
	}

	tests := []struct {
		query *database.ArtworkSampleQuery
		name  string
		want  []string
	}{
		{
			name:  "hidden media and hidden folders excluded",
			query: query(func(*database.ArtworkSampleQuery) {}),
			want:  []string{liked, likedFav, visible, snes},
		},
		{
			name:  "include hidden",
			query: query(func(q *database.ArtworkSampleQuery) { q.ExcludeHidden = false }),
			want:  []string{hidden, liked, likedFav, inHiddenDir, visible, snes},
		},
		{
			name: "systems",
			query: query(func(q *database.ArtworkSampleQuery) {
				q.Systems = []string{"SNES"}
			}),
			want: []string{snes},
		},
		{
			name: "path prefix",
			query: query(func(q *database.ArtworkSampleQuery) {
				q.ExcludeHidden = false
				q.PathPrefix = browseTestDir("roms", "NES", "Secret")
			}),
			want: []string{inHiddenDir},
		},
		{
			// Same rule as search: scoping into a hidden folder shows it.
			name: "path prefix inside a hidden folder",
			query: query(func(q *database.ArtworkSampleQuery) {
				q.PathPrefix = browseTestDir("roms", "NES", "Secret")
			}),
			want: []string{inHiddenDir},
		},
		{
			name: "tag AND",
			query: query(func(q *database.ArtworkSampleQuery) {
				q.Tags = []zapscript.TagFilter{{Type: "user", Value: "liked", Operator: zapscript.TagOperatorAND}}
			}),
			want: []string{liked, likedFav},
		},
		{
			name: "tag NOT",
			query: query(func(q *database.ArtworkSampleQuery) {
				q.Tags = []zapscript.TagFilter{{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT}}
			}),
			want: []string{liked, visible, snes},
		},
		{
			name: "tag OR",
			query: query(func(q *database.ArtworkSampleQuery) {
				q.Tags = []zapscript.TagFilter{
					{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorOR},
					{Type: "user", Value: "hidden", Operator: zapscript.TagOperatorOR},
				}
				q.ExcludeHidden = false
			}),
			want: []string{hidden, likedFav},
		},
		{
			name: "required hidden tag",
			query: query(func(q *database.ArtworkSampleQuery) {
				q.Tags = []zapscript.TagFilter{{Type: "user", Value: "hidden", Operator: zapscript.TagOperatorAND}}
				q.ExcludeHidden = false
			}),
			want: []string{hidden},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			assert.Equal(t, want, samplePaths(f.sample(t, 7, tt.query)))
		})
	}
}

// fairnessRun draws runs single-row samples and checks that every eligible
// row comes up and none comes up far more often than its fair share.
func fairnessRun(t *testing.T, f *artworkFixture, eligible []string, runs int) {
	t.Helper()
	counts := make(map[string]int, len(eligible))
	for i := range runs {
		got := f.sample(t, uint64(i)+1, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 1})
		require.Len(t, got, 1)
		counts[got[0].Path]++
	}
	fair := runs / len(eligible)
	for _, path := range eligible {
		assert.Positive(t, counts[path], "row %s never sampled", path)
		assert.Less(t, counts[path], 3*fair, "row %s sampled %d times, fair share %d", path, counts[path], fair)
	}
	assert.Len(t, counts, len(eligible), "sampled a row that is not eligible: %v", counts)
}

func TestSampleArtworkMedia_DenseDistribution(t *testing.T) {
	t.Parallel()
	rows := make([]artworkRow, 0, 40)
	var eligible []string
	for i := range 40 {
		name := string(rune('A'+i%26)) + string(rune('a'+i/26))
		path := nesPath(name + ".nes")
		rows = append(rows, artworkRow{"NES", name, path})
		if i%4 == 0 {
			eligible = append(eligible, path)
		}
	}
	f := newArtworkFixture(t, rows...)
	for _, path := range eligible {
		f.mediaArt(t, path, artBoxart)
	}
	fairnessRun(t, f, eligible, 1000)
}

// moveMediaDBID renumbers a media row so the DBID range becomes sparse. It
// must run before properties or tags reference the row.
func (f *artworkFixture) moveMediaDBID(t *testing.T, path string, dbid int64) {
	t.Helper()
	_, err := f.db.sql.Load().ExecContext(context.Background(),
		"UPDATE Media SET DBID = ? WHERE DBID = ?", dbid, f.media[path])
	require.NoError(t, err)
	f.media[path] = dbid
}

// A sparse DBID range makes the random draws miss, so the result has to come
// from the enumeration fallback.
func newSparseArtworkFixture(t *testing.T) (f *artworkFixture, eligible []string) {
	t.Helper()
	rows := make([]artworkRow, 0, 6)
	for i := range 6 {
		name := string(rune('A' + i))
		rows = append(rows, artworkRow{"NES", name, nesPath(name + ".nes")})
	}
	f = newArtworkFixture(t, rows...)
	f.moveMediaDBID(t, rows[5].path, 50_000_000)
	for i, row := range rows {
		if i%2 == 1 {
			f.mediaArt(t, row.path, artBoxart)
			eligible = append(eligible, row.path)
		}
	}
	return f, eligible
}

func TestSampleArtworkMedia_SparseFallback(t *testing.T) {
	t.Parallel()
	f, eligible := newSparseArtworkFixture(t)
	sort.Strings(eligible)

	got := f.sample(t, 11, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 10})
	assert.Equal(t, eligible, samplePaths(got))

	got = f.sample(t, 12, &database.ArtworkSampleQuery{
		ImageTypes:   []string{artBoxart},
		ExcludeDBIDs: []int64{f.media[eligible[0]]},
		Limit:        10,
	})
	assert.Equal(t, eligible[1:], samplePaths(got))

	fairnessRun(t, f, eligible, 600)
}

func TestSampleArtworkMedia_FallbackSkippedWithoutIndexes(t *testing.T) {
	t.Parallel()
	f, eligible := newSparseArtworkFixture(t)
	query := &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 10}

	require.NoError(t, f.db.DropSecondaryIndexes())
	ready, err := artworkFallbackIndexesPresent(context.Background(), f.db.sql.Load())
	require.NoError(t, err)
	require.False(t, ready)
	// Only probe hits remain; with eight batches over a 50M range the seeded
	// draws find none, and the result must not come from a full scan.
	assert.Empty(t, f.sample(t, 11, query))

	require.NoError(t, f.db.CreateSecondaryIndexes())
	sort.Strings(eligible)
	assert.Equal(t, eligible, samplePaths(f.sample(t, 11, query)))
}

func TestSampleArtworkMedia_RandomSourceError(t *testing.T) {
	t.Parallel()
	f := newArtworkFixture(t, artworkRow{"NES", "A", nesPath("A.nes")})
	f.mediaArt(t, nesPath("A.nes"), artBoxart)
	boom := errors.New("entropy unavailable")
	_, err := f.db.sampleArtworkMediaUsing(context.Background(),
		&database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 1},
		func(int) (int, error) { return 0, boom })
	require.ErrorIs(t, err, boom)
}

func TestSampleArtworkMedia_Cancelled(t *testing.T) {
	t.Parallel()
	f, _ := newSparseArtworkFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := seededRandomInt(1)
	_, err := f.db.sampleArtworkMediaUsing(ctx,
		&database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 10},
		func(n int) (int, error) {
			cancel()
			return random(n)
		})
	require.ErrorIs(t, err, context.Canceled)
}

// A rebuild while a sample is running must not panic or mix databases: the
// request either fails or answers entirely from the database it started on.
func TestSampleArtworkMedia_RecreateMidRequest(t *testing.T) {
	t.Parallel()
	f, eligible := newSparseArtworkFixture(t)
	random := seededRandomInt(3)
	recreated := false
	var recreateErr error
	got, err := f.db.sampleArtworkMediaUsing(context.Background(),
		&database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 10},
		func(n int) (int, error) {
			if !recreated {
				recreated = true
				recreateErr = f.db.Recreate(false)
			}
			return random(n)
		})
	require.True(t, recreated)
	if recreateErr != nil {
		t.Logf("recreate failed with the connection pinned: %v", recreateErr)
	}
	if err == nil {
		for _, s := range got {
			assert.Contains(t, eligible, s.Path)
		}
	}
	if recreateErr == nil {
		after := f.sample(t, 4, &database.ArtworkSampleQuery{ImageTypes: []string{artBoxart}, Limit: 10})
		assert.Empty(t, after)
	}
}

// scaleMediaStats multiplies the seeded statistics, modelled on a 99k-row
// library, by ten so the planner sees a million-row library, and adds
// statistics for the property tables, which the seed does not have.
func scaleMediaStats(t *testing.T, db sqlQueryable) {
	t.Helper()
	ctx := context.Background()
	type statRow struct {
		tbl  string
		stat string
		idx  sql.NullString
	}
	readStats := func() []statRow {
		rows, err := db.QueryContext(ctx, "SELECT tbl, idx, stat FROM sqlite_stat1")
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var stats []statRow
		for rows.Next() {
			var row statRow
			require.NoError(t, rows.Scan(&row.tbl, &row.idx, &row.stat))
			stats = append(stats, row)
		}
		require.NoError(t, rows.Err())
		return stats
	}
	stats := readStats()
	require.NotEmpty(t, stats)

	_, err := db.ExecContext(ctx, "DELETE FROM sqlite_stat1")
	require.NoError(t, err)
	insert := func(tbl string, idx any, stat string) {
		_, insertErr := db.ExecContext(ctx,
			"INSERT INTO sqlite_stat1 (tbl, idx, stat) VALUES (?, ?, ?)", tbl, idx, stat)
		require.NoError(t, insertErr)
	}
	for _, row := range stats {
		fields := strings.Fields(row.stat)
		for i, field := range fields {
			n, convErr := strconv.Atoi(field)
			if convErr == nil && n > 1 {
				fields[i] = strconv.Itoa(n * 10)
			}
		}
		var idx any
		if row.idx.Valid {
			idx = row.idx.String
		}
		insert(row.tbl, idx, strings.Join(fields, " "))
	}
	insert("MediaProperties", "sqlite_autoindex_MediaProperties_1", "2000000 2 1")
	insert("MediaProperties", "mediaproperties_typetag_idx", "2000000 200000")
	insert("MediaTitleProperties", "sqlite_autoindex_MediaTitleProperties_1", "1500000 2 1")
	insert("MediaTitleProperties", "mediatitleproperties_typetag_idx", "1500000 150000")
	_, err = db.ExecContext(ctx, "ANALYZE sqlite_schema")
	require.NoError(t, err)
}

var artworkFullScan = regexp.MustCompile(
	`(?i)\bSCAN (Media|MediaTitles|MediaProperties|MediaTitleProperties|ap|atp|ep|etp|em)\b`)

// Both statements must drive from their ID source on a fresh database, where
// the property tables have no statistics, and on a large analyzed one. A plan
// that drives from a system or TypeTag index instead visits every row in scope.
func TestSampleArtworkMedia_QueryPlans(t *testing.T) {
	t.Parallel()
	for _, scaled := range []bool{false, true} {
		name := "fresh"
		if scaled {
			name = "million rows"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := newArtworkFixture(t, artworkRow{"NES", "A", nesPath("A.nes")})
			require.NoError(t, f.db.CreateSecondaryIndexes())
			hideMediaPaths(t, f.db, nesPath("A.nes"))
			hideDirs(t, f.db, browseTestDir("roms", "NES", "Secret"))
			db := f.db.sql.Load()
			if scaled {
				scaleMediaStats(t, db)
			}

			rangePlan := randomQueryPlan(t, db, artworkDBIDRangeSQL)
			assert.NotRegexp(t, artworkFullScan, rangePlan)

			typeTags, err := resolveArtworkTypeTags(ctx, db, []string{artBoxart, artScreenshot})
			require.NoError(t, err)
			require.Len(t, typeTags, 2)
			typeDBIDs := []any{typeTags[0].dbid, typeTags[1].dbid}

			query := &database.ArtworkSampleQuery{
				ImageTypes:   []string{artBoxart, artScreenshot},
				ExcludeDBIDs: []int64{7, 8},
				Limit:        10,
				MediaQuery: database.MediaQuery{
					Systems: []string{"NES", "SNES"},
					Tags: []zapscript.TagFilter{
						{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT},
					},
				},
			}
			query.Tags, err = discoveryTags(ctx, db, query.Tags, true, recursiveScope(""))
			require.NoError(t, err)
			require.Len(t, query.Tags, 3, "expected the hidden tag and hidden folder filters")

			whereClause, whereArgs := buildMediaCandidateQueryWhereClause(&query.MediaQuery)
			probeSQL, probeArgs := artworkProbeQuery(
				&query.MediaQuery, whereClause, whereArgs, typeDBIDs, []int64{1, 2, 3, 4})
			probePlan := randomQueryPlan(t, db, probeSQL, probeArgs...)
			assertRandomPlanContains(t, probePlan, "SEARCH Media USING INTEGER PRIMARY KEY")
			assertRandomPlanContains(t, probePlan, "sqlite_autoindex_MediaProperties_1")
			assertRandomPlanContains(t, probePlan, "sqlite_autoindex_MediaTitleProperties_1")
			assert.NotRegexp(t, artworkFullScan, probePlan)

			enumSQL, enumArgs := artworkEnumerateQuery(query, typeDBIDs)
			enumPlan := randomQueryPlan(t, db, enumSQL, enumArgs...)
			assertRandomPlanContains(t, enumPlan, "mediaproperties_typetag_idx")
			assertRandomPlanContains(t, enumPlan, "mediatitleproperties_typetag_idx")
			assert.Regexp(t,
				`SEARCH em USING (COVERING )?INDEX (media_title_present_idx|media_mediatitle_idx)`, enumPlan)
			assertRandomPlanContains(t, enumPlan, "SEARCH Media USING INTEGER PRIMARY KEY")
			assert.NotRegexp(t, artworkFullScan, enumPlan)
		})
	}
}

func BenchmarkSampleArtworkMedia_50k(b *testing.B) {
	benchmarkSampleArtworkMedia(b, 50_000)
}

func BenchmarkSampleArtworkMedia_500k(b *testing.B) {
	benchmarkSampleArtworkMedia(b, 500_000)
}

// benchmarkSampleArtworkMedia covers the probe path (half the library has box
// art) and the fallback path (one title in 5000 has a screenshot).
func benchmarkSampleArtworkMedia(b *testing.B, rows int) {
	ctx := context.Background()
	mediaDB, cleanup := setupBrowseBenchMediaDB(b)
	defer cleanup()
	seedRandomGameBenchmark(b, mediaDB, rows)

	db := mediaDB.sql.Load()
	_, err := db.ExecContext(ctx, `
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES (3, 'property', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
			(3, 3, 'image-boxart'),
			(4, 3, 'image-screenshot');
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text)
			SELECT DBID, 3, 'boxart.png' FROM Media WHERE DBID % 2 = 0;
		INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text)
			SELECT DBID, 4, 'screenshot.png' FROM MediaTitles WHERE DBID % 5000 = 0;
	`)
	require.NoError(b, err)

	queries := []struct {
		name  string
		query database.ArtworkSampleQuery
	}{
		{name: "dense-probe", query: database.ArtworkSampleQuery{
			ImageTypes: []string{artBoxart}, Limit: 10, ExcludeHidden: true,
		}},
		{name: "dense-probe-systems", query: database.ArtworkSampleQuery{
			ImageTypes: []string{artBoxart, artScreenshot}, Limit: 50, ExcludeHidden: true,
			MediaQuery: database.MediaQuery{Systems: []string{"NES", "SNES"}},
		}},
		{name: "sparse-fallback", query: database.ArtworkSampleQuery{
			ImageTypes: []string{artScreenshot}, Limit: 10, ExcludeHidden: true,
		}},
	}
	for i := range queries {
		testCase := &queries[i]
		b.Run(testCase.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				got, sampleErr := mediaDB.SampleArtworkMedia(ctx, &testCase.query)
				if sampleErr != nil {
					b.Fatal(sampleErr)
				}
				if len(got) == 0 {
					b.Fatal("no samples")
				}
			}
		})
	}
}
