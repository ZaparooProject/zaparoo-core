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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlistmemory"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPlaylistMemoryEnv is a playlist test service backed by a real UserDB,
// with playlist memory wired the way Start wires it.
func setupPlaylistMemoryEnv(t *testing.T) (*ServiceContext, *userdb.UserDB) {
	t.Helper()
	svc := setupPlaylistTestEnv(t)
	db, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	svc.DB = &database.Database{UserDB: db}
	svc.PlaylistMemory = playlistmemory.New(db, func() string {
		if profile := svc.State.ActiveProfile(); profile != nil {
			return profile.ProfileID
		}
		return ""
	})
	svc.PlaylistMemory.Start()
	t.Cleanup(svc.PlaylistMemory.Stop)
	svc.State.SetPlaylistObserver(svc.PlaylistMemory.PlaylistChanged)
	return svc, db
}

func recordOpenPlaylist(t *testing.T, db *userdb.UserDB, slot string, pls *playlists.Playlist) {
	t.Helper()
	snapshot, err := playlists.EncodeSnapshot(pls)
	require.NoError(t, err)
	require.NoError(t, db.UpsertOpenPlaylist(&database.OpenPlaylist{
		Slot: slot, ProfileID: pls.ProfileID, Snapshot: snapshot,
	}))
}

func TestRestoreOpenPlaylists_RestoresPausedWithoutLaunching(t *testing.T) {
	t.Parallel()
	svc, db := setupPlaylistMemoryEnv(t)
	pm := &servicePlaybackRecorder{}
	svc.PlaybackManager = pm

	pls := makeServicePlaylist()
	pls.Index = 2
	pls.Idle = 1
	pls.Unsafe = true
	pls.AllowedCommands = tokens.NewCommandPolicy("launch")
	recordOpenPlaylist(t, db, mediaslot.Primary, playlists.Play(*pls))

	restoreOpenPlaylists(svc, nil)

	got := svc.State.GetActivePlaylist()
	require.NotNil(t, got)
	assert.Equal(t, "id", got.ID)
	assert.Equal(t, 2, got.Index)
	assert.False(t, got.Playing, "nothing plays until someone asks")
	assert.False(t, got.Armed, "an idle playlist does not start itself either")
	assert.True(t, got.Unsafe)
	assert.False(t, got.AllowedCommands.Allows("execute"))
	assert.Empty(t, pm.played)
	assert.Nil(t, svc.State.GetBackgroundPlaylist())
}

func TestRestoreOpenPlaylists_DeckChecks(t *testing.T) {
	t.Parallel()
	svc, db := setupPlaylistMemoryEnv(t)
	require.NoError(t, db.CreateDeck(&database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Theirs", Owned: false}))

	deleted := makeServicePlaylist()
	deleted.DeckID = "bbbbbbbbbbbb"
	recordOpenPlaylist(t, db, mediaslot.Primary, deleted)
	unowned := makeServicePlaylist()
	unowned.ID = "deck://aaaaaaaaaaaa"
	unowned.DeckID = "aaaaaaaaaaaa"
	unowned.Slot = mediaslot.Background
	recordOpenPlaylist(t, db, mediaslot.Background, unowned)

	restoreOpenPlaylists(svc, nil)

	assert.Nil(t, svc.State.GetActivePlaylist(), "a playlist whose deck was deleted stays closed")
	got := svc.State.GetBackgroundPlaylist()
	require.NotNil(t, got)
	assert.True(t, got.Unsafe, "a deck this device does not own is untrusted")
}

func TestHandlePlaylist_CompletedClearStartsOverNextTime(t *testing.T) {
	t.Parallel()
	svc, _ := setupPlaylistMemoryEnv(t)

	pls := makeServicePlaylist()
	svc.PlaylistMemory.Prepare(pls, 0)
	handlePlaylist(svc, playlists.Goto(*pls, 2), nil)
	handlePlaylist(svc, &playlists.Playlist{ID: "id", Clear: true}, nil)

	reloaded := makeServicePlaylist()
	svc.PlaylistMemory.Prepare(reloaded, 0)
	assert.Equal(t, 2, reloaded.Index, "a playlist that was stopped keeps its place")

	handlePlaylist(svc, playlists.Goto(*reloaded, 2), nil)
	handlePlaylist(svc, &playlists.Playlist{ID: "id", Clear: true, Completed: true}, nil)

	reloaded = makeServicePlaylist()
	svc.PlaylistMemory.Prepare(reloaded, 0)
	assert.Equal(t, 0, reloaded.Index, "a playlist that played to its end starts over")
}

func TestPlaylistMemory_DirectBackgroundClearClosesOpenPlaylist(t *testing.T) {
	t.Parallel()
	svc, db := setupPlaylistMemoryEnv(t)

	pls := makeServicePlaylist()
	pls.Slot = mediaslot.Background
	handlePlaylist(svc, pls, nil)
	svc.PlaylistMemory.Flush(context.Background())
	rows, err := db.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// media.control stop on the background slot clears state directly.
	svc.State.SetBackgroundPlaylist(nil)
	svc.PlaylistMemory.Flush(context.Background())
	rows, err = db.ListOpenPlaylists()
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestPlaylistMemory_FailurePauseIsRecorded(t *testing.T) {
	t.Parallel()
	svc, db := setupPlaylistMemoryEnv(t)

	pls := playlists.Play(*makeServicePlaylist())
	svc.State.SetActivePlaylist(pls)
	require.True(t, svc.State.PausePlaylistIfCurrent("", pls))
	svc.PlaylistMemory.Flush(context.Background())

	rows, err := db.ListOpenPlaylists()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, mediaslot.Primary, rows[0].Slot)
}

// deckErrorUserDB fails every deck lookup, as a busy or damaged database would.
type deckErrorUserDB struct {
	*userdb.UserDB
}

func (deckErrorUserDB) GetDeck(string) (*database.Deck, error) {
	return nil, errors.New("database is locked")
}

func TestRestoreOpenPlaylists_UncheckableDeckIsUntrusted(t *testing.T) {
	t.Parallel()
	svc, db := setupPlaylistMemoryEnv(t)
	svc.DB = &database.Database{UserDB: deckErrorUserDB{UserDB: db}}

	pls := makeServicePlaylist()
	pls.DeckID = "aaaaaaaaaaaa"
	recordOpenPlaylist(t, db, mediaslot.Primary, pls)

	restoreOpenPlaylists(svc, nil)

	got := svc.State.GetActivePlaylist()
	require.NotNil(t, got, "a deck that cannot be checked is still restored")
	assert.True(t, got.Unsafe, "but untrusted, since its ownership was not confirmed")
}
