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
	"slices"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var planNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // fixed test clock

func planFor(t *testing.T, lib *fakeLibrary, profileID string, opts Options) *Plan {
	t.Helper()
	plan, err := BuildPlan(context.Background(), lib.deps(), profileID, planNow, opts)
	require.NoError(t, err)
	require.NotNil(t, plan)
	return plan
}

func planTitles(plan *Plan) []int64 {
	var ids []int64
	for i := range plan.Sections {
		for j := range plan.Sections[i].Items {
			ids = append(ids, plan.Sections[i].Items[j].TitleDBID)
		}
	}
	return ids
}

func sectionKinds(plan *Plan) []string {
	kinds := make([]string, len(plan.Sections))
	for i := range plan.Sections {
		kinds[i] = plan.Sections[i].Kind
	}
	return kinds
}

func seedSections(plan *Plan) []PlanSection {
	var sections []PlanSection
	for i := range plan.Sections {
		if plan.Sections[i].Kind == SectionSeed {
			sections = append(sections, plan.Sections[i])
		}
	}
	return sections
}

func hours(n float64) int {
	return int(n * secondsPerHour)
}

func TestBuildPlan_NoSignals(t *testing.T) {
	t.Parallel()
	plan := planFor(t, seriesLibrary(), "", Options{})
	assert.NotNil(t, plan.Sections)
	assert.Empty(t, plan.Sections, "with nothing to go on there is nothing to recommend")
}

func TestBuildPlan_AccidentalLaunchIsNotASeed(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.play("", 1, 10, planNow.Add(-time.Hour))
	assert.Empty(t, planFor(t, lib, "", Options{}).Sections, "ten seconds is not play")

	lib.play("", 1, 61, planNow.Add(-time.Hour))
	assert.Empty(t, planFor(t, lib, "", Options{}).Sections, "one minute is too little to say anything")

	lib.play("", 1, hours(1), planNow.Add(-time.Hour))
	plan := planFor(t, lib, "", Options{})
	require.NotEmpty(t, plan.Sections)
	assert.Equal(t, SectionSeed, plan.Sections[0].Kind)
	assert.Equal(t, int64(1), plan.Sections[0].SeedTitleDBID)
	assert.Equal(t, SignalPlayed, plan.Sections[0].Signal)
}

func TestBuildPlan_ProfilesKeepTheirOwnPlay(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.play("dad", 21, hours(3), planNow.Add(-time.Hour))
	lib.play("", 41, hours(3), planNow.Add(-time.Hour))

	dad := planFor(t, lib, "dad", Options{})
	require.Len(t, seedSections(dad), 1)
	assert.Equal(t, int64(21), dad.Sections[0].SeedTitleDBID)

	shared := planFor(t, lib, "", Options{})
	require.Len(t, seedSections(shared), 1)
	assert.Equal(t, int64(41), shared.Sections[0].SeedTitleDBID,
		"with no profile active only play made with none active counts")

	assert.Empty(t, planFor(t, lib, "kid", Options{}).Sections)
}

func TestBuildPlan_SectionsAndCaps(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(1, tags.TagUserFavorite)
	lib.play("", 21, hours(4), planNow.Add(-2*24*time.Hour))
	lib.flag(61, tags.TagUserPlayLater)

	plan := planFor(t, lib, "", Options{})
	require.Equal(t, []string{SectionSeed, SectionPlayLater, SectionSeries, SectionSeed}, sectionKinds(plan))

	favorite := plan.Sections[0]
	assert.Equal(t, int64(1), favorite.SeedTitleDBID, "a favorite outranks a few hours of play")
	assert.Equal(t, SignalFavorite, favorite.Signal)
	mario := 0
	for _, item := range favorite.Items {
		require.NotEmpty(t, item.Reasons)
		if item.TitleDBID <= 10 {
			mario++
		}
	}
	assert.Equal(t, maxPerSeries, mario, "one series does not fill a section")

	assert.Equal(t, []PlanItem{{TitleDBID: 61}}, plan.Sections[1].Items,
		"a play later item with nothing in common with the seeds carries no reason")

	series := plan.Sections[2]
	require.NotNil(t, series.Tag)
	assert.Equal(t, "franchise:mario", series.Tag.Tag)
	assert.Equal(t, string(tags.TagTypeSearch), series.Tag.Type)
	for _, item := range series.Items {
		assert.LessOrEqual(t, item.TitleDBID, int64(10))
		assert.Equal(t, ReasonSeries, item.Reasons[0].Code)
	}

	shooter := plan.Sections[3]
	assert.Equal(t, int64(21), shooter.SeedTitleDBID)
	assert.Equal(t, SignalPlayed, shooter.Signal)

	all := planTitles(plan)
	sorted := slices.Clone(all)
	slices.Sort(sorted)
	assert.Len(t, slices.Compact(sorted), len(all), "a title appears in one section only")
	assert.NotContains(t, all, int64(1), "a seed is not recommended")
	assert.NotContains(t, all, int64(21))
	for _, id := range all {
		assert.Less(t, id, int64(91), "untagged titles are never recommended")
	}
}

func TestBuildPlan_DislikedNeverAppears(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(1, tags.TagUserFavorite)
	lib.flag(2, tags.TagUserDisliked)
	lib.flag(62, tags.TagUserDisliked)
	lib.flag(62, tags.TagUserPlayLater)

	plan := planFor(t, lib, "", Options{MaxResults: 30})
	all := planTitles(plan)
	require.NotEmpty(t, all)
	assert.NotContains(t, all, int64(2))
	assert.NotContains(t, all, int64(62), "a dislike outweighs a play later mark")
	for _, section := range seedSections(plan) {
		assert.NotEqual(t, int64(2), section.SeedTitleDBID)
	}
}

func TestBuildPlan_HiddenGameIsNotASeed(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.play("", 1, hours(5), planNow.Add(-time.Hour))
	lib.flag(1, tags.TagUserHidden)

	assert.Empty(t, planFor(t, lib, "", Options{}).Sections, "a hidden game says nothing about what to suggest")
}

func TestBuildPlan_DislikedLowersItsNeighbours(t *testing.T) {
	t.Parallel()
	lib := newFakeLibrary()
	lib.add(1, "NES", tagPlatformer, tagNintendo)
	// 2 and 3 are equally like the seed. 3 is also like a disliked game.
	lib.add(2, "NES", tagPlatformer, tagNintendo)
	lib.add(3, "NES", tagPlatformer, tagNintendo, tagPuzzle)
	lib.add(4, "NES", tagPlatformer, tagNintendo, tagShooter)
	lib.add(5, "NES", tagPuzzle)
	for id := int64(6); id <= 20; id++ {
		lib.add(id, "SNES", tagRPG)
	}
	lib.flag(1, tags.TagUserFavorite)

	before := planFor(t, lib, "", Options{})
	require.Len(t, before.Sections, 1)
	lib.flag(5, tags.TagUserDisliked)
	after := planFor(t, lib, "", Options{})
	require.Len(t, after.Sections, 1)

	position := func(plan *Plan, id int64) int {
		return slices.IndexFunc(plan.Sections[0].Items, func(item PlanItem) bool { return item.TitleDBID == id })
	}
	require.GreaterOrEqual(t, position(after, 3), 0, "a neighbour of a disliked game is lowered, not removed")
	assert.Greater(t, position(after, 3), position(after, 4))
	assert.Less(t, position(before, 3), position(before, 4))
}

func TestBuildPlan_PortsAreOneGame(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.port(1, 7)
	lib.port(3, 8)
	lib.flag(1, tags.TagUserFavorite)

	all := planTitles(planFor(t, lib, "", Options{MaxResults: 30}))
	require.NotEmpty(t, all)
	assert.NotContains(t, all, int64(7), "the seed's game on another system is not a suggestion")
	both := slices.Contains(all, int64(3)) && slices.Contains(all, int64(8))
	assert.False(t, both, "a game held on two systems is suggested once")
}

func TestBuildPlan_OneResultPerGame(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.link(22, 23, 24)
	// Play on one version of the seed's game counts for the game.
	lib.link(21, 32)
	lib.play("", 32, hours(2), planNow.Add(-time.Hour))

	plan := planFor(t, lib, "", Options{MaxResults: 30})
	require.Len(t, plan.Sections, 1)
	all := planTitles(plan)
	versions := 0
	for _, id := range all {
		if id >= 22 && id <= 24 {
			versions++
		}
	}
	assert.Equal(t, 1, versions, "three versions of one game take one place")
	assert.NotContains(t, all, int64(21), "another version of the seed is not recommended")
	assert.NotContains(t, all, int64(32))
}

func TestBuildPlan_BingeStillLeavesRoomForOtherSeeds(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.play("", 1, hours(200), planNow.Add(-time.Hour))
	lib.play("", 21, 20*60, planNow.Add(-time.Hour))
	lib.play("", 41, 20*60, planNow.Add(-time.Hour))
	lib.play("", 61, 20*60, planNow.Add(-time.Hour))

	plan := planFor(t, lib, "", Options{MaxSections: 12})
	seeds := make([]int64, 0, 4)
	for _, section := range seedSections(plan) {
		seeds = append(seeds, section.SeedTitleDBID)
	}
	assert.GreaterOrEqual(t, len(seeds), 3)
	assert.Equal(t, int64(1), seeds[0])
	assert.Subset(t, []int64{1, 21, 41, 61}, seeds)

	game := &GameSignal{PlaySecs: int64(hours(200)), LastPlayed: planNow}
	affinity, _ := game.Affinity(planNow)
	assert.InDelta(t, 1.0, affinity, 1e-9, "play stops counting for more past the saturation point")
}

func TestBuildPlan_PlayedGamesAreNotRecommended(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(21, tags.TagUserFavorite)
	lib.play("", 22, hours(1), planNow.Add(-time.Hour))
	lib.play("", 23, 5*60, planNow.Add(-time.Hour))

	plan := planFor(t, lib, "", Options{MaxResults: 30, MaxSections: 12})
	require.NotEmpty(t, plan.Sections)
	first := plan.Sections[0]
	require.Equal(t, int64(21), first.SeedTitleDBID)
	ids := make([]int64, 0, len(first.Items))
	for _, item := range first.Items {
		ids = append(ids, item.TitleDBID)
	}
	assert.NotContains(t, planTitles(plan), int64(22), "a game played at length is already known")
	require.Contains(t, ids, int64(23))
	assert.Equal(t, int64(23), ids[len(ids)-1], "a game tried briefly ranks below ones never played")
}

func TestBuildPlan_PlayLaterOnly(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(61, tags.TagUserPlayLater)
	lib.flag(91, tags.TagUserPlayLater)
	plan := planFor(t, lib, "", Options{})
	require.Equal(t, []string{SectionPlayLater}, sectionKinds(plan), "play later is not a sign of taste")
	assert.Equal(t, []PlanItem{{TitleDBID: 61}, {TitleDBID: 91}}, plan.Sections[0].Items)
}

func TestBuildPlan_SeedsAreNotPlayLaterItems(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(1, tags.TagUserFavorite)
	lib.flag(1, tags.TagUserPlayLater)
	lib.play("", 21, hours(4), planNow.Add(-2*24*time.Hour))
	lib.flag(21, tags.TagUserPlayLater)
	lib.flag(61, tags.TagUserPlayLater)

	plan := planFor(t, lib, "", Options{})
	seeds := make([]int64, 0, len(plan.Sections))
	for _, section := range seedSections(plan) {
		seeds = append(seeds, section.SeedTitleDBID)
	}
	require.ElementsMatch(t, []int64{1, 21}, seeds)
	i := slices.IndexFunc(plan.Sections, func(s PlanSection) bool { return s.Kind == SectionPlayLater })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, []PlanItem{{TitleDBID: 61}}, plan.Sections[i].Items,
		"a game heading its own section is not listed again under play later")
	all := planTitles(plan)
	assert.NotContains(t, all, int64(1))
	assert.NotContains(t, all, int64(21))
}

// A play later mark on another system's copy of a seed does not list the
// seed's game, and a game marked on two systems is listed once.
func TestBuildPlan_PlayLaterPortsAreOneGame(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.port(1, 7)
	lib.port(61, 62)
	lib.flag(1, tags.TagUserFavorite)
	lib.flag(7, tags.TagUserPlayLater)
	lib.flag(61, tags.TagUserPlayLater)
	lib.flag(62, tags.TagUserPlayLater)

	plan := planFor(t, lib, "", Options{})
	i := slices.IndexFunc(plan.Sections, func(s PlanSection) bool { return s.Kind == SectionPlayLater })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, []PlanItem{{TitleDBID: 61}}, plan.Sections[i].Items)
	assert.NotContains(t, planTitles(plan), int64(7), "the seed's port is the seed")
}

// A dislike on any port of a game keeps every port of it out, whichever of
// them the plan meets first.
func TestBuildPlan_DislikedPortNeverAppears(t *testing.T) {
	t.Parallel()
	for _, disliked := range []int64{2, 8} {
		lib := seriesLibrary()
		lib.port(2, 8)
		lib.flag(1, tags.TagUserFavorite)
		lib.flag(disliked, tags.TagUserDisliked)

		all := planTitles(planFor(t, lib, "", Options{MaxResults: 30}))
		require.NotEmpty(t, all)
		assert.NotContains(t, all, int64(2), "disliked %d", disliked)
		assert.NotContains(t, all, int64(8), "disliked %d", disliked)
	}
}

// A game played at length is not suggested again for being marked to play
// later.
func TestBuildPlan_PlayedPlayLaterIsNotListed(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.play("", 91, hours(1), planNow.Add(-time.Hour))
	lib.flag(91, tags.TagUserPlayLater)
	lib.play("", 92, 5*60, planNow.Add(-time.Hour))
	lib.flag(92, tags.TagUserPlayLater)
	lib.flag(93, tags.TagUserPlayLater)

	plan := planFor(t, lib, "", Options{})
	require.Equal(t, []string{SectionPlayLater}, sectionKinds(plan))
	assert.Equal(t, []PlanItem{{TitleDBID: 92}, {TitleDBID: 93}}, plan.Sections[0].Items,
		"a game only tried briefly stays listed")
}

func TestBuildPlan_TinyLibrary(t *testing.T) {
	t.Parallel()
	lib := newFakeLibrary()
	for id := int64(1); id <= 20; id++ {
		switch {
		case id <= 3:
			lib.add(id, "NES", tagPlatformer)
		case id <= 5:
			lib.add(id, "NES", tagShooter)
		default:
			lib.add(id, "NES")
		}
	}
	lib.play("", 1, hours(2), planNow.Add(-time.Hour))
	lib.play("", 4, hours(2), planNow.Add(-time.Hour))
	lib.flag(10, tags.TagUserFavorite)

	plan := planFor(t, lib, "", Options{})
	assert.Empty(t, plan.Sections, "two matches are too few for a section, and none is better than a thin one")
}

func TestBuildPlan_RecentPlayOutranksDecks(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.decks = []database.Deck{{DeckID: "mine", Owned: true}}
	for id := int64(41); id <= 50; id++ {
		lib.flag(id, tags.DeckTag("mine"))
	}
	for id := int64(11); id <= 14; id++ {
		lib.flag(id, tags.DeckTag("mine"))
	}
	lib.play("", 21, 20*60, planNow.Add(-time.Hour))

	plan := planFor(t, lib, "", Options{MaxResults: 30})
	require.NotEmpty(t, plan.Sections)
	assert.Equal(t, SignalPlayed, plan.Sections[0].Signal, "twenty minutes of play says more than deck membership")
	assert.Equal(t, int64(21), plan.Sections[0].SeedTitleDBID)
}

func TestBuildPlan_OnlyOwnedDecksCount(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(21, tags.DeckTag("fetched1"))
	lib.decks = []database.Deck{{DeckID: "fetched1", Owned: false, Saved: true}}
	assert.Empty(t, planFor(t, lib, "", Options{}).Sections, "someone else's deck says nothing about this profile")

	lib.flag(41, tags.DeckTag("mine1"))
	lib.decks = append(lib.decks, database.Deck{DeckID: "mine1", Owned: true})
	plan := planFor(t, lib, "", Options{})
	require.Len(t, plan.Sections, 1)
	assert.Equal(t, int64(41), plan.Sections[0].SeedTitleDBID)
	assert.Equal(t, SignalDeck, plan.Sections[0].Signal)
}

func TestBuildPlan_SystemsFilterAndLimits(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(1, tags.TagUserFavorite)
	lib.flag(21, tags.TagUserFavorite)
	lib.flag(41, tags.TagUserFavorite)
	lib.flag(61, tags.TagUserPlayLater)

	plan := planFor(t, lib, "", Options{Systems: map[string]struct{}{"SNES": {}}, MaxResults: 2})
	require.NotEmpty(t, plan.Sections)
	for _, id := range planTitles(plan) {
		assert.Equal(t, "SNES", lib.titles[id].system)
	}
	for i := range plan.Sections {
		assert.LessOrEqual(t, len(plan.Sections[i].Items), 2+planSpare)
	}

	one := planFor(t, lib, "", Options{MaxSections: 1})
	assert.Len(t, one.Sections, 1)
}

func TestBuildPlan_Deterministic(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.flag(1, tags.TagUserFavorite)
	lib.flag(11, tags.TagUserLiked)
	lib.play("", 21, hours(4), planNow.Add(-2*24*time.Hour))
	lib.play("", 41, hours(4), planNow.Add(-2*24*time.Hour))
	lib.flag(61, tags.TagUserPlayLater)
	first := planFor(t, lib, "", Options{MaxSections: 12})
	for range 20 {
		require.Equal(t, first, planFor(t, lib, "", Options{MaxSections: 12}))
	}
}

func TestAffinity(t *testing.T) {
	t.Parallel()
	recent := planNow.Add(-time.Hour)
	tests := []struct {
		name       string
		wantSignal string
		game       GameSignal
		want       float64
	}{
		{name: "nothing", game: GameSignal{}},
		{
			name: "favorite barely played", game: GameSignal{Favorite: true, PlaySecs: 300, LastPlayed: recent},
			want: favoriteAffinity, wantSignal: SignalFavorite,
		},
		{name: "liked", game: GameSignal{Liked: true}, want: likedAffinity, wantSignal: SignalLiked},
		{name: "owned deck", game: GameSignal{OwnedDeck: true}, want: ownedDeckAffinity, wantSignal: SignalDeck},
		{name: "disliked", game: GameSignal{Disliked: true, PlaySecs: 99999, LastPlayed: recent}},
		{name: "play later alone", game: GameSignal{PlayLater: true}},
		{
			name: "long recent play beats liked",
			game: GameSignal{Liked: true, PlaySecs: int64(hours(20)), LastPlayed: planNow},
			want: 1.0, wantSignal: SignalPlayed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, signal := tt.game.Affinity(planNow)
			assert.InDelta(t, tt.want, got, 1e-9)
			assert.Equal(t, tt.wantSignal, signal)
		})
	}

	fresh := GameSignal{PlaySecs: int64(hours(5)), LastPlayed: planNow}
	halved := GameSignal{PlaySecs: int64(hours(5)), LastPlayed: planNow.Add(-45 * 24 * time.Hour)}
	ancient := GameSignal{PlaySecs: int64(hours(5)), LastPlayed: planNow.Add(-360 * 24 * time.Hour)}
	freshValue, _ := fresh.Affinity(planNow)
	halvedValue, _ := halved.Affinity(planNow)
	ancientValue, _ := ancient.Affinity(planNow)
	assert.InDelta(t, freshValue/2, halvedValue, 1e-9, "play counts for half as much after the half-life")
	assert.InDelta(t, freshValue*playDecayFloor, ancientValue, 1e-9, "old play never fades to nothing")
}

func TestGatherSignals_MergesVersionsAndFlags(t *testing.T) {
	t.Parallel()
	lib := seriesLibrary()
	lib.link(1, 2)
	lib.play("", 1, 600, planNow.Add(-3*time.Hour))
	lib.play("", 2, 1800, planNow.Add(-time.Hour))
	lib.flag(1, tags.TagUserDisliked)
	lib.flag(2, tags.TagUserLiked)
	lib.flag(3, tags.TagUserDisliked)
	// A session on media the index no longer holds is dropped.
	lib.play("", 9999, 3600, planNow.Add(-time.Hour))

	signals, err := GatherSignals(context.Background(), lib.deps(), "", planNow)
	require.NoError(t, err)
	require.Len(t, signals.ByGroup, 2)
	game := signals.ByGroup[1]
	require.NotNil(t, game)
	assert.Equal(t, int64(2400), game.PlaySecs)
	assert.Equal(t, int64(2), game.TitleDBID, "the most played version stands for the game")
	assert.Equal(t, planNow.Add(-time.Hour), game.LastPlayed)
	assert.True(t, game.Liked)
	assert.False(t, game.Disliked, "a like on one version outweighs a dislike on another")
	assert.True(t, signals.ByGroup[3].Disliked)
}
