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

package recommend

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pickIDs(picks []Pick) []int64 {
	ids := make([]int64, len(picks))
	for i := range picks {
		ids[i] = picks[i].TitleDBID
	}
	return ids
}

func reasonCodes(reasons []Reason) []string {
	codes := make([]string, len(reasons))
	for i := range reasons {
		codes[i] = reasons[i].Code
	}
	return codes
}

func TestSimilar_SeriesOutranksGenre(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	picks, err := Similar(context.Background(), lib, 1, SimilarOptions{Limit: 12})
	require.NoError(t, err)
	require.Len(t, picks, 12)

	ids := pickIDs(picks)
	assert.NotContains(t, ids, int64(1), "a title is not like itself")
	assert.ElementsMatch(t, []int64{2, 3, 4, 5, 6, 7, 8, 9, 10}, ids[:9],
		"every other game of the series comes before games that only share a genre")
	assert.Equal(t, []int64{2, 3, 4, 5, 6}, ids[:5], "the same system comes first")
	assert.Equal(t, ReasonSeries, picks[0].Reasons[0].Code, "the strongest reason leads")
	assert.Contains(t, reasonCodes(picks[0].Reasons), ReasonGenre)
	assert.Equal(t, []string{ReasonGenre}, reasonCodes(picks[11].Reasons),
		"a pick is only given the reasons it has")
	for _, id := range ids {
		assert.NotContains(t, []int64{91, 92, 93, 94, 95}, id, "untagged titles are never picked")
	}
}

func TestSimilar_GenreFamilyAndYear(t *testing.T) {
	t.Parallel()
	lib := newFakeLibrary()
	lib.add(1, "NES", tagPlatformer, tagNintendo, "year=1988")
	lib.add(2, "NES", tagMaze, tagNintendo, "year=1988")
	lib.add(3, "NES", tagMaze, tagNintendo, "year=1984")
	lib.add(4, "NES", tagShooter, "year=1988")

	picks, err := Similar(context.Background(), lib, 1, SimilarOptions{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, pickIDs(picks),
		"a shared year alone does not make two games alike")
	assert.ElementsMatch(t, []string{ReasonDeveloper, ReasonGenre, ReasonYear}, reasonCodes(picks[0].Reasons))
	assert.Equal(t, ReasonGenre, picks[0].Reasons[0].Code)
	assert.Equal(t, "action", picks[0].Reasons[0].Tags[0].Tag, "a related genre is named by the family")
	assert.ElementsMatch(t, []string{ReasonDeveloper, ReasonGenre, ReasonDecade}, reasonCodes(picks[1].Reasons))
	assert.Equal(t, ReasonDecade, picks[1].Reasons[2].Code)
	assert.Equal(t, "1980", picks[1].Reasons[2].Tags[0].Tag)
}

func TestSimilar_OneResultPerGame(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	// 2 and 3 are versions of one game, and so are the seed and 4.
	lib.link(2, 3)
	lib.link(1, 4)
	picks, err := Similar(context.Background(), lib, 1, SimilarOptions{Limit: 50})
	require.NoError(t, err)
	ids := pickIDs(picks)
	assert.NotContains(t, ids, int64(4), "another version of the same game is not similar media")
	assert.Contains(t, ids, int64(2))
	assert.NotContains(t, ids, int64(3), "a game appears once however many versions it has")
}

func TestSimilar_SystemsFilter(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	picks, err := Similar(context.Background(), lib, 1,
		SimilarOptions{Limit: 50, Systems: map[string]struct{}{"SNES": {}}})
	require.NoError(t, err)
	assert.Equal(t, []int64{7, 8, 9, 10}, pickIDs(picks))
}

func TestSimilar_NothingToCompare(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	for _, id := range []int64{91, 9999} {
		picks, err := Similar(context.Background(), lib, id, SimilarOptions{Limit: 10})
		require.NoError(t, err)
		assert.Empty(t, picks, "title %d", id)
	}
	picks, err := Similar(context.Background(), lib, 1, SimilarOptions{})
	require.NoError(t, err)
	assert.Empty(t, picks)
}

func TestSimilar_UbiquitousTagFindsNothing(t *testing.T) {
	t.Parallel()
	lib := newFakeLibrary()
	// Every title is a platformer: the genre says nothing here. Two share a
	// developer.
	for id := int64(1); id <= 120; id++ {
		titleTags := []string{tagPlatformer}
		if id <= 2 {
			titleTags = append(titleTags, tagNintendo)
		}
		lib.add(id, "NES", titleTags...)
	}
	picks, err := Similar(context.Background(), lib, 1, SimilarOptions{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{2}, pickIDs(picks), "only the rare tag finds candidates")
	assert.Equal(t, []string{ReasonDeveloper, ReasonGenre}, reasonCodes(picks[0].Reasons),
		"the common tag still counts towards a match the rare one made")

	picks, err = Similar(context.Background(), lib, 50, SimilarOptions{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, picks, "a title with only the common tag has no similar media")
}

func TestSimilar_Deterministic(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	first, err := Similar(context.Background(), lib, 71, SimilarOptions{Limit: 30})
	require.NoError(t, err)
	for range 20 {
		again, err := Similar(context.Background(), lib, 71, SimilarOptions{Limit: 30})
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
}

func TestRarity(t *testing.T) {
	t.Parallel()
	assert.Greater(t, rarity(1000, 2), rarity(1000, 200))
	assert.Greater(t, rarity(1000, 200), rarity(1000, 900))
	assert.Greater(t, rarity(1000, 1000), 0.0)
	assert.Greater(t, rarity(10, 500), 0.0, "an estimate below the count does not go negative")
	assert.Equal(t, minUbiquitousTitles, ubiquitousCutoff(100))
	assert.Equal(t, 2000, ubiquitousCutoff(100_000))
}

func TestSimilar_PortsAreOneGame(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	// 7 is the seed's game on another system; 8 and 3 are one other game.
	lib.port(1, 7)
	lib.port(3, 8)
	picks, err := Similar(context.Background(), lib, 1, SimilarOptions{Limit: 12})
	require.NoError(t, err)

	ids := pickIDs(picks)
	assert.NotContains(t, ids, int64(7), "the same game on another system is not a similar game")
	assert.Contains(t, ids, int64(3), "the port on the seed's system stands for the game")
	assert.NotContains(t, ids, int64(8))
}
