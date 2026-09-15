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

package zapscript

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/rs/zerolog/log"
)

const (
	// deckRefreshTimeout bounds refreshing a deck from its source before it
	// opens, so a slow or unreachable service never holds up a tap.
	deckRefreshTimeout = 3 * time.Second
	// deckRefreshFreshFor skips a refresh when the deck was just fetched,
	// such as by the ZapLink tap that opened it.
	deckRefreshFreshFor = 30 * time.Second
)

// ErrDeckNotFound reports a deck:// argument naming no deck on this device.
var ErrDeckNotFound = errors.New("deck not found")

// adoptZapLinkDeck recognizes a ZapLink body that serves a deck and caches
// the deck locally, returning a script that opens the local copy instead. A
// deck opened this way plays offline next time, and a deck this device owns
// opens its own editable copy. Any other body is returned unchanged.
func adoptZapLinkDeck(db *database.Database, link, body string) string {
	if db == nil || db.UserDB == nil {
		return body
	}
	deckID, ok := decks.DeckIDFromZapLinkURL(link)
	if !ok {
		return body
	}
	arg, ok := decks.ParseDeckPlaylist(body, deckID)
	if !ok {
		return body
	}
	if err := decks.StoreFetchedDeck(db.UserDB, link, deckID, &arg); err != nil {
		log.Warn().Err(err).Str("deck", deckID).Msg("failed to cache deck from zap link; playing it as served")
		return body
	}
	return "**" + zapscript.ZapScriptCmdPlaylistOpen + ":" + decks.DeckURI(deckID)
}

// loadDeckPlaylist builds a playlist from a deck on this device, refreshing
// it from its source first when that is quick.
//
//nolint:gocritic // env is passed by pointer from the playlist loader
func loadDeckPlaylist(
	pl platforms.Platform, env *platforms.CmdEnv, deckID string, args *zapscript.PlaylistArgs,
) (*playlists.Playlist, error) {
	if env.Database == nil || env.Database.UserDB == nil {
		return nil, fmt.Errorf("%w: %s", ErrDeckNotFound, deckID)
	}
	deck, err := env.Database.UserDB.GetDeck(deckID)
	if errors.Is(err, database.ErrDeckNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrDeckNotFound, deckID)
	}
	if err != nil {
		return nil, fmt.Errorf("load deck %s: %w", deckID, err)
	}
	if refreshed := refreshDeck(pl, env, deck); refreshed != nil {
		deck = refreshed
	}

	ctx := env.ServiceCtx
	if ctx == nil {
		ctx = context.Background()
	}
	items := make([]playlists.PlaylistItem, 0, len(deck.Items))
	for _, item := range decks.PlaylistItems(ctx, env.Database.MediaDB, deck) {
		items = append(items, playlists.PlaylistItem{Name: item.Name, ZapScript: item.ZapScript})
	}
	if zapscript.IsModeShuffle(args.Mode) && len(items) > 1 {
		//nolint:gosec // Playlist order is not security sensitive.
		rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	}

	pls := playlists.NewPlaylist(decks.PlaylistID(deck.DeckID), deck.Name, items)
	slot, slotErr := mediaslot.Normalize(env.Cmd.AdvArgs.Get(zapscript.KeySlot))
	if slotErr != nil {
		return nil, fmt.Errorf("normalize media slot: %w", slotErr)
	}
	pls.Slot = slot
	pls.Loop = zapscript.IsRepeatAll(args.Repeat)
	pls.LoopOne = zapscript.IsRepeatOne(args.Repeat)
	return pls, nil
}

// refreshDeck brings a deck up to date as it opens. An owned deck opens its
// local copy at once and asks the sync hook to look for changes in the
// background, since a tap never waits on the network; the open playlist is
// refreshed in place if anything changed. A cached copy of somebody else's
// deck is fetched from its ZapLink again first, within a short bound. It
// returns the reloaded deck, or nil to use the copy already loaded. Every
// failure, including being offline, is logged at debug and leaves the local
// copy to open.
func refreshDeck(pl platforms.Platform, env *platforms.CmdEnv, deck *database.Deck) *database.Deck {
	parent := env.ServiceCtx
	if parent == nil {
		parent = context.Background()
	}
	if deck.Owned {
		if env.RefreshOwnedDeck != nil {
			env.RefreshOwnedDeck(parent, deck.DeckID)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, deckRefreshTimeout)
	defer cancel()

	switch {
	case deck.SourceURL != "":
		if time.Since(time.Unix(deck.FetchedAt, 0)) < deckRefreshFreshFor {
			return nil
		}
		platformID := ""
		if pl != nil {
			platformID = pl.ID()
		}
		body, err := getRemoteZapScriptContext(ctx, deck.SourceURL, platformID)
		if err != nil {
			log.Debug().Err(err).Str("deck", deck.DeckID).Msg("deck refresh skipped; opening cached copy")
			return nil
		}
		arg, ok := decks.ParseDeckPlaylist(string(body), deck.DeckID)
		if !ok {
			log.Debug().Str("deck", deck.DeckID).Msg("deck source no longer serves the deck; opening cached copy")
			return nil
		}
		if storeErr := decks.StoreFetchedDeck(env.Database.UserDB, deck.SourceURL, deck.DeckID, &arg); storeErr != nil {
			log.Debug().Err(storeErr).Str("deck", deck.DeckID).Msg("failed to store refreshed deck")
			return nil
		}
		if cacheErr := env.Database.UserDB.UpdateZapLinkCache(deck.SourceURL, string(body)); cacheErr != nil {
			log.Debug().Err(cacheErr).Str("deck", deck.DeckID).Msg("failed to update zap link cache")
		}
	default:
		return nil
	}

	reloaded, err := env.Database.UserDB.GetDeck(deck.DeckID)
	if err != nil {
		log.Debug().Err(err).Str("deck", deck.DeckID).Msg("failed to reload refreshed deck")
		return nil
	}
	return reloaded
}
