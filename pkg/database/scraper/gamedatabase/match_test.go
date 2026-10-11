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

package gamedatabase

import (
	"testing"

	gdb "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func release(id, date string, order int, regions ...tags.TagValue) *record {
	return &record{id: id, date: sortDate(date), order: order, regions: regions}
}

func regionSet(regions ...tags.TagValue) map[tags.TagValue]struct{} {
	set := make(map[tags.TagValue]struct{}, len(regions))
	for _, region := range regions {
		set[region] = struct{}{}
	}
	return set
}

func TestChoose(t *testing.T) {
	t.Parallel()
	japan := release("game", "1990-11-21", 0, tags.TagRegionJP)
	usa := release("game", "1991-08-23", 1, tags.TagRegionUS)
	europe := release("game", "1992-04-11", 2, tags.TagRegionEU)
	all := []*record{japan, usa, europe}
	defaults := []string{"us", "world"}

	for _, tc := range []struct {
		files map[tags.TagValue]struct{}
		want  *record
		name  string
		prefs []string
	}{
		{name: "the file's one region decides", files: regionSet(tags.TagRegionEU), prefs: defaults, want: europe},
		{name: "it decides against the preference", files: regionSet(tags.TagRegionJP), prefs: defaults, want: japan},
		{name: "no region on the file uses the preference", files: nil, prefs: defaults, want: usa},
		{
			name:  "several regions narrow to those, then the preference picks",
			files: regionSet(tags.TagRegionJP, tags.TagRegionEU), prefs: []string{"eu", "jp"}, want: europe,
		},
		{
			name:  "several regions with no preferred one fall to the earliest",
			files: regionSet(tags.TagRegionJP, tags.TagRegionEU), prefs: defaults, want: japan,
		},
		{
			name:  "a region no row has falls back to the preference",
			files: regionSet(tags.TagRegionBR), prefs: defaults, want: usa,
		},
		{name: "no preference matches: the earliest release", files: nil, prefs: []string{"kr"}, want: japan},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := choose(all, tc.files, tc.prefs)
			require.True(t, ok)
			assert.Same(t, tc.want, got)
		})
	}
}

func TestChooseBreaksTiesByDateThenUpstreamOrder(t *testing.T) {
	t.Parallel()
	later := release("game", "1996", 0, tags.TagRegionUS)
	dated := release("game", "1991-08-23", 1, tags.TagRegionUS)
	yearOnly := release("game", "1991", 2, tags.TagRegionUS)
	undated := release("game", "", 3, tags.TagRegionUS)
	got, ok := choose([]*record{later, dated, yearOnly, undated}, nil, []string{"us"})
	require.True(t, ok)
	assert.Same(t, dated, got, "a fully dated release comes before a bare year in the same year")

	first := release("game", "1991", 5, tags.TagRegionUS)
	second := release("game", "1991", 6, tags.TagRegionUS)
	got, ok = choose([]*record{second, first}, nil, []string{"us"})
	require.True(t, ok)
	assert.Same(t, first, got)
}

func TestChoosePassesOverVariantsWhenARetailRowExists(t *testing.T) {
	t.Parallel()
	prototype := release("gameproto", "1990-10-25", 0, tags.TagRegionUS)
	prototype.variant = true
	retail := release("game", "1991-08-23", 1, tags.TagRegionUS)

	got, ok := choose([]*record{prototype, retail}, nil, []string{"us"})
	require.True(t, ok, "a prototype under another ID does not make the title ambiguous")
	assert.Same(t, retail, got)

	got, ok = choose([]*record{prototype}, nil, []string{"us"})
	require.True(t, ok, "a title known only as a prototype is still described")
	assert.Same(t, prototype, got)
}

func TestChooseRefusesATitleTwoGamesShare(t *testing.T) {
	t.Parallel()
	taito := release("indianajonestaito", "1991", 0, tags.TagRegionUS)
	ubisoft := release("indianajonesubisoft", "1993", 1, tags.TagRegionUS)
	_, ok := choose([]*record{taito, ubisoft}, regionSet(tags.TagRegionUS), []string{"us"})
	assert.False(t, ok)

	// Region separates two games that share a title.
	american := release("bomberman", "1998", 0, tags.TagRegionUS)
	japanese := release("bombermangb2", "1995", 1, tags.TagRegionJP)
	got, ok := choose([]*record{american, japanese}, regionSet(tags.TagRegionJP), []string{"us"})
	require.True(t, ok)
	assert.Same(t, japanese, got)

	_, ok = choose(nil, nil, []string{"us"})
	assert.False(t, ok)
}

func TestMatchKeys(t *testing.T) {
	t.Parallel()
	slugger := newSlugger(slugs.MediaTypeGame)
	fileSlug := func(name string) string {
		return slugs.Slugify(slugs.MediaTypeGame, tags.ParseTitleFromFilenameForMedia(name, false, slugs.MediaTypeGame))
	}

	plain := matchKeys(&gdb.Row{Title: "Super Mario World", TitleScreen: gdb.SameAsTitle}, "supermarioworld", slugger)
	assert.Equal(t, []string{"supermarioworld"}, plain, "a key is listed once however many ways it is reached")

	alfred := &gdb.Row{Title: "Super Alfred Chicken", TitleScreen: "Alfred Chicken"}
	screen := matchKeys(alfred, "alfredchicken", slugger)
	assert.Equal(t, []string{"superalfredchicken", "alfredchicken"}, screen)

	// Upstream writes long vowels with macrons; dump names spell them out.
	moon := &gdb.Row{Title: "Bishōjo Senshi Sailor Moon", TitleScreen: gdb.SameAsTitle}
	sailor := matchKeys(moon, "bishoujosenshisailormoon", slugger)
	assert.Contains(t, sailor, fileSlug("Bishoujo Senshi Sailor Moon (Japan).sfc"))

	// A long "o" that dump names write "oo" is only reachable by the ID.
	okami := matchKeys(&gdb.Row{Title: "Ōkami Densetsu", TitleScreen: gdb.SameAsTitle}, "ookamidensetsu", slugger)
	assert.Contains(t, okami, fileSlug("Ookami Densetsu (Japan).sfc"))

	noID := matchKeys(&gdb.Row{Title: "Porter", TitleScreen: gdb.SameAsTitle}, "Not An ID", slugger)
	assert.Equal(t, []string{"porter"}, noID, "an ID Core cannot store is not a key")
}

func TestCatalogPrefersARetailRowForASet(t *testing.T) {
	t.Parallel()
	cat := &catalog{bySet: make(map[string]*record)}
	bootleg := &record{order: 0, variant: true, parsed: rowTags{setName: "froggert"}}
	retail := &record{order: 1, parsed: rowTags{setName: "froggert"}}
	later := &record{order: 2, parsed: rowTags{setName: "froggert"}}
	cat.addSet(bootleg)
	cat.addSet(retail)
	cat.addSet(later)
	assert.Same(t, retail, cat.bySet["froggert"])

	cat.addSet(&record{})
	assert.Len(t, cat.bySet, 1, "a row with no set name is not filed")
}

func TestAddTitlesFilesARowOncePerTitleAndRespectsPrecedence(t *testing.T) {
	t.Parallel()
	bySlug := map[string][]int64{"alfredchicken": {1}, "superalfredchicken": {1, 2}}
	rec := &record{keys: []string{"superalfredchicken", "alfredchicken"}}
	target := make(map[int64][]*record)
	require.True(t, addTitles(rec, bySlug, target, nil))
	assert.Len(t, target[1], 1, "both of a row's names reach title 1, which lists it once")
	assert.Len(t, target[2], 1)

	fallback := make(map[int64][]*record)
	assert.False(t, addTitles(rec, bySlug, fallback, target), "titles a primary bundle described are left to it")
	assert.Empty(t, fallback)
	assert.False(t, addTitles(&record{keys: []string{"unknown"}}, bySlug, fallback, nil))
}
