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
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const (
	// URIScheme names a local deck in a playlist command:
	// **playlist.open:deck://<id>.
	URIScheme = "deck"
	// MaxFetchedDecks bounds how many decks kept from links a device holds.
	// Any link that serves a playlist is kept, so the bound is what stops
	// taps piling decks up without end; the ones fetched longest ago go
	// first.
	MaxFetchedDecks = 200
)

// ErrNoUserDB reports a deck store reached with no user database open.
var ErrNoUserDB = errors.New("no user database")

// PlaylistItem is one entry of a deck as a playlist runs it.
type PlaylistItem struct {
	Name      string
	ZapScript string
}

// DeckURI returns the playlist argument that opens a deck.
func DeckURI(deckID string) string {
	return URIScheme + "://" + deckID
}

// ParseDeckURI returns the deck ID a deck:// playlist argument names.
func ParseDeckURI(arg string) (string, bool) {
	scheme, rest, found := strings.Cut(strings.TrimSpace(arg), "://")
	if !found || !strings.EqualFold(scheme, URIScheme) {
		return "", false
	}
	deckID, err := database.NormalizeDeckID(rest)
	if err != nil {
		return "", false
	}
	return deckID, true
}

// PlaylistID returns the ID a deck opens as. A deck kept from a link opens as
// the playlist ID its source served, so the local copy and the served
// playlist are one playlist; any other deck opens as its own deck:// URI.
func PlaylistID(deck *database.Deck) string {
	if deck.PlaylistID != "" {
		return deck.PlaylistID
	}
	return DeckURI(strings.ToLower(deck.DeckID))
}

// PlaylistArg is the JSON argument of a playlist command.
type PlaylistArg struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Items []PlaylistArgItem `json:"items"`
}

// PlaylistArgItem is one item of a playlist argument.
type PlaylistArgItem struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

// ParseServedPlaylist reads a fetched ZapLink body as a playlist to keep: one
// playlist.open, playlist.play or playlist.load command whose only argument
// is a JSON playlist with at least one item to run. It returns the command as
// served and its playlist. What the link looks like and what the playlist
// calls itself play no part, so every host's playlist is kept alike.
func ParseServedPlaylist(body string) (zapscript.Command, PlaylistArg, bool) {
	parsed, err := zapscript.NewParser(body).ParseScript()
	if err != nil || len(parsed.Cmds) != 1 {
		return zapscript.Command{}, PlaylistArg{}, false
	}
	cmd := parsed.Cmds[0]
	switch cmd.Name {
	case zapscript.ZapScriptCmdPlaylistOpen, zapscript.ZapScriptCmdPlaylistPlay, zapscript.ZapScriptCmdPlaylistLoad:
	default:
		return zapscript.Command{}, PlaylistArg{}, false
	}
	if len(cmd.Args) != 1 {
		return zapscript.Command{}, PlaylistArg{}, false
	}
	var arg PlaylistArg
	if jsonErr := json.Unmarshal([]byte(cmd.Args[0]), &arg); jsonErr != nil {
		return zapscript.Command{}, PlaylistArg{}, false
	}
	for _, item := range arg.Items {
		if strings.TrimSpace(item.ZapScript) != "" {
			return cmd, arg, true
		}
	}
	return zapscript.Command{}, PlaylistArg{}, false
}

// StoreFetchedDeck keeps a playlist served through a ZapLink as a read-only
// deck and returns the deck's ID on this device. The copy is known by the
// link it came from and nothing else, so it never stands in for, or is hidden
// by, a deck the user owns. Every served entry becomes a script item: a card
// with several scripts is served as a nested playlist command and is kept as
// that script. A deck that changed has its membership tags queued, so the
// tags and the files its items link to follow the fetch in the background.
func StoreFetchedDeck(db *database.Database, sourceURL string, arg *PlaylistArg) (string, error) {
	if db == nil || db.UserDB == nil {
		return "", ErrNoUserDB
	}
	items := make([]database.DeckItem, 0, len(arg.Items))
	for _, item := range arg.Items {
		if strings.TrimSpace(item.ZapScript) == "" {
			continue
		}
		items = append(items, database.DeckItem{
			Kind: database.DeckItemKindScript, Name: item.Name, ZapScript: item.ZapScript,
		})
	}
	if len(items) > database.DeckMaxItems {
		return "", fmt.Errorf("%w: fetched deck has %d items", database.ErrDeckItemLimit, len(items))
	}
	name := strings.TrimSpace(arg.Name)
	if name == "" {
		name = sourceURL
	}
	result, err := db.UserDB.UpsertFetchedDeck(&database.Deck{
		Name: name, SourceURL: sourceURL, PlaylistID: arg.ID, Items: items,
	}, MaxFetchedDecks)
	if err != nil {
		return "", fmt.Errorf("store fetched deck: %w", err)
	}
	if result.Changed {
		db.QueueDeckTags(result.DeckID)
	}
	if len(result.Evicted) > 0 {
		db.QueueDeckTags(result.Evicted...)
	}
	return result.DeckID, nil
}

// PlaylistItems turns a deck into the entries a playlist runs. A game item
// whose linked file is still indexed launches that exact file, so the pick
// made on this device is what plays; any other game item runs its title
// launch. A card item runs its script, or opens its scripts as a nested
// playlist when it has several, the way the card's own link does.
func PlaylistItems(ctx context.Context, mediaDB database.MediaDBI, deck *database.Deck) []PlaylistItem {
	available := anchoredAvailability(ctx, mediaDB, deck.Items)
	out := make([]PlaylistItem, 0, len(deck.Items))
	for i := range deck.Items {
		item := &deck.Items[i]
		switch item.Kind {
		case database.DeckItemKindScript:
			script := item.ZapScript
			if item.HasAnchor() && available[i] {
				script = zapscript.Command{
					Name:    zapscript.ZapScriptCmdLaunch,
					Args:    []string{item.Anchor.Path},
					AdvArgs: zapscript.NewAdvArgs(map[string]string{string(zapscript.KeySystem): item.Anchor.SystemID}),
				}.String()
			}
			if strings.TrimSpace(script) == "" {
				continue
			}
			out = append(out, PlaylistItem{Name: item.Name, ZapScript: script})
		case database.DeckItemKindCard:
			if script, ok := cardPlaylistScript(deck, item); ok {
				out = append(out, PlaylistItem{Name: item.Name, ZapScript: script})
			}
		}
	}
	return out
}

func cardPlaylistScript(deck *database.Deck, item *database.DeckItem) (string, bool) {
	switch len(item.Scripts) {
	case 0:
		return "", false
	case 1:
		return item.Scripts[0].ZapScript, item.Scripts[0].ZapScript != ""
	}
	nested := PlaylistArg{
		ID:    DeckURI(strings.ToLower(deck.DeckID)) + "/" + item.CardID,
		Name:  item.Name,
		Items: make([]PlaylistArgItem, 0, len(item.Scripts)),
	}
	for _, s := range item.Scripts {
		nested.Items = append(nested.Items, PlaylistArgItem{Name: s.Name, ZapScript: s.ZapScript})
	}
	encoded, err := json.Marshal(nested)
	if err != nil {
		return "", false
	}
	return "**" + zapscript.ZapScriptCmdPlaylistOpen + ":" + string(encoded), true
}

// anchoredAvailability reports, by item index, whether an anchored item's
// file is indexed and present.
func anchoredAvailability(ctx context.Context, mediaDB database.MediaDBI, items []database.DeckItem) map[int]bool {
	available := make(map[int]bool)
	if mediaDB == nil {
		return available
	}
	bySystem := make(map[string][]int)
	for i := range items {
		if items[i].Kind == database.DeckItemKindScript && items[i].HasAnchor() {
			bySystem[items[i].Anchor.SystemID] = append(bySystem[items[i].Anchor.SystemID], i)
		}
	}
	for systemID, indexes := range bySystem {
		found, err := mediaByAnchor(ctx, mediaDB, systemID, items, indexes)
		if err != nil {
			log.Debug().Err(err).Str("system", systemID).Msg("deck anchor lookup failed; using title launches")
			continue
		}
		for _, i := range indexes {
			if media, ok := found[items[i].Anchor.Path]; ok && !media.IsMissing {
				available[i] = true
			}
		}
	}
	return available
}
