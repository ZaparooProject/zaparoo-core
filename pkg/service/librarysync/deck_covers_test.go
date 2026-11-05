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
	"bytes"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/librarysync"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stageDeckImage = "POST /v1/device/decks/{id}/image-uploads"

// withDeckCovers gives the fixture's service a cover store in memory.
func withDeckCovers(t *testing.T, f *syncFixture) *decks.Covers {
	t.Helper()
	covers := &decks.Covers{FS: afero.NewMemMapFs(), Dir: "/data/decks/covers"}
	f.svc = librarysync.New(&librarysync.Options{
		Config: f.cfg, DB: f.db, NewClient: f.newClient, DeckCovers: covers,
		Now: func() time.Time { return time.Unix(f.now.Load(), 0).UTC() }, ResolvePace: time.Millisecond,
	})
	return covers
}

func coverPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return buf.Bytes()
}

// setCover sets a cover the way decks.update does.
func (f *syncFixture) setCover(t *testing.T, covers *decks.Covers, deckID string) (name string, data []byte) {
	t.Helper()
	data = coverPNG(t)
	name, err := covers.Save(deckID, data)
	require.NoError(t, err)
	f.editDeck(t, deckID, func(deck *database.Deck) {
		deck.CoverFile, deck.CoverPending = name, database.DeckCoverPendingSet
	})
	return name, data
}

func (f *syncFixture) removeCover(t *testing.T, deckID string) {
	t.Helper()
	f.editDeck(t, deckID, func(deck *database.Deck) {
		deck.CoverFile, deck.CoverPending = "", database.DeckCoverPendingRemove
	})
}

func TestSyncDecks_NewDeckCoverIsStagedAfterTheCreate(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	name, data := f.setCover(t, covers, "0123456789ab")

	f.syncDecks(t)

	assert.Equal(t, 1, f.online.count(stageDeckImage))
	changes := f.online.imageChanges()
	require.Len(t, changes, 2, "a create, then the image once the deck exists")
	assert.Empty(t, changes[0])
	assert.NotEmpty(t, changes[1])
	remote := f.online.deck("0123456789ab")
	require.NotNil(t, remote)
	require.NotNil(t, remote.ImageURL)

	local := f.localDeck(t, "0123456789ab")
	assert.Empty(t, local.CoverPending)
	assert.Equal(t, name, local.CoverFile, "the cover stays on the device")
	assert.Equal(t, *remote.ImageURL, local.ImageURL)
	stored, err := covers.Read(name)
	require.NoError(t, err)
	assert.Equal(t, data, stored)

	// Nothing is pending, so the next pass pushes nothing.
	f.syncDecks(t)
	assert.Len(t, f.online.imageChanges(), 2)
	assert.Equal(t, name, f.localDeck(t, "0123456789ab").CoverFile)
}

func TestSyncDecks_CoverRemovalIsPushed(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.setCover(t, covers, "0123456789ab")
	f.syncDecks(t)
	require.NotNil(t, f.online.deck("0123456789ab").ImageURL)

	f.removeCover(t, "0123456789ab")
	f.syncDecks(t)

	changes := f.online.imageChanges()
	assert.Equal(t, "remove", changes[len(changes)-1])
	assert.Nil(t, f.online.deck("0123456789ab").ImageURL)
	local := f.localDeck(t, "0123456789ab")
	assert.Empty(t, local.CoverPending)
	assert.Empty(t, local.ImageURL)
	assert.False(t, local.CoverFile != "" || local.RemoteImageURL() != "")
}

func TestSyncDecks_RemovalWithNothingOnTheAccountSendsNothing(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Plain", scriptDeckItem("A", "**a"))
	f.syncDecks(t)
	pushes := len(f.online.imageChanges())

	f.removeCover(t, "0123456789ab")
	f.syncDecks(t)

	assert.Len(t, f.online.imageChanges(), pushes)
	assert.Empty(t, f.localDeck(t, "0123456789ab").CoverPending)
}

func TestSyncDecks_AccountImageReplacesAnUnchangedLocalCover(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.setCover(t, covers, "0123456789ab")
	f.syncDecks(t)

	remote := f.online.deck("0123456789ab")
	other := "https://img.example/decks/other.png"
	remote.ImageURL = &other
	f.online.putDeck(remote)
	f.syncDecks(t)

	local := f.localDeck(t, "0123456789ab")
	assert.Empty(t, local.CoverFile, "the account's new image replaces the local cover")
	assert.Equal(t, other, local.ImageURL)
	assert.Equal(t, other, local.RemoteImageURL())
}

func TestSyncDecks_PendingLocalCoverWinsOverThePulledImage(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.syncDecks(t)

	remote := f.online.deck("0123456789ab")
	other := "https://img.example/decks/other.png"
	remote.ImageURL = &other
	f.online.putDeck(remote)
	name, _ := f.setCover(t, covers, "0123456789ab")
	f.syncDecks(t)

	local := f.localDeck(t, "0123456789ab")
	assert.Equal(t, name, local.CoverFile)
	assert.Empty(t, local.CoverPending)
	require.NotNil(t, f.online.deck("0123456789ab").ImageURL)
	assert.NotEqual(t, other, *f.online.deck("0123456789ab").ImageURL, "the local cover was pushed over it")
}

func TestSyncDecks_ExpiredUploadIsStagedAgain(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.syncDecks(t)
	f.online.mu.Lock()
	f.online.expireUploads = 1
	f.online.mu.Unlock()

	f.setCover(t, covers, "0123456789ab")
	f.syncDecks(t)

	assert.Equal(t, 2, f.online.count(stageDeckImage))
	assert.NotNil(t, f.online.deck("0123456789ab").ImageURL)
	assert.Empty(t, f.localDeck(t, "0123456789ab").CoverPending)
	assert.Empty(t, f.deckSyncRow(t, "0123456789ab").RejectedCode)
}

func TestSyncDecks_LockedDeckDropsTheCoverChange(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.syncDecks(t)
	f.online.lockDeck("0123456789ab")

	f.setCover(t, covers, "0123456789ab")
	f.syncDecks(t)

	local := f.localDeck(t, "0123456789ab")
	assert.Empty(t, local.CoverPending)
	assert.Empty(t, local.CoverFile)
	assert.Nil(t, f.online.deck("0123456789ab").ImageURL)
}

func TestSyncDecks_RefusedCoverStaysOnTheDevice(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.syncDecks(t)
	f.online.mu.Lock()
	f.online.stageStatus, f.online.stageCode = http.StatusBadRequest, "bad_request"
	f.online.mu.Unlock()
	pushes := len(f.online.imageChanges())

	name, _ := f.setCover(t, covers, "0123456789ab")
	f.syncDecks(t)

	assert.Len(t, f.online.imageChanges(), pushes, "a cover change alone is not pushed without its image")
	local := f.localDeck(t, "0123456789ab")
	assert.Equal(t, name, local.CoverFile)
	assert.Empty(t, local.CoverPending, "the refused cover is not tried again")
}

func TestSyncDecks_UnavailableUploadsTryAgainLater(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.syncDecks(t)
	f.online.mu.Lock()
	f.online.stageStatus, f.online.stageCode = http.StatusNotFound, "not_found"
	f.online.mu.Unlock()

	f.setCover(t, covers, "0123456789ab")
	f.syncDecks(t)
	assert.Equal(t, database.DeckCoverPendingSet, f.localDeck(t, "0123456789ab").CoverPending)

	f.online.mu.Lock()
	f.online.stageStatus, f.online.stageCode = 0, ""
	f.online.mu.Unlock()
	f.syncDecks(t)
	assert.Empty(t, f.localDeck(t, "0123456789ab").CoverPending)
	assert.NotNil(t, f.online.deck("0123456789ab").ImageURL)
}

func TestSyncDecks_PulledDeckKeepsItsImageURL(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	imageURL := "https://img.example/decks/pulled.png"
	f.online.putDeck(&fakeDeck{DeckID: "ABCDEFGHJKMN", Name: "From the web", ImageURL: &imageURL})
	f.syncDecks(t)

	local := f.localDeck(t, "abcdefghjkmn")
	assert.Equal(t, imageURL, local.ImageURL)
	assert.True(t, strings.HasPrefix(local.RemoteImageURL(), "https://"))
}

// A push that conflicts with a deck the account locked meanwhile drops the
// cover change too, so it never waits to be pushed once the deck unlocks.
func TestSyncDecks_ConflictWithLockedDeckDropsTheCoverChange(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t)
	covers := withDeckCovers(t, f)
	f.createDeck(t, "0123456789ab", "Covered", scriptDeckItem("A", "**a"))
	f.syncDecks(t)

	f.setCover(t, covers, "0123456789ab")
	f.online.onDeckPush(func(online *fakeOnline) {
		// Inside the request, so no locking helpers.
		online.moveDeckOn("0123456789AB", "Changed There")
		online.decks["0123456789AB"].IsLocked = true
	})
	f.syncDecks(t)

	local := f.localDeck(t, "0123456789ab")
	assert.Empty(t, local.CoverPending)
	assert.Empty(t, local.CoverFile)
	assert.Nil(t, f.online.deck("0123456789ab").ImageURL)
}
