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
	"errors"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// restoreOpenPlaylists reopens the playlists that were open when Core last
// stopped, for the profile active now. They come back paused, never playing
// and never waiting to start on idle, so nothing launches until someone asks.
// It runs before the token queue worker starts, so nothing else is changing
// the playlists while it does.
func restoreOpenPlaylists(svc *ServiceContext, player audio.Player) {
	for _, pls := range svc.PlaylistMemory.OpenPlaylists() {
		if pls.DeckID != "" && svc.DB != nil && svc.DB.UserDB != nil {
			deck, err := svc.DB.UserDB.GetDeck(pls.DeckID)
			switch {
			case errors.Is(err, database.ErrDeckNotFound):
				log.Info().Str("deck", pls.DeckID).Msg("open playlist not restored, its deck was deleted")
				continue
			case err != nil:
				log.Warn().Err(err).Str("deck", pls.DeckID).Msg("could not check the deck of a restored playlist")
			case !deck.Owned:
				// Trust is only ever narrowed on the way back in.
				pls.Unsafe = true
			}
		}
		log.Info().Any("pls", playlistForLog(pls)).Msg("restoring open playlist, paused")
		handlePlaylist(svc, pls, player)
	}
}
