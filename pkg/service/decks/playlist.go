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
	"net/url"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

const (
	// URIScheme names a local deck in a playlist command:
	// **playlist.open:deck://<id>.
	URIScheme = "deck"
	// PlaylistIDPrefix leads the playlist ID a deck opens as. It is the
	// prefix the online service uses when it serves a deck as a playlist, so
	// a deck opened locally and one opened from its ZapLink are the same
	// playlist.
	PlaylistIDPrefix = "ZON-"
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

// PlaylistID returns the ID a deck opens as.
func PlaylistID(deckID string) string {
	return PlaylistIDPrefix + strings.ToLower(deckID)
}

// DeckIDFromZapLinkURL returns the deck a ZapLink URL names. Only the
// official hosts serve decks, under a path of "d" followed by the deck ID;
// any other URL, including a card link, is not a deck.
func DeckIDFromZapLinkURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	official := false
	for _, host := range config.OfficialAuthHosts {
		if strings.EqualFold(u.Hostname(), host) {
			official = true
			break
		}
	}
	if !official {
		return "", false
	}
	path := strings.TrimPrefix(u.Path, "/")
	if len(path) < 2 || (path[0] != 'd' && path[0] != 'D') || strings.Contains(path, "/") {
		return "", false
	}
	deckID, err := database.NormalizeDeckID(path[1:])
	if err != nil {
		return "", false
	}
	return deckID, true
}

// PlaylistArg is the JSON argument of a playlist command, the shape the
// online service serves a deck in.
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

// ParseDeckPlaylist reads a fetched ZapLink body as the playlist a deck is
// served as: one playlist.open command whose JSON argument carries the
// deck's own playlist ID. Anything else is not that deck.
func ParseDeckPlaylist(body, deckID string) (PlaylistArg, bool) {
	parsed, err := zapscript.NewParser(body).ParseScript()
	if err != nil || len(parsed.Cmds) != 1 {
		return PlaylistArg{}, false
	}
	cmd := parsed.Cmds[0]
	if cmd.Name != zapscript.ZapScriptCmdPlaylistOpen || len(cmd.Args) != 1 {
		return PlaylistArg{}, false
	}
	var arg PlaylistArg
	if jsonErr := json.Unmarshal([]byte(cmd.Args[0]), &arg); jsonErr != nil {
		return PlaylistArg{}, false
	}
	if !strings.EqualFold(arg.ID, PlaylistID(deckID)) {
		return PlaylistArg{}, false
	}
	return arg, true
}

// StoreFetchedDeck caches a deck served through a ZapLink as a read-only
// copy. Every served entry becomes a script item: a card with several
// scripts is served as a nested playlist command and is kept as that script.
// A deck this device owns is left alone, since the owned copy is the one to
// open. A deck that changed has its membership tags queued, so the tags and
// the files its items link to follow the fetch in the background.
func StoreFetchedDeck(db *database.Database, sourceURL, deckID string, arg *PlaylistArg) error {
	if db == nil || db.UserDB == nil {
		return ErrNoUserDB
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
		return fmt.Errorf("%w: fetched deck %s has %d items", database.ErrDeckItemLimit, deckID, len(items))
	}
	name := strings.TrimSpace(arg.Name)
	if name == "" {
		name = strings.ToUpper(deckID)
	}
	changed, err := db.UserDB.UpsertRemoteDeck(&database.Deck{
		DeckID: deckID, Name: name, Owned: false, SourceURL: sourceURL, Items: items,
	})
	if errors.Is(err, database.ErrDeckOwned) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store fetched deck %s: %w", deckID, err)
	}
	if changed {
		db.QueueDeckTags(deckID)
	}
	return nil
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
			if script, ok := cardPlaylistScript(item); ok {
				out = append(out, PlaylistItem{Name: item.Name, ZapScript: script})
			}
		}
	}
	return out
}

func cardPlaylistScript(item *database.DeckItem) (string, bool) {
	switch len(item.Scripts) {
	case 0:
		return "", false
	case 1:
		return item.Scripts[0].ZapScript, item.Scripts[0].ZapScript != ""
	}
	nested := PlaylistArg{
		ID: PlaylistID(item.CardID), Name: item.Name, Items: make([]PlaylistArgItem, 0, len(item.Scripts)),
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
