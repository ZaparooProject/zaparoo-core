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

package decks_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeckURI(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "deck://0123456789ab", decks.DeckURI("0123456789ab"))

	for raw, want := range map[string]string{
		"deck://0123456789ab":   "0123456789ab",
		"DECK://0123456789AB":   "0123456789ab",
		"deck://ZZZZZZZZ":       "zzzzzzzz",
		"deck://0123456789aI":   "",
		"deck://":               "",
		"deckx://0123456789ab":  "",
		"/roms/deck/playlist.m": "",
		"0123456789ab":          "",
	} {
		got, ok := decks.ParseDeckURI(raw)
		assert.Equal(t, want != "", ok, raw)
		assert.Equal(t, want, got, raw)
	}
}

// The links a deck is reached through. One is a real link on the hosted
// service and the other belongs to nobody in particular: nothing about a host
// or a link's shape may decide how its playlist is treated.
const (
	hostedDeckLink     = "https://zpr.au/d$lhm6n9t8"
	thirdPartyDeckLink = "https://decks.example/shelf/anything/at/all?ref=1"
)

func servedPlaylist(t *testing.T, cmdName, id string, items ...decks.PlaylistArgItem) string {
	t.Helper()
	encoded, err := json.Marshal(decks.PlaylistArg{ID: id, Name: "Served", Items: items})
	require.NoError(t, err)
	return "**" + cmdName + ":" + string(encoded)
}

func TestParseServedPlaylist(t *testing.T) {
	t.Parallel()
	item := decks.PlaylistArgItem{Name: "A", ZapScript: "**launch.system:SNES"}

	// The body the hosted service serves for a deck, as fetched.
	hosted := `**playlist.open:{"id":"ZON-LHM6N9T8","name":"My Favourites","items":[` +
		`{"name":"Gunstar Heroes","zapscript":"@Genesis/Gunstar Heroes (year:1993)"}]}`
	cmd, arg, ok := decks.ParseServedPlaylist(hosted)
	require.True(t, ok)
	assert.Equal(t, zapscript.ZapScriptCmdPlaylistOpen, cmd.Name)
	assert.Equal(t, "ZON-LHM6N9T8", arg.ID, "the served ID is kept as it is, whatever it looks like")
	assert.Equal(t, "My Favourites", arg.Name)
	require.Len(t, arg.Items, 1)

	for _, id := range []string{"party-list", "", "deck://0123456789ab"} {
		_, arg, ok = decks.ParseServedPlaylist(servedPlaylist(t, zapscript.ZapScriptCmdPlaylistOpen, id, item))
		require.True(t, ok, "a playlist is kept whatever it calls itself: %q", id)
		assert.Equal(t, id, arg.ID)
	}
	for _, name := range []string{zapscript.ZapScriptCmdPlaylistPlay, zapscript.ZapScriptCmdPlaylistLoad} {
		cmd, _, ok = decks.ParseServedPlaylist(servedPlaylist(t, name, "p", item) + "?mode=shuffle")
		require.True(t, ok, name)
		assert.Equal(t, name, cmd.Name, "the command is returned as served")
		assert.Equal(t, "shuffle", cmd.AdvArgs.Get(zapscript.KeyMode))
	}

	for name, body := range map[string]string{
		"not a playlist":       "**launch.system:SNES",
		"more than a playlist": servedPlaylist(t, zapscript.ZapScriptCmdPlaylistOpen, "p", item) + "||**stop",
		"a playlist file":      "**playlist.open:/roms/list.pls",
		"a local deck":         "**playlist.open:deck://0123456789ab",
		"nothing to run": servedPlaylist(t, zapscript.ZapScriptCmdPlaylistOpen, "p",
			decks.PlaylistArgItem{Name: "Empty", ZapScript: "  "}),
		"no items":         servedPlaylist(t, zapscript.ZapScriptCmdPlaylistOpen, "p"),
		"another command":  "**playlist.goto:" + `{"id":"p","items":[{"zapscript":"**stop"}]}`,
		"unparseable json": "**playlist.open:{not json",
	} {
		_, _, ok = decks.ParseServedPlaylist(body)
		assert.False(t, ok, name)
	}
}

// Every host's playlist is kept the same way, and what it is kept under comes
// from this device alone.
func TestStoreFetchedDeck(t *testing.T) {
	t.Parallel()
	for name, link := range map[string]string{"hosted": hostedDeckLink, "third party": thirdPartyDeckLink} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newProjectFixture(t)
			nested := servedPlaylist(t, zapscript.ZapScriptCmdPlaylistOpen, "card1234",
				decks.PlaylistArgItem{ZapScript: "**a"}, decks.PlaylistArgItem{ZapScript: "**b"})
			arg := decks.PlaylistArg{ID: "party-list", Name: " Theirs ", Items: []decks.PlaylistArgItem{
				{Name: "Game", ZapScript: "**launch.title:SNES/Game"},
				{Name: "Card", ZapScript: nested},
				{Name: "Empty", ZapScript: "  "},
			}}
			deckID, err := decks.StoreFetchedDeck(f.db, link, &arg)
			require.NoError(t, err)
			assert.True(t, database.IsMintedDeckID(deckID), "the deck ID is minted here, not taken from the link")

			stored, err := f.userDB.GetDeck(deckID)
			require.NoError(t, err)
			assert.False(t, stored.Owned)
			assert.Equal(t, "Theirs", stored.Name)
			assert.Equal(t, link, stored.SourceURL)
			assert.Equal(t, "party-list", stored.PlaylistID)
			assert.Equal(t, "party-list", decks.PlaylistID(stored), "the copy opens as the playlist it was served as")
			require.Len(t, stored.Items, 2, "entries with no script are dropped")
			assert.Equal(t, nested, stored.Items[1].ZapScript, "a multi-script card is kept as its nested playlist")
			assert.Equal(t, []string{deckID}, f.tagQueue.queued(),
				"a newly kept deck is queued so its tags and file links follow")

			again, err := decks.StoreFetchedDeck(f.db, link, &arg)
			require.NoError(t, err)
			assert.Equal(t, deckID, again, "the same link is the same deck")
		})
	}
}

// A served playlist can never stand in for, or be hidden by, a deck the user
// owns, whatever ID it serves or link it sits at.
func TestStoreFetchedDeckNeverTouchesAnOwnedDeck(t *testing.T) {
	t.Parallel()
	f := newProjectFixture(t)
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Mine", Owned: true}))
	mine, err := f.userDB.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)

	arg := decks.PlaylistArg{ID: decks.PlaylistID(mine), Name: "Theirs", Items: []decks.PlaylistArgItem{
		{Name: "Game", ZapScript: "**launch.system:SNES"},
	}}
	deckID, err := decks.StoreFetchedDeck(f.db, "https://decks.example/daaaaaaaaaaaa", &arg)
	require.NoError(t, err)
	assert.NotEqual(t, "aaaaaaaaaaaa", deckID)

	mine, err = f.userDB.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine", mine.Name)
	assert.True(t, mine.Owned)
	assert.Empty(t, mine.Items)
	theirs, err := f.userDB.GetDeck(deckID)
	require.NoError(t, err)
	assert.Equal(t, "Theirs", theirs.Name)
	assert.False(t, theirs.Owned)
}

func TestPlaylistID(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "deck://0123456789ab", decks.PlaylistID(&database.Deck{DeckID: "0123456789AB"}),
		"a deck made on this device opens as its own URI")
	kept := &database.Deck{DeckID: "0123456789ab", PlaylistID: "ZON-LHM6N9T8"}
	assert.Equal(t, "ZON-LHM6N9T8", decks.PlaylistID(kept), "a deck kept from a link opens as it was served")
}

// A deck that is fetched again keeps the rows of the items it still holds, so
// the file each one is linked to on this device survives the refresh and the
// deck goes on launching the files the user has. Only a fetch that brings
// something new costs a write.
func TestStoreFetchedDeckKeepsLocalLinksAcrossRefresh(t *testing.T) {
	t.Parallel()
	present := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	f := newProjectFixture(t, present)
	const src = hostedDeckLink

	arg := decks.PlaylistArg{ID: "ZON-0123456789AB", Name: "Theirs", Items: []decks.PlaylistArgItem{
		{Name: "Metroid", ZapScript: decks.TitleLaunchScript("NES", "Metroid", nil)},
		{Name: "Other", ZapScript: "**launch.system:NES"},
	}}
	deckID, err := decks.StoreFetchedDeck(f.db, src, &arg)
	require.NoError(t, err)

	// The tagger links the item to the file this device matched.
	relinked, err := decks.ProjectDeck(f.ctx, f.deps, deckID)
	require.NoError(t, err)
	require.Equal(t, 1, relinked)
	linked, err := f.userDB.GetDeck(deckID)
	require.NoError(t, err)
	require.Equal(t, present, linked.Items[0].Anchor.Path)
	itemID := linked.Items[0].DBID
	launch := decks.PlaylistItems(f.ctx, f.mediaDB, linked)
	require.Len(t, launch, 2)
	require.Contains(t, launch[0].ZapScript, present, "the linked file is what plays")

	// The same deck served again changes nothing but the fetch time.
	before := preferencesRevision(t, f)
	_, err = decks.StoreFetchedDeck(f.db, src, &arg)
	require.NoError(t, err)
	same, err := f.userDB.GetDeck(deckID)
	require.NoError(t, err)
	assert.Equal(t, itemID, same.Items[0].DBID, "an unchanged item keeps its row")
	assert.Equal(t, present, same.Items[0].Anchor.Path, "an unchanged item keeps its local file")
	assert.GreaterOrEqual(t, same.FetchedAt, linked.FetchedAt, "the deck is still recorded as fetched")
	assert.Equal(t, before, preferencesRevision(t, f), "a fetch that changes nothing invalidates no browse cursor")
	assert.Equal(t, []string{deckID}, f.tagQueue.queued(), "an unchanged deck is not queued again")

	// A deck that reorders and adds an item keeps the links of the items it
	// still holds, and is queued because it changed.
	arg.Items = []decks.PlaylistArgItem{
		{Name: "Other", ZapScript: "**launch.system:NES"},
		{Name: "New", ZapScript: "**launch.system:SNES"},
		{Name: "Metroid", ZapScript: decks.TitleLaunchScript("NES", "Metroid", nil)},
	}
	_, err = decks.StoreFetchedDeck(f.db, src, &arg)
	require.NoError(t, err)
	moved, err := f.userDB.GetDeck(deckID)
	require.NoError(t, err)
	require.Len(t, moved.Items, 3)
	assert.Equal(t, itemID, moved.Items[2].DBID, "a reordered item keeps its row")
	assert.Equal(t, present, moved.Items[2].Anchor.Path, "a reordered item keeps its local file")
	assert.Equal(t, []string{deckID, deckID}, f.tagQueue.queued(), "a deck that changed is queued again")

	// An item the source dropped takes its row and link with it.
	arg.Items = []decks.PlaylistArgItem{{Name: "Other", ZapScript: "**launch.system:NES"}}
	_, err = decks.StoreFetchedDeck(f.db, src, &arg)
	require.NoError(t, err)
	shrunk, err := f.userDB.GetDeck(deckID)
	require.NoError(t, err)
	require.Len(t, shrunk.Items, 1)
	assert.Equal(t, "Other", shrunk.Items[0].Name)
}

func TestPlaylistItems(t *testing.T) {
	t.Parallel()
	present := filepath.ToSlash(filepath.Join("roms", "NES", "Metroid (USA).nes"))
	f := newProjectFixture(t, present)

	anchored, err := decks.ComposeMediaItem(f.ctx, f.mediaDB, "NES", present)
	require.NoError(t, err)
	gone := database.DeckItem{
		Kind: database.DeckItemKindScript, Name: "Gone", ZapScript: "**launch.title:NES/Gone",
		Anchor: database.DeckItemAnchor{SystemID: "NES", Path: "roms/NES/Gone.nes"},
	}
	deck := &database.Deck{DeckID: "0123456789ab", Items: []database.DeckItem{
		anchored,
		gone,
		{
			Kind: database.DeckItemKindCard, Name: "One", CardID: "card0001",
			Scripts: []database.DeckCardScript{{ZapScript: "**launch.system:NES"}},
		},
		{
			Kind: database.DeckItemKindCard, Name: "Many", CardID: "card0002",
			Scripts: []database.DeckCardScript{{Name: "a", ZapScript: "**a"}, {Name: "b", ZapScript: "**b"}},
		},
		{Kind: database.DeckItemKindCard, Name: "None", CardID: "card0003"},
	}}

	items := decks.PlaylistItems(f.ctx, f.mediaDB, deck)
	require.Len(t, items, 4, "a card with no scripts has nothing to play")

	launch, err := zapscript.NewParser(items[0].ZapScript).ParseScript()
	require.NoError(t, err)
	require.Len(t, launch.Cmds, 1)
	assert.Equal(t, zapscript.ZapScriptCmdLaunch, launch.Cmds[0].Name, "a present linked file launches by path")
	assert.Equal(t, []string{present}, launch.Cmds[0].Args)
	assert.Equal(t, "NES", launch.Cmds[0].AdvArgs.Get(zapscript.KeySystem))

	assert.Equal(t, "**launch.title:NES/Gone", items[1].ZapScript, "a missing linked file falls back to the title")
	assert.Equal(t, "**launch.system:NES", items[2].ZapScript)

	nested, err := zapscript.NewParser(items[3].ZapScript).ParseScript()
	require.NoError(t, err)
	require.Len(t, nested.Cmds, 1)
	assert.Equal(t, zapscript.ZapScriptCmdPlaylistOpen, nested.Cmds[0].Name)
	var arg decks.PlaylistArg
	require.NoError(t, json.Unmarshal([]byte(nested.Cmds[0].Args[0]), &arg))
	assert.Equal(t, "deck://0123456789ab/card0002", arg.ID)
	require.Len(t, arg.Items, 2)
	assert.Equal(t, "**b", arg.Items[1].ZapScript)
}
