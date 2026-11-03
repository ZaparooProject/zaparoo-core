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
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// The production link shape and a third-party host are kept alike.
	savedOnlineLink = "https://zpr.au/d$lhm6n9t8"
	savedOtherLink  = "https://decks.example.net/shelves/Party?list=1"
	getSavedDecks   = "GET /v1/device/saved-decks"
)

// fakeDeckFetcher serves the playlists of deck links without a network.
type fakeDeckFetcher struct {
	served map[string]decks.PlaylistArg
	fails  map[string]bool
	calls  []string
}

func (d *fakeDeckFetcher) fetch(_ context.Context, link string) (decks.PlaylistArg, error) {
	d.calls = append(d.calls, link)
	if d.fails[link] {
		return decks.PlaylistArg{}, errors.New("link unreachable")
	}
	arg, ok := d.served[link]
	if !ok {
		return decks.PlaylistArg{}, errors.New("link serves no deck")
	}
	return arg, nil
}

func servedDeck(name string, scripts ...string) decks.PlaylistArg {
	arg := decks.PlaylistArg{ID: "served-" + name, Name: name}
	for _, script := range scripts {
		arg.Items = append(arg.Items, decks.PlaylistArgItem{Name: script, ZapScript: script})
	}
	return arg
}

type savedDeckFixture struct {
	*syncFixture
	svc           *librarysync.Service
	fetcher       *fakeDeckFetcher
	notifications chan models.Notification
}

func newSavedDeckFixture(t *testing.T) *savedDeckFixture {
	t.Helper()
	f := &savedDeckFixture{
		syncFixture: newSyncFixture(t),
		fetcher: &fakeDeckFetcher{
			served: make(map[string]decks.PlaylistArg),
			fails:  make(map[string]bool),
		},
		notifications: make(chan models.Notification, 64),
	}
	f.svc = librarysync.New(&librarysync.Options{
		Config: f.cfg, DB: f.db, NewClient: f.newClient,
		Now:         func() time.Time { return time.Unix(f.now.Load(), 0).UTC() },
		ResolvePace: time.Millisecond, FetchDeck: f.fetcher.fetch, Notifications: f.notifications,
	})
	return f
}

func (f *savedDeckFixture) sync(t *testing.T) librarysync.DecksResult {
	t.Helper()
	result, err := f.svc.SyncDecks(f.ctx)
	require.NoError(t, err)
	return result
}

// changes drains the decks.changed notifications sent so far.
func (f *savedDeckFixture) changes(t *testing.T) []models.DecksChangedNotification {
	t.Helper()
	var out []models.DecksChangedNotification
	for {
		select {
		case n := <-f.notifications:
			require.Equal(t, models.NotificationDecksChanged, n.Method)
			var payload models.DecksChangedNotification
			require.NoError(t, json.Unmarshal(n.Params, &payload))
			out = append(out, payload)
		default:
			return out
		}
	}
}

// fetchedDecks returns the decks not owned here, by source link.
func (f *savedDeckFixture) fetchedDecks(t *testing.T) map[string]database.Deck {
	t.Helper()
	list, err := f.db.UserDB.ListDecks()
	require.NoError(t, err)
	out := make(map[string]database.Deck)
	for i := range list {
		if !list[i].Owned {
			out[list[i].SourceURL] = list[i]
		}
	}
	return out
}

func strPtr(s string) *string { return &s }

func TestSyncDecks_SavedDecksArriveFollowTheListAndLeave(t *testing.T) {
	f := newSavedDeckFixture(t)
	f.fetcher.served[savedOnlineLink] = servedDeck("Online deck", "**launch.random:snes")
	f.fetcher.served[savedOtherLink] = servedDeck("", "**launch.random:nes")
	f.online.saveDecks(
		fakeSavedDeck{DeckID: "lhm6n9t8", Link: savedOnlineLink, Name: strPtr("Online deck"), OwnedBy: strPtr("sam")},
		fakeSavedDeck{DeckID: "x", Link: savedOtherLink, Name: strPtr("Party")},
	)

	result := f.sync(t)
	assert.Equal(t, 2, result.Saved)
	kept := f.fetchedDecks(t)
	require.Len(t, kept, 2)
	online := kept[savedOnlineLink]
	assert.True(t, online.Saved)
	assert.Equal(t, "sam", online.OwnerName)
	assert.Equal(t, "Online deck", online.Name)
	assert.Equal(t, 1, online.ItemCount)
	other := kept[savedOtherLink]
	assert.True(t, other.Saved)
	assert.Empty(t, other.OwnerName, "an owner the account does not name stays empty")
	assert.Equal(t, "Party", other.Name, "a deck served without a name takes the account's")
	assert.ElementsMatch(t, []models.DecksChangedNotification{
		{DeckID: online.DeckID, Action: models.DecksChangedCreated},
		{DeckID: other.DeckID, Action: models.DecksChangedCreated},
	}, f.changes(t))

	// A deck already kept is not fetched again; opening it refreshes it.
	result = f.sync(t)
	assert.Zero(t, result.Saved)
	assert.Len(t, f.fetcher.calls, 2)
	assert.Empty(t, f.changes(t))

	// The owner's name follows the list; the deck stays where it is.
	f.online.saveDecks(
		fakeSavedDeck{DeckID: "lhm6n9t8", Link: savedOnlineLink, OwnedBy: strPtr("sam-renamed")},
		fakeSavedDeck{DeckID: "x", Link: savedOtherLink},
	)
	f.sync(t)
	kept = f.fetchedDecks(t)
	assert.Equal(t, "sam-renamed", kept[savedOnlineLink].OwnerName)
	assert.Equal(t, online.DeckID, kept[savedOnlineLink].DeckID)
	assert.Equal(t, []models.DecksChangedNotification{
		{DeckID: online.DeckID, Action: models.DecksChangedUpdated},
	}, f.changes(t), "clients hear about the new owner name")

	// A deck the list stops naming is removed.
	f.online.saveDecks(fakeSavedDeck{DeckID: "x", Link: savedOtherLink})
	result = f.sync(t)
	assert.Equal(t, 1, result.Unsaved)
	kept = f.fetchedDecks(t)
	require.Len(t, kept, 1)
	assert.Contains(t, kept, savedOtherLink)
	_, err := f.db.UserDB.GetDeck(online.DeckID)
	require.ErrorIs(t, err, database.ErrDeckNotFound)
	assert.Equal(t, []models.DecksChangedNotification{
		{DeckID: online.DeckID, Action: models.DecksChangedDeleted},
	}, f.changes(t))
}

// A deck kept from a tap on the same link is marked saved rather than kept
// twice. Links compare as URLs do: the host without regard to case, the path
// exactly.
func TestSyncDecks_SavedDeckTakesOverATappedCopy(t *testing.T) {
	f := newSavedDeckFixture(t)
	tapped, err := decks.StoreFetchedDeck(f.db, "https://ZPR.AU/d$lhm6n9t8", &decks.PlaylistArg{
		Name: "Tapped", Items: []decks.PlaylistArgItem{{Name: "a", ZapScript: "**a"}},
	})
	require.NoError(t, err)
	otherPath, err := decks.StoreFetchedDeck(f.db, "https://zpr.au/D$LHM6N9T8", &decks.PlaylistArg{
		Name: "Other path", Items: []decks.PlaylistArgItem{{Name: "a", ZapScript: "**a"}},
	})
	require.NoError(t, err)
	f.online.saveDecks(fakeSavedDeck{DeckID: "lhm6n9t8", Link: savedOnlineLink, OwnedBy: strPtr("sam")})

	result := f.sync(t)
	assert.Equal(t, 1, result.Saved)
	assert.Empty(t, f.fetcher.calls, "the tapped copy is kept, not fetched again")
	got, err := f.db.UserDB.GetDeck(tapped)
	require.NoError(t, err)
	assert.True(t, got.Saved)
	assert.Equal(t, "sam", got.OwnerName)
	assert.Equal(t, "Tapped", got.Name)
	other, err := f.db.UserDB.GetDeck(otherPath)
	require.NoError(t, err)
	assert.False(t, other.Saved, "a link whose path differs is another link")
	assert.Equal(t, []models.DecksChangedNotification{
		{DeckID: tapped, Action: models.DecksChangedUpdated},
	}, f.changes(t))

	// Unsaving removes the copy however it first arrived; the other link's
	// copy is no saved deck and stays.
	f.online.saveDecks()
	result = f.sync(t)
	assert.Equal(t, 1, result.Unsaved)
	_, err = f.db.UserDB.GetDeck(tapped)
	require.ErrorIs(t, err, database.ErrDeckNotFound)
	_, err = f.db.UserDB.GetDeck(otherPath)
	require.NoError(t, err)
}

func TestSyncDecks_SavedDeckThatFailsToFetchIsTriedAgain(t *testing.T) {
	f := newSavedDeckFixture(t)
	f.fetcher.served[savedOnlineLink] = servedDeck("Online deck", "**a")
	f.fetcher.served[savedOtherLink] = servedDeck("Other deck", "**b")
	f.fetcher.fails[savedOnlineLink] = true
	f.online.saveDecks(
		fakeSavedDeck{DeckID: "lhm6n9t8", Link: savedOnlineLink},
		fakeSavedDeck{DeckID: "x", Link: savedOtherLink},
	)

	result := f.sync(t)
	assert.Equal(t, 1, result.Saved, "one link failing does not hold up the rest")
	assert.NotContains(t, f.fetchedDecks(t), savedOnlineLink)

	delete(f.fetcher.fails, savedOnlineLink)
	result = f.sync(t)
	assert.Equal(t, 1, result.Saved)
	assert.True(t, f.fetchedDecks(t)[savedOnlineLink].Saved)
}

func TestSyncDecks_UnreadableSavedListRemovesNothing(t *testing.T) {
	f := newSavedDeckFixture(t)
	f.fetcher.served[savedOnlineLink] = servedDeck("Online deck", "**a")
	f.online.saveDecks(fakeSavedDeck{DeckID: "lhm6n9t8", Link: savedOnlineLink})
	f.sync(t)
	require.True(t, f.fetchedDecks(t)[savedOnlineLink].Saved)

	f.online.failSavedDecks(http.StatusInternalServerError)
	_, err := f.svc.SyncDecks(f.ctx)
	require.Error(t, err)
	assert.True(t, f.fetchedDecks(t)[savedOnlineLink].Saved)
}

// A saved deck is never one of the account's own decks: the owned pull and
// push leave it alone, and the saved pass never touches an owned deck.
func TestSyncDecks_SavedDecksStayOutOfOwnedSync(t *testing.T) {
	f := newSavedDeckFixture(t)
	f.createDeck(t, "0k3v9x2rq7bm", "Mine", scriptDeckItem("A", "**a"))
	f.fetcher.served[savedOnlineLink] = servedDeck("Online deck", "**a")
	f.online.saveDecks(fakeSavedDeck{DeckID: "lhm6n9t8", Link: savedOnlineLink})

	f.sync(t)
	assert.Equal(t, 1, f.online.count(getSavedDecks))
	f.online.mu.Lock()
	held := len(f.online.decks)
	f.online.mu.Unlock()
	assert.Equal(t, 1, held, "only the owned deck reaches the account")
	mine, err := f.db.UserDB.GetDeck("0k3v9x2rq7bm")
	require.NoError(t, err)
	assert.True(t, mine.Owned)
	assert.False(t, mine.Saved)
}

// The saved pass runs apart from the owned one, so an account whose owned
// decks fail to sync still has its saved decks kept.
func TestSyncDecks_OwnedFailureDoesNotHoldUpSavedDecks(t *testing.T) {
	f := newSavedDeckFixture(t)
	f.fetcher.served[savedOnlineLink] = servedDeck("Online deck", "**a")
	f.online.saveDecks(fakeSavedDeck{DeckID: "lhm6n9t8", Link: savedOnlineLink})
	f.online.failDeckPull(http.StatusInternalServerError)

	result, err := f.svc.SyncDecks(f.ctx)
	require.Error(t, err, "the owned failure is still reported")
	assert.False(t, librarysync.IsIdleError(err))
	assert.Equal(t, 1, result.Saved)
	assert.True(t, f.fetchedDecks(t)[savedOnlineLink].Saved)
}
