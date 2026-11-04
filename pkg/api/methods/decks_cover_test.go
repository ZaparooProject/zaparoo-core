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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func testPNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for x := range side {
		for y := range side {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// withDataDir gives the deck test env a platform whose data directory is a
// temporary folder, where deck covers are stored.
func (e *decksTestEnv) withDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: dir, HostManagedPaths: true})
	e.env.Platform = pl
	return dir
}

func (e *decksTestEnv) newDeck(t *testing.T, name, items string) string {
	t.Helper()
	created, ok := e.call(t, HandleDecksNew, fmt.Sprintf(`{"name":%q,"items":[%s]}`, name, items)).(models.DeckResponse)
	require.True(t, ok)
	return created.DeckID
}

func TestHandleDecks_MembershipForAGame(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	withGame := e.newDeck(t, "With", fmt.Sprintf(
		`{"kind":"script","name":"S","zapscript":"**a"},{"kind":"media","mediaId":%d}`, e.ids[0]))
	without := e.newDeck(t, "Without", fmt.Sprintf(`{"kind":"media","mediaId":%d}`, e.ids[1]))
	full, ok := e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, withGame)).(models.DeckResponse)
	require.True(t, ok)

	list, ok := e.call(t, HandleDecks, fmt.Sprintf(`{"system":"NES","path":%q}`, e.paths[0])).(models.DecksResponse)
	require.True(t, ok)
	byID := map[string][]int64{}
	for _, deck := range list.Decks {
		require.NotNil(t, deck.ContainsItemIDs, deck.DeckID)
		byID[deck.DeckID] = deck.ContainsItemIDs
	}
	assert.Equal(t, []int64{full.Items[1].ID}, byID[withGame])
	assert.Equal(t, []int64{}, byID[without])

	raw, err := json.Marshal(list.Decks[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"containsItemIds":`)

	plain, ok := e.call(t, HandleDecks, `{}`).(models.DecksResponse)
	require.True(t, ok)
	for _, deck := range plain.Decks {
		assert.Nil(t, deck.ContainsItemIDs)
		raw, err = json.Marshal(deck)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "containsItemIds")
	}

	for _, params := range []string{`{"system":"NES"}`, fmt.Sprintf(`{"path":%q}`, e.paths[0])} {
		_, err = HandleDecks(withParams(&e.env, params))
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr, params)
	}
}

func TestHandleDecks_CardItemHasImage(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	deckID := e.newDeck(t, "Cards",
		`{"kind":"card","cardId":"aaaa1111","metadata":{"rendered_url":"https://img.example/r.png"}},`+
			`{"kind":"card","cardId":"bbbb2222","metadata":{"image_url":"http://img.example/i.png"}},`+
			`{"kind":"card","cardId":"cccc3333"}`)
	got, ok := e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, deckID)).(models.DeckResponse)
	require.True(t, ok)
	assert.False(t, got.HasImage)
	require.Len(t, got.Items, 3)
	assert.True(t, got.Items[0].HasImage)
	assert.False(t, got.Items[1].HasImage, "only https images are served")
	assert.False(t, got.Items[2].HasImage)
}

func TestHandleDecksUpdate_CoverKinds(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	dataDir := e.withDataDir(t)
	deckID := e.newDeck(t, "Covered", `{"kind":"script","name":"S","zapscript":"**a"}`)
	e.expectNotification(t, deckID, models.DecksChangedCreated)

	coverPath := filepath.Join(t.TempDir(), "cover.png")
	coverPNG := testPNG(t, 8)
	require.NoError(t, os.WriteFile(coverPath, coverPNG, 0o600))
	before, err := e.env.Database.UserDB.GetDeck(deckID)
	require.NoError(t, err)

	got, ok := e.call(t, HandleDecksUpdate, fmt.Sprintf(
		`{"deckId":%q,"cover":{"kind":"file","path":%q}}`, deckID, coverPath)).(models.DeckResponse)
	require.True(t, ok)
	assert.True(t, got.HasImage)
	assert.GreaterOrEqual(t, got.UpdatedAt, before.UpdatedAt)
	e.expectNotification(t, deckID, models.DecksChangedUpdated)
	stored, err := e.env.Database.UserDB.GetDeck(deckID)
	require.NoError(t, err)
	assert.Equal(t, database.DeckCoverPendingSet, stored.CoverPending)
	kept, err := os.ReadFile(filepath.Join(dataDir, "decks", "covers", stored.CoverFile)) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, coverPNG, kept)

	served, ok := e.call(t, HandleDecksImage, fmt.Sprintf(`{"deckId":%q}`, deckID)).(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "deck:cover", served.TypeTag)
	assert.Equal(t, "inline", served.Delivery)
	assert.Equal(t, "image/png", served.ContentType)
	assert.Equal(t, base64.StdEncoding.EncodeToString(coverPNG), served.Data)

	// A game's art: the media has none indexed here, so it is refused.
	_, err = HandleDecksUpdate(withParams(&e.env, fmt.Sprintf(
		`{"deckId":%q,"cover":{"kind":"media","system":"NES","path":%q}}`, deckID, e.paths[0])))
	require.Error(t, err)

	got, ok = e.call(t, HandleDecksUpdate,
		fmt.Sprintf(`{"deckId":%q,"cover":{"kind":"none"}}`, deckID)).(models.DeckResponse)
	require.True(t, ok)
	assert.False(t, got.HasImage)
	stored, err = e.env.Database.UserDB.GetDeck(deckID)
	require.NoError(t, err)
	assert.Empty(t, stored.CoverFile)
	assert.Equal(t, database.DeckCoverPendingRemove, stored.CoverPending)
	_, err = HandleDecksImage(withParams(&e.env, fmt.Sprintf(`{"deckId":%q}`, deckID)))
	require.EqualError(t, err, "image not found")

	for _, cover := range []string{
		`{"kind":"file","path":"relative.png"}`,
		fmt.Sprintf(`{"kind":"file","path":%q}`, filepath.Join(t.TempDir(), "missing.png")),
		`{"kind":"media","system":"NES"}`,
		`{"kind":"none","path":"/x"}`,
		`{"kind":"other"}`,
	} {
		_, err = HandleDecksUpdate(withParams(&e.env, fmt.Sprintf(`{"deckId":%q,"cover":%s}`, deckID, cover)))
		require.Error(t, err, cover)
	}

	notImage := filepath.Join(t.TempDir(), "notes.png")
	require.NoError(t, os.WriteFile(notImage, []byte("not an image at all"), 0o600))
	_, err = HandleDecksUpdate(withParams(&e.env, fmt.Sprintf(
		`{"deckId":%q,"cover":{"kind":"file","path":%q}}`, deckID, notImage)))
	require.Error(t, err)
}

func TestHandleDecksUpdate_CoverRefusedOnReadOnlyDecks(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	e.withDataDir(t)
	coverPath := filepath.Join(t.TempDir(), "cover.png")
	require.NoError(t, os.WriteFile(coverPath, testPNG(t, 4), 0o600))

	_, err := e.env.Database.UserDB.UpsertRemoteDeck(&database.Deck{DeckID: "0123456789ab", Name: "Theirs"})
	require.NoError(t, err)
	require.NoError(t, e.env.Database.UserDB.CreateDeck(&database.Deck{
		DeckID: "abcdefghjkmn", Name: "Locked", Owned: true,
	}))
	require.NoError(t, e.env.Database.UserDB.UpsertDeckSync([]database.DeckSyncRow{
		{DeckID: "abcdefghjkmn", Revision: 2, Locked: true},
	}))
	for _, deckID := range []string{"0123456789ab", "abcdefghjkmn"} {
		for _, cover := range []string{fmt.Sprintf(`{"kind":"file","path":%q}`, coverPath), `{"kind":"none"}`} {
			_, err = HandleDecksUpdate(withParams(&e.env, fmt.Sprintf(`{"deckId":%q,"cover":%s}`, deckID, cover)))
			require.ErrorIs(t, err, database.ErrDeckReadOnly, deckID)
		}
	}
}

// The remote image tests swap the package's HTTP client and thumbnail cache,
// so they do not run in parallel.
func useDeckImageServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	previous := deckImageClient
	deckImageClient = server.Client()
	t.Cleanup(func() { deckImageClient = previous })
	installThumbCache(t, newMemThumbCache())
	return server
}

//nolint:paralleltest // swaps the deck image client and the thumbnail cache
func TestHandleDecksImage_RemoteImageIsFetchedOnceAndCached(t *testing.T) {
	cover := testPNG(t, 64)
	var hits atomic.Int32
	server := useDeckImageServer(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(cover)
	})
	e := newDecksTestEnv(t)
	_, err := e.env.Database.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: "0123456789ab", Name: "Online", ImageURL: server.URL + "/deck.png",
		Items: []database.DeckItem{{
			Kind: database.DeckItemKindCard, CardID: "aaaa1111",
			Metadata: json.RawMessage(fmt.Sprintf(`{"image_url":%q}`, server.URL+"/card.png")),
		}},
	})
	require.NoError(t, err)

	list, ok := e.call(t, HandleDecks, `{}`).(models.DecksResponse)
	require.True(t, ok)
	require.Len(t, list.Decks, 1)
	assert.True(t, list.Decks[0].HasImage)

	full, ok := e.call(t, HandleDecksImage, `{"deckId":"0123456789ab"}`).(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, base64.StdEncoding.EncodeToString(cover), full.Data)
	assert.Equal(t, "deck:cover", full.TypeTag)
	again, ok := e.call(t, HandleDecksImage, `{"deckId":"0123456789ab"}`).(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, full.Data, again.Data)
	assert.Equal(t, int32(1), hits.Load(), "the image is fetched once and then read from the cache")

	thumb, ok := e.call(t, HandleDecksImage,
		`{"deckId":"0123456789ab","maxSize":30,"delivery":"localPath"}`).(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "localPath", thumb.Delivery)
	assert.NotEmpty(t, thumb.LocalPath)
	assert.Equal(t, int32(1), hits.Load())

	got, ok := e.call(t, HandleDecksGet, `{"deckId":"0123456789ab"}`).(models.DeckResponse)
	require.True(t, ok)
	card, ok := e.call(t, HandleDecksImage, fmt.Sprintf(
		`{"deckId":"0123456789ab","itemId":%d}`, got.Items[0].ID)).(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "deck:card", card.TypeTag)
	assert.Equal(t, int32(2), hits.Load())

	_, err = HandleDecksImage(withParams(&e.env, `{"deckId":"0123456789ab","itemId":99999}`))
	require.ErrorIs(t, err, database.ErrDeckItemNotFound)
	_, err = HandleDecksImage(withParams(&e.env, `{"deckId":"0123456789ab","delivery":"localPath"}`))
	require.Error(t, err, "localPath needs a maxSize")
}

//nolint:paralleltest // swaps the deck image client and the thumbnail cache
func TestHandleDecksImage_OnlyHTTPSAndBounded(t *testing.T) {
	var hits atomic.Int32
	server := useDeckImageServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/big.png" {
			big := make([]byte, 10<<20+10)
			copy(big, testPNG(t, 2))
			_, _ = w.Write(big)
			return
		}
		http.NotFound(w, r)
	})
	e := newDecksTestEnv(t)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(testPNG(t, 2))
	}))
	t.Cleanup(plain.Close)

	for deckID, imageURL := range map[string]string{
		"0123456789ab": plain.URL + "/deck.png",
		"0123456789ac": server.URL + "/missing.png",
		"0123456789ad": server.URL + "/big.png",
	} {
		_, err := e.env.Database.UserDB.UpsertRemoteDeck(&database.Deck{
			DeckID: deckID, Name: deckID, ImageURL: imageURL,
		})
		require.NoError(t, err)
	}
	got, ok := e.call(t, HandleDecksGet, `{"deckId":"0123456789ab"}`).(models.DeckResponse)
	require.True(t, ok)
	assert.False(t, got.HasImage, "an http image is never offered")
	_, err := HandleDecksImage(withParams(&e.env, `{"deckId":"0123456789ab"}`))
	require.EqualError(t, err, "image not found")
	assert.Zero(t, hits.Load(), "an http URL is never fetched")

	_, err = HandleDecksImage(withParams(&e.env, `{"deckId":"0123456789ac"}`))
	require.EqualError(t, err, "image not found")
	_, err = HandleDecksImage(withParams(&e.env, `{"deckId":"0123456789ad"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10 MiB")
}

func TestHandleDecksUpdate_MediaCoverUsesTheGamesArt(t *testing.T) {
	t.Parallel()
	row := makeMediaFullRow(9301, 93010)
	blob := testPNG(t, 16)
	mockDB := testhelpers.NewMockMediaDBI()
	expectMediaImageResolve(mockDB, row)
	mockDB.On("GetMediaProperties", mock.Anything, row.DBID).Return([]database.MediaProperty{}, nil).Maybe()
	mockDB.On("GetMediaTitleProperties", mock.Anything, row.Title.DBID).Return([]database.MediaProperty{
		{TypeTag: "property:image-boxart", ContentType: "image/png", Binary: blob},
	}, nil).Maybe()

	userDB, cleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, userDB.CreateDeck(&database.Deck{DeckID: "0123456789ab", Name: "Mine", Owned: true}))
	env := makeMediaImageEnv(t, mockDB, nil)
	env.Database.UserDB = userDB
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), HostManagedPaths: true})
	env.Platform = pl

	result, err := HandleDecksUpdate(withParams(&env, fmt.Sprintf(
		`{"deckId":"0123456789ab","cover":{"kind":"media","system":%q,"path":%q}}`, row.System.SystemID, row.Path)))
	require.NoError(t, err)
	got, ok := result.(models.DeckResponse)
	require.True(t, ok)
	assert.True(t, got.HasImage)

	result, err = HandleDecksImage(withParams(&env, `{"deckId":"0123456789ab"}`))
	require.NoError(t, err)
	served, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, base64.StdEncoding.EncodeToString(blob), served.Data)
}

func TestHandleDecksGet_ItemMediaCarriesUserTags(t *testing.T) {
	t.Parallel()
	e := newDecksTestEnv(t)
	deckID := e.newDeck(t, "Flags", fmt.Sprintf(
		`{"kind":"media","mediaId":%d},{"kind":"media","mediaId":%d},{"kind":"script","name":"S","zapscript":"**a"}`,
		e.ids[0], e.ids[1]))
	e.call(t, HandleMediaTagsUpdate, fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite","user:playlater"]}`, e.ids[0]))

	got, ok := e.call(t, HandleDecksGet, fmt.Sprintf(`{"deckId":%q}`, deckID)).(models.DeckResponse)
	require.True(t, ok)
	require.Len(t, got.Items, 3)
	require.NotNil(t, got.Items[0].Media)
	assert.Equal(t, []database.TagInfo{{Type: "user", Tag: "favorite"}, {Type: "user", Tag: "playlater"}},
		got.Items[0].Media.UserTags)
	require.NotNil(t, got.Items[1].Media)
	assert.Equal(t, []database.TagInfo{}, got.Items[1].Media.UserTags)
	assert.Nil(t, got.Items[2].Media)

	raw, err := json.Marshal(got.Items[1])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"userTags":[]`)
}
