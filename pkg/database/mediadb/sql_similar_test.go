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
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/recommend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func titleTagsWrite(titleTags ...tags.CanonicalTag) *database.ScrapeWrite {
	write := &database.ScrapeWrite{}
	for _, tag := range titleTags {
		write.TitleTags = append(write.TitleTags, database.TagInfo{Type: string(tag.Type), Tag: string(tag.Value)})
	}
	return write
}

func genreTag(value tags.TagValue) tags.CanonicalTag {
	return tags.CanonicalTag{Type: tags.TagTypeGenre, Value: value}
}

func franchiseTag(value tags.TagValue) tags.CanonicalTag {
	return tags.CanonicalTag{Type: tags.TagTypeSearch, Value: value}
}

func facetTagID(t *testing.T, facets []database.TitleFacetTag, tagType, tag string) int64 {
	t.Helper()
	for i := range facets {
		if facets[i].Type == tagType && facets[i].Tag == tag {
			return facets[i].TagDBID
		}
	}
	require.Failf(t, "tag not found", "%s:%s", tagType, tag)
	return 0
}

func TestSimilarityReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lib := newVersionTestLibrary(t, "Mario One", "Mario Two", "Sonic One", "Plain Platformer", "Untagged")
	platformer := genreTag(tags.TagGameGenreActionPlatformer)
	lib.scrape(t, "Mario One", titleTagsWrite(platformer, franchiseTag(tags.TagSearchFranchiseMario),
		tags.CanonicalTag{Type: tags.TagTypeYear, Value: "1985"}))
	lib.scrape(t, "Mario Two", titleTagsWrite(platformer, franchiseTag(tags.TagSearchFranchiseMario)))
	lib.scrape(t, "Sonic One", titleTagsWrite(platformer, franchiseTag(tags.TagSearchFranchiseSonic)))
	lib.scrape(t, "Plain Platformer", titleTagsWrite(platformer))

	types := []string{string(tags.TagTypeGenre), string(tags.TagTypeSearch)}
	facets, err := lib.db.GetTitleFacetTags(ctx, []int64{lib.titles["Mario One"], lib.titles["Untagged"]}, types)
	require.NoError(t, err)
	require.Len(t, facets, 2, "only the asked-for types, and nothing for an untagged title")
	for i := range facets {
		assert.Equal(t, lib.titles["Mario One"], facets[i].TitleDBID)
		assert.Equal(t, systemdefs.SystemArcade, facets[i].SystemID)
	}
	genreID := facetTagID(t, facets, string(tags.TagTypeGenre), string(tags.TagGameGenreActionPlatformer))
	marioID := facetTagID(t, facets, string(tags.TagTypeSearch), string(tags.TagSearchFranchiseMario))

	none, err := lib.db.GetTitleFacetTags(ctx, nil, types)
	require.NoError(t, err)
	assert.Empty(t, none)

	counts, err := lib.db.CountTitlesByTag(ctx, []int64{genreID, marioID, genreID}, 3)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{genreID: 3, marioID: 2}, counts, "a count stops at its limit")

	found, err := lib.db.FindTitlesSharingTags(ctx, []database.WeightedTag{
		{TagDBID: genreID, Weight: 1}, {TagDBID: marioID, Weight: 3},
	}, []int64{lib.titles["Mario One"]}, 10)
	require.NoError(t, err)
	require.Len(t, found, 3)
	assert.Equal(t, lib.titles["Mario Two"], found[0].TitleDBID, "sharing both tags outranks sharing one")
	assert.InDelta(t, 4.0, found[0].Score, 1e-9)
	assert.InDelta(t, 1.0, found[1].Score, 1e-9)
	assert.Less(t, found[1].TitleDBID, found[2].TitleDBID, "ties are broken by title row")

	limited, err := lib.db.FindTitlesSharingTags(ctx, []database.WeightedTag{{TagDBID: genreID, Weight: 1}}, nil, 2)
	require.NoError(t, err)
	assert.Len(t, limited, 2)

	empty, err := lib.db.FindTitlesSharingTags(ctx, nil, nil, 10)
	require.NoError(t, err)
	assert.Empty(t, empty)

	estimate, err := lib.db.MediaTitleCountEstimate(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, estimate, int64(5))
}

// TestFindTitlesSharingTags_Plan pins the read to the tag index: without it
// every lookup would walk all of MediaTitleTags once per tag.
func TestFindTitlesSharingTags_Plan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lib := newVersionTestLibrary(t, "One")
	rows, err := lib.db.sql.Load().QueryContext(ctx, `EXPLAIN QUERY PLAN
		WITH tok(TagDBID, W) AS (VALUES (?,?),(?,?))
		SELECT p.MediaTitleDBID, SUM(tok.W) AS S
		FROM tok CROSS JOIN MediaTitleTags p ON p.TagDBID = tok.TagDBID
		WHERE p.MediaTitleDBID NOT IN (?)
		GROUP BY p.MediaTitleDBID ORDER BY S DESC, p.MediaTitleDBID ASC LIMIT ?`, 1, 1.0, 2, 1.0, 9, 10)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan.WriteString(detail + "\n")
	}
	require.NoError(t, rows.Err())
	assert.Contains(t, plan.String(), "mediatitletags_tag_idx")
	assert.NotContains(t, plan.String(), "SCAN p")
}

func TestGetUserTagMedia(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lib := newVersionTestLibrary(t, "Liked Game", "Other Game")
	media := lib.media["Liked Game"]
	changed, err := lib.db.SetMediaTagMembership(ctx,
		database.MediaTagRef{Type: string(tags.TagTypeUser), Tag: string(tags.TagUserLiked)},
		[]int64{media.DBID})
	require.NoError(t, err)
	require.True(t, changed)

	tagged, err := lib.db.GetUserTagMedia(ctx)
	require.NoError(t, err)
	assert.Equal(t, []database.UserTagMedia{{
		Tag: string(tags.TagUserLiked), MediaDBID: media.DBID, TitleDBID: media.MediaTitleDBID,
	}}, tagged)
}

// TestSimilar_OnIndex runs the whole lookup against a real index: tags read
// from scraped titles, candidates from the tag index, and versions of the
// asked-about game left out by its version group.
func TestSimilar_OnIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The fillers make a platformer something less than the whole library,
	// so the genre still tells titles apart.
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission", "Mario One", "Mario Two", "Plain Platformer",
		"Untagged", "Filler A", "Filler B", "Filler C", "Filler D", "Filler E", "Filler F")
	platformer := genreTag(tags.TagGameGenreActionPlatformer)
	mario := franchiseTag(tags.TagSearchFranchiseMario)
	withTags := func(write *database.ScrapeWrite, titleTags ...tags.CanonicalTag) *database.ScrapeWrite {
		write.TitleTags = titleTagsWrite(titleTags...).TitleTags
		return write
	}
	// ASO and Alpha Mission are one game under two names.
	lib.scrape(t, "ASO", withTags(setNameWrite("aso", ""), platformer, mario))
	lib.scrape(t, "Alpha Mission", withTags(setNameWrite("alphamis", "aso"), platformer, mario))
	lib.scrape(t, "Mario One", titleTagsWrite(platformer, mario))
	lib.scrape(t, "Mario Two", titleTagsWrite(platformer, mario))
	lib.scrape(t, "Plain Platformer", titleTagsWrite(platformer))
	lib.regroup(t)

	picks, err := recommend.Similar(ctx, lib.db, lib.titles["ASO"], recommend.SimilarOptions{Limit: 10})
	require.NoError(t, err)
	ids := make([]int64, len(picks))
	for i := range picks {
		ids[i] = picks[i].TitleDBID
	}
	assert.ElementsMatch(t,
		[]int64{lib.titles["Mario One"], lib.titles["Mario Two"], lib.titles["Plain Platformer"]}, ids,
		"the other name of the same game is a version, not similar media")
	assert.Equal(t, lib.titles["Plain Platformer"], ids[2], "sharing the series outranks sharing only the genre")
	require.NotEmpty(t, picks[0].Reasons)
	assert.Equal(t, recommend.ReasonSeries, picks[0].Reasons[0].Code)
	assert.Equal(t, systemdefs.SystemArcade, picks[0].SystemID)

	// From the series, the other name of ASO's game is offered once.
	picks, err = recommend.Similar(ctx, lib.db, lib.titles["Mario One"], recommend.SimilarOptions{Limit: 10})
	require.NoError(t, err)
	versions := 0
	for i := range picks {
		if picks[i].TitleDBID == lib.titles["ASO"] || picks[i].TitleDBID == lib.titles["Alpha Mission"] {
			versions++
		}
	}
	assert.Equal(t, 1, versions, "a game with two names takes one place")

	none, err := recommend.Similar(ctx, lib.db, lib.titles["Untagged"], recommend.SimilarOptions{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, none)
}
