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
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
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

func renameDeck(name, description string) func(*database.Deck) error {
	return func(deck *database.Deck) error {
		deck.Name, deck.Description = name, description
		return nil
	}
}

func setDeckItems(items ...database.DeckItem) func(*database.Deck) error {
	return func(deck *database.Deck) error {
		deck.Items = items
		return nil
	}
}

func preferencesRevision(t *testing.T, db *UserDB) string {
	t.Helper()
	revision, _, err := db.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	return revision
}

func TestDeckEditsAndDelete(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	deck := testDeck("0123456789ab", "First", scriptItem("A", "**a"), scriptItem("B", "**b"))
	deck.Metadata = json.RawMessage(`{"a":1}`)
	require.NoError(t, db.CreateDeck(deck))

	before := preferencesRevision(t, db)

	updated, err := db.UpdateDeck("0123456789ab", renameDeck("Renamed", "desc"))
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Name)
	assert.Equal(t, "desc", got.Description)
	assert.JSONEq(t, `{"a":1}`, string(got.Metadata), "an edit that leaves metadata alone keeps it")
	_, err = db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		deck.Metadata = json.RawMessage(`{"a":2}`)
		return nil
	})
	require.NoError(t, err)
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":2}`, string(got.Metadata))
	_, err = db.UpdateDeck("zzzzzzzzzzzz", renameDeck("x", ""))
	require.ErrorIs(t, err, database.ErrDeckNotFound)

	_, err = db.UpdateDeck("0123456789ab", setDeckItems(scriptItem("C", "**c"), scriptItem("A", "**a")))
	require.NoError(t, err)
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
	assert.Equal(t, []int{1, 2}, []int{got.Items[0].Position, got.Items[1].Position})
	assert.Equal(t, "C", got.Items[0].Name)

	assert.NotEqual(t, before, preferencesRevision(t, db), "deck edits invalidate browse cursors")

	existed, err := db.DeleteDeck("0123456789ab")
	require.NoError(t, err)
	assert.True(t, existed)
	_, err = db.GetDeck("0123456789ab")
	require.ErrorIs(t, err, database.ErrDeckNotFound)
	var orphans int
	require.NoError(t, db.sql.Load().QueryRowContext(t.Context(), `select count(*) from DeckItems`).Scan(&orphans))
	assert.Zero(t, orphans, "items go with their deck")

	afterDelete := preferencesRevision(t, db)
	existed, err = db.DeleteDeck("0123456789ab")
	require.NoError(t, err)
	assert.False(t, existed)
	assert.Equal(t, afterDelete, preferencesRevision(t, db), "deleting nothing changes nothing")
}

// Item IDs are what clients remove items by, so an edit must keep the rows of
// the items it keeps: their IDs and creation times survive moves, removals of
// other items and appends. Only content changes move an item's UpdatedAt.
func TestUpdateDeckKeepsItemRows(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	anchored := scriptItem("Metroid", "**launch.title:NES/Metroid")
	anchored.Anchor = database.DeckItemAnchor{
		SystemID: "NES", Path: "roms/NES/Metroid (USA).nes", MediaName: "Metroid", Tags: []string{"region:us"},
	}
	deck := testDeck("0123456789ab", "Keep", scriptItem("A", "**a"), scriptItem("B", "**b"), anchored)
	require.NoError(t, db.CreateDeck(deck))
	original, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	idA, idB, idM := original.Items[0].DBID, original.Items[1].DBID, original.Items[2].DBID
	_, err = db.sql.Load().ExecContext(t.Context(), `update DeckItems set CreatedAt = 1, UpdatedAt = 1;`)
	require.NoError(t, err)

	// Move the anchored item to the front, drop A, rename B and append D.
	updated, err := db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		b, m := deck.Items[1], deck.Items[2]
		b.Name = "B renamed"
		deck.Items = []database.DeckItem{m, b, scriptItem("D", "**d")}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, updated.Items, 3)

	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	require.Len(t, got.Items, 3)
	assert.Equal(t, updated.Items, got.Items, "the returned deck is the stored deck")

	assert.Equal(t, idM, got.Items[0].DBID, "a moved item keeps its ID")
	assert.Equal(t, 1, got.Items[0].Position)
	assert.Equal(t, int64(1), got.Items[0].CreatedAt)
	assert.Equal(t, int64(1), got.Items[0].UpdatedAt, "a move is not a content change")
	assert.Equal(t, "roms/NES/Metroid (USA).nes", got.Items[0].Anchor.Path, "a kept item keeps its file link")
	assert.Equal(t, []string{"region:us"}, got.Items[0].Anchor.Tags)

	assert.Equal(t, idB, got.Items[1].DBID, "an edited item keeps its ID")
	assert.Equal(t, "B renamed", got.Items[1].Name)
	assert.Equal(t, int64(1), got.Items[1].CreatedAt)
	assert.NotEqual(t, int64(1), got.Items[1].UpdatedAt, "a content change moves UpdatedAt")

	assert.NotContains(t, []int64{idA, idB, idM}, got.Items[2].DBID, "an appended item gets a new ID")
	assert.Equal(t, "D", got.Items[2].Name)

	// Reversing the list moves every item and still keeps every ID.
	ids := []int64{got.Items[0].DBID, got.Items[1].DBID, got.Items[2].DBID}
	_, err = db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		slices.Reverse(deck.Items)
		return nil
	})
	require.NoError(t, err)
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[2], ids[1], ids[0]},
		[]int64{got.Items[0].DBID, got.Items[1].DBID, got.Items[2].DBID})
	assert.Equal(t, []int{1, 2, 3}, []int{got.Items[0].Position, got.Items[1].Position, got.Items[2].Position})

	// An item listed twice keeps its row once; the copy is a new item. An
	// ID from another deck is never taken over.
	other := testDeck("aaaaaaaaaaaa", "Other", scriptItem("X", "**x"))
	require.NoError(t, db.CreateDeck(other))
	foreign := other.Items[0]
	_, err = db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		deck.Items = []database.DeckItem{deck.Items[0], deck.Items[0], foreign}
		return nil
	})
	require.NoError(t, err)
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	require.Len(t, got.Items, 3)
	assert.Equal(t, ids[2], got.Items[0].DBID)
	assert.NotEqual(t, ids[2], got.Items[1].DBID)
	assert.Equal(t, got.Items[0].Name, got.Items[1].Name)
	assert.NotEqual(t, foreign.DBID, got.Items[2].DBID)
	otherGot, err := db.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	require.Len(t, otherGot.Items, 1)
	assert.Equal(t, foreign.DBID, otherGot.Items[0].DBID, "the other deck is untouched")
}

// A failed edit writes nothing: not the name it also asked for, not the
// items, and not the preferences revision.
func TestUpdateDeckFailureWritesNothing(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	require.NoError(t, db.CreateDeck(testDeck("0123456789ab", "Before", scriptItem("A", "**a"))))
	revision := preferencesRevision(t, db)

	editErr := errors.New("edit refused")
	_, err := db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		deck.Name = "After"
		deck.Items = nil
		return editErr
	})
	require.ErrorIs(t, err, editErr)

	tooMany := make([]database.DeckItem, database.DeckMaxItems+1)
	for i := range tooMany {
		tooMany[i] = scriptItem("x", "**x")
	}
	_, err = db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		deck.Name = "After"
		deck.Items = tooMany
		return nil
	})
	require.ErrorIs(t, err, database.ErrDeckItemLimit)

	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, "Before", got.Name)
	require.Len(t, got.Items, 1)
	assert.Equal(t, revision, preferencesRevision(t, db))
}

// Edits made at the same moment each apply to the deck as the other left it,
// so none is lost, and none fails because an unrelated write committed
// between its read and its write.
func TestUpdateDeckConcurrentEdits(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.CreateDeck(testDeck("0123456789ab", "Shared")))

	const editors = 16
	var wg sync.WaitGroup
	errs := make(chan error, editors*2)
	for i := range editors {
		wg.Go(func() {
			_, err := db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
				deck.Items = append(deck.Items, scriptItem(fmt.Sprintf("item %d", i), "**x"))
				return nil
			})
			errs <- err
		})
		wg.Go(func() {
			errs <- db.SetMediaUserFavorite("NES", fmt.Sprintf("roms/NES/%d.nes", i), true)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Len(t, got.Items, editors, "every concurrent append is kept")
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
	_, err := db.UpdateDeck("0123456789ab", setDeckItems(tooMany...))
	require.ErrorIs(t, err, database.ErrDeckItemLimit)

	// Only a deck's own size is bounded. How many decks a device holds is not,
	// so a library of them, owned or cached, keeps working.
	const many = 250
	for i := 1; i < many; i++ {
		require.NoError(t, db.CreateDeck(testDeck(fmt.Sprintf("%012d", i), "Deck")))
	}
	count, err := db.CountOwnedDecks()
	require.NoError(t, err)
	assert.Equal(t, many, count)

	remote := &database.Deck{DeckID: "yyyyyyyyyyyy", Name: "Theirs", Owned: false}
	_, err = db.UpsertRemoteDeck(remote)
	require.NoError(t, err)
	count, err = db.CountOwnedDecks()
	require.NoError(t, err)
	assert.Equal(t, many, count, "a cached copy is not one of this device's own")
}

func TestUpsertRemoteDeck(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)

	remote := &database.Deck{
		DeckID: "0123456789ab", Name: "Theirs", Owned: false, SourceURL: "https://zpr.au/x",
		Items: []database.DeckItem{scriptItem("A", "**a")},
	}
	changed, err := db.UpsertRemoteDeck(remote)
	require.NoError(t, err)
	assert.True(t, changed, "a deck the device did not hold is new")
	got, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.False(t, got.Owned)
	assert.NotZero(t, got.FetchedAt)
	firstDBID := got.DBID

	// A refresh replaces the whole deck and keeps the row.
	remote.Name = "Theirs v2"
	remote.Items = []database.DeckItem{scriptItem("B", "**b"), scriptItem("C", "**c")}
	changed, err = db.UpsertRemoteDeck(remote)
	require.NoError(t, err)
	assert.True(t, changed)
	got, err = db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, firstDBID, got.DBID)
	assert.Equal(t, "Theirs v2", got.Name)
	require.Len(t, got.Items, 2)
	assert.Equal(t, "B", got.Items[0].Name)

	// An owned deck is never clobbered by a cached copy of the same ID.
	require.NoError(t, db.CreateDeck(testDeck("aaaaaaaaaaaa", "Mine", scriptItem("M", "**m"))))
	theirs := &database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Impostor", Owned: false}
	_, err = db.UpsertRemoteDeck(theirs)
	require.ErrorIs(t, err, database.ErrDeckOwned)
	got, err = db.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine", got.Name)

	// An ID from elsewhere is stored normalized, so the API finds it.
	_, err = db.UpsertRemoteDeck(&database.Deck{DeckID: " KQ7RIL0U ", Name: "Legacy", Owned: false})
	require.NoError(t, err)
	got, err = db.GetDeck("kq7ril0u")
	require.NoError(t, err)
	assert.Equal(t, "Legacy", got.Name)
	_, err = db.UpsertRemoteDeck(&database.Deck{DeckID: "not-an-id", Name: "Bad"})
	require.ErrorIs(t, err, database.ErrInvalidDeckID)

	// A sync pull of an owned deck may replace it.
	mine := &database.Deck{DeckID: "aaaaaaaaaaaa", Name: "Mine (server)", Owned: true}
	_, err = db.UpsertRemoteDeck(mine)
	require.NoError(t, err)
	got, err = db.GetDeck("aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.Equal(t, "Mine (server)", got.Name)
	assert.True(t, got.Owned)
}

func TestSetDeckItemAnchor(t *testing.T) {
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

	stored, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	require.Len(t, stored.Items, 2)
	assert.Equal(t, itemID, stored.Items[0].DBID)
	assert.Equal(t, "roms/SNES/Unresolved.sfc", stored.Items[0].Anchor.Path, "anchor paths are stored canonical")
	assert.Equal(t, []string{"region:eu"}, stored.Items[0].Anchor.Tags)
	assert.Equal(t, database.DeckItemKindCard, stored.Items[1].Kind)
	assert.Empty(t, stored.Items[1].Anchor.Path)
}

// TestUpdateDeck_RefusesLockedDeck pins that the lock is enforced where the
// deck is written, not only where a request is checked. A sync pass can lock
// a deck between an API handler's check and its write, and a sync row that
// cannot be read must refuse the edit rather than allow it.
func TestUpdateDeck_RefusesLockedDeck(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Locked", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**a"}},
	}))
	require.NoError(t, db.UpsertDeckSync([]database.DeckSyncRow{
		{DeckID: "0123456789ab", Revision: 3, Locked: true},
	}))

	_, err := db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		deck.Name = "Changed"
		return nil
	})
	require.ErrorIs(t, err, database.ErrDeckReadOnly)

	_, err = db.DeleteDeck("0123456789ab")
	require.ErrorIs(t, err, database.ErrDeckReadOnly)

	stored, err := db.GetDeck("0123456789ab")
	require.NoError(t, err)
	assert.Equal(t, "Locked", stored.Name, "the refused edit changed nothing")

	// Sync still writes the account's own copy of a locked deck.
	changed, err := db.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "From Online", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "B", ZapScript: "**b"}},
	})
	require.NoError(t, err)
	assert.True(t, changed)
}

// TestUpdateDeck_UnlockedDeckStillEdits pins that an unsynced deck, which has
// no sync row at all, is not caught by the lock check.
func TestUpdateDeck_UnlockedDeckStillEdits(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempUserDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.CreateDeck(&database.Deck{DeckID: "0123456789ab", Name: "Mine", Owned: true}))

	updated, err := db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		deck.Name = "Renamed"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)

	require.NoError(t, db.UpsertDeckSync([]database.DeckSyncRow{{DeckID: "0123456789ab", Revision: 1}}))
	_, err = db.UpdateDeck("0123456789ab", func(deck *database.Deck) error {
		deck.Name = "Renamed Again"
		return nil
	})
	require.NoError(t, err, "a synced but unlocked deck still edits")

	existed, err := db.DeleteDeck("0123456789ab")
	require.NoError(t, err)
	assert.True(t, existed)
}
