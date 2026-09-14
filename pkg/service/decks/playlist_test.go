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
	assert.Equal(t, "ZON-0123456789ab", decks.PlaylistID("0123456789AB"))

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

func TestDeckIDFromZapLinkURL(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"https://zpr.au/d0123456789ab":           "0123456789ab",
		"https://zpr.au/DABCDEFGH":               "abcdefgh",
		"https://edge.zaparoo.com/d0123456789ab": "0123456789ab",
		"https://zpr.au/c0123456789ab":           "",
		"https://zpr.au/0123456789ab":            "",
		"https://zpr.au/d0123456789ab/extra":     "",
		"http://zpr.au/d0123456789ab":            "",
		"https://example.com/d0123456789ab":      "",
		"https://zpr.au/dIIIIIIIIIII":            "",
		"not a url %%":                           "",
	} {
		got, ok := decks.DeckIDFromZapLinkURL(raw)
		assert.Equal(t, want != "", ok, raw)
		assert.Equal(t, want, got, raw)
	}
}

func servedDeck(t *testing.T, id string, items ...decks.PlaylistArgItem) string {
	t.Helper()
	encoded, err := json.Marshal(decks.PlaylistArg{ID: "ZON-" + id, Name: "Served", Items: items})
	require.NoError(t, err)
	return "**playlist.open:" + string(encoded)
}

func TestParseDeckPlaylist(t *testing.T) {
	t.Parallel()
	body := servedDeck(t, "0123456789AB", decks.PlaylistArgItem{Name: "A", ZapScript: "**launch.system:SNES"})
	arg, ok := decks.ParseDeckPlaylist(body, "0123456789ab")
	require.True(t, ok, "the served ID matches regardless of case")
	assert.Equal(t, "Served", arg.Name)
	require.Len(t, arg.Items, 1)

	_, ok = decks.ParseDeckPlaylist(body, "zzzzzzzzzzzz")
	assert.False(t, ok, "a playlist for another ID is not this deck")
	_, ok = decks.ParseDeckPlaylist("**launch.system:SNES", "0123456789ab")
	assert.False(t, ok)
	_, ok = decks.ParseDeckPlaylist(body+"||**stop", "0123456789ab")
	assert.False(t, ok, "a body with more than the playlist is not a served deck")
	_, ok = decks.ParseDeckPlaylist("**playlist.open:/roms/list.pls", "0123456789ab")
	assert.False(t, ok)
}

func TestStoreFetchedDeck(t *testing.T) {
	t.Parallel()
	f := newProjectFixture(t)

	nested := servedDeck(t, "card1234",
		decks.PlaylistArgItem{ZapScript: "**a"}, decks.PlaylistArgItem{ZapScript: "**b"})
	arg := decks.PlaylistArg{ID: "ZON-0123456789AB", Name: " Theirs ", Items: []decks.PlaylistArgItem{
		{Name: "Game", ZapScript: "**launch.title:SNES/Game"},
		{Name: "Card", ZapScript: nested},
		{Name: "Empty", ZapScript: "  "},
	}}
	require.NoError(t, decks.StoreFetchedDeck(f.userDB, "https://zpr.au/d0123456789ab", "0123456789ab", &arg))
	stored, err := f.userDB.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.False(t, stored.Owned)
	assert.Equal(t, "Theirs", stored.Name)
	assert.Equal(t, "https://zpr.au/d0123456789ab", stored.SourceURL)
	require.Len(t, stored.Items, 2, "entries with no script are dropped")
	assert.Equal(t, nested, stored.Items[1].ZapScript, "a multi-script card is kept as its nested playlist")

	// An owned deck of the same ID is left alone without an error.
	require.NoError(t, f.userDB.CreateDeck(&database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Mine", Owned: true}))
	require.NoError(t, decks.StoreFetchedDeck(f.userDB, "https://zpr.au/daaaaaaaaaaaa", "aaaaaaaaaaaa", &arg))
	mine, err := f.userDB.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine", mine.Name)
	assert.True(t, mine.Owned)
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
	assert.Equal(t, "ZON-card0002", arg.ID)
	require.Len(t, arg.Items, 2)
	assert.Equal(t, "**b", arg.Items[1].ZapScript)
}
