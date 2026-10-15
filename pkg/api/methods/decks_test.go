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

package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type decksTestEnv struct {
	ns     <-chan models.Notification
	tagger *decks.Tagger
	paths  []string
	ids    []int64
	env    requests.RequestEnv
}

func newDecksTestEnv(t *testing.T) *decksTestEnv {
	t.Helper()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)

	paths := []string{
		filepath.Join("roms", "NES", "Metroid (USA).nes"),
		filepath.Join("roms", "NES", "Contra (USA) (Hack).nes"),
	}
	ids := addTestMediaPaths(t, mediaDB, paths...)
	tagger := decks.NewTagger(&decks.ResolveDeps{MediaDB: mediaDB, UserDB: userDB}, nil)
	return &decksTestEnv{
		env: requests.RequestEnv{
			Context:  context.Background(),
			Database: &database.Database{MediaDB: mediaDB, UserDB: userDB, DeckTags: tagger},
			State:    st,
		},
		ns:     ns,
		tagger: tagger,
		paths:  paths,
		ids:    ids,
	}
}

// tagDecks does the deck tag work the handlers queued.
func (e *decksTestEnv) tagDecks(t *testing.T) {
	t.Helper()
	require.False(t, e.tagger.Drain(context.Background()), "every queued deck is tagged")
}

func (e *decksTestEnv) call(t *testing.T, handler func(requests.RequestEnv) (any, error), params string) any {
	t.Helper()
	result, err := handler(withParams(&e.env, params))
	require.NoError(t, err)
	return result
}

func (e *decksTestEnv) expectNotification(t *testing.T, deckID, action string) {
	t.Helper()
	select {
	case n := <-e.ns:
		assert.Equal(t, models.NotificationDecksChanged, n.Method)
		var payload models.DecksChangedNotification
		require.NoError(t, json.Unmarshal(n.Params, &payload))
		assert.Equal(t, deckID, payload.DeckID)
		assert.Equal(t, action, payload.Action)
	case <-time.After(time.Second):
		t.Fatalf("no %s notification for deck %s", action, deckID)
	}
}

func TestHandleDecks_CreateGetListDelete(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)

	created, ok := e.call(t, HandleDecksNew, fmt.Sprintf(`{
		"name": " Weekend ", "description": "Games",
		"items": [
			{"kind": "media", "mediaId": %d},
			{"kind": "media", "system": "NES", "path": %q, "name": "My Hack"},
			{"kind": "script", "name": "Random", "zapscript": "**launch.random:SNES"},
			{"kind": "card", "cardId": "abcd1234", "name": "A card",
			 "scripts": [{"name": "Play", "zapscript": "**launch.system:SNES"}],
			 "metadata": {"image_url": "https://x/y.png"}}
		]}`, e.ids[0], filepath.ToSlash(e.paths[1]))).(models.DeckResponse)
	require.True(t, ok)
	assert.Len(t, created.DeckID, database.DeckIDLength)
	assert.Equal(t, "Weekend", created.Name)
	assert.Equal(t, "Games", created.Description)
	assert.True(t, created.Owned)
	assert.Equal(t, 4, created.ItemCount)
	require.Len(t, created.Items, 4)
	e.expectNotification(t, created.DeckID, models.DecksChangedCreated)

	metroid := created.Items[0]
	assert.Equal(t, "script", metroid.Kind)
	assert.Equal(t, "Metroid", metroid.Name)
	assert.Contains(t, metroid.ZapScript, "**launch.title:NES/Metroid")
	require.NotNil(t, metroid.Media)
	assert.Equal(t, filepath.ToSlash(e.paths[0]), metroid.Media.Path)
	assert.True(t, metroid.Media.Available)

	hack := created.Items[1]
	assert.Equal(t, "My Hack", hack.Name, "a supplied name overrides the indexed title")
	assert.Contains(t, hack.ZapScript, "(unlicensed:hack)")

	assert.Equal(t, "Random", created.Items[2].Name)
	assert.Nil(t, created.Items[2].Media)
	assert.Equal(t, "abcd1234", created.Items[3].CardID)
	assert.JSONEq(t, `{"image_url":"https://x/y.png"}`, string(created.Items[3].Metadata))
	require.Len(t, created.Items[3].Scripts, 1)

	got, ok := e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)).(models.DeckResponse)
	require.True(t, ok)
	assert.Equal(t, created.DeckID, got.DeckID)
	require.Len(t, got.Items, 4)
	positions := make([]int, 0, len(got.Items))
	for _, item := range got.Items {
		positions = append(positions, item.Position)
	}
	assert.Equal(t, []int{1, 2, 3, 4}, positions)

	list, ok := e.call(t, HandleDecks, `{}`).(models.DecksResponse)
	require.True(t, ok)
	require.Len(t, list.Decks, 1)
	assert.Nil(t, list.Decks[0].Items)
	assert.Equal(t, 4, list.Decks[0].ItemCount)

	// Both game items carry the deck's membership tag, so the deck can be
	// browsed and searched like any other tag.
	e.tagDecks(t)
	tagged := searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID})
	taggedIDs := make([]int64, 0, len(tagged.Results))
	for _, r := range tagged.Results {
		taggedIDs = append(taggedIDs, r.MediaID)
	}
	assert.ElementsMatch(t, e.ids, taggedIDs)

	_, isNoContent := e.call(t, HandleDecksDelete, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)).(NoContent)
	assert.True(t, isNoContent)
	e.expectNotification(t, created.DeckID, models.DecksChangedDeleted)
	e.tagDecks(t)
	assert.Empty(t, searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID}).Results,
		"deleting a deck removes its membership tags")
	_, err := HandleDecksGet(withParams(&e.env, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)))
	require.ErrorIs(t, err, database.ErrDeckNotFound)
	_, err = HandleDecksDelete(withParams(&e.env, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)))
	require.ErrorIs(t, err, database.ErrDeckNotFound)
}

func TestHandleDecksUpdate(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)

	created, ok := e.call(t, HandleDecksNew, `{"name": "Edit me", "items": [
		{"kind": "script", "name": "A", "zapscript": "**a"},
		{"kind": "script", "name": "B", "zapscript": "**b"}]}`).(models.DeckResponse)
	require.True(t, ok)
	e.expectNotification(t, created.DeckID, models.DecksChangedCreated)

	updated, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "name": "Edited",
		"removeItemIds": [%d], "addItems": [{"kind": "media", "mediaId": %d}]}`,
		created.DeckID, created.Items[0].ID, e.ids[0])).(models.DeckResponse)
	require.True(t, ok)
	assert.Equal(t, "Edited", updated.Name)
	require.Len(t, updated.Items, 2)
	assert.Equal(t, "B", updated.Items[0].Name)
	assert.Equal(t, 1, updated.Items[0].Position)
	assert.Equal(t, "Metroid", updated.Items[1].Name)
	assert.Equal(t, 2, updated.Items[1].Position)
	e.expectNotification(t, created.DeckID, models.DecksChangedUpdated)

	e.tagDecks(t)
	tagged := searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID})
	require.Len(t, tagged.Results, 1, "the media item added by the update carries the tag")
	assert.Equal(t, e.ids[0], tagged.Results[0].MediaID)

	replaced, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "items": [
		{"kind": "script", "name": "Only", "zapscript": "**only"}]}`, created.DeckID)).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, replaced.Items, 1)
	assert.Equal(t, "Only", replaced.Items[0].Name)
	assert.Equal(t, "Edited", replaced.Name, "an items-only update keeps the name")
	e.tagDecks(t)
	assert.Empty(t, searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID}).Results,
		"replacing the items moves the tag with them")

	_, err := HandleDecksUpdate(withParams(&e.env, fmt.Sprintf(`{"deckId":%q, "name": "  "}`, created.DeckID)))
	require.Error(t, err)
	_, err = HandleDecksUpdate(withParams(&e.env, fmt.Sprintf(
		`{"deckId":%q, "addItems": [{"kind": "script", "name": "x"}]}`, created.DeckID)))
	require.Error(t, err, "a script item needs a zapscript")
	_, err = HandleDecksUpdate(withParams(&e.env, fmt.Sprintf(
		`{"deckId":%q, "addItems": [{"kind": "media", "mediaId": 999999}]}`, created.DeckID)))
	require.Error(t, err, "an unknown media id is rejected")
}

func (e *decksTestEnv) expectNoNotification(t *testing.T) {
	t.Helper()
	select {
	case n := <-e.ns:
		t.Fatalf("unexpected %s notification: %s", n.Method, n.Params)
	default:
	}
}

func deckItemIDs(items []models.DeckItemResponse) []int64 {
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	return ids
}

// A title item that resolves on this device is tagged and linked to the file
// it matched once the queued deck tag work runs. A linked file that a later
// scan did not find is reported unavailable.
func TestHandleDecks_TitleItemLinkedByTagger(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)

	created, ok := e.call(t, HandleDecksNew, `{"name": "Titles", "items": [
		{"kind": "script", "name": "Metroid", "zapscript": "**launch.title:NES/Metroid"}]}`).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, created.Items, 1)
	assert.Nil(t, created.Items[0].Media, "the reply does not wait for title matching")
	assert.Empty(t, searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID}).Results)

	e.tagDecks(t)
	tagged := searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID})
	require.Len(t, tagged.Results, 1)
	assert.Equal(t, e.ids[0], tagged.Results[0].MediaID)
	got, ok := e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)).(models.DeckResponse)
	require.True(t, ok)
	require.NotNil(t, got.Items[0].Media, "the item is linked to the file its title matched")
	assert.Equal(t, filepath.ToSlash(e.paths[0]), got.Items[0].Media.Path)
	assert.True(t, got.Items[0].Media.Available)

	addTestMediaPaths(t, e.env.Database.MediaDB, e.paths[1])
	got, ok = e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)).(models.DeckResponse)
	require.True(t, ok)
	require.NotNil(t, got.Items[0].Media)
	assert.Equal(t, filepath.ToSlash(e.paths[0]), got.Items[0].Media.Path, "a missing file keeps the link")
	assert.False(t, got.Items[0].Media.Available, "a missing file is not available")
}

// A request that renames a deck and adds an item that cannot be resolved
// fails as a whole: the name it asked for is not kept either.
func TestHandleDecksUpdate_FailedEditChangesNothing(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	created, ok := e.call(t, HandleDecksNew, `{"name": "Before", "items": [
		{"kind": "script", "name": "A", "zapscript": "**a"}]}`).(models.DeckResponse)
	require.True(t, ok)
	e.expectNotification(t, created.DeckID, models.DecksChangedCreated)

	for _, params := range []string{
		`{"deckId":%q, "name": "After", "addItems": [{"kind": "media", "mediaId": 999999}]}`,
		`{"deckId":%q, "description": "After", "items": [{"kind": "script", "name": "x"}]}`,
		`{"deckId":%q, "name": "After", "removeItemIds": [%d], "addItems": [{"kind": "card"}]}`,
	} {
		var raw string
		if strings.Contains(params, "removeItemIds") {
			raw = fmt.Sprintf(params, created.DeckID, created.Items[0].ID)
		} else {
			raw = fmt.Sprintf(params, created.DeckID)
		}
		_, err := HandleDecksUpdate(withParams(&e.env, raw))
		require.Error(t, err, raw)
	}
	e.expectNoNotification(t)

	got, ok := e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)).(models.DeckResponse)
	require.True(t, ok)
	assert.Equal(t, "Before", got.Name)
	assert.Empty(t, got.Description)
	assert.Equal(t, deckItemIDs(created.Items), deckItemIDs(got.Items))
}

// Item IDs are stable, so a client holding the IDs it listed can still
// remove an item after another client changed the deck.
func TestHandleDecksUpdate_ItemIDsSurviveOtherEdits(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	created, ok := e.call(t, HandleDecksNew, fmt.Sprintf(`{"name": "Stable", "items": [
		{"kind": "script", "name": "A", "zapscript": "**a"},
		{"kind": "media", "mediaId": %d},
		{"kind": "script", "name": "C", "zapscript": "**c"}]}`, e.ids[0])).(models.DeckResponse)
	require.True(t, ok)
	listed := deckItemIDs(created.Items)

	// Another client appends an item and renames the deck.
	appended, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "name": "Stable 2",
		"addItems": [{"kind": "script", "name": "D", "zapscript": "**d"}]}`, created.DeckID)).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, appended.Items, 4)
	assert.Equal(t, listed, deckItemIDs(appended.Items[:3]), "an append leaves the other IDs alone")

	// The first client removes the game by the ID it listed before.
	removed, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "removeItemIds": [%d]}`,
		created.DeckID, listed[1])).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, removed.Items, 3)
	assert.Equal(t, []int64{listed[0], listed[2], appended.Items[3].ID}, deckItemIDs(removed.Items))
	assert.Equal(t, []string{"A", "C", "D"},
		[]string{removed.Items[0].Name, removed.Items[1].Name, removed.Items[2].Name})
	assert.Equal(t, 2, removed.Items[1].Position)

	// Removing an item that is already gone is not an error.
	again, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "removeItemIds": [%d]}`,
		created.DeckID, listed[1])).(models.DeckResponse)
	require.True(t, ok)
	assert.Len(t, again.Items, 3)
}

// items may keep existing entries by id, so a client can reorder a deck
// without re-adding anything: kept items keep their IDs and their file links,
// even when the file is not indexed right now and could not be added again.
func TestHandleDecksUpdate_ItemReferences(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)

	gone := database.DeckItem{
		Kind: database.DeckItemKindScript, Name: "Gone", ZapScript: "**launch.title:NES/Gone",
		Anchor: database.DeckItemAnchor{SystemID: "NES", Path: "roms/NES/Gone (USA).nes", MediaName: "Gone"},
	}
	deck := &database.Deck{DeckID: "0123456789ab", Name: "Refs", Owned: true, Items: []database.DeckItem{
		{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**a"}, gone,
	}}
	require.NoError(t, e.env.Database.UserDB.CreateDeck(deck))
	got, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":"0123456789ab",
		"addItems": [{"kind": "media", "mediaId": %d}]}`, e.ids[0])).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, got.Items, 3)
	idA, idGone, idMetroid := got.Items[0].ID, got.Items[1].ID, got.Items[2].ID
	require.NotNil(t, got.Items[1].Media)
	assert.False(t, got.Items[1].Media.Available)

	reordered, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":"0123456789ab", "items": [
		{"id": %d}, {"kind": "script", "name": "New", "zapscript": "**new"}, {"id": %d}, {"id": %d}]}`,
		idMetroid, idGone, idA)).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, reordered.Items, 4)
	assert.Equal(t, []string{"Metroid", "New", "Gone", "A"},
		[]string{reordered.Items[0].Name, reordered.Items[1].Name, reordered.Items[2].Name, reordered.Items[3].Name})
	assert.Equal(t, idMetroid, reordered.Items[0].ID)
	assert.NotContains(t, []int64{idA, idGone, idMetroid}, reordered.Items[1].ID)
	assert.Equal(t, idGone, reordered.Items[2].ID)
	assert.Equal(t, idA, reordered.Items[3].ID)
	require.NotNil(t, reordered.Items[0].Media)
	assert.True(t, reordered.Items[0].Media.Available)
	require.NotNil(t, reordered.Items[2].Media, "a kept item keeps its file link")
	assert.Equal(t, "roms/NES/Gone (USA).nes", reordered.Items[2].Media.Path)
	assert.Equal(t, "**launch.title:NES/Gone", reordered.Items[2].ZapScript)
	positions := make([]int, 0, len(reordered.Items))
	for _, item := range reordered.Items {
		positions = append(positions, item.Position)
	}
	assert.Equal(t, []int{1, 2, 3, 4}, positions)

	// Dropping an item from the list removes it.
	trimmed, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":"0123456789ab", "items": [
		{"id": %d}, {"id": %d}]}`, idA, idMetroid)).(models.DeckResponse)
	require.True(t, ok)
	assert.Equal(t, []int64{idA, idMetroid}, deckItemIDs(trimmed.Items))

	for _, tc := range []struct {
		target error
		params string
	}{
		{
			database.ErrDeckItemNotFound,
			fmt.Sprintf(`{"deckId":"0123456789ab", "name": "No", "items": [{"id": %d}]}`, idGone),
		},
		{
			database.ErrDeckItemRepeated,
			fmt.Sprintf(`{"deckId":"0123456789ab", "items": [{"id": %d}, {"id": %d}]}`, idA, idA),
		},
		{nil, fmt.Sprintf(`{"deckId":"0123456789ab", "addItems": [{"id": %d}]}`, idA)},
		{nil, fmt.Sprintf(`{"deckId":"0123456789ab", "items": [{"id": %d, "name": "Renamed"}]}`, idA)},
		{nil, fmt.Sprintf(`{"deckId":"0123456789ab", "items": [{"id": %d, "kind": "script"}]}`, idA)},
		{nil, `{"deckId":"0123456789ab", "items": [{"name": "No kind"}]}`},
	} {
		_, err := HandleDecksUpdate(withParams(&e.env, tc.params))
		require.Error(t, err, tc.params)
		if tc.target != nil {
			require.ErrorIs(t, err, tc.target, tc.params)
		}
	}
	_, err := HandleDecksNew(withParams(&e.env, fmt.Sprintf(`{"name": "Copy", "items": [{"id": %d}]}`, idA)))
	require.Error(t, err, "decks.new cannot name an existing item")

	after, ok := e.call(t, HandleDecksGet, `{"deckId":"0123456789ab"}`).(models.DeckResponse)
	require.True(t, ok)
	assert.Equal(t, "Refs", after.Name, "a refused edit changes nothing")
	assert.Equal(t, []int64{idA, idMetroid}, deckItemIDs(after.Items))
}

// A deck with no items still lists them, as [], wherever items are part of
// the answer; only the decks list leaves them out.
func TestHandleDecks_EmptyDeckItemsList(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)

	hasItemsKey := func(result any) (json.RawMessage, bool) {
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(encoded, &fields))
		items, ok := fields["items"]
		return items, ok
	}

	created := e.call(t, HandleDecksNew, `{"name": "Empty"}`)
	items, ok := hasItemsKey(created)
	require.True(t, ok, "decks.new lists the items of an empty deck")
	assert.JSONEq(t, `[]`, string(items))
	deck, ok := created.(models.DeckResponse)
	require.True(t, ok)

	items, ok = hasItemsKey(e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, deck.DeckID)))
	require.True(t, ok, "decks.get lists the items of an empty deck")
	assert.JSONEq(t, `[]`, string(items))

	e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "addItems": [
		{"kind": "script", "name": "A", "zapscript": "**a"}]}`, deck.DeckID))
	items, ok = hasItemsKey(e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "items": []}`, deck.DeckID)))
	require.True(t, ok, "decks.update lists the items of a deck it emptied")
	assert.JSONEq(t, `[]`, string(items))

	list, ok := e.call(t, HandleDecks, `{}`).(models.DecksResponse)
	require.True(t, ok)
	require.Len(t, list.Decks, 1)
	_, ok = hasItemsKey(list.Decks[0])
	assert.False(t, ok, "the decks list leaves items out")
}

// An update that asks for nothing returns the deck untouched and tells no
// one about it.
func TestHandleDecksUpdate_NoChange(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	created, ok := e.call(t, HandleDecksNew, `{"name": "Same", "items": [
		{"kind": "script", "name": "A", "zapscript": "**a"}]}`).(models.DeckResponse)
	require.True(t, ok)
	e.expectNotification(t, created.DeckID, models.DecksChangedCreated)
	revision, _, err := e.env.Database.UserDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)

	got, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "addItems": [], "removeItemIds": []}`,
		strings.ToUpper(created.DeckID))).(models.DeckResponse)
	require.True(t, ok)
	assert.Equal(t, created.DeckID, got.DeckID)
	assert.Equal(t, deckItemIDs(created.Items), deckItemIDs(got.Items))
	e.expectNoNotification(t)
	after, _, err := e.env.Database.UserDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	assert.Equal(t, revision, after)

	_, err = HandleDecksUpdate(withParams(&e.env, `{"deckId":"zzzzzzzzzzzz"}`))
	require.ErrorIs(t, err, database.ErrDeckNotFound)
}

func TestHandleDecksUpdate_CachedDeckIsReadOnly(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	_, err := e.env.Database.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Theirs", Owned: false,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**a"}},
	})
	require.NoError(t, err)
	got, ok := e.call(t, HandleDecksGet, `{"deckId":"0123456789AB"}`).(models.DeckResponse)
	require.True(t, ok)
	assert.False(t, got.Owned)

	_, err = HandleDecksUpdate(withParams(&e.env, `{"deckId":"0123456789ab", "name": "Mine now"}`))
	require.ErrorIs(t, err, database.ErrDeckReadOnly)

	_, err = HandleDecksUpdate(withParams(&e.env, `{"deckId":"0123456789ab", "removeItemIds": [1]}`))
	require.ErrorIs(t, err, database.ErrDeckReadOnly)

	// A cached deck can still be removed.
	_, isNoContent := e.call(t, HandleDecksDelete, `{"deckId":"0123456789ab"}`).(NoContent)
	assert.True(t, isNoContent)

	// A legacy eight-character ID may hold I, L, O and U.
	_, err = e.env.Database.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "KQ7RIL0U", Name: "Old deck", Owned: false,
	})
	require.NoError(t, err)
	legacy, ok := e.call(t, HandleDecksGet, `{"deckId":"kq7riL0u"}`).(models.DeckResponse)
	require.True(t, ok)
	assert.Equal(t, "kq7ril0u", legacy.DeckID)
	assert.Equal(t, "Old deck", legacy.Name)
	_, isNoContent = e.call(t, HandleDecksDelete, `{"deckId":"KQ7RIL0U"}`).(NoContent)
	assert.True(t, isNoContent)
}

func TestHandleDecks_LockedDeckIsReadOnly(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	require.NoError(t, e.env.Database.UserDB.CreateDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Locked", Owned: true,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**a"}},
	}))
	require.NoError(t, e.env.Database.UserDB.UpsertDeckSync([]database.DeckSyncRow{
		{DeckID: "0123456789ab", Revision: 3, Locked: true},
	}))

	var signals atomic.Int32
	e.env.State.SetLibrarySyncSignals(state.LibrarySyncSignals{
		DecksAccessed: func() { signals.Add(1) },
		DecksChanged:  func() { t.Error("a refused edit must not signal a push") },
	})

	got, ok := e.call(t, HandleDecksGet, `{"deckId":"0123456789ab"}`).(models.DeckResponse)
	require.True(t, ok)
	assert.True(t, got.Locked)
	list, ok := e.call(t, HandleDecks, `{}`).(models.DecksResponse)
	require.True(t, ok)
	require.Len(t, list.Decks, 1)
	assert.True(t, list.Decks[0].Locked)
	assert.Equal(t, int32(2), signals.Load(), "looking at decks asks for a pull")

	_, err := HandleDecksUpdate(withParams(&e.env, `{"deckId":"0123456789ab", "name": "Changed"}`))
	require.ErrorIs(t, err, database.ErrDeckReadOnly)
	_, err = HandleDecksDelete(withParams(&e.env, `{"deckId":"0123456789ab"}`))
	require.ErrorIs(t, err, database.ErrDeckReadOnly)
}

func TestHandleDecksNew_SignalsSync(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	var changed atomic.Int32
	e.env.State.SetLibrarySyncSignals(state.LibrarySyncSignals{DecksChanged: func() { changed.Add(1) }})
	e.call(t, HandleDecksNew, `{"name":"Weekend"}`)
	assert.Equal(t, int32(1), changed.Load())
}

func TestHandleDecksNew_Validation(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)

	items := make([]models.DeckItemInput, 0, database.DeckMaxItems+1)
	for range database.DeckMaxItems + 1 {
		items = append(items, models.DeckItemInput{Kind: "script", Name: "x", ZapScript: "**x"})
	}
	tooMany, err := json.Marshal(models.DecksNewParams{Name: "Big", Items: items})
	require.NoError(t, err)
	_, err = HandleDecksNew(withParams(&e.env, string(tooMany)))
	require.Error(t, err, "more than the item cap is rejected")

	_, err = HandleDecksNew(withParams(&e.env, `{"name": "", "items": []}`))
	require.Error(t, err)
	_, err = HandleDecksNew(withParams(&e.env, `{"name": "Bad kind", "items": [{"kind": "deck"}]}`))
	require.Error(t, err)
	_, err = HandleDecksNew(withParams(&e.env, `{"name": "Bad card", "items": [{"kind": "card"}]}`))
	require.Error(t, err)
	_, err = HandleDecksNew(withParams(&e.env, `{"name": "Blank card script", "items": [
		{"kind": "card", "cardId": "abcd1234", "scripts": [{"name": "Play", "zapscript": "   "}]}]}`))
	require.Error(t, err, "a card script that is only whitespace is rejected")
	card, ok := e.call(t, HandleDecksNew, `{"name": "Trimmed card", "items": [
		{"kind": "card", "cardId": "abcd1234",
		 "scripts": [{"name": " Play ", "zapscript": " **launch.random:NES "}]}]}`,
	).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, card.Items[0].Scripts, 1)
	assert.Equal(t, "Play", card.Items[0].Scripts[0].Name)
	assert.Equal(t, "**launch.random:NES", card.Items[0].Scripts[0].ZapScript)
	_, err = HandleDecksNew(withParams(&e.env, `{"name": "Bad media", "items": [{"kind": "media", "system": "NES"}]}`))
	require.Error(t, err, "a media item needs a mediaId or system plus path")
	_, err = HandleDecksGet(withParams(&e.env, `{"deckId": "not-an-id"}`))
	require.ErrorIs(t, err, database.ErrInvalidDeckID)
}

// decks.open runs the playlist command that names the deck through the same
// path as run, so it waits for the result the way a run request does.
func TestHandleDecksOpen(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	run := newRunTestEnv(t)
	created, ok := e.call(t, HandleDecksNew, `{"name": "Open me", "items": [
		{"kind": "script", "name": "A", "zapscript": "**launch.system:NES"}]}`).(models.DeckResponse)
	require.True(t, ok)
	e.expectNotification(t, created.DeckID, models.DecksChangedCreated)

	env := e.env
	env.State = run.st
	env.TokenQueue = run.queue
	env.Params = []byte(fmt.Sprintf(`{"deckId":%q, "slot":"background"}`, strings.ToUpper(created.DeckID)))
	out := make(chan runOutcome, 1)
	go func() {
		result, err := HandleDecksOpen(env)
		out <- runOutcome{result: result, err: err}
	}()
	tok := run.receiveToken(t)
	parsed, err := zapscript.NewParser(tok.Text).ParseScript()
	require.NoError(t, err)
	require.Len(t, parsed.Cmds, 1)
	assert.Equal(t, zapscript.ZapScriptCmdPlaylistOpen, parsed.Cmds[0].Name)
	assert.Equal(t, []string{"deck://" + created.DeckID}, parsed.Cmds[0].Args)
	assert.Equal(t, "background", parsed.Cmds[0].AdvArgs.Get(zapscript.KeySlot))
	require.True(t, tok.Completion.Complete(nil))
	o := waitRun(t, out)
	require.NoError(t, o.err)

	_, err = HandleDecksOpen(withParams(&e.env, `{"deckId":"zzzzzzzzzzzz"}`))
	require.ErrorIs(t, err, database.ErrDeckNotFound)
}

// TestHandleDecks_SyncStateReadFailureIsReported pins that a deck's lock is
// never reported as "unlocked" because the sync bookkeeping could not be
// read. Answering false would show a locked deck as editable.
func TestHandleDecks_SyncStateReadFailureIsReported(t *testing.T) {
	t.Parallel()
	mockUserDB := testhelpers.NewMockUserDBI()
	readErr := errors.New("deck sync unavailable")
	mockUserDB.On("ListDecks").Return([]database.Deck{
		{DeckID: "0123456789ab", Name: "Weekend", Owned: true},
	}, nil)
	mockUserDB.On("ListDeckSync").Return(nil, readErr)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}
	_, err := HandleDecks(withParams(&env, `{}`))
	require.ErrorIs(t, err, readErr, "the list fails rather than showing every deck as unlocked")
}

// TestHandleDecksGet_SyncStateReadFailureIsReported is the same for one deck.
func TestHandleDecksGet_SyncStateReadFailureIsReported(t *testing.T) {
	t.Parallel()
	mockUserDB := testhelpers.NewMockUserDBI()
	readErr := errors.New("deck sync unavailable")
	mockUserDB.On("GetDeck", "0123456789ab").Return(&database.Deck{
		DeckID: "0123456789ab", Name: "Weekend", Owned: true,
	}, nil)
	mockUserDB.On("GetDeckSync", "0123456789ab").Return(database.DeckSyncRow{}, false, readErr)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{UserDB: mockUserDB},
	}
	_, err := HandleDecksGet(withParams(&env, `{"deckId":"0123456789ab"}`))
	require.ErrorIs(t, err, readErr)
}
