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

package librarysync

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/rs/zerolog/log"
)

const pathSavedDecks = "/v1/device/saved-decks"

// DeckFetcher fetches the playlist a deck's link serves.
type DeckFetcher func(ctx context.Context, link string) (decks.PlaylistArg, error)

type savedDecksResponse struct {
	Items []savedDeckLink `json:"items"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type savedDeckLink struct {
	Name    *string `json:"name"`
	OwnedBy *string `json:"owned_by"`
	Link    string  `json:"link"`
}

// syncSavedDecks keeps the decks the account saved from other accounts as
// read-only copies of their links. The account's list is the whole set, so a
// listed link with no copy here is fetched and kept, a copy kept from a
// listed link before, such as by a tap, is marked saved, and a saved copy the
// list no longer names is removed. Copies are matched to links the way URLs
// compare: the scheme and host without regard to case, the rest exactly. A
// link that cannot be fetched now is left for the next pass, and a list that
// cannot be read removes nothing.
func (s *Service) syncSavedDecks(ctx context.Context) (DecksResult, error) {
	var result DecksResult
	if err := s.lockSavedDecks(ctx); err != nil {
		return result, err
	}
	defer s.unlockSavedDecks()
	if !s.cfg.LibrarySyncEnabled() {
		return result, ErrDisabled
	}
	client, err := s.client()
	if err != nil {
		return result, err
	}
	var listed savedDecksResponse
	err = client.RetryRateLimited(ctx, func() error {
		listed = savedDecksResponse{}
		return client.DoJSON(ctx, http.MethodGet, pathSavedDecks, nil, &listed)
	})
	if err != nil {
		return result, fmt.Errorf("list saved decks: %w", err)
	}
	local, err := s.db.UserDB.ListDecks()
	if err != nil {
		return result, fmt.Errorf("read decks: %w", err)
	}

	copies := make(map[string]*database.Deck, len(local))
	for i := range local {
		deck := &local[i]
		if deck.Owned || deck.SourceURL == "" {
			continue
		}
		key := deckLinkKey(deck.SourceURL)
		if prior, ok := copies[key]; key == "" || (ok && prior.Saved) {
			continue
		}
		copies[key] = deck
	}
	wanted := make(map[string]*savedDeckLink, len(listed.Items))
	for i := range listed.Items {
		if key := deckLinkKey(listed.Items[i].Link); key != "" {
			if _, dup := wanted[key]; !dup {
				wanted[key] = &listed.Items[i]
			}
		}
	}

	for i := range local {
		deck := &local[i]
		if deck.Owned || !deck.Saved {
			continue
		}
		if _, ok := wanted[deckLinkKey(deck.SourceURL)]; ok {
			continue
		}
		if _, delErr := s.db.UserDB.DeleteDeck(deck.DeckID); delErr != nil {
			return result, fmt.Errorf("remove unsaved deck %s: %w", deck.DeckID, delErr)
		}
		s.db.QueueDeckTags(deck.DeckID)
		s.notifyDecks(deck.DeckID, models.DecksChangedDeleted)
		result.Unsaved++
	}

	for i := range listed.Items {
		item := &listed.Items[i]
		key := deckLinkKey(item.Link)
		if wanted[key] != item {
			continue
		}
		owner := ""
		if item.OwnedBy != nil {
			owner = *item.OwnedBy
		}
		if deck, ok := copies[key]; ok {
			if deck.Saved && deck.OwnerName == owner {
				continue
			}
			if saveErr := s.db.UserDB.SetDeckSaved(deck.DeckID, true, owner); saveErr != nil {
				return result, fmt.Errorf("mark deck %s saved: %w", deck.DeckID, saveErr)
			}
			if !deck.Saved {
				s.notifyDecks(deck.DeckID, models.DecksChangedUpdated)
				result.Saved++
			}
			continue
		}
		if s.fetchDeck == nil {
			continue
		}
		if ctx.Err() != nil {
			return result, fmt.Errorf("keep saved decks: %w", ctx.Err())
		}
		if s.keepSavedDeck(ctx, item, owner) {
			result.Saved++
		}
	}
	return result, nil
}

// keepSavedDeck fetches a saved deck's link and keeps it marked saved. A
// link that fails is logged and left for the next pass.
func (s *Service) keepSavedDeck(ctx context.Context, item *savedDeckLink, owner string) bool {
	arg, err := s.fetchDeck(ctx, item.Link)
	if err != nil {
		log.Warn().Err(err).Str("link", item.Link).Msg("failed to fetch saved deck; trying again next pass")
		return false
	}
	if strings.TrimSpace(arg.Name) == "" && item.Name != nil {
		arg.Name = *item.Name
	}
	deckID, err := decks.StoreFetchedDeck(s.db, item.Link, &arg)
	if err != nil {
		log.Warn().Err(err).Str("link", item.Link).Msg("failed to keep saved deck; trying again next pass")
		return false
	}
	if err := s.db.UserDB.SetDeckSaved(deckID, true, owner); err != nil {
		log.Warn().Err(err).Str("deck", deckID).Msg("failed to mark deck saved; trying again next pass")
		return false
	}
	s.notifyDecks(deckID, models.DecksChangedCreated)
	return true
}

// deckLinkKey spells a link the one way two equal URLs share: scheme and host
// lower-cased, everything else as given. It is empty for a link that is not
// an absolute URL.
func deckLinkKey(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

func (s *Service) lockSavedDecks(ctx context.Context) error {
	select {
	case s.savedSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for saved deck sync: %w", ctx.Err())
	}
}

func (s *Service) unlockSavedDecks() {
	<-s.savedSem
}
