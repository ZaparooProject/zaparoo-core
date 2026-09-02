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

package titles

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	discDir       = "/roms/PSX/B_064/"
	discTrackPath = "/roms/PSX/B_064/B_064 (Track 001).bin"
	discCuePath   = "/roms/PSX/B_064/B_064.cue"
)

// setupContainerPromotion points the container lookup for discDir at a launch
// target and lets GetMediaByDBID return that target's row.
func setupContainerPromotion(
	m *helpers.MockMediaDBI, launchMediaID int64, launchPath string, launchTags []database.TagInfo,
) {
	m.On("FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, mock.Anything, discDir,
	).Return(&database.Media{DBID: launchMediaID, Path: launchPath}, nil)
	m.On("GetMediaByDBID", mock.Anything, launchMediaID).
		Return(database.SearchResultWithCursor{
			SystemID: "PSX",
			Name:     "B_064",
			Path:     launchPath,
			MediaID:  launchMediaID,
			Tags:     launchTags,
		}, nil)
}

func newPromotionTestConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := helpers.NewTestConfig(nil, t.TempDir())
	require.NoError(t, err)
	return cfg
}

// TestResolveTitle_PromotesTrackToContainerCue is the shape from the device:
// the slug search is capped at 50 rows and every file in a disc folder shares
// one title, so the cue sheet is not among the candidates at all. Selection can
// only return a track; the container lookup is what reaches the cue.
func TestResolveTitle_PromotesTrackToContainerCue(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discTrackPath,
		MediaID:  7,
	}}, nil)
	setupContainerPromotion(mockMediaDB, 99, discCuePath, nil)
	cacheWritten := setupCacheWriteSync(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discCuePath, result.Result.Path)
	assert.Equal(t, int64(99), result.Result.MediaID)

	select {
	case <-cacheWritten:
	case <-time.After(time.Second):
		t.Fatal("cache write goroutine did not complete in time")
	}
	// The promoted ID must be what gets cached, or the next launch of this
	// title replays the wrong pick straight out of the cache.
	mockMediaDB.AssertCalled(t, "SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, int64(99), mock.Anything)
}

// TestResolveTitle_PromotesDiscToPlaylist covers the multi-disc layout that ES-DE
// platforms index with stock launchers: a .chd is a companion of an .m3u, so a
// disc image must still reach the playlist that stands for the whole set.
func TestResolveTitle_PromotesDiscToPlaylist(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discDir + "B_064 (Disc 001).chd",
		MediaID:  7,
	}}, nil)
	setupContainerPromotion(mockMediaDB, 42, discDir+"B_064.m3u", nil)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discDir+"B_064.m3u", result.Result.Path)
}

// TestResolveTitle_PromotesCueToPlaylist proves a .cue is inside the gate: it is
// a container target for its own tracks but a companion of an enclosing .m3u.
func TestResolveTitle_PromotesCueToPlaylist(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discCuePath,
		MediaID:  7,
	}}, nil)
	setupContainerPromotion(mockMediaDB, 42, discDir+"B_064.m3u", nil)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discDir+"B_064.m3u", result.Result.Path)
}

// TestResolveTitle_SkipsContainerLookupForPlainRom pins the cost gate: an
// ordinary rom can never be standing in for a container, so launching one must
// not add a database round trip.
func TestResolveTitle_SkipsContainerLookupForPlainRom(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	romPath := "/roms/NES/Mario/Mario.nes"
	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "NES",
		Name:     "Mario",
		Path:     romPath,
		MediaID:  3,
	}}, nil)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "NES",
		GameName:  "Mario",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, romPath, result.Result.Path)
	mockMediaDB.AssertNotCalled(t, "FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, mock.Anything, mock.Anything)
}

// TestResolveTitle_KeepsRequestedDiscOverPlaylist guards the one case where
// promotion is wrong: a query naming a disc must get that disc, not the playlist
// that stands for every disc.
func TestResolveTitle_KeepsRequestedDiscOverPlaylist(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	discPath := discDir + "B_064 (Disc 2).chd"
	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discPath,
		MediaID:  7,
		Tags:     []database.TagInfo{{Type: "disc", Tag: "2"}},
	}}, nil)
	// The playlist carries no disc tag, which is exactly what makes it the
	// wrong answer to a query that named one.
	setupContainerPromotion(mockMediaDB, 42, discDir+"B_064.m3u", nil)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:       "PSX",
		GameName:       "B_064",
		AdditionalTags: []zapscript.TagFilter{{Type: "disc", Value: "2", Operator: zapscript.TagOperatorAND}},
		MediaDB:        mockMediaDB,
		Cfg:            cfg,
		MediaType:      slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discPath, result.Result.Path)
}

// TestResolveTitle_PromotesWhenBothRowsCarryRequestedTag is the other half of
// the guard: a tag both rows share was never what distinguished them, so it must
// not block promotion.
func TestResolveTitle_PromotesWhenBothRowsCarryRequestedTag(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	usa := []database.TagInfo{{Type: "region", Tag: "us"}}
	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discTrackPath,
		MediaID:  7,
		Tags:     usa,
	}}, nil)
	setupContainerPromotion(mockMediaDB, 99, discCuePath, usa)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:       "PSX",
		GameName:       "B_064",
		AdditionalTags: []zapscript.TagFilter{{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND}},
		MediaDB:        mockMediaDB,
		Cfg:            cfg,
		MediaType:      slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discCuePath, result.Result.Path)
}

// TestResolveTitle_ContainerLookupErrorKeepsSelection: a container lookup exists
// to launch the better file, never to fail a launch that would otherwise work.
func TestResolveTitle_ContainerLookupErrorKeepsSelection(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discTrackPath,
		MediaID:  7,
	}}, nil)
	mockMediaDB.On("FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, mock.Anything, discDir,
	).Return((*database.Media)(nil), errors.New("database is busy"))
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discTrackPath, result.Result.Path)
}

// TestResolveTitle_PromotesAtLowerConfidenceExit pins the second call site.
// A row with no tags scores 0.65 against a tag filter, below the early-return
// threshold, so this resolution leaves through the final block instead.
func TestResolveTitle_PromotesAtLowerConfidenceExit(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discTrackPath,
		MediaID:  7,
	}}, nil)
	setupContainerPromotion(mockMediaDB, 99, discCuePath, nil)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:       "PSX",
		GameName:       "B_064",
		AdditionalTags: []zapscript.TagFilter{{Type: "region", Value: "us", Operator: zapscript.TagOperatorAND}},
		MediaDB:        mockMediaDB,
		Cfg:            cfg,
		MediaType:      slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Less(t, result.Confidence, ConfidenceHigh)
	assert.Equal(t, discCuePath, result.Result.Path)
}

// TestResolveTitle_CacheHitSkipsContainerLookup: the cached ID was already
// promoted when it was written, so the cache path must not pay for it again.
func TestResolveTitle_CacheHitSkipsContainerLookup(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return(int64(99), StrategyExactMatch, true)
	mockMediaDB.On("GetMediaByDBID", mock.Anything, int64(99)).
		Return(database.SearchResultWithCursor{
			SystemID: "PSX",
			Name:     "B_064",
			Path:     discCuePath,
			MediaID:  99,
		}, nil)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discCuePath, result.Result.Path)
	mockMediaDB.AssertNotCalled(t, "FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, mock.Anything, mock.Anything)
}

// TestPromotionLosesRequestedTag covers the guard that decides when a container
// promotion would throw away something the query asked for. Each operator has
// its own notion of "satisfied", and getting any of them wrong either launches
// the wrong file or refuses a promotion that was correct.
func TestPromotionLosesRequestedTag(t *testing.T) {
	t.Parallel()

	tag := func(typ, value string) database.TagInfo {
		return database.TagInfo{Type: typ, Tag: value}
	}
	filter := func(typ, value string, op zapscript.TagOperator) zapscript.TagFilter {
		return zapscript.TagFilter{Type: typ, Value: value, Operator: op}
	}

	tests := []struct {
		name     string
		selected []database.TagInfo
		promoted []database.TagInfo
		filters  []zapscript.TagFilter
		want     bool
	}{
		{
			name: "no filters never blocks promotion",
			want: false,
		},
		{
			name:     "AND: playlist lacks the disc the query named",
			selected: []database.TagInfo{tag("disc", "2")},
			filters:  []zapscript.TagFilter{filter("disc", "2", zapscript.TagOperatorAND)},
			want:     true,
		},
		{
			name:     "AND: a tag both rows carry was never the distinction",
			selected: []database.TagInfo{tag("region", "us")},
			promoted: []database.TagInfo{tag("region", "us")},
			filters:  []zapscript.TagFilter{filter("region", "us", zapscript.TagOperatorAND)},
			want:     false,
		},
		{
			name:    "AND: a tag neither row carries is not lost by promoting",
			filters: []zapscript.TagFilter{filter("region", "us", zapscript.TagOperatorAND)},
			want:    false,
		},
		{
			name:     "NOT: promotion carries the tag the query excluded",
			promoted: []database.TagInfo{tag("unfinished", "beta")},
			filters:  []zapscript.TagFilter{filter("unfinished", "beta", zapscript.TagOperatorNOT)},
			want:     true,
		},
		{
			name:    "NOT: neither row carries the excluded tag",
			filters: []zapscript.TagFilter{filter("unfinished", "beta", zapscript.TagOperatorNOT)},
			want:    false,
		},
		{
			name:     "OR: promotion satisfies none of the group",
			selected: []database.TagInfo{tag("region", "us")},
			filters: []zapscript.TagFilter{
				filter("region", "us", zapscript.TagOperatorOR),
				filter("region", "eu", zapscript.TagOperatorOR),
			},
			want: true,
		},
		{
			// Comparing an OR group filter by filter would wrongly block this.
			name:     "OR: promotion satisfies the group a different way",
			selected: []database.TagInfo{tag("region", "us")},
			promoted: []database.TagInfo{tag("region", "eu")},
			filters: []zapscript.TagFilter{
				filter("region", "us", zapscript.TagOperatorOR),
				filter("region", "eu", zapscript.TagOperatorOR),
			},
			want: false,
		},
		{
			name: "OR: the selection satisfied nothing in the group either",
			filters: []zapscript.TagFilter{
				filter("region", "us", zapscript.TagOperatorOR),
				filter("region", "eu", zapscript.TagOperatorOR),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			selected := database.SearchResultWithCursor{MediaID: 1, Tags: tt.selected}
			promoted := database.SearchResultWithCursor{MediaID: 2, Tags: tt.promoted}
			assert.Equal(t, tt.want, promotionLosesRequestedTag(&selected, &promoted, tt.filters))
		})
	}
}

// TestResolveTitle_PromotedRowUnreadableKeepsSelection: the container named a
// different launch target but its row could not be read. A launch that would
// otherwise work must not fail because of that.
func TestResolveTitle_PromotedRowUnreadableKeepsSelection(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discTrackPath,
		MediaID:  7,
	}}, nil)
	mockMediaDB.On("FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, mock.Anything, discDir,
	).Return(&database.Media{DBID: 99, Path: discCuePath}, nil)
	mockMediaDB.On("GetMediaByDBID", mock.Anything, int64(99)).
		Return(database.SearchResultWithCursor{}, errors.New("row is gone"))
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discTrackPath, result.Result.Path)
	assert.Equal(t, int64(7), result.Result.MediaID)
	// Keeping the selection is also what happens when promotion never runs, so
	// pin that it did run and failed at the re-read.
	mockMediaDB.AssertCalled(t, "FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, "PSX", discDir)
	mockMediaDB.AssertCalled(t, "GetMediaByDBID", mock.Anything, int64(99))
}

// TestResolveTitle_KeepsSelectionWhenDirectoryIsNotAContainer is the ordinary
// case for a disc image: the extension passes the gate, so the lookup runs, but
// a flat library folder holds many games and collapses to nothing.
func TestResolveTitle_KeepsSelectionWhenDirectoryIsNotAContainer(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	flatPath := "/roms/PSX/B_064.chd"
	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     flatPath,
		MediaID:  7,
	}}, nil)
	mockMediaDB.On("FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, "PSX", "/roms/PSX/",
	).Return((*database.Media)(nil), nil)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, flatPath, result.Result.Path)
	// The point of the case is that a .chd passes the gate and the lookup finds
	// no container, not that the gate skipped it.
	mockMediaDB.AssertCalled(t, "FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, "PSX", "/roms/PSX/")
	mockMediaDB.AssertNotCalled(t, "GetMediaByDBID", mock.Anything, mock.Anything)
}

// TestResolveTitle_KeepsSelectionWhenAlreadyTheContainerTarget: selection landed
// on the cue sheet itself, so there is nothing to swap and no row to re-read.
func TestResolveTitle_KeepsSelectionWhenAlreadyTheContainerTarget(t *testing.T) {
	t.Parallel()

	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := newPromotionTestConfig(t)

	setupCacheMiss(mockMediaDB)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return([]database.SearchResultWithCursor{{
		SystemID: "PSX",
		Name:     "B_064",
		Path:     discCuePath,
		MediaID:  99,
	}}, nil)
	mockMediaDB.On("FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, "PSX", discDir,
	).Return(&database.Media{DBID: 99, Path: discCuePath}, nil)
	setupCacheWrite(mockMediaDB)

	result, err := ResolveTitle(context.Background(), &ResolveParams{
		SystemID:  "PSX",
		GameName:  "B_064",
		MediaDB:   mockMediaDB,
		Cfg:       cfg,
		MediaType: slugs.MediaTypeGame,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, discCuePath, result.Result.Path)
	// The lookup must run and name the row already selected; skipping it
	// entirely would look identical from the result alone.
	mockMediaDB.AssertCalled(t, "FindSingleContainerLaunchMediaBySystemID",
		mock.Anything, "PSX", discDir)
	mockMediaDB.AssertNotCalled(t, "GetMediaByDBID", mock.Anything, mock.Anything)
}
