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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/rs/zerolog/log"
)

const (
	deckItemKindMedia = "media"
	errDecksNoUserDB  = "user database is not available"
)

// HandleDecks lists every deck without its items.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDecks(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received decks list request")
	if env.Database == nil || env.Database.UserDB == nil {
		return nil, errors.New(errDecksNoUserDB)
	}
	env.State.NotifyLibraryDecksAccessed()
	list, err := env.Database.UserDB.ListDecks()
	if err != nil {
		return nil, fmt.Errorf("failed to list decks: %w", err)
	}
	locked, err := lockedDecks(&env)
	if err != nil {
		return nil, err
	}
	resp := models.DecksResponse{Decks: make([]models.DeckResponse, 0, len(list))}
	for i := range list {
		deck := deckSummary(&list[i])
		deck.Locked = locked[deck.DeckID]
		resp.Decks = append(resp.Decks, deck)
	}
	return resp, nil
}

// HandleDecksGet returns one deck with its items.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDecksGet(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received decks get request")
	var params models.DecksGetParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	env.State.NotifyLibraryDecksAccessed()
	deck, err := loadDeck(&env, params.DeckID)
	if err != nil {
		return nil, err
	}
	return lockedDeckResponse(&env, deck)
}

// HandleDecksNew creates a deck owned by this device.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDecksNew(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received decks new request")
	if env.Database == nil || env.Database.UserDB == nil {
		return nil, errors.New(errDecksNoUserDB)
	}
	var params models.DecksNewParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	items, err := buildDeckItems(&env, params.Items, false)
	if err != nil {
		return nil, err
	}
	deckID, err := database.NewDeckID()
	if err != nil {
		return nil, fmt.Errorf("failed to mint deck id: %w", err)
	}
	deck := &database.Deck{
		DeckID:      deckID,
		Name:        strings.TrimSpace(params.Name),
		Description: strings.TrimSpace(params.Description),
		Owned:       true,
		Items:       items,
	}
	if deck.Name == "" {
		return nil, models.ClientErrf("invalid params: name is required")
	}
	if err := env.Database.UserDB.CreateDeck(deck); err != nil {
		return nil, deckError(err)
	}
	env.Database.QueueDeckTags(deck.DeckID)
	notifyDecksChanged(&env, deck.DeckID, models.DecksChangedCreated)
	env.State.NotifyLibraryDecksChanged()
	return deckResponse(deck, anchorAvailability(&env, deck.Items)), nil
}

// HandleDecksUpdate edits an owned deck's name, description or items. The
// edit is applied in one transaction to the deck as stored at that moment, so
// a failed edit changes nothing and concurrent edits do not overwrite each
// other. A request that asks for no change returns the deck as it is.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDecksUpdate(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received decks update request")
	var params models.DecksUpdateParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	deck, err := loadDeck(&env, params.DeckID)
	if err != nil {
		return nil, err
	}
	if params.Name == nil && params.Description == nil && params.Items == nil &&
		len(params.AddItems) == 0 && len(params.RemoveItemIDs) == 0 {
		return lockedDeckResponse(&env, deck)
	}
	if !deck.Owned {
		return nil, models.ClientErr(database.ErrDeckReadOnly)
	}

	var name, description *string
	if params.Name != nil {
		trimmed := strings.TrimSpace(*params.Name)
		if trimmed == "" {
			return nil, models.ClientErrf("invalid params: name cannot be empty")
		}
		name = &trimmed
	}
	if params.Description != nil {
		trimmed := strings.TrimSpace(*params.Description)
		description = &trimmed
	}
	var replaced []database.DeckItem
	if params.Items != nil {
		if replaced, err = buildDeckItems(&env, *params.Items, true); err != nil {
			return nil, err
		}
	}
	added, err := buildDeckItems(&env, params.AddItems, false)
	if err != nil {
		return nil, err
	}

	updated, err := env.Database.UserDB.UpdateDeck(deck.DeckID, func(stored *database.Deck) error {
		if !stored.Owned {
			return database.ErrDeckReadOnly
		}
		if name != nil {
			stored.Name = *name
		}
		if description != nil {
			stored.Description = *description
		}
		items, editErr := editedDeckItems(stored.Items, &params, replaced, added)
		if editErr != nil {
			return editErr
		}
		stored.Items = items
		return nil
	})
	if err != nil {
		return nil, deckError(err)
	}
	env.Database.QueueDeckTags(updated.DeckID)
	notifyDecksChanged(&env, updated.DeckID, models.DecksChangedUpdated)
	env.State.NotifyLibraryDecksChanged()
	return deckResponse(updated, anchorAvailability(&env, updated.Items)), nil
}

// HandleDecksDelete removes a deck, owned or cached.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDecksDelete(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received decks delete request")
	if env.Database == nil || env.Database.UserDB == nil {
		return nil, errors.New(errDecksNoUserDB)
	}
	var params models.DecksDeleteParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	deckID, err := database.NormalizeDeckID(params.DeckID)
	if err != nil {
		return nil, models.ClientErr(err)
	}
	existed, err := env.Database.UserDB.DeleteDeck(deckID)
	if err != nil {
		return nil, deckError(err)
	}
	if !existed {
		return nil, models.ClientErr(database.ErrDeckNotFound)
	}
	env.Database.QueueDeckTags(deckID)
	notifyDecksChanged(&env, deckID, models.DecksChangedDeleted)
	env.State.NotifyLibraryDecksChanged()
	return NoContent{}, nil
}

// HandleDecksOpen opens a deck as the active playlist by running the
// playlist command that names it, so it takes the same path as a card tap.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDecksOpen(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received decks open request")
	var params models.DecksOpenParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	deck, err := loadDeck(&env, params.DeckID)
	if err != nil {
		return nil, err
	}
	cmd := zapscript.Command{Name: zapscript.ZapScriptCmdPlaylistOpen, Args: []string{decks.DeckURI(deck.DeckID)}}
	if slot := strings.TrimSpace(params.Slot); slot != "" {
		cmd.AdvArgs = zapscript.NewAdvArgs(map[string]string{string(zapscript.KeySlot): slot})
	}
	text := cmd.String()
	runParams, err := json.Marshal(models.RunParams{Text: &text})
	if err != nil {
		return nil, fmt.Errorf("failed to build open request: %w", err)
	}
	env.Params = runParams
	return HandleRun(env)
}

func loadDeck(env *requests.RequestEnv, rawID string) (*database.Deck, error) {
	if env.Database == nil || env.Database.UserDB == nil {
		return nil, errors.New(errDecksNoUserDB)
	}
	deckID, err := database.NormalizeDeckID(rawID)
	if err != nil {
		return nil, models.ClientErr(err)
	}
	deck, err := env.Database.UserDB.GetDeck(deckID)
	if err != nil {
		return nil, deckError(err)
	}
	return deck, nil
}

// deckError maps storage errors a client caused to client errors.
func deckError(err error) error {
	switch {
	case errors.Is(err, database.ErrDeckNotFound),
		errors.Is(err, database.ErrDeckLimit),
		errors.Is(err, database.ErrDeckItemLimit),
		errors.Is(err, database.ErrDeckItemNotFound),
		errors.Is(err, database.ErrDeckItemRepeated),
		errors.Is(err, database.ErrDeckReadOnly),
		errors.Is(err, database.ErrInvalidDeckID):
		return models.ClientErr(err)
	default:
		return fmt.Errorf("deck operation failed: %w", err)
	}
}

func notifyDecksChanged(env *requests.RequestEnv, deckID, action string) {
	if env.State == nil {
		return
	}
	notifications.DecksChanged(env.State.Notifications, models.DecksChangedNotification{
		DeckID: deckID, Action: action,
	})
}

// editedDeckItems applies an update's item edits to the stored list in the
// order remove, replace, append. Stored items that stay keep their IDs, as do
// the items the replacement names by id; every other item is new.
func editedDeckItems(
	current []database.DeckItem, params *models.DecksUpdateParams, replaced, added []database.DeckItem,
) ([]database.DeckItem, error) {
	items := current
	switch {
	case params.Items != nil:
		kept, err := resolveDeckItemReferences(current, replaced)
		if err != nil {
			return nil, err
		}
		items = kept
	case len(params.RemoveItemIDs) > 0:
		remove := make(map[int64]struct{}, len(params.RemoveItemIDs))
		for _, id := range params.RemoveItemIDs {
			remove[id] = struct{}{}
		}
		items = make([]database.DeckItem, 0, len(current))
		for i := range current {
			if _, drop := remove[current[i].DBID]; !drop {
				items = append(items, current[i])
			}
		}
	}
	return slices.Concat(items, added), nil
}

// resolveDeckItemReferences swaps each reference in items for the stored item
// it names. A reference is an item with no kind, built by deckItemReference.
func resolveDeckItemReferences(stored, items []database.DeckItem) ([]database.DeckItem, error) {
	byID := make(map[int64]int, len(stored))
	for i := range stored {
		byID[stored[i].DBID] = i
	}
	kept := make(map[int64]struct{})
	out := make([]database.DeckItem, 0, len(items))
	for i := range items {
		if items[i].Kind != "" {
			out = append(out, items[i])
			continue
		}
		id := items[i].DBID
		index, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: item %d", database.ErrDeckItemNotFound, id)
		}
		if _, repeated := kept[id]; repeated {
			return nil, fmt.Errorf("%w: item %d", database.ErrDeckItemRepeated, id)
		}
		kept[id] = struct{}{}
		out = append(out, stored[index])
	}
	return out, nil
}

// buildDeckItems turns client item inputs into stored items. A media item is
// resolved through the media database and composed into a title launch with
// its anchor; script and card items are stored as written. An input naming
// an existing item by id is only accepted where references are allowed, and
// becomes a reference for editedDeckItems to resolve.
func buildDeckItems(
	env *requests.RequestEnv, inputs []models.DeckItemInput, allowReferences bool,
) ([]database.DeckItem, error) {
	items := make([]database.DeckItem, 0, len(inputs))
	for i := range inputs {
		input := &inputs[i]
		var item database.DeckItem
		var err error
		if input.ID != nil {
			item, err = deckItemReference(input, allowReferences)
		} else {
			item, err = buildDeckItem(env, input)
		}
		if err != nil {
			return nil, models.ClientErrf("invalid item %d: %w", i+1, err)
		}
		items = append(items, item)
	}
	return items, nil
}

// deckItemReference is the placeholder for an input that keeps an existing
// item: an item with only its DBID set.
func deckItemReference(input *models.DeckItemInput, allowed bool) (database.DeckItem, error) {
	if !allowed {
		return database.DeckItem{}, errors.New("only the items of decks.update can name an existing item by id")
	}
	if input.Kind != "" || input.MediaID != nil || input.System != "" || input.Path != "" ||
		input.Name != "" || input.ZapScript != "" || input.CardID != "" ||
		len(input.Scripts) > 0 || len(input.Metadata) > 0 {
		return database.DeckItem{}, errors.New("an item named by id takes no other fields")
	}
	return database.DeckItem{DBID: *input.ID}, nil
}

func buildDeckItem(env *requests.RequestEnv, input *models.DeckItemInput) (database.DeckItem, error) {
	switch input.Kind {
	case deckItemKindMedia:
		return buildMediaDeckItem(env, input)
	case database.DeckItemKindScript:
		script := strings.TrimSpace(input.ZapScript)
		if script == "" {
			return database.DeckItem{}, errors.New("a script item needs a zapscript")
		}
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return database.DeckItem{}, errors.New("a script item needs a name")
		}
		return database.DeckItem{Kind: database.DeckItemKindScript, Name: name, ZapScript: script}, nil
	case database.DeckItemKindCard:
		cardID := strings.TrimSpace(input.CardID)
		if cardID == "" {
			return database.DeckItem{}, errors.New("a card item needs a cardId")
		}
		scripts := make([]database.DeckCardScript, 0, len(input.Scripts))
		for _, s := range input.Scripts {
			script := strings.TrimSpace(s.ZapScript)
			if script == "" {
				return database.DeckItem{}, errors.New("a card script needs a zapscript")
			}
			scripts = append(scripts, database.DeckCardScript{Name: strings.TrimSpace(s.Name), ZapScript: script})
		}
		return database.DeckItem{
			Kind: database.DeckItemKindCard, Name: strings.TrimSpace(input.Name), CardID: cardID,
			Scripts: scripts, Metadata: input.Metadata,
		}, nil
	case "":
		return database.DeckItem{}, errors.New("an item needs a kind, or an id to keep an existing item")
	default:
		return database.DeckItem{}, fmt.Errorf("unknown item kind %q", input.Kind)
	}
}

func buildMediaDeckItem(env *requests.RequestEnv, input *models.DeckItemInput) (database.DeckItem, error) {
	if env.Database == nil || env.Database.MediaDB == nil {
		return database.DeckItem{}, errors.New("media database is not available")
	}
	ref := mediaRefParam{MediaID: input.MediaID, System: input.System, Path: input.Path}
	if err := validateMediaRef(ref); err != nil {
		return database.DeckItem{}, err
	}
	resolved, err := resolveMediaRefs(env, []mediaRefParam{ref})
	if err != nil {
		return database.DeckItem{}, err
	}
	if len(resolved) != 1 || resolved[0].Row == nil {
		if len(resolved) == 1 && resolved[0].Err != nil {
			return database.DeckItem{}, resolved[0].Err
		}
		return database.DeckItem{}, errors.New("media not found")
	}
	row := resolved[0].Row
	item, err := decks.ComposeMediaItem(env.Context, env.Database.MediaDB, row.System.SystemID, row.Path)
	if err != nil {
		return database.DeckItem{}, fmt.Errorf("compose media item: %w", err)
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		item.Name = name
	}
	return item, nil
}

// anchorAvailability reports, per item id, whether an anchored item's file
// is currently indexed. Items without an anchor are absent from the map.
func anchorAvailability(env *requests.RequestEnv, items []database.DeckItem) map[int64]bool {
	if env.Database == nil || env.Database.MediaDB == nil {
		return nil
	}
	bySystem := make(map[string][]int)
	for i := range items {
		if items[i].HasAnchor() {
			bySystem[items[i].Anchor.SystemID] = append(bySystem[items[i].Anchor.SystemID], i)
		}
	}
	if len(bySystem) == 0 {
		return nil
	}
	available := make(map[int64]bool, len(items))
	for systemID, indexes := range bySystem {
		system, err := env.Database.MediaDB.FindSystemBySystemID(systemID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			log.Debug().Err(err).Str("system", systemID).Msg("deck anchor system lookup failed")
			continue
		}
		paths := make([]string, 0, len(indexes))
		for _, i := range indexes {
			paths = append(paths, items[i].Anchor.Path)
		}
		found, err := env.Database.MediaDB.FindMediaBySystemAndPaths(env.Context, system.DBID, paths)
		if err != nil {
			log.Debug().Err(err).Str("system", systemID).Msg("deck anchor media lookup failed")
			continue
		}
		for _, i := range indexes {
			media, ok := found[items[i].Anchor.Path]
			available[items[i].DBID] = ok && !media.IsMissing
		}
	}
	return available
}

// isDeckLocked reports whether the linked account locked a deck, which keeps
// it read-only here. A sync row that cannot be read is an error rather than
// an unlocked deck, so a failure never opens a locked deck up to edits.
// Mutations are refused inside their own transaction as well; this answers
// the clients that ask.
func isDeckLocked(env *requests.RequestEnv, deckID string) (bool, error) {
	row, found, err := env.Database.UserDB.GetDeckSync(deckID)
	if err != nil {
		return false, fmt.Errorf("failed to read deck sync state: %w", err)
	}
	return found && row.Locked, nil
}

// lockedDecks names every deck the account locked, for the decks list.
func lockedDecks(env *requests.RequestEnv) (map[string]bool, error) {
	rows, err := env.Database.UserDB.ListDeckSync()
	if err != nil {
		return nil, fmt.Errorf("failed to read deck sync state: %w", err)
	}
	locked := make(map[string]bool, len(rows))
	for i := range rows {
		if rows[i].Locked {
			locked[rows[i].DeckID] = true
		}
	}
	return locked, nil
}

// lockedDeckResponse is a deck with its items and its lock state.
func lockedDeckResponse(env *requests.RequestEnv, deck *database.Deck) (models.DeckResponse, error) {
	resp := deckResponse(deck, anchorAvailability(env, deck.Items))
	locked, err := isDeckLocked(env, deck.DeckID)
	if err != nil {
		return models.DeckResponse{}, err
	}
	resp.Locked = locked
	return resp, nil
}

// deckSummary is a deck as the decks list shows it, without items.
func deckSummary(deck *database.Deck) models.DeckResponse {
	return models.DeckResponse{
		Metadata:    deck.Metadata,
		DeckID:      deck.DeckID,
		Name:        deck.Name,
		Description: deck.Description,
		CreatedAt:   deck.CreatedAt,
		UpdatedAt:   deck.UpdatedAt,
		ItemCount:   deck.ItemCount,
		Owned:       deck.Owned,
	}
}

// deckResponse is a deck with its items, which are never omitted: an empty
// deck has an empty list.
func deckResponse(deck *database.Deck, available map[int64]bool) models.DeckResponse {
	resp := deckSummary(deck)
	resp.ItemCount = len(deck.Items)
	resp.Items = make([]models.DeckItemResponse, 0, len(deck.Items))
	for i := range deck.Items {
		item := &deck.Items[i]
		out := models.DeckItemResponse{
			Metadata: item.Metadata, Kind: item.Kind, Name: item.Name, ZapScript: item.ZapScript,
			CardID: item.CardID, ID: item.DBID, Position: item.Position,
		}
		for _, s := range item.Scripts {
			out.Scripts = append(out.Scripts, models.DeckCardScriptInput{Name: s.Name, ZapScript: s.ZapScript})
		}
		if item.HasAnchor() {
			out.Media = &models.DeckItemMedia{
				System: item.Anchor.SystemID, Path: item.Anchor.Path, Name: item.Anchor.MediaName,
				Tags: item.Anchor.Tags, Available: available[item.DBID],
			}
		}
		resp.Items = append(resp.Items, out)
	}
	return resp
}
