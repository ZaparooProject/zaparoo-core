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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/rs/zerolog/log"
)

// watchDecksForPlaylistRefresh keeps an open deck playlist in step with its
// deck: when the deck changes, through a sync pull or a local edit, the
// active playlist with that deck's ID takes the new items in place, keeping
// its position and playback.
func watchDecksForPlaylistRefresh(ctx context.Context, b *broker.Broker, svc *ServiceContext) {
	notifChan, subID := b.Subscribe(32, models.NotificationDecksChanged)
	defer b.Unsubscribe(subID)
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-notifChan:
			if !ok {
				return
			}
			refreshOpenDeckPlaylist(ctx, svc, &notification)
		}
	}
}

// refreshOpenDeckPlaylist queues an in-place refresh for every active
// playlist that is the changed deck.
func refreshOpenDeckPlaylist(ctx context.Context, svc *ServiceContext, notification *models.Notification) {
	var payload models.DecksChangedNotification
	if err := json.Unmarshal(notification.Params, &payload); err != nil {
		return
	}
	if payload.Action != models.DecksChangedRefreshed && payload.Action != models.DecksChangedUpdated {
		return
	}
	deckID, err := database.NormalizeDeckID(payload.DeckID)
	if err != nil {
		return
	}
	playlistID := decks.PlaylistID(deckID)
	for _, active := range []*playlists.Playlist{svc.State.GetActivePlaylist(), svc.State.GetBackgroundPlaylist()} {
		if active == nil || active.ID != playlistID {
			continue
		}
		deck, getErr := svc.DB.UserDB.GetDeck(deckID)
		if getErr != nil {
			log.Debug().Err(getErr).Str("deck", deckID).Msg("open deck playlist not refreshed")
			return
		}
		items := make([]playlists.PlaylistItem, 0, len(deck.Items))
		for _, item := range decks.PlaylistItems(ctx, svc.DB.MediaDB, deck) {
			items = append(items, playlists.PlaylistItem{Name: item.Name, ZapScript: item.ZapScript})
		}
		// Unsafe is deliberately absent: a refresh carries only what it
		// replaces, and handlePlaylist keeps the open playlist's own trust.
		// Setting it here would let an omission grant trust instead of
		// withholding it.
		refreshed := &playlists.Playlist{
			ID: playlistID, Name: deck.Name, Slot: active.Slot, Items: items,
			Loop: active.Loop, LoopOne: active.LoopOne, Refresh: true,
		}
		select {
		case svc.PlaylistQueue <- refreshed:
		case <-ctx.Done():
			return
		}
	}
}
