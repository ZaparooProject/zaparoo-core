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

package online

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
)

const (
	maxDeckPush    = 200
	maxImageBytes  = 8 << 20
	imageUploadTTL = time.Hour
	deckIDAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	deckIDLength   = 12
)

// Deck is one deck on the account.
//
//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type Deck struct {
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	ImageURL    *string         `json:"image_url,omitempty"`
	DeckID      string          `json:"deck_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Visibility  string          `json:"visibility"`
	Items       []DeckItem      `json:"items,omitempty"`
	Revision    int64           `json:"revision"`
	ItemCount   int             `json:"item_count"`
	IsLocked    bool            `json:"is_locked"`
	Deleted     bool            `json:"deleted"`
}

// DeckItem is one item in a deck: a card from the account, or a ZapScript.
//
//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type DeckItem struct {
	Metadata  json.RawMessage           `json:"metadata,omitempty"`
	Kind      string                    `json:"kind"`
	CardID    string                    `json:"card_id,omitempty"`
	Name      string                    `json:"name,omitempty"`
	ZapScript string                    `json:"zapscript,omitempty"`
	Scripts   []database.DeckCardScript `json:"scripts,omitempty"`
	Position  int                       `json:"position"`
}

// Card is one card on the account that deck items can name.
type Card struct {
	Metadata json.RawMessage           `json:"metadata,omitempty"`
	Name     string                    `json:"name"`
	Scripts  []database.DeckCardScript `json:"scripts,omitempty"`
}

// SavedDeck is one deck the account saved from another account.
//
//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type SavedDeck struct {
	Name    *string `json:"name,omitempty"`
	OwnedBy *string `json:"owned_by,omitempty"`
	DeckID  string  `json:"deck_id"`
	Link    string  `json:"link"`
	SavedAt string  `json:"saved_at"`
}

type imageUpload struct {
	ExpiresAt time.Time `json:"expiresAt"`
	DeckID    string    `json:"deckId"`
	Data      []byte    `json:"data"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type deckPushRecord struct {
	Name          *string     `json:"name"`
	Description   *string     `json:"description"`
	Items         *[]DeckItem `json:"items"`
	DeckID        string      `json:"deck_id"`
	ImageUploadID string      `json:"image_upload_id"`
	BaseRevision  int64       `json:"base_revision"`
	Deleted       bool        `json:"deleted"`
	RemoveImage   bool        `json:"remove_image"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type deckResult struct {
	Deck      *Deck  `json:"deck"`
	ItemIndex *int   `json:"item_index,omitempty"`
	Status    string `json:"status"`
	Code      string `json:"code,omitempty"`
	// ItemErrors lists every refused item by its position in the pushed
	// items, on the item-level codes only.
	ItemErrors []deckItemError `json:"item_errors,omitempty"`
	Index      int             `json:"index"`
}

type deckItemError struct {
	Code  string `json:"code"`
	Index int    `json:"index"`
}

// refusedCards is the rejection for a record naming cards the account does
// not hold.
func refusedCards(refused []deckItemError) deckResult {
	first := refused[0].Index
	return deckResult{Status: "rejected", Code: "unknown_card", ItemIndex: &first, ItemErrors: refused}
}

// AddCard puts a card on the account, so deck items naming it are filled in
// and accepted.
func (s *Server) AddCard(cardID, name string, metadata json.RawMessage, scripts ...database.DeckCardScript) {
	s.mu.Lock()
	s.state.Cards[cardID] = Card{Name: name, Metadata: metadata, Scripts: scripts}
	s.mu.Unlock()
	_ = s.save()
}

// PutDeck writes a deck as if another device or the website had, filling
// card items from the account's cards and giving it the next revision.
func (s *Server) PutDeck(deck *Deck) Deck {
	s.mu.Lock()
	stored := *deck
	stored.DeckID = strings.ToUpper(deck.DeckID)
	stored.Items = s.enrichDeckItems(deck.Items)
	stored.ItemCount = len(stored.Items)
	if stored.Visibility == "" {
		stored.Visibility = "private"
	}
	s.state.DeckRev++
	stored.Revision = s.state.DeckRev
	s.state.Decks[stored.DeckID] = &stored
	s.mu.Unlock()
	_ = s.save()
	return stored
}

// Decks returns the account's decks, tombstones included, by revision.
func (s *Server) Decks() []Deck {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Deck, 0, len(s.state.Decks))
	for _, deck := range s.state.Decks {
		out = append(out, *deck)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out
}

// SetSavedDecks replaces the account's saved deck list.
func (s *Server) SetSavedDecks(items ...SavedDeck) {
	s.mu.Lock()
	s.state.SavedDecks = slices.Clone(items)
	s.mu.Unlock()
	_ = s.save()
}

// enrichDeckItems numbers items and fills card items from the account's
// cards. Call with s.mu held.
func (s *Server) enrichDeckItems(items []DeckItem) []DeckItem {
	out := make([]DeckItem, 0, len(items))
	for i, item := range items {
		item.Position = i + 1
		if item.Kind == "card" {
			if card, ok := s.state.Cards[item.CardID]; ok {
				item.Name = card.Name
				item.Scripts = card.Scripts
				item.Metadata = card.Metadata
			}
		}
		out = append(out, item)
	}
	return out
}

// unknownCard reports whether an item names a card the account lacks. Call
// with s.mu held.
// unknownCards lists the positions of card items naming a card the account
// does not hold.
func (s *Server) unknownCards(items []DeckItem) []deckItemError {
	var out []deckItemError
	for i, item := range items {
		if item.Kind == "card" {
			if _, ok := s.state.Cards[item.CardID]; !ok {
				out = append(out, deckItemError{Index: i, Code: "unknown_card"})
			}
		}
	}
	return out
}

// mintedDeckID reports whether a create's deck ID is one a device mints:
// twelve characters of Crockford base32.
func mintedDeckID(deckID string) bool {
	if len(deckID) != deckIDLength {
		return false
	}
	for _, r := range strings.ToUpper(deckID) {
		if !strings.ContainsRune(deckIDAlphabet, r) {
			return false
		}
	}
	return true
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online device API.
type imageUploadResponse struct {
	ExpiresAt     time.Time `json:"expires_at"`
	ImageUploadID string    `json:"image_upload_id"`
	PreviewURL    string    `json:"preview_url"`
}

// handleDeckImageUpload stages an image for a deck the account holds. The
// next push naming the upload sets it as the deck's image.
func (s *Server) handleDeckImageUpload(w http.ResponseWriter, r *http.Request, _ string) {
	deckID := strings.ToUpper(r.PathValue("id"))
	data, err := io.ReadAll(io.LimitReader(r.Body, maxImageBytes+1))
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "image body is required")
		return
	}
	if len(data) > maxImageBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "")
		return
	}
	base := s.baseURL(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	deck := s.state.Decks[deckID]
	if deck == nil || deck.Deleted {
		writeError(w, http.StatusNotFound, "not_found", "no such deck")
		return
	}
	if deck.IsLocked {
		writeError(w, http.StatusForbidden, "deck_locked", "")
		return
	}
	s.state.Sequence++
	uploadID := fmt.Sprintf("00000000-0000-4000-8000-%012d", s.state.Sequence)
	upload := &imageUpload{DeckID: deckID, Data: data, ExpiresAt: now().Add(imageUploadTTL)}
	s.state.ImageUploads[uploadID] = upload
	s.state.Images["preview-"+uploadID] = data
	fakectl.WriteJSON(w, http.StatusCreated, imageUploadResponse{
		ImageUploadID: uploadID, PreviewURL: base + pathImages + "preview-" + uploadID,
		ExpiresAt: upload.ExpiresAt,
	})
}

// applyImageChange applies a record's image change, or returns the code it
// is rejected with. Call with s.mu held.
func (s *Server) applyImageChange(record *deckPushRecord, deck *Deck, base string) string {
	switch {
	case record.ImageUploadID != "":
		upload, ok := s.state.ImageUploads[record.ImageUploadID]
		if ok && now().After(upload.ExpiresAt) {
			delete(s.state.ImageUploads, record.ImageUploadID)
			return "image_upload_expired"
		}
		if !ok || upload.DeckID != deck.DeckID {
			return "image_upload_not_found"
		}
		delete(s.state.ImageUploads, record.ImageUploadID)
		name := deck.DeckID + "-" + record.ImageUploadID
		s.state.Images[name] = upload.Data
		imageURL := base + pathImages + name
		deck.ImageURL = &imageURL
	case record.RemoveImage:
		deck.ImageURL = nil
	}
	return ""
}

// handleDeckPush applies deck records against their base revision. A base
// revision of zero creates the deck under the ID the device minted.
func (s *Server) handleDeckPush(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct {
		Items []deckPushRecord `json:"items"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Items) == 0 || len(body.Items) > maxDeckPush {
		writeError(w, http.StatusBadRequest, "validation_error", "items must hold 1 to 200 records")
		return
	}
	// One bad record refuses the whole request before any is applied.
	for i := range body.Items {
		record := &body.Items[i]
		if (record.BaseRevision == 0 && !mintedDeckID(record.DeckID)) ||
			(record.ImageUploadID != "" && record.RemoveImage) {
			writeErrorFields(w, http.StatusBadRequest, "validation_error", "invalid deck record",
				map[string]string{fmt.Sprintf("items[%d]", i): "invalid"})
			return
		}
	}
	base := s.baseURL(r)
	resp := pushResponse[deckResult]{Items: make([]deckResult, 0, len(body.Items))}
	s.mu.Lock()
	for i := range body.Items {
		result := s.applyDeckRecord(&body.Items[i], base)
		if result.Deck != nil {
			copied := *result.Deck
			result.Deck = &copied
		}
		result.Index = i
		resp.Items = append(resp.Items, result)
		resp.count(result.Status)
	}
	s.mu.Unlock()
	fakectl.WriteJSON(w, http.StatusOK, resp)
}

// applyDeckRecord applies one record. Call with s.mu held.
func (s *Server) applyDeckRecord(record *deckPushRecord, base string) deckResult {
	deckID := strings.ToUpper(record.DeckID)
	deck := s.state.Decks[deckID]
	if record.BaseRevision == 0 {
		return s.createDeck(record, deckID, deck)
	}
	switch {
	case deck == nil:
		return deckResult{Status: "conflict"}
	case deck.Revision != record.BaseRevision:
		return deckResult{Status: "conflict", Deck: deck}
	case deck.IsLocked:
		return deckResult{Status: "rejected", Code: "deck_locked", Deck: deck}
	}
	if record.Deleted {
		s.state.DeckRev++
		deck.Deleted = true
		deck.Items = nil
		deck.ItemCount = 0
		deck.Revision = s.state.DeckRev
		return deckResult{Status: "applied", Deck: deck}
	}
	if record.Items != nil {
		if refused := s.unknownCards(*record.Items); len(refused) > 0 {
			return refusedCards(refused)
		}
	}
	before := *deck
	if imageCode := s.applyImageChange(record, deck, base); imageCode != "" {
		*deck = before
		return deckResult{Status: "rejected", Code: imageCode}
	}
	// An update against a tombstone's revision brings the deck back.
	deck.Deleted = false
	if record.Name != nil {
		deck.Name = *record.Name
	}
	if record.Description != nil {
		deck.Description = *record.Description
	}
	if record.Items != nil {
		deck.Items = s.enrichDeckItems(*record.Items)
		deck.ItemCount = len(deck.Items)
	}
	s.state.DeckRev++
	deck.Revision = s.state.DeckRev
	return deckResult{Status: "applied", Deck: deck}
}

// createDeck applies a record with base revision zero. A deck that already
// exists under the ID is the answer to a retried create. Call with s.mu held.
func (s *Server) createDeck(record *deckPushRecord, deckID string, existing *Deck) deckResult {
	if record.ImageUploadID != "" || record.RemoveImage {
		return deckResult{Status: "rejected", Code: "image_needs_existing_deck"}
	}
	if existing != nil {
		if existing.Deleted {
			return deckResult{Status: "conflict", Deck: existing}
		}
		return deckResult{Status: "applied", Deck: existing}
	}
	if record.Name == nil || *record.Name == "" {
		return deckResult{Status: "rejected", Code: "validation_error"}
	}
	var items []DeckItem
	if record.Items != nil {
		items = *record.Items
	}
	if refused := s.unknownCards(items); len(refused) > 0 {
		return refusedCards(refused)
	}
	description := ""
	if record.Description != nil {
		description = *record.Description
	}
	s.state.DeckRev++
	created := &Deck{
		DeckID: deckID, Name: *record.Name, Description: description, Visibility: "private",
		Items: s.enrichDeckItems(items), Revision: s.state.DeckRev,
		Metadata: json.RawMessage(`{"zaps":0}`),
	}
	created.ItemCount = len(created.Items)
	s.state.Decks[deckID] = created
	return deckResult{Status: "applied", Deck: created}
}

// handleDeckPull pages decks changed since a revision. A first pull leaves
// out tombstones.
func (s *Server) handleDeckPull(w http.ResponseWriter, r *http.Request, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	win, ok := pullWindow(r, s.state.DeckFloor)
	since, limit, reset := win.since, win.limit, win.reset
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "limit must be 1 to 500")
		return
	}
	decks := make([]Deck, 0)
	for _, deck := range s.state.Decks {
		if deck.Revision > since && (since > 0 || !deck.Deleted) {
			decks = append(decks, *deck)
		}
	}
	decks, next, more := page(decks, since, limit, func(deck Deck) int64 { return deck.Revision })
	fakectl.WriteJSON(w, http.StatusOK, pullResponse[Deck]{
		Items: decks, NextSince: next, HasMore: more, Reset: reset,
	})
}

func (s *Server) handleSavedDecks(w http.ResponseWriter, _ *http.Request, _ string) {
	s.mu.Lock()
	items := slices.Clone(s.state.SavedDecks)
	s.mu.Unlock()
	if items == nil {
		items = []SavedDeck{}
	}
	fakectl.WriteJSON(w, http.StatusOK, map[string][]SavedDeck{"items": items})
}
