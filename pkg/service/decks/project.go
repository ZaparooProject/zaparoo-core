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

package decks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

// titleResolveTimeout bounds one title resolution while projecting a deck,
// so a slow lookup cannot stall an edit or a reindex.
const titleResolveTimeout = 2 * time.Second

// ResolveDeps carries what resolving deck items to local media needs.
type ResolveDeps struct {
	MediaDB database.MediaDBI
	UserDB  database.UserDBI
	Cfg     *config.Instance
	// LaunchersForSystem returns the launchers used to rank title matches,
	// the same set a title launch ranks with.
	LaunchersForSystem func(systemID string) []platforms.Launcher
}

// DeckTagRef is the media tag that marks membership of one deck.
func DeckTagRef(deckID string) database.MediaTagRef {
	return database.MediaTagRef{Type: string(tags.TagTypeUser), Tag: string(tags.DeckTag(deckID))}
}

// ParseTitleLaunch returns the system and title (with any inline tags) a
// script names when the script is a single launch.title command.
func ParseTitleLaunch(script string) (system *systemdefs.System, gameName string, ok bool) {
	parsed, err := zapscript.NewParser(script).ParseScript()
	if err != nil || len(parsed.Cmds) != 1 {
		return nil, "", false
	}
	cmd := parsed.Cmds[0]
	if cmd.Name != zapscript.ZapScriptCmdLaunchTitle || len(cmd.Args) != 1 {
		return nil, "", false
	}
	systemID, game, found := strings.Cut(cmd.Args[0], "/")
	if !found || systemID == "" || game == "" {
		return nil, "", false
	}
	system, err = systemdefs.LookupSystem(systemID)
	if err != nil || system == nil {
		return nil, "", false
	}
	return system, game, true
}

// resolvedItems is the outcome of resolving a deck's items to local media.
type resolvedItems struct {
	// relinks maps an item's DBID to the anchor its title resolved to, for
	// items whose anchor was missing or no longer indexed.
	relinks  map[int64]database.DeckItemAnchor
	mediaIDs []int64
}

// resolveItems maps a deck's game items to indexed media. Anchored items are
// looked up by path in one query per system; items without a usable anchor
// resolve by title and are re-linked. Card items and scripts that are not a
// title launch never map to media.
func resolveItems(ctx context.Context, deps *ResolveDeps, items []database.DeckItem) (resolvedItems, error) {
	out := resolvedItems{relinks: make(map[int64]database.DeckItemAnchor)}
	seen := make(map[int64]struct{}, len(items))
	add := func(id int64) {
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			out.mediaIDs = append(out.mediaIDs, id)
		}
	}

	bySystem := make(map[string][]int)
	for i := range items {
		if items[i].Kind == database.DeckItemKindScript && items[i].HasAnchor() {
			bySystem[items[i].Anchor.SystemID] = append(bySystem[items[i].Anchor.SystemID], i)
		}
	}
	anchored := make(map[int]struct{}, len(items))
	for systemID, indexes := range bySystem {
		found, err := mediaByAnchor(ctx, deps.MediaDB, systemID, items, indexes)
		if err != nil {
			return out, err
		}
		for _, i := range indexes {
			if media, ok := found[items[i].Anchor.Path]; ok && !media.IsMissing {
				add(media.DBID)
				anchored[i] = struct{}{}
			}
		}
	}

	for i := range items {
		item := &items[i]
		if item.Kind != database.DeckItemKindScript {
			continue
		}
		if _, ok := anchored[i]; ok {
			continue
		}
		mediaID, anchor, ok := resolveByTitle(ctx, deps, item.ZapScript)
		if !ok {
			continue
		}
		add(mediaID)
		if item.DBID != 0 {
			out.relinks[item.DBID] = anchor
		}
	}
	return out, nil
}

func mediaByAnchor(
	ctx context.Context, mediaDB database.MediaDBI, systemID string, items []database.DeckItem, indexes []int,
) (map[string]database.Media, error) {
	system, err := mediaDB.FindSystemBySystemID(systemID)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]database.Media{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find system %q: %w", systemID, err)
	}
	paths := make([]string, 0, len(indexes))
	for _, i := range indexes {
		paths = append(paths, items[i].Anchor.Path)
	}
	found, err := mediaDB.FindMediaBySystemAndPaths(ctx, system.DBID, paths)
	if err != nil {
		return nil, fmt.Errorf("find anchored media for %q: %w", systemID, err)
	}
	return found, nil
}

// resolveByTitle resolves a title launch the way a card tap would and
// returns the matched media with an anchor snapshot. Matches below the
// acceptable confidence are ignored, so a weak fuzzy match never tags or
// re-links a file.
func resolveByTitle(
	ctx context.Context, deps *ResolveDeps, script string,
) (mediaID int64, anchor database.DeckItemAnchor, ok bool) {
	system, gameName, parsed := ParseTitleLaunch(script)
	if !parsed {
		return 0, database.DeckItemAnchor{}, false
	}
	var launchers []platforms.Launcher
	if deps.LaunchersForSystem != nil {
		launchers = deps.LaunchersForSystem(system.ID)
	}
	resolveCtx, cancel := context.WithTimeout(ctx, titleResolveTimeout)
	defer cancel()
	result, err := titles.ResolveTitle(resolveCtx, &titles.ResolveParams{
		MediaDB:   deps.MediaDB,
		Cfg:       deps.Cfg,
		SystemID:  system.ID,
		GameName:  gameName,
		MediaType: system.GetMediaType(),
		Launchers: launchers,
	})
	if err != nil || result == nil || result.Confidence < titles.ConfidenceAcceptable {
		if err != nil && !errors.Is(err, titles.ErrNoMatch) && !errors.Is(err, titles.ErrLowConfidence) {
			log.Debug().Err(err).Str("script", script).Msg("deck item title resolution failed")
		}
		return 0, database.DeckItemAnchor{}, false
	}
	anchor = database.DeckItemAnchor{
		SystemID:  result.Result.SystemID,
		Path:      result.Result.Path,
		MediaName: result.Result.Name,
	}
	if identity, found, idErr := database.LookupMediaIdentity(
		ctx, deps.MediaDB, result.Result.SystemID, result.Result.Path,
	); idErr == nil && found {
		anchor.MediaName = identity.DisplayName
		anchor.Tags = identity.LegacyTags()
	}
	return result.Result.MediaID, anchor, true
}

// ProjectDeck makes exactly the media a deck's game items resolve to carry
// the deck's membership tag, and re-links items that resolved by title. It
// returns how many media carry the tag.
func ProjectDeck(ctx context.Context, deps *ResolveDeps, deck *database.Deck) (int, error) {
	resolved, err := resolveItems(ctx, deps, deck.Items)
	if err != nil {
		return 0, err
	}
	for itemDBID, anchor := range resolved.relinks {
		if deps.UserDB == nil {
			break
		}
		if linkErr := deps.UserDB.SetDeckItemAnchor(itemDBID, &anchor); linkErr != nil {
			log.Warn().Err(linkErr).Str("deck", deck.DeckID).Int64("item", itemDBID).
				Msg("failed to re-link deck item to resolved media")
		}
	}
	if _, err = deps.MediaDB.SetMediaTagMembership(ctx, DeckTagRef(deck.DeckID), resolved.mediaIDs); err != nil {
		return 0, fmt.Errorf("project deck %s: %w", deck.DeckID, err)
	}
	return len(resolved.mediaIDs), nil
}

// ClearDeckProjection removes a deck's membership tag from every media row.
func ClearDeckProjection(ctx context.Context, mediaDB database.MediaDBI, deckID string) error {
	if _, err := mediaDB.SetMediaTagMembership(ctx, DeckTagRef(deckID), nil); err != nil {
		return fmt.Errorf("clear deck %s projection: %w", deckID, err)
	}
	return nil
}

// ReapplyDeckTags re-materializes every deck's membership tags from the user
// database after the media rows have been rebuilt, re-linking items whose
// files moved or were never resolved on this device. It returns how many
// decks were projected. A deck that fails is logged and skipped so one bad
// deck cannot stop the rest.
func ReapplyDeckTags(ctx context.Context, deps *ResolveDeps) (int, error) {
	links, err := deps.UserDB.ListDeckItemLinks()
	if err != nil {
		return 0, fmt.Errorf("list deck items: %w", err)
	}
	order := make([]string, 0)
	byDeck := make(map[string][]database.DeckItem)
	for i := range links {
		link := &links[i]
		if _, known := byDeck[link.DeckID]; !known {
			order = append(order, link.DeckID)
		}
		byDeck[link.DeckID] = append(byDeck[link.DeckID], database.DeckItem{
			DBID: link.ItemDBID, Kind: link.Kind, ZapScript: link.ZapScript, Anchor: link.Anchor,
		})
	}
	projected := 0
	for _, deckID := range order {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return projected, fmt.Errorf("reapply deck tags: %w", ctxErr)
		}
		deck := &database.Deck{DeckID: deckID, Items: byDeck[deckID]}
		if _, projErr := ProjectDeck(ctx, deps, deck); projErr != nil {
			log.Warn().Err(projErr).Str("deck", deckID).Msg("failed to re-apply deck tags")
			continue
		}
		projected++
	}
	return projected, nil
}
