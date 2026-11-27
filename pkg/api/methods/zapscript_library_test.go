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

package methods

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/recommend"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newSearchLibrary(t *testing.T, mediaDB *helpers.MockMediaDBI) *ZapScriptLibrary {
	t.Helper()
	pl := mocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot-uuid")
	return &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{
			Context:  ctx,
			Database: &database.Database{UserDB: helpers.NewMockUserDBI(), MediaDB: mediaDB},
			Platform: pl,
			State:    st,
			Config:   &config.Instance{},
		}
	}}
}

func flaggedRows(first int64, n int) []database.SearchResultWithCursor {
	rows := make([]database.SearchResultWithCursor, 0, n)
	for i := range n {
		id := first + int64(i)
		rows = append(rows, database.SearchResultWithCursor{
			SystemID: "NES", Name: fmt.Sprintf("Game %04d", id),
			Path: fmt.Sprintf("roms/nes/%04d.nes", id), MediaID: id,
		})
	}
	return rows
}

func TestZapScriptLibrary_FlaggedReadsEveryPage(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	firstPage := func(f *database.SearchFilters) bool {
		return f.Cursor == nil && f.SortCursor == nil
	}
	isFavorite := func(f *database.SearchFilters) bool {
		return len(f.Tags) == 2 && f.Query == "" &&
			f.Tags[0] == zapscript.TagFilter{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorAND} &&
			f.Tags[1] == zapscript.TagFilter{Type: "user", Value: "hidden", Operator: zapscript.TagOperatorNOT}
	}
	// A full page plus one more row says another page follows.
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.MatchedBy(func(f *database.SearchFilters) bool {
		return isFavorite(f) && firstPage(f)
	})).Return(flaggedRows(1, zapScriptFlaggedPage+1), nil).Once()
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.MatchedBy(func(f *database.SearchFilters) bool {
		return isFavorite(f) && !firstPage(f)
	})).Return(flaggedRows(zapScriptFlaggedPage+1, 1), nil).Once()

	items, err := newSearchLibrary(t, mediaDB).Flagged(context.Background(), "favorite")
	require.NoError(t, err)
	require.Len(t, items, zapScriptFlaggedPage+1)
	assert.Equal(t, platforms.LibraryItem{
		SystemID: "NES", Name: "Game 0001", Path: "roms/nes/0001.nes", MediaID: 1,
	}, items[0])
	assert.Equal(t, int64(zapScriptFlaggedPage+1), items[len(items)-1].MediaID)
	mediaDB.AssertExpectations(t)
}

func TestZapScriptLibrary_FlaggedHiddenIncludesHiddenMedia(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.MatchedBy(func(f *database.SearchFilters) bool {
		return len(f.Tags) == 1 && f.Tags[0].Value == "hidden" &&
			f.Tags[0].Operator == zapscript.TagOperatorAND && !f.ExcludeHidden
	})).Return(flaggedRows(1, 2), nil).Once()

	items, err := newSearchLibrary(t, mediaDB).Flagged(context.Background(), "hidden")
	require.NoError(t, err)
	assert.Len(t, items, 2)
	mediaDB.AssertExpectations(t)
}

func TestLibraryItem_NamesVersionsByWhatSetsThemApart(t *testing.T) {
	t.Parallel()
	result := models.SearchResultMedia{
		Name: "Quest", Path: "roms/snes/Quest (USA).sfc", MediaID: 7,
		System: models.System{ID: "SNES"},
		DisambiguatingTags: []database.TagInfo{
			{Type: "region", Tag: "us"}, {Type: "lang", Tag: "en"},
		},
	}
	assert.Equal(t, platforms.LibraryItem{
		SystemID: "SNES", Name: "Quest (us, en)", Path: "roms/snes/Quest (USA).sfc", MediaID: 7,
	}, libraryItem(&result))

	result.DisambiguatingTags = nil
	assert.Equal(t, "Quest", libraryItem(&result).Name)
}

func TestLinkedDeckItemIDs_MatchesTheFileWhateverItsSpelling(t *testing.T) {
	t.Parallel()
	items := []database.DeckItem{
		{DBID: 1, Anchor: database.DeckItemAnchor{SystemID: "SNES", Path: "roms/snes/Quest.sfc"}},
		{DBID: 2, Anchor: database.DeckItemAnchor{SystemID: "NES", Path: "roms/snes/Quest.sfc"}},
		{DBID: 3, Anchor: database.DeckItemAnchor{SystemID: "snes", Path: "roms/snes/Quest.sfc"}},
		{DBID: 4},
		{DBID: 5, Anchor: database.DeckItemAnchor{SystemID: "SNES", Path: "roms/snes/Other.sfc"}},
	}
	ref := platforms.MediaRef{SystemID: "SNES", Path: `roms\snes\Quest.sfc`}
	assert.Equal(t, []int64{1, 3}, linkedDeckItemIDs(items, ref))
	assert.Empty(t, linkedDeckItemIDs(items, platforms.MediaRef{SystemID: "SNES", Path: "roms/snes/None.sfc"}))
}

func TestZapScriptLibrary_EditsRefusedWhileLocked(t *testing.T) {
	t.Parallel()
	f := newKioskFixture(t)
	f.lockOpen(t)
	require.True(t, f.lock.Locked())
	library := &ZapScriptLibrary{Env: func(context.Context) requests.RequestEnv { return f.env(nil) }}
	ref := platforms.MediaRef{SystemID: "SNES", Path: "roms/snes/Quest.sfc", MediaID: 7}

	err := library.SetUserFlag(context.Background(), ref, "favorite", true)
	require.ErrorIs(t, err, platforms.ErrLibraryRefused)
	require.ErrorIs(t, err, ErrKioskLocked)

	changed, err := library.EditDeck(context.Background(), "deck-1", ref, true)
	require.ErrorIs(t, err, platforms.ErrLibraryRefused)
	assert.False(t, changed)
}

func TestZapScriptLibrary_EditDeckLeavesAMissingDeckRefused(t *testing.T) {
	t.Parallel()
	userDB, cleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	library := &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{Context: ctx, Database: &database.Database{UserDB: userDB}}
	}}
	changed, err := library.EditDeck(context.Background(), "nope",
		platforms.MediaRef{SystemID: "SNES", Path: "roms/snes/Quest.sfc"}, true)
	require.ErrorIs(t, err, platforms.ErrLibraryRefused)
	assert.False(t, changed)
}

// Most played reads media.history.top for the system asked for and leaves out
// hidden media, as every other list does.
func TestZapScriptLibrary_MostPlayedLeavesOutHiddenMedia(t *testing.T) {
	t.Parallel()
	userDB := helpers.NewMockUserDBI()
	mediaDB := helpers.NewMockMediaDBI()
	shown, hidden := "/roms/SNES/Shown.sfc", "/roms/SNES/Hidden.sfc"
	userDB.On("GetMediaHistoryTop", []string{"SNES"}, (*time.Time)(nil), 25).Return([]database.MediaHistoryTopEntry{
		{SystemID: "SNES", MediaName: "Shown", MediaPath: shown, SessionCount: 4, LastPlayedAt: time.Now()},
		{SystemID: "SNES", MediaName: "Hidden", MediaPath: hidden, SessionCount: 2, LastPlayedAt: time.Now()},
	}, nil)
	mediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).Return([]database.MediaPathID{
		{SystemID: "SNES", Path: shown, DBID: 1, MediaTitleDBID: 10},
		{SystemID: "SNES", Path: hidden, DBID: 2, MediaTitleDBID: 20},
	}, nil)
	mediaDB.On("GetMediaTagsByMediaRefs", mock.Anything, mock.Anything).Return(map[int64][]database.TagInfo{
		1: {{Type: "user", Tag: "favorite"}},
		2: {{Type: "region", Tag: "hidden"}, {Type: "user", Tag: "hidden"}},
	}, nil)
	library := &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{
			Context:  ctx,
			Database: &database.Database{UserDB: userDB, MediaDB: mediaDB},
			Config:   &config.Instance{},
		}
	}}

	items, err := library.MostPlayed(context.Background(), "SNES")
	require.NoError(t, err)
	assert.Equal(t, []platforms.LibraryItem{{SystemID: "SNES", Name: "Shown", Path: shown, MediaID: 1}}, items)
	userDB.AssertExpectations(t)
}

func TestHasUserTag(t *testing.T) {
	t.Parallel()
	list := []database.TagInfo{{Type: "region", Tag: "hidden"}, {Type: "user", Tag: "liked"}}
	assert.False(t, hasUserTag(list, tags.TagUserHidden), "only a user tag counts")
	assert.True(t, hasUserTag(list, tags.TagUserLiked))
	assert.False(t, hasUserTag(nil, tags.TagUserHidden))
}

// Recommended reads the same plan the app shows: every section, one section
// by kind, or the queue layout.
//
//nolint:paralleltest // swaps the package's recommendation clock and plan cache
func TestZapScriptLibrary_Recommended(t *testing.T) {
	fixture := newRecommendedFixture(t)
	fixture.mediaDB.On("MediaPreferencesRevision", mock.Anything).Return("1", nil)
	library := &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{
			Context:  ctx,
			Database: &database.Database{MediaDB: fixture.mediaDB, UserDB: fixture.userDB},
			Config:   &config.Instance{},
			State:    fixture.state,
		}
	}}
	ctx := context.Background()
	want := fixture.call(t, nil).Sections[0].Items
	wantIDs := make([]int64, 0, len(want))
	for i := range want {
		wantIDs = append(wantIDs, want[i].MediaID)
	}
	ids := func(items []platforms.LibraryItem) []int64 {
		out := make([]int64, 0, len(items))
		for i := range items {
			out = append(out, items[i].MediaID)
		}
		return out
	}

	all, err := library.Recommended(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, wantIDs, ids(all))

	seed, err := library.Recommended(ctx, recommend.SectionSeed)
	require.NoError(t, err)
	assert.Equal(t, wantIDs, ids(seed))

	series, err := library.Recommended(ctx, "series")
	require.NoError(t, err)
	assert.Empty(t, series, "a section the plan does not hold lists nothing")

	queue, err := library.Recommended(ctx, platforms.RecommendedQueue)
	require.NoError(t, err)
	assert.ElementsMatch(t, wantIDs, ids(queue))
}

// A request the API refuses reaches ZapScript as a refused library call.
func TestZapScriptLibrary_ClientErrorIsRefused(t *testing.T) {
	t.Parallel()
	library := newSearchLibrary(t, helpers.NewMockMediaDBI())
	_, err := library.Similar(context.Background(), platforms.MediaRef{})
	require.ErrorIs(t, err, platforms.ErrLibraryRefused)
	_, err = library.Versions(context.Background(), platforms.MediaRef{})
	require.ErrorIs(t, err, platforms.ErrLibraryRefused)
}

func decksLibrary(e *decksTestEnv) *ZapScriptLibrary {
	return &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		env := e.env
		env.Context = ctx
		return env
	}}
}

// SetUserFlag writes the flag through media.tags.update, by media ID or by
// system and path.
func TestZapScriptLibrary_SetUserFlag(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	library := decksLibrary(e)
	ctx := context.Background()
	metroid := pathutil.CanonicalMediaPath(e.paths[0])
	contra := pathutil.CanonicalMediaPath(e.paths[1])

	favorite := func(path string) bool {
		t.Helper()
		data, found, err := e.env.Database.UserDB.GetMediaUserData("NES", path)
		require.NoError(t, err)
		return found && data.IsFavorite
	}

	require.NoError(t, library.SetUserFlag(ctx,
		platforms.MediaRef{SystemID: "NES", Path: metroid, MediaID: e.ids[0]}, "favorite", true))
	assert.True(t, favorite(metroid))
	require.NoError(t, library.SetUserFlag(ctx,
		platforms.MediaRef{SystemID: "NES", Path: metroid, MediaID: e.ids[0]}, "favorite", false))
	assert.False(t, favorite(metroid))

	require.NoError(t, library.SetUserFlag(ctx, platforms.MediaRef{SystemID: "NES", Path: contra}, "favorite", true))
	assert.True(t, favorite(contra), "a ref with no media ID is found by system and path")

	err := library.SetUserFlag(ctx, platforms.MediaRef{SystemID: "NES", Path: "roms/NES/None.nes"}, "favorite", true)
	require.ErrorIs(t, err, platforms.ErrLibraryRefused, "media the index does not hold is refused")
}

// EditDeck adds the game once and removes every item linked to it, leaving a
// deck that already holds it, or never did, as it is.
func TestZapScriptLibrary_EditDeck(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	library := decksLibrary(e)
	ctx := context.Background()
	created, ok := e.call(t, HandleDecksNew, `{"name": "Weekend"}`).(models.DeckResponse)
	require.True(t, ok)
	metroid := platforms.MediaRef{SystemID: "NES", Path: pathutil.CanonicalMediaPath(e.paths[0]), MediaID: e.ids[0]}
	contra := platforms.MediaRef{SystemID: "NES", Path: pathutil.CanonicalMediaPath(e.paths[1])}
	itemCount := func() int {
		t.Helper()
		deck, err := e.env.Database.UserDB.GetDeck(created.DeckID)
		require.NoError(t, err)
		return len(deck.Items)
	}

	changed, err := library.EditDeck(ctx, created.DeckID, metroid, true)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = library.EditDeck(ctx, created.DeckID, metroid, true)
	require.NoError(t, err)
	assert.False(t, changed, "a deck already holding the game is left as it is")
	assert.Equal(t, 1, itemCount())

	changed, err = library.EditDeck(ctx, created.DeckID, contra, true)
	require.NoError(t, err)
	assert.True(t, changed, "a ref with no media ID is added by system and path")
	assert.Equal(t, 2, itemCount())

	changed, err = library.EditDeck(ctx, created.DeckID, metroid, false)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = library.EditDeck(ctx, created.DeckID, metroid, false)
	require.NoError(t, err)
	assert.False(t, changed, "removing a game the deck does not hold changes nothing")
	assert.Equal(t, 1, itemCount())
}

func manyVersionRows(n int) []database.SearchResultWithCursor {
	rows := make([]database.SearchResultWithCursor, 0, n)
	for i := range n {
		id := int64(1000 + i)
		rows = append(rows, versionRow(id, contraTitle, "Contra", fmt.Sprintf("/roms/NES/Contra %04d.nes", id)))
	}
	return rows
}

// Versions reads every page of media.versions, so a game with more versions
// than one page lists them all.
func TestZapScriptLibrary_VersionsReadsEveryPage(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	rows := manyVersionRows(zapScriptVersionsPage + 20)
	expectVersionsRead(mediaDB, 1000, contraTitle, nil, rows)
	library := &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{
			Context: ctx, Database: &database.Database{MediaDB: mediaDB}, Config: &config.Instance{},
		}
	}}

	items, err := library.Versions(context.Background(), platforms.MediaRef{SystemID: "NES", MediaID: 1000})
	require.NoError(t, err)
	require.Len(t, items, len(rows))
	seen := make(map[int64]bool, len(items))
	for i := range items {
		seen[items[i].MediaID] = true
	}
	assert.Len(t, seen, len(rows), "no version is listed twice across pages")
}

// Versions and Similar find a ref with no media ID by its system and path.
func TestZapScriptLibrary_ListsBySystemAndPath(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	path := "/roms/NES/Contra (USA).nes"
	mediaDB.On("FindSystemBySystemID", "NES").Return(database.System{DBID: 1, SystemID: "NES"}, nil)
	mediaDB.On("FindMediaBySystemAndPaths", mock.Anything, int64(1), []string{path}).
		Return(map[string]database.Media{path: {DBID: 101, MediaTitleDBID: contraTitle, Path: path}}, nil)
	expectVersionsRead(mediaDB, 101, contraTitle, nil, contraRows()[:2])
	library := &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{
			Context: ctx, Database: &database.Database{MediaDB: mediaDB}, Config: &config.Instance{},
		}
	}}

	items, err := library.Versions(context.Background(), platforms.MediaRef{SystemID: "NES", Path: path})
	require.NoError(t, err)
	assert.Equal(t, []int64{101, 102}, []int64{items[0].MediaID, items[1].MediaID})
}

// Similar lists what media.similar finds for the game.
func TestZapScriptLibrary_Similar(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{11}).
		Return(map[int64]database.MediaFullRow{11: mediaFullRow(11, 1, "")}, nil)
	expectSimilarReads(mediaDB, 2, 3, 4)
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).Return([]database.SearchResultWithCursor{
		suggestionRow(4, "Salamander"), suggestionRow(2, "Gradius II"),
	}, nil)
	library := &ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{
			Context: ctx, Database: &database.Database{MediaDB: mediaDB}, Config: &config.Instance{},
		}
	}}

	items, err := library.Similar(context.Background(), platforms.MediaRef{SystemID: "NES", MediaID: 11})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Gradius II", items[0].Name)
	assert.Equal(t, int64(21), items[0].MediaID)
}
