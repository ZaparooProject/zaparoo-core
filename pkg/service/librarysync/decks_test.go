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
	"context"
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	postDecks = "POST /v1/device/decks"
	getDecks  = "GET /v1/device/decks"
)

// syncDeckTags projects queued decks then and there, so a test sees the tags
// a pass asks for without running the background tagger.
type syncDeckTags struct {
	deps  *decks.ResolveDeps
	db    *database.Database
	decks []string
	all   int
}

func (q *syncDeckTags) QueueDeckTags(deckIDs ...string) {
	q.decks = append(q.decks, deckIDs...)
	for _, deckID := range deckIDs {
		_, _ = decks.ProjectDeck(context.Background(), q.deps, deckID)
	}
}

func (q *syncDeckTags) QueueAllDeckTags() {
	q.all++
	list, err := q.db.UserDB.ListDecks()
	if err != nil {
		return
	}
	for i := range list {
		_, _ = decks.ProjectDeck(context.Background(), q.deps, list[i].DeckID)
	}
}

func scriptDeckItem(name, script string) database.DeckItem {
	return database.DeckItem{Kind: database.DeckItemKindScript, Name: name, ZapScript: script}
}

func (f *syncFixture) syncDecks(t *testing.T) librarysync.DecksResult {
	t.Helper()
	result, err := f.svc.SyncDecks(f.ctx)
	require.NoError(t, err)
	return result
}

func (f *syncFixture) createDeck(t *testing.T, deckID, name string, items ...database.DeckItem) {
	t.Helper()
	require.NoError(t, f.db.UserDB.CreateDeck(&database.Deck{DeckID: deckID, Name: name, Owned: true, Items: items}))
}

// editDeck makes a local edit the way a client does, so the stored deck
// carries the same item rows an API edit would leave behind.
func (f *syncFixture) editDeck(t *testing.T, deckID string, edit func(deck *database.Deck)) {
	t.Helper()
	_, err := f.db.UserDB.UpdateDeck(deckID, func(deck *database.Deck) error {
		edit(deck)
		return nil
	})
	require.NoError(t, err)
}

func (f *syncFixture) localDeck(t *testing.T, deckID string) *database.Deck {
	t.Helper()
	deck, err := f.db.UserDB.GetDeck(deckID)
	require.NoError(t, err)
	return deck
}

func itemScripts(deck *database.Deck) []string {
	out := make([]string, 0, len(deck.Items))
	for i := range deck.Items {
		if deck.Items[i].Kind == database.DeckItemKindCard {
			out = append(out, "card:"+deck.Items[i].CardID)
			continue
		}
		out = append(out, deck.Items[i].ZapScript)
	}
	return out
}

func fakeItemScripts(deck *fakeDeck) []string {
	out := make([]string, 0, len(deck.Items))
	for _, item := range deck.Items {
		if item.Kind == "card" {
			out = append(out, "card:"+item.CardID)
			continue
		}
		out = append(out, item.ZapScript)
	}
	return out
}

func TestSyncDecks_LocalDeckIsCreatedOnTheAccount(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.online.addCard("CARD0001", "Party Card", json.RawMessage(`{"run_mode":"custom"}`),
		database.DeckCardScript{Name: "Go", ZapScript: "**launch.system:NES"})
	f.createDeck(t, "0123456789ab", "Weekend",
		scriptDeckItem("Metroid", "**launch.title:NES/Metroid"),
		database.DeckItem{Kind: database.DeckItemKindCard, CardID: "CARD0001"},
	)

	result := f.syncDecks(t)
	assert.Equal(t, 1, result.Pushed)
	remote := f.online.deck("0123456789ab")
	require.NotNil(t, remote)
	assert.Equal(t, "Weekend", remote.Name)
	assert.Equal(t, []string{"**launch.title:NES/Metroid", "card:CARD0001"}, fakeItemScripts(remote))

	local := f.localDeck(t, "0123456789ab")
	require.Len(t, local.Items, 2)
	assert.Equal(t, "Party Card", local.Items[1].Name, "the account's card scripts are stored for offline runs")
	require.Len(t, local.Items[1].Scripts, 1)
	assert.JSONEq(t, `{"zaps":0}`, string(local.Metadata))

	f.online.resetCalls()
	result = f.syncDecks(t)
	assert.Zero(t, result.Pushed)
	assert.Zero(t, f.online.count(postDecks), "an agreed deck is not pushed again")
	assert.Equal(t, 1, f.online.count(getDecks))
}

func TestSyncDecks_AccountDeckArrivesWithTags(t *testing.T) {
	f := newSyncFixture(t, metroidUSA)
	f.online.putDeck(&fakeDeck{
		DeckID: "ABCDEFGHJKMN", Name: "From Online", Description: "made there",
		Items: []fakeDeckItem{{Kind: "script", Name: "Metroid", ZapScript: "**launch.title:NES/Metroid"}},
	})

	result := f.syncDecks(t)
	assert.Equal(t, 1, result.Pulled)
	local := f.localDeck(t, "abcdefghjkmn")
	assert.True(t, local.Owned)
	assert.Equal(t, "From Online", local.Name)
	require.Len(t, local.Items, 1)
	assert.Equal(t, metroidUSA, local.Items[0].Anchor.Path, "the game item is linked to the local file")

	nes, err := systemdefs.GetSystem("NES")
	require.NoError(t, err)
	ref := decks.DeckTagRef("abcdefghjkmn")
	tagged, err := f.db.MediaDB.SearchMediaWithFilters(f.ctx, &database.SearchFilters{
		Systems: []systemdefs.System{*nes},
		Tags:    []zapscript.TagFilter{{Type: ref.Type, Value: ref.Tag, Operator: zapscript.TagOperatorAND}},
		Limit:   10,
	})
	require.NoError(t, err)
	require.Len(t, tagged, 1)
	assert.Equal(t, metroidUSA, tagged[0].Path)

	f.online.resetCalls()
	f.syncDecks(t)
	assert.Zero(t, f.online.count(postDecks), "a pulled deck is not echoed back")

	// A later edit on the account keeps the file link.
	remote := f.online.deck("abcdefghjkmn")
	remote.Name = "Renamed There"
	remote.Items = append(remote.Items,
		fakeDeckItem{Kind: "script", Name: "Zelda", ZapScript: "**launch.title:NES/Zelda"})
	f.online.putDeck(remote)
	f.syncDecks(t)
	local = f.localDeck(t, "abcdefghjkmn")
	assert.Equal(t, "Renamed There", local.Name)
	require.Len(t, local.Items, 2)
	assert.Equal(t, metroidUSA, local.Items[0].Anchor.Path)
}

func TestSyncDecks_BothSidesEditedAreMerged(t *testing.T) {
	f := newSyncFixture(t)
	f.createDeck(t, "0123456789ab", "Weekend",
		scriptDeckItem("A", "**a"), scriptDeckItem("B", "**b"))
	f.syncDecks(t)

	// Online removes B and adds D; this device renames and adds C after A.
	remote := f.online.deck("0123456789ab")
	remote.Items = []fakeDeckItem{
		{Kind: "script", Name: "A", ZapScript: "**a"}, {Kind: "script", Name: "D", ZapScript: "**d"},
	}
	f.online.putDeck(remote)
	f.editDeck(t, "0123456789ab", func(deck *database.Deck) {
		deck.Name = "Weekend Plus"
		deck.Items = []database.DeckItem{
			scriptDeckItem("A", "**a"), scriptDeckItem("C", "**c"), scriptDeckItem("B", "**b"),
		}
	})

	f.syncDecks(t)
	want := []string{"**a", "**c", "**d"}
	assert.Equal(t, want, itemScripts(f.localDeck(t, "0123456789ab")))
	merged := f.online.deck("0123456789ab")
	assert.Equal(t, want, fakeItemScripts(merged))
	assert.Equal(t, "Weekend Plus", merged.Name)
	assert.Equal(t, "Weekend Plus", f.localDeck(t, "0123456789ab").Name)
}

func TestSyncDecks_DeletesTravelBothWays(t *testing.T) {
	f := newSyncFixture(t)
	f.createDeck(t, "0123456789ab", "Mine", scriptDeckItem("A", "**a"))
	f.createDeck(t, "bbbbbbbbbbbb", "Other", scriptDeckItem("B", "**b"))
	f.syncDecks(t)

	existed, err := f.db.UserDB.DeleteDeck("0123456789ab")
	require.NoError(t, err)
	require.True(t, existed)
	f.syncDecks(t)
	assert.True(t, f.online.deck("0123456789ab").Deleted, "a deck deleted here is deleted on the account")

	remote := f.online.deck("bbbbbbbbbbbb")
	remote.Deleted = true
	remote.Items = nil
	f.online.putDeck(remote)
	f.syncDecks(t)
	_, err = f.db.UserDB.GetDeck("bbbbbbbbbbbb")
	require.ErrorIs(t, err, database.ErrDeckNotFound, "a deck deleted on the account is deleted here")

	f.online.resetCalls()
	f.syncDecks(t)
	assert.Zero(t, f.online.count(postDecks))
}

func TestSyncDecks_TakenIDIsMintedAgain(t *testing.T) {
	f := newSyncFixture(t)
	f.createDeck(t, "0123456789ab", "Mine", scriptDeckItem("A", "**a"))
	f.online.takeDeckID("0123456789ab")

	f.syncDecks(t)
	_, err := f.db.UserDB.GetDeck("0123456789ab")
	require.ErrorIs(t, err, database.ErrDeckNotFound)
	list, err := f.db.UserDB.ListDecks()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.NotEqual(t, "0123456789ab", list[0].DeckID)
	assert.Equal(t, "Mine", list[0].Name)
	require.NotNil(t, f.online.deck(list[0].DeckID), "the deck is created under its new ID")
}

func TestSyncDecks_LockedDeckTakesTheAccountCopy(t *testing.T) {
	f := newSyncFixture(t)
	f.createDeck(t, "0123456789ab", "Mine", scriptDeckItem("A", "**a"))
	f.syncDecks(t)

	remote := f.online.deck("0123456789ab")
	remote.IsLocked = true
	remote.Name = "Locked There"
	f.online.putDeck(remote)
	// The edit here races the lock: the cursor already passed it.
	require.NoError(t, f.db.UserDB.SetDeviceState(librarysync.DeviceStateKeyDecksSince, "999"))
	f.editDeck(t, "0123456789ab", func(deck *database.Deck) {
		deck.Items = []database.DeckItem{scriptDeckItem("A", "**a"), scriptDeckItem("B", "**b")}
	})

	f.syncDecks(t)
	local := f.localDeck(t, "0123456789ab")
	assert.Equal(t, "Locked There", local.Name)
	assert.Equal(t, []string{"**a"}, itemScripts(local))
	row, found, err := f.db.UserDB.GetDeckSync("0123456789ab")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.Locked)
}

func TestSyncDecks_RejectedDeckWaitsForAChange(t *testing.T) {
	f := newSyncFixture(t)
	f.createDeck(t, "0123456789ab", "Mine", database.DeckItem{Kind: database.DeckItemKindCard, CardID: "NOPE0000"})

	result := f.syncDecks(t)
	assert.Equal(t, 1, result.Rejected)
	f.online.resetCalls()
	f.syncDecks(t)
	assert.Zero(t, f.online.count(postDecks), "a rejected deck is not sent again unchanged")

	f.editDeck(t, "0123456789ab", func(deck *database.Deck) {
		deck.Items = []database.DeckItem{scriptDeckItem("A", "**a")}
	})
	result = f.syncDecks(t)
	assert.Equal(t, 1, result.Pushed)
}

func TestRefreshDeckPullsBeforeOpening(t *testing.T) {
	f := newSyncFixture(t)
	f.createDeck(t, "0123456789ab", "Mine", scriptDeckItem("A", "**a"))
	f.syncDecks(t)

	remote := f.online.deck("0123456789ab")
	remote.Items = append(remote.Items, fakeDeckItem{Kind: "script", Name: "B", ZapScript: "**b"})
	f.online.putDeck(remote)
	f.online.resetCalls()

	f.svc.RefreshDeck(f.ctx, "0123456789ab")
	assert.Equal(t, []string{"**a", "**b"}, itemScripts(f.localDeck(t, "0123456789ab")))
	assert.Zero(t, f.online.count(postDecks), "opening a deck never pushes")

	f.online.resetCalls()
	require.NoError(t, f.svc.PullDecksIfStale(f.ctx))
	assert.Zero(t, f.online.count(getDecks), "a fresh pull is not repeated for a client listing decks")
}
