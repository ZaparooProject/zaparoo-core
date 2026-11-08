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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/rs/zerolog/log"
)

// Reject codes the account gives a deck record for its image change. The
// record is left unapplied, content included.
const (
	codeImageNeedsExistingDeck = "image_needs_existing_deck"
	codeImageUploadNotFound    = "image_upload_not_found"
	codeImageUploadExpired     = "image_upload_expired"
	codeImageUploadConsumed    = "image_upload_consumed"
	codeImageUploadDiscarded   = "image_upload_discarded"
)

// coverPass is what one deck pass remembers about cover uploads: the upload
// staged for each deck's cover file, so a retried record reuses it, and the
// decks whose cover is held back for the rest of the pass.
type coverPass struct {
	staged   map[string]string
	restaged map[string]bool
	held     map[string]bool
}

func newCoverPass() coverPass {
	return coverPass{staged: make(map[string]string), restaged: make(map[string]bool), held: make(map[string]bool)}
}

func stagedKey(deckID, coverFile string) string { return deckID + "/" + coverFile }

type stageImageResponse struct {
	ImageUploadID string `json:"image_upload_id"` //nolint:tagliatelle // Zaparoo Online API contract.
}

// settleUnneededRemoval clears a removal made here for a deck the account
// holds no image for, which has nothing to send, and returns the deck's
// cover change still to send.
func (p *deckPass) settleUnneededRemoval(deck *database.Deck) (string, error) {
	if deck.CoverPending != database.DeckCoverPendingRemove {
		return deck.CoverPending, nil
	}
	held := *deck
	held.CoverPending = ""
	if held.RemoteImageURL() != "" {
		return deck.CoverPending, nil
	}
	if err := p.userDB().SettleDeckCoverPush(
		deck.DeckID, database.DeckCoverPendingRemove, deck.CoverFile, deck.ImageURL,
	); err != nil {
		return "", fmt.Errorf("settle deck cover removal: %w", err)
	}
	return "", nil
}

// attachCover adds a record's cover change: a removal as is, and a new
// cover as an upload staged for it. A create cannot carry an image, so its
// cover goes in the round after the deck exists. A cover that cannot be
// staged now is left pending for a later pass.
func (p *deckPass) attachCover(ctx context.Context, pending *pendingDeck) {
	if pending.coverPending == "" || pending.record.Deleted || pushIsCreate(p.rows[pending.deckID]) ||
		p.covers.held[pending.deckID] {
		return
	}
	switch pending.coverPending {
	case database.DeckCoverPendingRemove:
		pending.record.RemoveImage = true
		pending.coverSent = true
	case database.DeckCoverPendingSet:
		if uploadID, ok := p.stageCover(ctx, pending); ok {
			pending.record.ImageUploadID = uploadID
			pending.coverSent = true
		}
	}
}

// stageCover uploads a deck's cover for the push to commit, or returns the
// upload already staged for it in this pass.
func (p *deckPass) stageCover(ctx context.Context, pending *pendingDeck) (string, bool) {
	key := stagedKey(pending.deckID, pending.coverFile)
	if uploadID, ok := p.covers.staged[key]; ok {
		return uploadID, true
	}
	if p.svc.deckCovers == nil {
		return "", false
	}
	data, err := p.svc.deckCovers.Read(pending.coverFile)
	if err != nil {
		log.Warn().Err(err).Str("deck", pending.deckID).Msg("deck cover file cannot be read; not sending it")
		p.covers.held[pending.deckID] = true
		return "", false
	}
	var response stageImageResponse
	path := pathDecks + "/" + url.PathEscape(pending.deckID) + "/image-uploads"
	err = p.client.RetryRateLimited(ctx, func() error {
		response = stageImageResponse{}
		return p.client.DoBytes(ctx, http.MethodPost, path, data, nil, &response)
	})
	if err == nil && response.ImageUploadID != "" {
		p.covers.staged[key] = response.ImageUploadID
		return response.ImageUploadID, true
	}
	p.covers.held[pending.deckID] = true
	var apiErr *backup.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == http.StatusForbidden && apiErr.Code == codeDeckLocked:
			// The next pull brings the lock; the cover change cannot land.
			log.Info().Str("deck", pending.deckID).Msg("deck is locked on the account; dropping its cover change")
			imageURL := p.deckImageURL(pending.deckID)
			if discardErr := p.userDB().DiscardDeckCover(pending.deckID, imageURL); discardErr != nil {
				log.Warn().Err(discardErr).Str("deck", pending.deckID).Msg("failed to drop deck cover change")
			}
			return "", false
		case apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusRequestEntityTooLarge:
			// The account will never take this image: keep it here only.
			log.Warn().Str("deck", pending.deckID).Str("code", apiErr.Code).
				Msg("the account refused the deck cover; keeping it on this device only")
			p.svc.notifyInbox("Deck cover not synced", fmt.Sprintf(
				"The cover of %q could not be sent to Zaparoo Online (%s). It is kept on this device.",
				pending.name, apiErr.Message), inbox.CategoryNone)
			if settleErr := p.userDB().SettleDeckCoverPush(
				pending.deckID, database.DeckCoverPendingSet, pending.coverFile, p.deckImageURL(pending.deckID),
			); settleErr != nil {
				log.Warn().Err(settleErr).Str("deck", pending.deckID).Msg("failed to settle refused deck cover")
			}
			return "", false
		}
	}
	log.Debug().Err(err).Str("deck", pending.deckID).Msg("deck cover upload failed; trying again later")
	return "", false
}

// deckImageURL is the account's image the deck holds now.
func (p *deckPass) deckImageURL(deckID string) string {
	deck, err := p.userDB().GetDeck(deckID)
	if err != nil {
		return ""
	}
	return deck.ImageURL
}

// settleCover records the account's answer to a record's cover change once
// the record applied.
func (p *deckPass) settleCover(pending *pendingDeck, remote *deckSync) error {
	if !pending.coverSent || remote == nil {
		return nil
	}
	imageURL := ""
	if remote.ImageURL != nil {
		imageURL = *remote.ImageURL
	}
	delete(p.covers.staged, stagedKey(pending.deckID, pending.coverFile))
	if err := p.userDB().SettleDeckCoverPush(
		pending.deckID, pending.coverPending, pending.coverFile, imageURL,
	); err != nil {
		return fmt.Errorf("settle deck cover %s: %w", pending.deckID, err)
	}
	return nil
}

// discardCoverChange drops a deck's cover change made here and keeps the
// account's image, for a deck the account locked.
func (p *deckPass) discardCoverChange(deckID string, remote *deckSync) error {
	imageURL := ""
	if remote != nil && remote.ImageURL != nil {
		imageURL = *remote.ImageURL
	}
	if err := p.userDB().DiscardDeckCover(deckID, imageURL); err != nil && !errors.Is(err, database.ErrDeckNotFound) {
		return fmt.Errorf("drop deck cover change %s: %w", deckID, err)
	}
	return nil
}

// handleCoverRejected handles a record the account rejected over its image
// change. A stale upload is staged once more within the pass; a record that
// may not carry an image yet goes again without one. It reports whether it
// handled the rejection and whether the push should run another round.
func (p *deckPass) handleCoverRejected(pending *pendingDeck, result *deckPushResult) (retry, handled bool) {
	if !pending.coverSent {
		return false, false
	}
	switch result.Code {
	case codeImageUploadNotFound, codeImageUploadExpired, codeImageUploadConsumed, codeImageUploadDiscarded:
		delete(p.covers.staged, stagedKey(pending.deckID, pending.coverFile))
		if p.covers.restaged[pending.deckID] {
			log.Warn().Str("deck", pending.deckID).Str("code", result.Code).
				Msg("deck cover upload was refused again; trying next pass")
			p.covers.held[pending.deckID] = true
		}
		p.covers.restaged[pending.deckID] = true
		return true, true
	case codeImageNeedsExistingDeck:
		p.covers.held[pending.deckID] = true
		return true, true
	}
	return false, false
}

// pruneDeckCovers removes cover files no deck names after a pass may have
// replaced covers with the account's images.
func (s *Service) pruneDeckCovers() {
	if s.deckCovers == nil {
		return
	}
	list, err := s.db.UserDB.ListDecks()
	if err != nil {
		log.Debug().Err(err).Msg("failed to list decks for cover pruning")
		return
	}
	keep := make(map[string]struct{}, len(list))
	for i := range list {
		if list[i].CoverFile != "" {
			keep[list[i].CoverFile] = struct{}{}
		}
	}
	s.deckCovers.Prune(keep, time.Now())
}
