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

package librarysync_test

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	postState = "POST /v1/device/library/state"
	getState  = "GET /v1/device/library/state"
)

var (
	metroidUSA = nesPath("Metroid (USA).nes")
	metroidEU  = nesPath("Metroid (Europe).nes")
	contraHack = nesPath("Contra (USA) (Hack).nes")
	contra     = nesPath("Contra (USA).nes")
	zelda      = nesPath("Zelda (USA).nes")
	metroidFre = nesPath("Metroid (USA) [T-Fre].nes")
)

// setLocalFlag sets a flag the way the tags API does: through
// ApplyMediaUserFlags, so the media.db projection and the identity snapshot
// are recorded beside it.
func (f *syncFixture) setLocalFlag(t *testing.T, path string, flag database.MediaUserFlag, value bool) {
	t.Helper()
	mediaDBID := f.mediaDBID(t, path)
	_, err := database.ApplyMediaUserFlags(
		f.ctx, f.db, "NES", path, mediaDBID, map[database.MediaUserFlag]bool{flag: value})
	require.NoError(t, err)
	identity, found, err := database.LookupMediaIdentity(f.ctx, f.db.MediaDB, "NES", path)
	require.NoError(t, err)
	if found {
		require.NoError(t, f.db.UserDB.SetMediaUserSnapshot(
			"NES", path, identity.DisplayName, identity.CoreSlug, identity.LegacyTags()))
	}
}

// mediaDBID returns the indexed row ID for a path, or 0 when it is not indexed.
func (f *syncFixture) mediaDBID(t *testing.T, path string) int64 {
	t.Helper()
	system, err := systemdefs.LookupSystem("NES")
	require.NoError(t, err)
	results, err := f.db.MediaDB.SearchMediaPathExact(f.ctx, []systemdefs.System{*system}, path)
	require.NoError(t, err)
	if len(results) == 0 {
		return 0
	}
	return results[0].MediaID
}

func (f *syncFixture) localRow(t *testing.T, path string) database.MediaUserData {
	t.Helper()
	row, _, err := f.db.UserDB.GetMediaUserData("NES", path)
	require.NoError(t, err)
	return row
}

func (f *syncFixture) syncState(t *testing.T) librarysync.StateResult {
	t.Helper()
	result, err := f.svc.SyncState(f.ctx)
	require.NoError(t, err)
	return result
}

func (f *syncFixture) hasUserTag(t *testing.T, path, flag string) bool {
	t.Helper()
	identity, found, err := database.LookupMediaIdentity(f.ctx, f.db.MediaDB, "NES", path)
	require.NoError(t, err)
	require.True(t, found)
	results, err := f.db.MediaDB.SearchMediaBySlug(f.ctx, "NES", identity.CoreSlug, nil)
	require.NoError(t, err)
	for i := range results {
		if results[i].Path != path {
			continue
		}
		tagInfos, tagErr := f.db.MediaDB.GetMediaTagsByMediaDBID(f.ctx, results[i].MediaID)
		require.NoError(t, tagErr)
		for _, tag := range tagInfos {
			if tag.Type == "user" && tag.Tag == flag {
				return true
			}
		}
	}
	return false
}

func TestSyncState_LocalFavoritePushes(t *testing.T) {
	f := newSyncFixture(t, metroidUSA, zelda)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.setLocalFlag(t, zelda, database.MediaUserFlagPlayLater, true)
	f.setLocalFlag(t, zelda, database.MediaUserFlagHidden, true)

	result := f.syncState(t)
	assert.Equal(t, 2, result.Pushed)
	metroid := f.online.stateRow("Game", "NES", "metroid")
	require.NotNil(t, metroid)
	assert.True(t, metroid.Favorite)
	assert.Equal(t, "Metroid", metroid.Title)
	assert.Contains(t, metroid.PreferredTags, "region:us", "the starred copy names the version")
	assert.NotContains(t, metroid.PreferredTags, "extension:nes", "only tags that tell versions apart")
	zeldaRow := f.online.stateRow("Game", "NES", "zelda")
	require.NotNil(t, zeldaRow)
	assert.Equal(t, "play_later", zeldaRow.Intent)

	for _, batch := range f.online.pushedState() {
		for _, item := range batch {
			assert.NotContains(t, item.Tags, "user:hidden", "hidden never leaves the device")
		}
	}

	f.online.resetCalls()
	result = f.syncState(t)
	assert.Zero(t, result.Pushed)
	assert.Zero(t, f.online.count(postState), "an agreed state is not pushed again")
	assert.Equal(t, 1, f.online.count(getState), "every pass pulls first")
}

func TestSyncState_UnfavoriteClearsTheAccountRow(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.syncState(t)

	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, false)
	f.syncState(t)
	row := f.online.stateRow("Game", "NES", "metroid")
	require.NotNil(t, row)
	assert.True(t, row.Deleted, "clearing a game's state leaves a tombstone")
	assert.False(t, row.Favorite)
}

func TestSyncState_PullAppliesAccountState(t *testing.T) {
	f := newSyncFixture(t, metroidUSA, metroidEU)
	f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Title: "Metroid",
		Favorite: true, Reaction: "liked", PreferredTags: []string{"region:eu"},
	})

	result := f.syncState(t)
	assert.Equal(t, 1, result.Pulled)
	assert.Zero(t, f.online.count(postState), "applying a pulled row never echoes it back")

	eu := f.localRow(t, metroidEU)
	assert.True(t, eu.IsFavorite, "the preferred version carries the state")
	assert.True(t, eu.IsLiked)
	usa := f.localRow(t, metroidUSA)
	assert.False(t, usa.IsFavorite)
	assert.True(t, f.hasUserTag(t, metroidEU, "favorite"), "browse sees the pulled favorite")

	f.online.resetCalls()
	f.syncState(t)
	assert.Zero(t, f.online.count(postState))
}

func TestSyncState_GameNotOnDeviceKeepsAccountState(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.online.putStateRow(&fakeStateRow{MediaType: "Game", SystemID: "NES", CoreSlug: "zelda", Favorite: true})

	f.syncState(t)
	f.online.resetCalls()
	f.syncState(t)
	assert.Zero(t, f.online.count(postState), "holding no copy is not the same as clearing the favorite")
	require.True(t, f.online.stateRow("Game", "NES", "zelda").Favorite)

	// The game turns up in a later index and picks the favorite up.
	scantest.IndexMediaPaths(t, f.db.MediaDB, "NES", metroidUSA, zelda)
	f.bumpGeneration(t)
	f.syncState(t)
	assert.True(t, f.localRow(t, zelda).IsFavorite)
	assert.Zero(t, f.online.count(postState))
}

func TestSyncState_HackIsItsOwnGame(t *testing.T) {
	f := newSyncFixture(t, contra, contraHack)
	f.setLocalFlag(t, contraHack, database.MediaUserFlagFavorite, true)

	f.syncState(t)
	hack := f.online.stateRow("Game", "NES", "contra", "unlicensed:hack")
	require.NotNil(t, hack)
	assert.True(t, hack.Favorite)
	assert.Nil(t, f.online.stateRow("Game", "NES", "contra"), "the plain release holds no state")

	f.online.putStateRow(&fakeStateRow{MediaType: "Game", SystemID: "NES", CoreSlug: "contra", Reaction: "disliked"})
	f.syncState(t)
	assert.True(t, f.localRow(t, contra).IsDisliked)
	assert.False(t, f.localRow(t, contraHack).IsDisliked, "the hack keeps its own state")
	assert.True(t, f.localRow(t, contraHack).IsFavorite)
}

func TestSyncState_ConflictMergesBothSides(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.syncState(t)

	// Another device likes the game, and the pull cursor has already moved
	// past that write, so only the push can find it.
	written := f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Favorite: true, Reaction: "liked",
	})
	require.NoError(t, f.db.UserDB.SetDeviceState(librarysync.DeviceStateKeyStateSince, "999"))
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagPlayLater, true)

	result := f.syncState(t)
	assert.Equal(t, 1, result.Conflicts)
	row := f.online.stateRow("Game", "NES", "metroid")
	require.NotNil(t, row)
	assert.Greater(t, row.Revision, written.Revision)
	assert.Equal(t, "play_later", row.Intent, "the local change is kept")
	assert.Equal(t, "liked", row.Reaction, "the other device's change is kept")
	assert.True(t, f.localRow(t, metroidUSA).IsLiked, "and applied here")
}

func TestSyncState_AccountEraseKeepsLocalState(t *testing.T) {
	f := newSyncFixture(t, metroidUSA, zelda)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.syncState(t)
	f.setLocalFlag(t, zelda, database.MediaUserFlagLiked, true)
	f.online.putStateRow(&fakeStateRow{MediaType: "Game", SystemID: "NES", CoreSlug: "kidicarus", Favorite: true})
	f.syncState(t)

	f.online.eraseState()
	result := f.syncState(t)
	assert.True(t, f.localRow(t, metroidUSA).IsFavorite, "an erase on the account never touches local state")
	assert.True(t, f.localRow(t, zelda).IsLiked)
	assert.Equal(t, 2, result.Pushed, "local state pushes again as new rows")
	metroid := f.online.stateRow("Game", "NES", "metroid")
	require.NotNil(t, metroid)
	assert.True(t, metroid.Favorite)
	assert.Nil(t, f.online.stateRow("Game", "NES", "kidicarus"), "state for a game this device never held is gone")
}

func TestSyncState_NullConflictKeepsLocalState(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.syncState(t)

	// The account erased its state, and the pull cursor sits past the erase
	// so the pull reports nothing; only the push finds the row gone.
	f.online.eraseState()
	require.NoError(t, f.db.UserDB.SetDeviceState(librarysync.DeviceStateKeyStateSince, "999"))
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagLiked, true)

	result := f.syncState(t)
	assert.Equal(t, 1, result.Conflicts)
	assert.True(t, f.localRow(t, metroidUSA).IsFavorite, "the local flags stay")
	assert.True(t, f.localRow(t, metroidUSA).IsLiked)
	row := f.online.stateRow("Game", "NES", "metroid")
	require.NotNil(t, row, "and push again as a new row in the same pass")
	assert.True(t, row.Favorite)
	assert.Equal(t, "liked", row.Reaction)
}

func TestSyncState_PullKeepsPerCopyFlags(t *testing.T) {
	f := newSyncFixture(t, metroidUSA, metroidEU)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagLiked, true)
	f.setLocalFlag(t, metroidEU, database.MediaUserFlagDisliked, true)
	f.syncState(t)
	require.Equal(t, "liked", f.online.stateRow("Game", "NES", "metroid").Reaction, "the copies roll up to liked")

	// Another device puts the game on the play-later list.
	f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Reaction: "liked", Intent: "play_later",
	})
	f.syncState(t)

	usa, eu := f.localRow(t, metroidUSA), f.localRow(t, metroidEU)
	assert.True(t, usa.IsLiked, "a field the account did not change is not touched")
	assert.True(t, eu.IsDisliked, "on any copy")
	assert.True(t, usa.IsPlayLater, "the new flag lands on the copy a launch would pick")
	assert.False(t, eu.IsPlayLater)

	// The account clears the reaction: that goes on every copy.
	f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Reaction: "none", Intent: "play_later",
	})
	f.syncState(t)
	assert.False(t, f.localRow(t, metroidUSA).IsLiked)
	assert.False(t, f.localRow(t, metroidEU).IsDisliked)
	assert.True(t, f.localRow(t, metroidUSA).IsPlayLater)
}

func TestSyncState_PullNeverMovesAStar(t *testing.T) {
	f := newSyncFixture(t, metroidUSA, metroidEU)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.syncState(t)

	// The account picks the Europe version and adds a like.
	f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Favorite: true, Reaction: "liked",
		PreferredTags: []string{"region:eu"},
	})
	f.syncState(t)
	assert.True(t, f.localRow(t, metroidUSA).IsFavorite, "the star stays where the user put it")
	assert.False(t, f.localRow(t, metroidEU).IsFavorite)
	assert.True(t, f.localRow(t, metroidEU).IsLiked, "a new flag goes on the account's preferred version")
	assert.False(t, f.localRow(t, metroidUSA).IsLiked)

	f.online.resetCalls()
	f.syncState(t)
	assert.Zero(t, f.online.count(postState), "the account's version choice is not fought over")
	assert.Equal(t, []string{"region:eu"}, f.online.stateRow("Game", "NES", "metroid").PreferredTags)
}

func TestSyncState_StarMoveResendsPreferredTags(t *testing.T) {
	f := newSyncFixture(t, metroidUSA, metroidEU)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.syncState(t)
	require.Contains(t, f.online.stateRow("Game", "NES", "metroid").PreferredTags, "region:us")

	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, false)
	f.setLocalFlag(t, metroidEU, database.MediaUserFlagFavorite, true)
	result := f.syncState(t)
	assert.Equal(t, 1, result.Pushed, "the game stays a favorite, only its version moved")
	row := f.online.stateRow("Game", "NES", "metroid")
	require.NotNil(t, row)
	assert.True(t, row.Favorite)
	assert.Contains(t, row.PreferredTags, "region:eu")
	assert.NotContains(t, row.PreferredTags, "region:us")

	f.online.resetCalls()
	f.syncState(t)
	assert.Zero(t, f.online.count(postState))
}

func TestSyncState_RejectedItemWaitsForAChange(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.online.setRejectState("invalid_identity")
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)

	result := f.syncState(t)
	assert.Equal(t, 1, result.Rejected)
	f.online.resetCalls()
	f.syncState(t)
	assert.Zero(t, f.online.count(postState), "a rejected request is not repeated")

	f.online.setRejectState("")
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagLiked, true)
	result = f.syncState(t)
	assert.Equal(t, 1, result.Pushed)
}

func TestSyncState_RelinkStartsBookkeepingOver(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagFavorite, true)
	f.syncState(t)

	f.online.mu.Lock()
	f.online.stateRows = make(map[string]*fakeStateRow)
	f.online.token = "library-token-2"
	f.online.mu.Unlock()
	config.SetAuthCfgForTesting(map[string]config.CredentialEntry{
		config.RemoteAuthLookupURL(f.online.server.URL): {Bearer: "library-token-2"},
	})

	result := f.syncState(t)
	assert.Equal(t, 1, result.Pushed, "a new link pushes local state as new rows")
	require.NotNil(t, f.online.stateRow("Game", "NES", "metroid"))
}

func TestSyncState_Disabled(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.cfg.SetLibrarySync(false)
	_, err := f.svc.SyncState(f.ctx)
	require.ErrorIs(t, err, librarysync.ErrDisabled)
}

func TestSyncState_PulledFavoriteClearsTheDislikedTag(t *testing.T) {
	f := newSyncFixture(t, metroidUSA, metroidEU)
	// One copy is disliked and another liked, so the game reads as liked and
	// a pulled favorite touches only the favorite field.
	f.setLocalFlag(t, metroidUSA, database.MediaUserFlagDisliked, true)
	f.setLocalFlag(t, metroidEU, database.MediaUserFlagLiked, true)
	f.syncState(t)
	require.True(t, f.hasUserTag(t, metroidUSA, "disliked"))

	// The account stars the game and names the version this device disliked.
	f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Title: "Metroid",
		Favorite: true, Reaction: "liked", PreferredTags: []string{"region:us"},
	})
	f.syncState(t)

	usa := f.localRow(t, metroidUSA)
	require.True(t, usa.IsFavorite, "the preferred version takes the star")
	require.False(t, usa.IsDisliked, "a favorite cannot stay disliked")
	assert.False(t, f.hasUserTag(t, metroidUSA, "disliked"),
		"browse must not still list a favorite under disliked")
	assert.True(t, f.hasUserTag(t, metroidUSA, "favorite"))
}

func TestSyncState_PullWaitsForTheIndexToSettle(t *testing.T) {
	// Only the European copy is indexed so far, as during a first index.
	f := newSyncFixture(t, metroidEU)
	require.NoError(t, f.db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusRunning))
	f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Title: "Metroid",
		Favorite: true, PreferredTags: []string{"region:us"},
	})

	_, err := f.svc.SyncState(f.ctx)
	require.ErrorIs(t, err, librarysync.ErrNotSettled)
	assert.False(t, f.localRow(t, metroidEU).IsFavorite,
		"a half-built index must not decide which copy the star lands on")
	assert.Zero(t, f.online.count(getState), "the account is not even asked")

	// The index finishes and the copy the account asked for exists.
	scantest.IndexMediaPaths(t, f.db.MediaDB, "NES", metroidEU, metroidUSA)
	require.NoError(t, f.db.MediaDB.SetIndexingStatus(mediadb.IndexingStatusCompleted))
	f.bumpGeneration(t)
	f.syncState(t)

	assert.True(t, f.localRow(t, metroidUSA).IsFavorite, "the preferred version takes the star")
	assert.False(t, f.localRow(t, metroidEU).IsFavorite)
}

func TestSyncState_PreferredVersionPrefersAnExactMatch(t *testing.T) {
	// A translation carries the plain release's tags plus its own, so holding
	// every preferred tag does not make it the version the account named. Two
	// interchangeable copies of the plain release leave the launch ranking
	// with nothing to separate them, which is when the extra tags used to win.
	usaA := filepath.ToSlash(filepath.Join("roms", "NES", "collection a", "Metroid (USA).nes"))
	usaB := filepath.ToSlash(filepath.Join("roms", "NES", "collection b", "Metroid (USA).nes"))
	f := newSyncFixture(t, usaA, usaB, metroidFre)
	f.online.putStateRow(&fakeStateRow{
		MediaType: "Game", SystemID: "NES", CoreSlug: "metroid", Title: "Metroid",
		Favorite: true, PreferredTags: []string{"lang:en", "region:us"},
	})

	f.syncState(t)

	assert.False(t, f.localRow(t, metroidFre).IsFavorite, "a translation of a version is not that version")
	starred := 0
	for _, path := range []string{usaA, usaB} {
		if f.localRow(t, path).IsFavorite {
			starred++
		}
	}
	assert.Equal(t, 1, starred, "exactly one copy of the version the account named is starred")
}
