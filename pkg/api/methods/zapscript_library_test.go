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

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
