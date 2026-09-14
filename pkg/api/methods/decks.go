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
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
	list, err := env.Database.UserDB.ListDecks()
	if err != nil {
		return nil, fmt.Errorf("failed to list decks: %w", err)
	}
	resp := models.DecksResponse{Decks: make([]models.DeckResponse, 0, len(list))}
	for i := range list {
		resp.Decks = append(resp.Decks, deckResponse(&list[i], nil))
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
	deck, err := loadDeck(&env, params.DeckID)
	if err != nil {
		return nil, err
	}
	return deckResponse(deck, anchorAvailability(&env, deck.Items)), nil
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
	items, err := buildDeckItems(&env, params.Items)
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
	projectDeck(&env, deck)
	notifyDecksChanged(&env, deck.DeckID, models.DecksChangedCreated)
	return deckResponse(deck, anchorAvailability(&env, deck.Items)), nil
}

// HandleDecksUpdate edits an owned deck's name, description or items.
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
	if !deck.Owned {
		return nil, models.ClientErr(database.ErrDeckReadOnly)
	}

	if params.Name != nil || params.Description != nil {
		name, description := deck.Name, deck.Description
		if params.Name != nil {
			name = strings.TrimSpace(*params.Name)
		}
		if params.Description != nil {
			description = strings.TrimSpace(*params.Description)
		}
		if name == "" {
			return nil, models.ClientErrf("invalid params: name cannot be empty")
		}
		if metaErr := env.Database.UserDB.UpdateDeckMeta(deck.DeckID, name, description, nil); metaErr != nil {
			return nil, deckError(metaErr)
		}
	}

	if params.Items != nil || len(params.AddItems) > 0 || len(params.RemoveItemIDs) > 0 {
		items, buildErr := editedDeckItems(&env, deck.Items, &params)
		if buildErr != nil {
			return nil, buildErr
		}
		if itemsErr := env.Database.UserDB.ReplaceDeckItems(deck.DeckID, items); itemsErr != nil {
			return nil, deckError(itemsErr)
		}
	}

	updated, err := loadDeck(&env, deck.DeckID)
	if err != nil {
		return nil, err
	}
	projectDeck(&env, updated)
	notifyDecksChanged(&env, updated.DeckID, models.DecksChangedUpdated)
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
	if env.Database.MediaDB != nil {
		if clearErr := decks.ClearDeckProjection(env.Context, env.Database.MediaDB, deckID); clearErr != nil {
			log.Warn().Err(clearErr).Str("deck", deckID).Msg("failed to clear deck membership tags")
		}
	}
	notifyDecksChanged(&env, deckID, models.DecksChangedDeleted)
	return NoContent{}, nil
}

// projectDeck tags the media a deck's game items resolve to with the deck's
// membership tag. The deck itself is already stored, so a failure only means
// the tag is missing until the next reindex re-applies it; it is logged, not
// returned.
func projectDeck(env *requests.RequestEnv, deck *database.Deck) {
	if env.Database == nil || env.Database.MediaDB == nil {
		return
	}
	launchers := func(systemID string) []platforms.Launcher {
		if env.LauncherCache == nil {
			return nil
		}
		return env.LauncherCache.GetLaunchersBySystem(systemID)
	}
	deps := deckResolveDeps(env.Database, env.Config, launchers)
	if _, err := decks.ProjectDeck(env.Context, deps, deck); err != nil {
		log.Warn().Err(err).Str("deck", deck.DeckID).Msg("failed to project deck membership tags")
	}
}

func deckResolveDeps(
	db *database.Database, cfg *config.Instance, launchers func(string) []platforms.Launcher,
) *decks.ResolveDeps {
	return &decks.ResolveDeps{
		MediaDB: db.MediaDB, UserDB: db.UserDB, Cfg: cfg, LaunchersForSystem: launchers,
	}
}

// platformLaunchers returns a lookup of a platform's launchers for one
// system, the set a title launch ranks matches with.
func platformLaunchers(pl platforms.Platform, cfg *config.Instance) func(string) []platforms.Launcher {
	return func(systemID string) []platforms.Launcher {
		if pl == nil {
			return nil
		}
		all := pl.Launchers(cfg)
		out := make([]platforms.Launcher, 0, len(all))
		for i := range all {
			if all[i].SystemID == systemID {
				out = append(out, all[i])
			}
		}
		return out
	}
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

// editedDeckItems applies an update's item edits to the current list in the
// order remove, replace, append.
func editedDeckItems(
	env *requests.RequestEnv, current []database.DeckItem, params *models.DecksUpdateParams,
) ([]database.DeckItem, error) {
	items := make([]database.DeckItem, 0, len(current))
	if len(params.RemoveItemIDs) > 0 {
		remove := make(map[int64]struct{}, len(params.RemoveItemIDs))
		for _, id := range params.RemoveItemIDs {
			remove[id] = struct{}{}
		}
		for i := range current {
			if _, drop := remove[current[i].DBID]; !drop {
				items = append(items, current[i])
			}
		}
	} else {
		items = append(items, current...)
	}
	if params.Items != nil {
		replaced, err := buildDeckItems(env, *params.Items)
		if err != nil {
			return nil, err
		}
		items = replaced
	}
	if len(params.AddItems) > 0 {
		added, err := buildDeckItems(env, params.AddItems)
		if err != nil {
			return nil, err
		}
		items = append(items, added...)
	}
	if len(items) > database.DeckMaxItems {
		return nil, models.ClientErr(database.ErrDeckItemLimit)
	}
	return items, nil
}

// buildDeckItems turns client item inputs into stored items. A media item is
// resolved through the media database and composed into a title launch with
// its anchor; script and card items are stored as written.
func buildDeckItems(env *requests.RequestEnv, inputs []models.DeckItemInput) ([]database.DeckItem, error) {
	items := make([]database.DeckItem, 0, len(inputs))
	for i := range inputs {
		input := &inputs[i]
		item, err := buildDeckItem(env, input)
		if err != nil {
			return nil, models.ClientErrf("invalid item %d: %w", i+1, err)
		}
		items = append(items, item)
	}
	return items, nil
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
			scripts = append(scripts, database.DeckCardScript{Name: s.Name, ZapScript: s.ZapScript})
		}
		return database.DeckItem{
			Kind: database.DeckItemKindCard, Name: strings.TrimSpace(input.Name), CardID: cardID,
			Scripts: scripts, Metadata: input.Metadata,
		}, nil
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
		found, err := env.Database.MediaDB.FindMediaBySystemAndPaths(
			context.WithoutCancel(env.Context), system.DBID, paths,
		)
		if err != nil {
			log.Debug().Err(err).Str("system", systemID).Msg("deck anchor media lookup failed")
			continue
		}
		for _, i := range indexes {
			_, ok := found[items[i].Anchor.Path]
			available[items[i].DBID] = ok
		}
	}
	return available
}

func deckResponse(deck *database.Deck, available map[int64]bool) models.DeckResponse {
	resp := models.DeckResponse{
		Metadata:    deck.Metadata,
		DeckID:      deck.DeckID,
		Name:        deck.Name,
		Description: deck.Description,
		CreatedAt:   deck.CreatedAt,
		UpdatedAt:   deck.UpdatedAt,
		ItemCount:   deck.ItemCount,
		Owned:       deck.Owned,
	}
	if deck.Items == nil {
		return resp
	}
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
