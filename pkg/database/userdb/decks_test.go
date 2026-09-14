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

package userdb

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeck(id, name string, items ...database.DeckItem) *database.Deck {
	return &database.Deck{DeckID: id, Name: name, Owned: true, Items: items}
}

func scriptItem(name, script string) database.DeckItem {
	return database.DeckItem{Kind: database.DeckItemKindScript, Name: name, ZapScript: script}
}

func TestDeckCreateGetRoundTrip(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	anchorPath := filepath.Join("roms", "SNES", "Super Metroid (USA).sfc")
	deck := testDeck("0123456789ab", "Weekend", scriptItem("Super Metroid", "**launch.title:SNES/Super Metroid"),
		database.DeckItem{
			Kind: database.DeckItemKindCard, Name: "Card", CardID: "abcd1234",
			Scripts:  []database.DeckCardScript{{Name: "Play", ZapScript: "**launch.system:SNES"}},
			Metadata: json.RawMessage(`{"image_url":"https://x/y.png"}`),
		},
	)
	deck.Description = "Games for the weekend"
	deck.Metadata = json.RawMessage(`{"zaps":3}`)
	deck.Items[0].Anchor = database.DeckItemAnchor{
		SystemID: "SNES", Path: anchorPath, MediaName: "Super Metroid", Tags: []string{"region:us"},
	}
	require.NoError(t, db.CreateDeck(deck))
	assert.NotZero(t, deck.DBID)
	assert.NotZero(t, deck.CreatedAt)

	_, err := db.GetDeck("0123456789AB")
	require.ErrorIs(t, err, database.ErrDeckNotFound, "lookups are by the normalized (lower-case) ID")
	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, "Weekend", got.Name)
	assert.Equal(t, "Games for the weekend", got.Description)
	assert.True(t, got.Owned)
	assert.JSONEq(t, `{"zaps":3}`, string(got.Metadata))
	require.Len(t, got.Items, 2)
	assert.Equal(t, 1, got.Items[0].Position)
	assert.Equal(t, database.DeckItemKindScript, got.Items[0].Kind)
	assert.Equal(t, "**launch.title:SNES/Super Metroid", got.Items[0].ZapScript)
	assert.Equal(t, "SNES", got.Items[0].Anchor.SystemID)
	assert.Equal(t, filepath.ToSlash(anchorPath), got.Items[0].Anchor.Path)
	assert.Equal(t, []string{"region:us"}, got.Items[0].Anchor.Tags)
	assert.Equal(t, 2, got.Items[1].Position)
	assert.Equal(t, "abcd1234", got.Items[1].CardID)
	assert.Equal(t, []database.DeckCardScript{{Name: "Play", ZapScript: "**launch.system:SNES"}}, got.Items[1].Scripts)
	assert.JSONEq(t, `{"image_url":"https://x/y.png"}`, string(got.Items[1].Metadata))
	assert.False(t, got.Items[1].HasAnchor())

	list, err := db.ListDecks()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Nil(t, list[0].Items)
	assert.Equal(t, 2, list[0].ItemCount)
}

func TestDeckEditsAndDelete(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	deck := testDeck("0123456789ab", "First", scriptItem("A", "**a"), scriptItem("B", "**b"))
	deck.Metadata = json.RawMessage(`{"a":1}`)
	require.NoError(t, db.CreateDeck(deck))

	before, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)

	require.NoError(t, db.UpdateDeckMeta("0123456789ab", "Renamed", "desc", nil))
	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Name)
	assert.Equal(t, "desc", got.Description)
	assert.JSONEq(t, `{"a":1}`, string(got.Metadata), "nil metadata keeps the stored value")
	require.NoError(t, db.UpdateDeckMeta("0123456789ab", "Renamed", "desc", json.RawMessage(`{"a":2}`)))
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":2}`, string(got.Metadata))
	require.ErrorIs(t, db.UpdateDeckMeta("zzzzzzzzzzzz", "x", "", nil), database.ErrDeckNotFound)

	require.NoError(t, db.ReplaceDeckItems("0123456789ab",
		[]database.DeckItem{scriptItem("C", "**c"), scriptItem("A", "**a")}))
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
	assert.Equal(t, []int{1, 2}, []int{got.Items[0].Position, got.Items[1].Position})
	assert.Equal(t, "C", got.Items[0].Name)

	after, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	assert.NotEqual(t, before, after, "deck edits invalidate browse cursors")

	existed, err := db.DeleteDeck("0123456789ab")
	require.NoError(t, err)
	assert.True(t, existed)
	_, err = db.GetDeck("0123456789ab")
	require.ErrorIs(t, err, database.ErrDeckNotFound)
	var orphans int
	require.NoError(t, db.sql.Load().QueryRowContext(t.Context(), `select count(*) from DeckItems`).Scan(&orphans))
	assert.Zero(t, orphans, "items go with their deck")
	existed, err = db.DeleteDeck("0123456789ab")
	require.NoError(t, err)
	assert.False(t, existed)
}

func TestDeckCaps(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	tooMany := make([]database.DeckItem, database.DeckMaxItems+1)
	for i := range tooMany {
		tooMany[i] = scriptItem("x", "**x")
	}
	require.ErrorIs(t, db.CreateDeck(testDeck("0123456789ab", "Big", tooMany...)), database.ErrDeckItemLimit)
	require.NoError(t, db.CreateDeck(testDeck("0123456789ab", "Small")))
	require.ErrorIs(t, db.ReplaceDeckItems("0123456789ab", tooMany), database.ErrDeckItemLimit)

	for i := 1; i < database.DeckMaxLive; i++ {
		require.NoError(t, db.CreateDeck(testDeck(fmt.Sprintf("%012d", i), "Deck")))
	}
	count, err := db.CountOwnedDecks()
	require.NoError(t, err)
	assert.Equal(t, database.DeckMaxLive, count)
	require.ErrorIs(t, db.CreateDeck(testDeck("zzzzzzzzzzzz", "One too many")), database.ErrDeckLimit)

	// A cached copy of somebody else's deck does not count against the cap.
	remote := &database.Deck{DeckID: "yyyyyyyyyyyy", Name: "Theirs", Owned: false}
	require.NoError(t, db.UpsertRemoteDeck(remote))
	count, err = db.CountOwnedDecks()
	require.NoError(t, err)
	assert.Equal(t, database.DeckMaxLive, count)
}

func TestUpsertRemoteDeck(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	remote := &database.Deck{
		DeckID: "0123456789ab", Name: "Theirs", Owned: false, SourceURL: "https://zpr.au/x",
		Items: []database.DeckItem{scriptItem("A", "**a")},
	}
	require.NoError(t, db.UpsertRemoteDeck(remote))
	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.False(t, got.Owned)
	assert.NotZero(t, got.FetchedAt)
	firstDBID := got.DBID

	// A refresh replaces the whole deck and keeps the row.
	remote.Name = "Theirs v2"
	remote.Items = []database.DeckItem{scriptItem("B", "**b"), scriptItem("C", "**c")}
	require.NoError(t, db.UpsertRemoteDeck(remote))
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, firstDBID, got.DBID)
	assert.Equal(t, "Theirs v2", got.Name)
	require.Len(t, got.Items, 2)
	assert.Equal(t, "B", got.Items[0].Name)

	// An owned deck is never clobbered by a cached copy of the same ID.
	require.NoError(t, db.CreateDeck(testDeck("aaaaaaaaaaaa", "Mine", scriptItem("M", "**m"))))
	theirs := &database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Impostor", Owned: false}
	require.ErrorIs(t, db.UpsertRemoteDeck(theirs), database.ErrDeckOwned)
	got, err = db.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine", got.Name)

	// A sync pull of an owned deck may replace it.
	mine := &database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Mine (server)", Owned: true}
	require.NoError(t, db.UpsertRemoteDeck(mine))
	got, err = db.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine (server)", got.Name)
	assert.True(t, got.Owned)
}

func TestDeckItemAnchorsAndLinks(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	deck := testDeck("0123456789ab", "Anchors",
		scriptItem("Unresolved", "**launch.title:SNES/Unresolved"),
		database.DeckItem{Kind: database.DeckItemKindCard, CardID: "card1234"},
	)
	require.NoError(t, db.CreateDeck(deck))
	itemID := deck.Items[0].DBID
	require.NotZero(t, itemID)

	require.NoError(t, db.SetDeckItemAnchor(itemID, &database.DeckItemAnchor{
		SystemID: "SNES", Path: "roms\\SNES\\Unresolved.sfc", MediaName: "Unresolved", Tags: []string{"region:eu"},
	}))
	require.NoError(t, db.SetDeckItemAnchor(999999, &database.DeckItemAnchor{SystemID: "X", Path: "y"}),
		"anchoring a missing item is a no-op, never an insert")

	links, err := db.ListDeckItemLinks()
	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.Equal(t, "0123456789ab", links[0].DeckID)
	assert.Equal(t, itemID, links[0].ItemDBID)
	assert.Equal(t, "roms/SNES/Unresolved.sfc", links[0].Anchor.Path, "anchor paths are stored canonical")
	assert.Equal(t, []string{"region:eu"}, links[0].Anchor.Tags)
	assert.Equal(t, database.DeckItemKindCard, links[1].Kind)
	assert.Empty(t, links[1].Anchor.Path)
}
