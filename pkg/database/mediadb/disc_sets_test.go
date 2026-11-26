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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discSetSeed inserts media rows into the PSX system (DBID 2) of an alias
// test database. Each row names its title slug, so rows sharing a slug share
// a title.
type discSetSeed struct {
	path    string
	title   string
	missing bool
}

func seedDiscSetRows(t *testing.T, mediaDB *MediaDB, rows ...discSetSeed) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := mediaDB.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	titles := make(map[string]int64)
	ids := make(map[string]int64, len(rows))
	for _, row := range rows {
		titleID, ok := titles[row.title]
		if !ok {
			res, insertErr := tx.ExecContext(ctx,
				`INSERT INTO MediaTitles (SystemDBID, Slug, Name) VALUES (2, ?, ?)`, row.title, row.title)
			require.NoError(t, insertErr)
			titleID, err = res.LastInsertId()
			require.NoError(t, err)
			titles[row.title] = titleID
		}
		res, insertErr := tx.ExecContext(ctx,
			`INSERT INTO Media (MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing) VALUES (?, 2, ?, ?, ?)`,
			titleID, row.path, ParentDirForMediaPath(row.path), row.missing)
		require.NoError(t, insertErr)
		id, idErr := res.LastInsertId()
		require.NoError(t, idErr)
		ids[row.path] = id
	}
	require.NoError(t, tx.Commit())
	return ids
}

func discSetPaths(sets map[string][]database.Media) map[string][]string {
	out := make(map[string][]string, len(sets))
	for dir, rows := range sets {
		for i := range rows {
			out[dir] = append(out[dir], rows[i].Path)
		}
	}
	return out
}

func TestFindDiscSets(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	p := func(parts ...string) string {
		return filepath.ToSlash(filepath.Join(append([]string{parent}, parts...)...))
	}
	virtualDir := "source://romm/PSX/Virtual%20Game/"
	seedDiscSetRows(t, mediaDB,
		// Inserted out of path order: the set comes back sorted.
		discSetSeed{path: p("Set", "Game (Disc 2).chd"), title: "game"},
		discSetSeed{path: p("Set", "Game (Disc 1).chd"), title: "game"},
		discSetSeed{path: p("Set", "Game (Disc 3).chd"), title: "game"},
		// A playlist stands in for its discs, so the folder is one file.
		discSetSeed{path: p("Playlist", "Game.m3u"), title: "pl"},
		discSetSeed{path: p("Playlist", "Game (Disc 1).cue"), title: "pl-1"},
		discSetSeed{path: p("Playlist", "Game (Disc 1).bin"), title: "pl-1b"},
		discSetSeed{path: p("Cue", "Game.cue"), title: "cue"},
		discSetSeed{path: p("Cue", "Game.bin"), title: "cue-bin"},
		// Different games in one folder are a collection.
		discSetSeed{path: p("Mixed", "Alpha.chd"), title: "alpha"},
		discSetSeed{path: p("Mixed", "Beta.chd"), title: "beta"},
		// Media below the folder makes it a collection too.
		discSetSeed{path: p("Nested", "Game (Disc 1).chd"), title: "nested"},
		discSetSeed{path: p("Nested", "Game (Disc 2).chd"), title: "nested"},
		discSetSeed{path: p("Nested", "Bonus", "Extra.chd"), title: "extra"},
		// A missing disc is not part of the set.
		discSetSeed{path: p("Missing", "Game (Disc 1).chd"), title: "missing"},
		discSetSeed{path: p("Missing", "Game (Disc 2).chd"), title: "missing", missing: true},
		discSetSeed{path: p("Single", "Game.chd"), title: "single"},
		discSetSeed{path: virtualDir + "Game%20(Disc%201).chd", title: "virtual"},
		discSetSeed{path: virtualDir + "Game%20(Disc%202).chd", title: "virtual"},
	)

	dirs := []string{
		// With and without the trailing slash, and repeated.
		p("Set"), p("Set") + "/", p("Playlist"), p("Cue") + "/", p("Mixed"), p("Nested"),
		p("Missing"), p("Single"), p("Nowhere"), virtualDir, "",
	}
	sets, err := mediaDB.FindDiscSets(ctx, 2, dirs)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		p("Set") + "/": {p("Set", "Game (Disc 1).chd"), p("Set", "Game (Disc 2).chd"), p("Set", "Game (Disc 3).chd")},
		virtualDir:     {virtualDir + "Game%20(Disc%201).chd", virtualDir + "Game%20(Disc%202).chd"},
	}, discSetPaths(sets))
	for _, row := range sets[p("Set")+"/"] {
		assert.NotZero(t, row.DBID)
		assert.NotZero(t, row.MediaTitleDBID)
	}

	// Another system's folders are not looked at.
	other, err := mediaDB.FindDiscSets(ctx, 1, dirs)
	require.NoError(t, err)
	assert.Empty(t, other)

	empty, err := mediaDB.FindDiscSets(ctx, 2, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestFindDiscSetsAcrossQueryChunks(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	count := aliasCandidatesPerQuery*2 + 1
	seeds := make([]discSetSeed, 0, count*2)
	dirs := make([]string, 0, count)
	for i := range count {
		name := fmt.Sprintf("Game%04d", i)
		dirs = append(dirs, aliasTestDir(parent, name))
		for disc := 1; disc <= 2; disc++ {
			seeds = append(seeds, discSetSeed{
				path:  filepath.ToSlash(filepath.Join(parent, name, fmt.Sprintf("%s (Disc %d).chd", name, disc))),
				title: name,
			})
		}
	}
	seedDiscSetRows(t, mediaDB, seeds...)

	sets, err := mediaDB.FindDiscSets(context.Background(), 2, dirs)
	require.NoError(t, err)
	require.Len(t, sets, count)
	for _, dir := range dirs {
		assert.Len(t, sets[dir], 2, dir)
	}
}

func TestFindDiscSetsCancelledContextFails(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mediaDB.FindDiscSets(ctx, 2, []string{aliasTestDir("roms", "PSX", "Set")})
	require.ErrorIs(t, err, context.Canceled)
}

// seedUserTags adds a user tag type and its flags to an alias test database
// and links the given flags to each media row.
func seedUserTags(t *testing.T, mediaDB *MediaDB, links map[int64][]string) {
	t.Helper()
	ctx := context.Background()
	_, err := mediaDB.sql.Load().ExecContext(ctx, `
		INSERT INTO TagTypes (DBID, Type, IsExclusive) VALUES (20, 'user', 0);
		INSERT INTO Tags (DBID, TypeDBID, Tag) VALUES
			(21, 20, 'favorite'), (22, 20, 'liked'), (23, 20, 'disliked'),
			(24, 20, 'hidden'), (25, 20, 'playlater');
	`)
	require.NoError(t, err)
	tagIDs := map[string]int64{"favorite": 21, "liked": 22, "disliked": 23, "hidden": 24, "playlater": 25}
	for mediaID, flags := range links {
		for _, flag := range flags {
			_, err = mediaDB.sql.Load().ExecContext(ctx,
				`INSERT INTO MediaTags (MediaDBID, TagDBID) VALUES (?, ?)`, mediaID, tagIDs[flag])
			require.NoError(t, err)
		}
	}
}

func userTagNames(tags []database.TagInfo) []string {
	var out []string
	for _, tag := range tags {
		if tag.Type == "user" {
			out = append(out, tag.Tag)
		}
	}
	return out
}

func TestResolveSingletonContainerAliases_DiscSetShowsEveryDiscsFlags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	parent := filepath.ToSlash(filepath.Join("roms", "PSX"))
	p := func(parts ...string) string {
		return filepath.ToSlash(filepath.Join(append([]string{parent}, parts...)...))
	}
	ids := seedDiscSetRows(t, mediaDB,
		discSetSeed{path: p("Fav", "Game (Disc 1).chd"), title: "fav"},
		discSetSeed{path: p("Fav", "Game (Disc 2).chd"), title: "fav"},
		discSetSeed{path: p("Mixed", "Game (Disc 1).chd"), title: "mixed"},
		discSetSeed{path: p("Mixed", "Game (Disc 2).chd"), title: "mixed"},
		discSetSeed{path: p("Hidden", "Game (Disc 1).chd"), title: "hidden"},
		discSetSeed{path: p("Hidden", "Game (Disc 2).chd"), title: "hidden"},
		discSetSeed{path: p("Single", "Game.chd"), title: "single"},
		discSetSeed{path: p("Playlist", "Game.m3u"), title: "pl"},
		discSetSeed{path: p("Playlist", "Game (Disc 1).cue"), title: "pl-1"},
	)
	seedUserTags(t, mediaDB, map[int64][]string{
		ids[p("Fav", "Game (Disc 2).chd")]:      {"favorite", "playlater"},
		ids[p("Mixed", "Game (Disc 1).chd")]:    {"disliked"},
		ids[p("Mixed", "Game (Disc 2).chd")]:    {"liked"},
		ids[p("Hidden", "Game (Disc 2).chd")]:   {"hidden", "disliked"},
		ids[p("Single", "Game.chd")]:            {"favorite"},
		ids[p("Playlist", "Game (Disc 1).cue")]: {"favorite"},
	})

	aliases, err := mediaDB.ResolveSingletonContainerAliases(ctx, 2, []database.SingletonAliasCandidate{
		{ChildDir: aliasTestDir(parent, "Fav"), FileCount: 2},
		{ChildDir: aliasTestDir(parent, "Mixed"), FileCount: 2},
		{ChildDir: aliasTestDir(parent, "Hidden"), FileCount: 2},
		{ChildDir: aliasTestDir(parent, "Single"), FileCount: 1},
		{ChildDir: aliasTestDir(parent, "Playlist"), FileCount: 2},
	})
	require.NoError(t, err)
	byDir := make(map[string]database.SingletonContainerAlias, len(aliases))
	for i := range aliases {
		byDir[aliases[i].ChildDir] = aliases[i]
	}
	require.Len(t, byDir, 5)

	fav := byDir[aliasTestDir(parent, "Fav")]
	assert.Equal(t, p("Fav", "Game (Disc 1).chd"), fav.Row.Path, "the launch disc is still the first")
	assert.Equal(t, []string{"favorite", "playlater"}, userTagNames(fav.Tags), "the second disc's flags show")

	assert.Equal(t, []string{"liked"}, userTagNames(byDir[aliasTestDir(parent, "Mixed")].Tags),
		"a like on any disc outranks a dislike")

	assert.Equal(t, []string{"disliked"}, userTagNames(byDir[aliasTestDir(parent, "Hidden")].Tags),
		"another disc's hidden flag stays its own")

	assert.Equal(t, []string{"favorite"}, userTagNames(byDir[aliasTestDir(parent, "Single")].Tags))
	playlist := byDir[aliasTestDir(parent, "Playlist")]
	assert.False(t, playlist.MultiDisc)
	assert.Empty(t, userTagNames(playlist.Tags), "a playlist folder shows the playlist's own flags")
}

func TestMatchMediaTagFilters(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	ids := seedDiscSetRows(t, mediaDB,
		discSetSeed{path: "roms/PSX/A/A.chd", title: "a"},
		discSetSeed{path: "roms/PSX/B/B.chd", title: "b"},
		discSetSeed{path: "roms/PSX/C/C.chd", title: "c"},
	)
	a, b, c := ids["roms/PSX/A/A.chd"], ids["roms/PSX/B/B.chd"], ids["roms/PSX/C/C.chd"]
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, a, []database.TagInfo{{Type: "genre", Tag: "rpg"}}))
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, c, []database.TagInfo{{Type: "genre", Tag: "puzzle"}}))
	var titleB int64
	require.NoError(t, mediaDB.sql.Load().QueryRowContext(ctx,
		`SELECT MediaTitleDBID FROM Media WHERE DBID = ?`, b).Scan(&titleB))
	require.NoError(t, mediaDB.UpsertMediaTitleTags(ctx, titleB, []database.TagInfo{
		{Type: "developer", Tag: "squaresoft"},
		{Type: "distribution", Tag: "virtual-console"},
	}))

	match := func(filters ...zapscript.TagFilter) []int64 {
		t.Helper()
		matched, err := mediaDB.MatchMediaTagFilters(ctx, []int64{a, b, c, a}, filters)
		require.NoError(t, err)
		out := make([]int64, 0, len(matched))
		for id := range matched {
			out = append(out, id)
		}
		return out
	}
	and := func(typ, val string) zapscript.TagFilter {
		return zapscript.TagFilter{Type: typ, Value: val, Operator: zapscript.TagOperatorAND}
	}

	assert.ElementsMatch(t, []int64{a}, match(and("genre", "rpg")), "a media tag")
	assert.ElementsMatch(t, []int64{b}, match(and("credit", "squaresoft")), "a credit filter on a title tag")
	assert.ElementsMatch(t, []int64{b}, match(and("distribution", "virtualconsole")), "a deprecated spelling")
	assert.ElementsMatch(t, []int64{b, c}, match(zapscript.TagFilter{
		Type: "genre", Value: "rpg", Operator: zapscript.TagOperatorNOT,
	}))
	assert.ElementsMatch(t, []int64{a, c}, match(
		zapscript.TagFilter{Type: "genre", Value: "rpg", Operator: zapscript.TagOperatorOR},
		zapscript.TagFilter{Type: "genre", Value: "puzzle", Operator: zapscript.TagOperatorOR},
	))
	assert.Empty(t, match(and("genre", "rpg"), and("developer", "squaresoft")))

	matched, err := mediaDB.MatchMediaTagFilters(ctx, nil, []zapscript.TagFilter{and("genre", "rpg")})
	require.NoError(t, err)
	assert.Empty(t, matched)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = mediaDB.MatchMediaTagFilters(cancelled, []int64{a}, []zapscript.TagFilter{and("genre", "rpg")})
	require.ErrorIs(t, err, context.Canceled)
}

func TestMatchMediaTagFiltersSpansChunks(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	ctx := context.Background()

	seeds := make([]discSetSeed, 0, batchLookupIDsPerQuery+1)
	for i := range batchLookupIDsPerQuery + 1 {
		seeds = append(seeds, discSetSeed{path: fmt.Sprintf("roms/PSX/G%03d/G.chd", i), title: fmt.Sprintf("g%d", i)})
	}
	ids := seedDiscSetRows(t, mediaDB, seeds...)
	all := make([]int64, 0, len(ids))
	for _, id := range ids {
		all = append(all, id)
	}
	last := ids[seeds[len(seeds)-1].path]
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, last, []database.TagInfo{{Type: "genre", Tag: "rpg"}}))

	matched, err := mediaDB.MatchMediaTagFilters(ctx, all, []zapscript.TagFilter{
		{Type: "genre", Value: "rpg", Operator: zapscript.TagOperatorAND},
	})
	require.NoError(t, err)
	assert.Equal(t, map[int64]struct{}{last: {}}, matched)
}

func TestMatchMediaTagFiltersNullSQL(t *testing.T) {
	t.Parallel()
	_, err := (&MediaDB{}).MatchMediaTagFilters(context.Background(), []int64{1}, nil)
	require.ErrorIs(t, err, ErrNullSQL)
}

func TestMatchMediaTagFiltersQueryError(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupAliasTestDB(t)
	defer cleanup()
	require.NoError(t, mediaDB.sql.Load().Close())

	_, err := mediaDB.MatchMediaTagFilters(context.Background(), []int64{1}, []zapscript.TagFilter{
		{Type: "genre", Value: "rpg", Operator: zapscript.TagOperatorAND},
	})
	require.Error(t, err)
}
