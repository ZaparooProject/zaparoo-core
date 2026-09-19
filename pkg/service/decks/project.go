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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

// titleResolveTimeout bounds one title resolution while projecting a deck,
// so a slow lookup cannot stall an edit or a reindex.
const titleResolveTimeout = 2 * time.Second

// projectMu serializes deck projections; see ProjectDeck.
var projectMu syncutil.Mutex

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
	// relinks maps an item's index to the anchor its title resolved to, for
	// items whose anchored file is no longer in the media database.
	relinks  map[int]database.DeckItemAnchor
	mediaIDs []int64
}

// resolveItems maps a deck's game items to indexed media. Anchored items are
// looked up by path in one query per system. An item whose file is gone from
// the media database, or that has no anchor, resolves by title and is
// re-linked. An item whose file is only marked missing also resolves by title
// but keeps its anchor, since the file may come back, as a drive plugged in
// again does. Card items and scripts that are not a title launch never map to
// media.
func resolveItems(ctx context.Context, deps *ResolveDeps, items []database.DeckItem) (resolvedItems, error) {
	out := resolvedItems{relinks: make(map[int]database.DeckItemAnchor)}
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
	keepAnchor := make(map[int]struct{})
	for systemID, indexes := range bySystem {
		found, err := mediaByAnchor(ctx, deps.MediaDB, systemID, items, indexes)
		if err != nil {
			return out, err
		}
		for _, i := range indexes {
			media, ok := found[items[i].Anchor.Path]
			switch {
			case !ok:
			case media.IsMissing:
				keepAnchor[i] = struct{}{}
			default:
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
		match, found, err := resolveByTitle(ctx, deps, item.ZapScript)
		if err != nil {
			return out, err
		}
		if !found {
			continue
		}
		add(match.mediaID)
		if _, keep := keepAnchor[i]; !keep && item.DBID != 0 {
			out.relinks[i] = match.anchor
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

// titleMatch is the file a deck item's title launch resolved to.
type titleMatch struct {
	anchor  database.DeckItemAnchor
	mediaID int64
}

// resolveByTitle resolves a title launch the way a card tap would and
// returns the matched media with an anchor snapshot, and whether anything
// matched. Matches below the acceptable confidence are ignored, so a weak
// match never tags or re-links a file. The slug cache is skipped so the match
// is selected afresh from the library. A lookup that fails, rather than finding
// nothing, is returned as an error: the item's file is unknown, not absent.
func resolveByTitle(ctx context.Context, deps *ResolveDeps, script string) (titleMatch, bool, error) {
	system, gameName, parsed := ParseTitleLaunch(script)
	if !parsed {
		return titleMatch{}, false, nil
	}
	var launchers []platforms.Launcher
	if deps.LaunchersForSystem != nil {
		launchers = deps.LaunchersForSystem(system.ID)
	}
	resolveCtx, cancel := context.WithTimeout(ctx, titleResolveTimeout)
	defer cancel()
	result, err := titles.ResolveTitle(resolveCtx, &titles.ResolveParams{
		MediaDB:          deps.MediaDB,
		Cfg:              deps.Cfg,
		SystemID:         system.ID,
		GameName:         gameName,
		MediaType:        system.GetMediaType(),
		Launchers:        launchers,
		SkipCachedResult: true,
	})
	switch {
	case errors.Is(err, titles.ErrNoMatch), errors.Is(err, titles.ErrLowConfidence):
		return titleMatch{}, false, nil
	case err != nil:
		return titleMatch{}, false, fmt.Errorf("resolve %s: %w", script, err)
	case result == nil || result.Confidence < titles.ConfidenceAcceptable:
		return titleMatch{}, false, nil
	}
	anchor := database.DeckItemAnchor{
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
	return titleMatch{anchor: anchor, mediaID: result.Result.MediaID}, true, nil
}

// ProjectDeck makes the media carrying a deck's membership tag match the
// deck as it is stored when the projection runs: exactly the files its game
// items resolve to, or none when the deck no longer exists. Items that
// resolved by title because their file is gone are linked to the file they
// matched, and the number of such items is returned. Projections run one at a
// time and each reads the deck itself, so a projection can never write the
// membership of an older copy of the deck over a newer one.
func ProjectDeck(ctx context.Context, deps *ResolveDeps, deckID string) (relinked int, err error) {
	projectMu.Lock()
	defer projectMu.Unlock()

	deck, err := deps.UserDB.GetDeck(deckID)
	switch {
	case errors.Is(err, database.ErrDeckNotFound):
		deck = nil
	case err != nil:
		return 0, fmt.Errorf("load deck %s: %w", deckID, err)
	}
	var mediaIDs []int64
	if deck != nil {
		resolved, resolveErr := resolveItems(ctx, deps, deck.Items)
		if resolveErr != nil {
			return 0, fmt.Errorf("resolve deck %s: %w", deckID, resolveErr)
		}
		for i, anchor := range resolved.relinks {
			if linkErr := deps.UserDB.SetDeckItemAnchor(deck.Items[i].DBID, &anchor); linkErr != nil {
				log.Warn().Err(linkErr).Str("deck", deckID).Int64("item", deck.Items[i].DBID).
					Msg("failed to link deck item to resolved media")
				continue
			}
			relinked++
		}
		mediaIDs = resolved.mediaIDs
	}
	if _, err = deps.MediaDB.SetMediaTagMembership(ctx, DeckTagRef(deckID), mediaIDs); err != nil {
		return relinked, fmt.Errorf("project deck %s: %w", deckID, err)
	}
	return relinked, nil
}
