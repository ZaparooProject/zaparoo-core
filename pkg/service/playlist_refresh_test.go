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

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decksChangedNotification(t *testing.T, deckID, action string) models.Notification {
	t.Helper()
	params, err := json.Marshal(models.DecksChangedNotification{DeckID: deckID, Action: action})
	require.NoError(t, err)
	return models.Notification{Method: models.NotificationDecksChanged, Params: params}
}

func TestRefreshOpenDeckPlaylist_QueuesRefreshForTheOpenDeck(t *testing.T) {
	t.Parallel()
	svc := setupPlaylistTestEnv(t)
	mockUserDB, ok := svc.DB.UserDB.(*testhelpers.MockUserDBI)
	require.True(t, ok)
	mockUserDB.On("GetDeck", "0123456789ab").Return(&database.Deck{
		DeckID: "0123456789ab", Name: "Weekend Plus", Owned: true,
		Items: []database.DeckItem{
			{Kind: database.DeckItemKindScript, Name: "First", ZapScript: "**a"},
			{Kind: database.DeckItemKindScript, Name: "Second", ZapScript: "**b"},
		},
	}, nil).Once()
	active := playlists.NewPlaylist("ZON-0123456789ab", "Weekend",
		[]playlists.PlaylistItem{{Name: "First", ZapScript: "**a"}})
	active.Index = 0
	active.Playing = true
	svc.State.SetActivePlaylist(active)

	notification := decksChangedNotification(t, "0123456789AB", models.DecksChangedRefreshed)
	refreshOpenDeckPlaylist(context.Background(), svc, &notification)

	select {
	case queued := <-svc.PlaylistQueue:
		assert.True(t, queued.Refresh)
		assert.Equal(t, "ZON-0123456789ab", queued.ID)
		assert.Equal(t, "Weekend Plus", queued.Name)
		assert.Len(t, queued.Items, 2)
	default:
		t.Fatal("expected a refresh to be queued for the open deck")
	}
	mockUserDB.AssertExpectations(t)
}

func TestRefreshOpenDeckPlaylist_IgnoresOtherDecksAndActions(t *testing.T) {
	t.Parallel()
	svc := setupPlaylistTestEnv(t)
	active := playlists.NewPlaylist("ZON-0123456789ab", "Weekend", []playlists.PlaylistItem{{ZapScript: "**a"}})
	svc.State.SetActivePlaylist(active)

	other := decksChangedNotification(t, "bbbbbbbbbbbb", models.DecksChangedRefreshed)
	refreshOpenDeckPlaylist(context.Background(), svc, &other)
	deleted := decksChangedNotification(t, "0123456789ab", models.DecksChangedDeleted)
	refreshOpenDeckPlaylist(context.Background(), svc, &deleted)

	select {
	case queued := <-svc.PlaylistQueue:
		t.Fatalf("unexpected playlist update queued: %+v", queued)
	default:
	}
}
