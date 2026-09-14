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
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type decksTestEnv struct {
	ns    <-chan models.Notification
	paths []string
	ids   []int64
	env   requests.RequestEnv
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
	return &decksTestEnv{
		env: requests.RequestEnv{
			Context:  context.Background(),
			Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
			State:    st,
		},
		ns:    ns,
		paths: paths,
		ids:   ids,
	}
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
	tagged := searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID})
	taggedIDs := make([]int64, 0, len(tagged.Results))
	for _, r := range tagged.Results {
		taggedIDs = append(taggedIDs, r.MediaID)
	}
	assert.ElementsMatch(t, e.ids, taggedIDs)

	_, isNoContent := e.call(t, HandleDecksDelete, fmt.Sprintf(`{"deckId":%q}`, created.DeckID)).(NoContent)
	assert.True(t, isNoContent)
	e.expectNotification(t, created.DeckID, models.DecksChangedDeleted)
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

	tagged := searchByTags(t, &e.env, []string{"user:deck:" + created.DeckID})
	require.Len(t, tagged.Results, 1, "the media item added by the update carries the tag")
	assert.Equal(t, e.ids[0], tagged.Results[0].MediaID)

	replaced, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(`{"deckId":%q, "items": [
		{"kind": "script", "name": "Only", "zapscript": "**only"}]}`, created.DeckID)).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, replaced.Items, 1)
	assert.Equal(t, "Only", replaced.Items[0].Name)
	assert.Equal(t, "Edited", replaced.Name, "an items-only update keeps the name")
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

func TestHandleDecksUpdate_CachedDeckIsReadOnly(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	require.NoError(t, e.env.Database.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Theirs", Owned: false,
		Items: []database.DeckItem{{Kind: database.DeckItemKindScript, Name: "A", ZapScript: "**a"}},
	}))
	got, ok := e.call(t, HandleDecksGet, `{"deckId":"0123456789AB"}`).(models.DeckResponse)
	require.True(t, ok)
	assert.False(t, got.Owned)

	_, err := HandleDecksUpdate(withParams(&e.env, `{"deckId":"0123456789ab", "name": "Mine now"}`))
	require.ErrorIs(t, err, database.ErrDeckReadOnly)

	// A cached deck can still be removed.
	_, isNoContent := e.call(t, HandleDecksDelete, `{"deckId":"0123456789ab"}`).(NoContent)
	assert.True(t, isNoContent)
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
