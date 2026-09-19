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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
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

// The links a deck is reached through in these tests. One is a real link on
// the hosted service and the other belongs to nobody in particular: nothing
// about a host or a link's shape may decide how its playlist is treated.
const (
	hostedDeckLink     = "https://zpr.au/d$lhm6n9t8"
	thirdPartyDeckLink = "https://decks.example/shelf/anything/at/all?ref=1"
)

func servedDeckBody(t *testing.T, id, name string, items ...decks.PlaylistArgItem) string {
	t.Helper()
	encoded, err := json.Marshal(decks.PlaylistArg{ID: id, Name: name, Items: items})
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
	assert.Equal(t, "deck://0123456789ab", pls.ID, "a deck made here opens as its own URI")
	assert.Equal(t, "0123456789ab", pls.DeckID)
	assert.Equal(t, "Weekend", pls.Name)
	require.Len(t, pls.Items, 2)
	assert.Equal(t, "**launch.system:NES", pls.Items[0].ZapScript)
	assert.Equal(t, int32(1), refreshed.Load(), "an owned deck asks the sync hook for a background refresh")
	assert.False(t, pls.Unsafe, "the user's own deck is trusted")
}

func TestCmdPlaylistLoad_NotOwnedDeckIsUntrusted(t *testing.T) {
	t.Parallel()
	env, db, queue := deckTestEnv(t, "deck://0123456789ab")
	_, err := db.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Theirs", Owned: false,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**input.keyboard:a"}},
	})
	require.NoError(t, err)

	_, err = cmdPlaylistLoad(newPlaylistTestPlatform(), env)
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

// Any link that serves a playlist is kept as a deck, whoever hosts it and
// whatever the link or the playlist's ID look like.
func TestAdoptZapLinkDeck(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ link, playlistID string }{
		"hosted":      {link: hostedDeckLink, playlistID: "ZON-LHM6N9T8"},
		"third party": {link: thirdPartyDeckLink, playlistID: "party-list"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, cleanup := testhelpers.NewTestDatabase(t)
			t.Cleanup(cleanup)
			body := servedDeckBody(t, tc.playlistID, "Theirs",
				decks.PlaylistArgItem{Name: "Game", ZapScript: "**launch.title:SNES/Game"})

			rewritten := adoptZapLinkDeck(db, tc.link, body)
			all, err := db.UserDB.ListDecks()
			require.NoError(t, err)
			require.Len(t, all, 1)
			parsed, err := zapscript.NewParser(rewritten).ParseScript()
			require.NoError(t, err)
			require.Len(t, parsed.Cmds, 1)
			assert.Equal(t, zapscript.ZapScriptCmdPlaylistOpen, parsed.Cmds[0].Name)
			assert.Equal(t, []string{decks.DeckURI(all[0].DeckID)}, parsed.Cmds[0].Args)
			stored, err := db.UserDB.GetDeck(all[0].DeckID)
			require.NoError(t, err)
			assert.False(t, stored.Owned)
			assert.Equal(t, tc.link, stored.SourceURL)
			assert.Equal(t, tc.playlistID, decks.PlaylistID(stored), "the copy opens as the playlist it was served as")
			require.Len(t, stored.Items, 1)

			assert.Equal(t, rewritten, adoptZapLinkDeck(db, tc.link, body), "the same link is the same deck")
			all, err = db.UserDB.ListDecks()
			require.NoError(t, err)
			assert.Len(t, all, 1)
		})
	}
}

func TestAdoptZapLinkDeck_KeepsTheServedCommand(t *testing.T) {
	t.Parallel()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	encoded, err := json.Marshal(decks.PlaylistArg{ID: "p", Name: "Mix", Items: []decks.PlaylistArgItem{
		{Name: "A", ZapScript: "**launch.system:SNES"},
	}})
	require.NoError(t, err)

	rewritten := adoptZapLinkDeck(db, thirdPartyDeckLink, "**playlist.play:"+string(encoded)+"?mode=shuffle")
	parsed, err := zapscript.NewParser(rewritten).ParseScript()
	require.NoError(t, err)
	require.Len(t, parsed.Cmds, 1)
	assert.Equal(t, zapscript.ZapScriptCmdPlaylistPlay, parsed.Cmds[0].Name, "a playlist served to play still plays")
	assert.Equal(t, "shuffle", parsed.Cmds[0].AdvArgs.Get(zapscript.KeyMode))
	require.Len(t, parsed.Cmds[0].Args, 1)
	_, isDeck := decks.ParseDeckURI(parsed.Cmds[0].Args[0])
	assert.True(t, isDeck)
}

func TestAdoptZapLinkDeck_OtherBodiesPassThrough(t *testing.T) {
	t.Parallel()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	for _, body := range []string{
		"**launch.system:SNES",
		"**playlist.open:/roms/list.pls",
		servedDeckBody(t, "p", "Two") + "||**stop",
		servedDeckBody(t, "p", "Nothing to run"),
	} {
		assert.Equal(t, body, adoptZapLinkDeck(db, hostedDeckLink, body))
	}
	all, err := db.UserDB.ListDecks()
	require.NoError(t, err)
	assert.Empty(t, all)
}

// A served playlist never reaches a deck the user owns, whatever the link or
// the playlist calls itself: it is kept as its own read-only copy.
func TestAdoptZapLinkDeck_NeverResolvesToAnOwnedDeck(t *testing.T) {
	t.Parallel()
	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Mine", Owned: true}))

	body := servedDeckBody(t, "deck://aaaaaaaaaaaa", "Imposter",
		decks.PlaylistArgItem{Name: "A", ZapScript: "**input.keyboard:a"})
	rewritten := adoptZapLinkDeck(db, "https://decks.example/daaaaaaaaaaaa", body)
	assert.NotContains(t, rewritten, "aaaaaaaaaaaa")

	mine, err := db.UserDB.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine", mine.Name)
	assert.Empty(t, mine.Items)
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

	serve.Store(servedDeckBody(t, "party-list", "New",
		decks.PlaylistArgItem{Name: "B", ZapScript: "**b"}, decks.PlaylistArgItem{Name: "C", ZapScript: "**c"}))
	refreshed := refreshDeck(newPlaylistTestPlatform(), &env, stale)
	require.NotNil(t, refreshed)
	assert.Equal(t, "New", refreshed.Name)
	require.Len(t, refreshed.Items, 2)
	cached, err := db.UserDB.GetZapLinkCache(server.URL)
	require.NoError(t, err)
	assert.Contains(t, cached, `"party-list"`, "the link cache follows the refresh for offline taps")
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
		_, _ = w.Write([]byte(servedDeckBody(t, "party-list", "Theirs",
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

// deckZapLinkTransport answers a deck ZapLink: the well-known probe that
// marks the host as serving ZapScript, then the deck body itself.
type deckZapLinkTransport struct{ body string }

func (t deckZapLinkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	if strings.HasSuffix(req.URL.Path, WellKnownPath) {
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Header: header,
			Body: io.NopCloser(strings.NewReader(`{"zapscript":1}`)),
		}, nil
	}
	header.Set("Content-Type", MIMEZaparooZapScript)
	return &http.Response{
		StatusCode: http.StatusOK, Header: header,
		Body: io.NopCloser(strings.NewReader(t.body)),
	}, nil
}

func useDeckZapLinkTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	oldWellKnown, oldZap := currentWellKnownFetchClient(), currentZapFetchClient()
	t.Cleanup(func() {
		zapFetchClientMu.Lock()
		wellKnownFetchClient, zapFetchClient = oldWellKnown, oldZap
		zapFetchClientMu.Unlock()
	})
	zapFetchClientMu.Lock()
	wellKnownFetchClient = newWellKnownFetchClient(transport)
	zapFetchClient = newZapFetchClient(transport)
	zapFetchClientMu.Unlock()
}

// TestRunCommand_DeckZapLinkIsKeptFromAnyHost pins the whole tap: a link on
// the hosted service, in the shape it really has, and a link on somebody
// else's host both end up as a read-only deck that opens untrusted.
func TestRunCommand_DeckZapLinkIsKeptFromAnyHost(t *testing.T) {
	for name, tc := range map[string]struct{ link, playlistID string }{
		"hosted":      {link: hostedDeckLink, playlistID: "ZON-LHM6N9T8"},
		"third party": {link: thirdPartyDeckLink, playlistID: "party-list"},
	} {
		t.Run(name, func(t *testing.T) {
			useDeckZapLinkTransport(t, deckZapLinkTransport{body: servedDeckBody(t, tc.playlistID, "Theirs",
				decks.PlaylistArgItem{Name: "A", ZapScript: "**input.keyboard:a"})})
			db, cleanup := testhelpers.NewTestDatabase(t)
			t.Cleanup(cleanup)

			queue := make(chan *playlists.Playlist, 1)
			result, err := RunCommand(
				t.Context(), newPlaylistTestPlatform(), &config.Instance{},
				playlists.PlaylistController{Queue: queue},
				tokens.Token{Text: tc.link},
				zapscript.Command{Name: zapscript.ZapScriptCmdLaunch, Args: []string{tc.link}},
				1, 0, db, &RunCommandOptions{}, &zapscript.ArgExprEnv{},
			)
			require.NoError(t, err)
			assert.True(t, result.Unsafe, "a fetched ZapLink flags the token unsafe")

			all, err := db.UserDB.ListDecks()
			require.NoError(t, err)
			require.Len(t, all, 1, "the served playlist is kept as a deck")
			assert.False(t, all[0].Owned)
			assert.Equal(t, tc.link, all[0].SourceURL)

			pls := <-queue
			assert.Equal(t, tc.playlistID, pls.ID, "the copy opens as the playlist it was served as")
			assert.Equal(t, all[0].DeckID, pls.DeckID)
			assert.True(t, pls.Unsafe, "somebody else's deck runs its items untrusted")
		})
	}
}

// TestRunCommand_DeckZapLinkNeverOpensAnOwnedDeck pins that nothing a link
// serves can steer a tap into a trusted open of a deck the user owns. Only
// the account vouching for the link makes it trusted, and that is decided by
// who answered, never by what the link or the playlist is called.
func TestRunCommand_DeckZapLinkNeverOpensAnOwnedDeck(t *testing.T) {
	const deckID = "0123456789ab"
	link := "https://decks.example/d" + deckID
	useDeckZapLinkTransport(t, deckZapLinkTransport{body: servedDeckBody(t, "deck://"+deckID, "Imposter",
		decks.PlaylistArgItem{Name: "A", ZapScript: "**input.keyboard:a"})})

	db, cleanup := testhelpers.NewTestDatabase(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.UserDB.CreateDeck(&database.Deck{
		DeckID: deckID, Name: "Mine", Owned: true,
		Items: []database.DeckItem{
			{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**input.keyboard:a"},
		},
	}))

	queue := make(chan *playlists.Playlist, 1)
	_, err := RunCommand(
		t.Context(), newPlaylistTestPlatform(), &config.Instance{},
		playlists.PlaylistController{Queue: queue},
		tokens.Token{Text: link},
		zapscript.Command{Name: zapscript.ZapScriptCmdLaunch, Args: []string{link}},
		1, 0, db, &RunCommandOptions{}, &zapscript.ArgExprEnv{},
	)
	require.NoError(t, err)

	pls := <-queue
	assert.NotEqual(t, deckID, pls.DeckID, "the link does not resolve to the owned deck")
	assert.Equal(t, "Imposter", pls.Name)
	assert.True(t, pls.Unsafe)
}
