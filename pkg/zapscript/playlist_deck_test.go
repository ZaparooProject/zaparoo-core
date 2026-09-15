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

package zapscript

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deckTestEnv(t *testing.T, arg string) (platforms.CmdEnv, *database.Database, chan *playlists.Playlist) {
	t.Helper()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	queue := make(chan *playlists.Playlist, 1)
	return platforms.CmdEnv{
		ServiceCtx: t.Context(),
		Cmd:        zapscript.Command{Name: zapscript.ZapScriptCmdPlaylistLoad, Args: []string{arg}},
		Cfg:        &config.Instance{},
		Database:   db,
		Playlist:   playlists.PlaylistController{Queue: queue},
	}, db, queue
}

func servedDeckBody(t *testing.T, id, name string, items ...decks.PlaylistArgItem) string {
	t.Helper()
	encoded, err := json.Marshal(decks.PlaylistArg{ID: "ZON-" + id, Name: name, Items: items})
	require.NoError(t, err)
	return "**playlist.open:" + string(encoded)
}

func TestCmdPlaylistLoad_Deck(t *testing.T) {
	t.Parallel()
	env, db, queue := deckTestEnv(t, "deck://0123456789AB")
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Weekend", Owned: true,
		Items: []database.DeckItem{
			{Kind: database.DeckItemKindScript, Name: "First", ZapScript: "**launch.system:NES"},
			{Kind: database.DeckItemKindScript, Name: "Second", ZapScript: "**launch.title:NES/Metroid"},
		},
	}))
	var refreshed atomic.Int32
	env.RefreshOwnedDeck = func(_ context.Context, deckID string) {
		assert.Equal(t, "0123456789ab", deckID)
		refreshed.Add(1)
	}

	result, err := cmdPlaylistLoad(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	assert.True(t, result.PlaylistChanged)
	pls := <-queue
	assert.Equal(t, "ZON-0123456789ab", pls.ID, "a deck opens under the playlist ID its link uses")
	assert.Equal(t, "Weekend", pls.Name)
	require.Len(t, pls.Items, 2)
	assert.Equal(t, "**launch.system:NES", pls.Items[0].ZapScript)
	assert.Equal(t, int32(1), refreshed.Load(), "an owned deck asks the sync hook for a background refresh")
	assert.False(t, pls.Unsafe, "the user's own deck is trusted")
}

func TestCmdPlaylistLoad_NotOwnedDeckIsUntrusted(t *testing.T) {
	t.Parallel()
	env, db, queue := deckTestEnv(t, "deck://0123456789ab")
	require.NoError(t, db.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Theirs", Owned: false,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**input.keyboard:a"}},
	}))

	_, err := cmdPlaylistLoad(newPlaylistTestPlatform(), env)
	require.NoError(t, err)
	pls := <-queue
	assert.True(t, pls.Unsafe, "a cached copy of somebody else's deck runs its items untrusted")
}

func TestCmdPlaylistLoad_DeckNotFound(t *testing.T) {
	t.Parallel()
	env, _, _ := deckTestEnv(t, "deck://0123456789ab")
	_, err := cmdPlaylistLoad(newPlaylistTestPlatform(), env)
	require.ErrorIs(t, err, ErrDeckNotFound)
}

func TestAdoptZapLinkDeck(t *testing.T) {
	t.Parallel()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	body := servedDeckBody(t, "0123456789AB", "Theirs",
		decks.PlaylistArgItem{Name: "Game", ZapScript: "**launch.title:SNES/Game"})

	rewritten := adoptZapLinkDeck(db, "https://zpr.au/d0123456789ab", body)
	assert.Equal(t, "**playlist.open:deck://0123456789ab", rewritten)
	stored, err := db.UserDB.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.False(t, stored.Owned)
	assert.Equal(t, "https://zpr.au/d0123456789ab", stored.SourceURL)
	require.Len(t, stored.Items, 1)

	// Anything else passes through untouched.
	assert.Equal(t, body, adoptZapLinkDeck(db, "https://zpr.au/c0123456789ab", body), "a card link is not a deck")
	assert.Equal(t, "**launch.system:SNES",
		adoptZapLinkDeck(db, "https://zpr.au/d0123456789ab", "**launch.system:SNES"))
	other := servedDeckBody(t, "zzzzzzzzzzzz", "Other")
	assert.Equal(t, other, adoptZapLinkDeck(db, "https://zpr.au/d0123456789ab", other),
		"a served playlist for another ID is not this deck")

	// A deck this device owns keeps its own copy and still opens locally.
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Mine", Owned: true}))
	mineBody := servedDeckBody(t, "AAAAAAAAAAAA", "Server copy")
	assert.Equal(t, "**playlist.open:deck://aaaaaaaaaaaa",
		adoptZapLinkDeck(db, "https://zpr.au/daaaaaaaaaaaa", mineBody))
	mine, err := db.UserDB.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine", mine.Name)
}

// A cached copy of somebody else's deck is fetched again before it opens,
// and opens as cached when the fetch fails.
func TestRefreshDeck_CachedCopy(t *testing.T) {
	t.Parallel()
	var serve atomic.Value
	serve.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body, ok := serve.Load().(string)
		if !ok || body == "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", MIMEZaparooZapScript)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	env, db, _ := deckTestEnv(t, "deck://0123456789ab")
	// The source is fetched only when it is a host ZapLinks are served from,
	// the same gate the tap that cached the deck passed.
	require.NoError(t, db.UserDB.UpdateZapLinkHost(server.URL, 1))
	_, err := db.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Old", Owned: false, SourceURL: server.URL,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**a"}},
	})
	require.NoError(t, err)
	stale, err := db.UserDB.GetDeck("0123456789ab")
	require.NoError(t, err)

	fresh := *stale
	assert.Nil(t, refreshDeck(newPlaylistTestPlatform(), &env, &fresh),
		"a deck fetched moments ago is not fetched again")

	stale.FetchedAt = 0
	assert.Nil(t, refreshDeck(newPlaylistTestPlatform(), &env, stale), "a failed fetch opens the cached copy")

	serve.Store(servedDeckBody(t, "0123456789AB", "New",
		decks.PlaylistArgItem{Name: "B", ZapScript: "**b"}, decks.PlaylistArgItem{Name: "C", ZapScript: "**c"}))
	refreshed := refreshDeck(newPlaylistTestPlatform(), &env, stale)
	require.NotNil(t, refreshed)
	assert.Equal(t, "New", refreshed.Name)
	require.Len(t, refreshed.Items, 2)
	cached, err := db.UserDB.GetZapLinkCache(server.URL)
	require.NoError(t, err)
	assert.Contains(t, cached, `"ZON-0123456789AB"`, "the link cache follows the refresh for offline taps")
}

// A stored source that is not a ZapLink host is never fetched, so a user
// database holding an address from somewhere else cannot turn opening a deck
// into a request to it.
func TestRefreshDeck_SourceMustBeAZapLink(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", MIMEZaparooZapScript)
		_, _ = w.Write([]byte(servedDeckBody(t, "0123456789AB", "Theirs",
			decks.PlaylistArgItem{Name: "B", ZapScript: "**b"})))
	}))
	t.Cleanup(server.Close)

	env, db, _ := deckTestEnv(t, "deck://0123456789ab")
	_, err := db.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Old", Owned: false, SourceURL: server.URL,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**a"}},
	})
	require.NoError(t, err)
	stale, err := db.UserDB.GetDeck("0123456789ab")
	require.NoError(t, err)
	stale.FetchedAt = 0

	assert.Nil(t, refreshDeck(newPlaylistTestPlatform(), &env, stale), "the cached copy opens")
	assert.Zero(t, hits.Load(), "the unknown source is never fetched")
	unchanged, err := db.UserDB.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, "Old", unchanged.Name)
}
