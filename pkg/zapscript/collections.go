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
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/google/uuid"
)

// collection is a link that stands for a group of items: a deck on this
// device, or a collection a launcher can list, such as a Kodi TV show or
// album.
type collection struct {
	// id and name identify the playlist the collection makes.
	id   string
	name string
	// deckID is set when the collection is a deck on this device.
	deckID string
	items  []playlists.PlaylistItem
	// unsafe means the items must run without the rights of the device owner.
	unsafe bool
	// media means every item is a plain media path that launches directly,
	// rather than ZapScript of its own.
	media bool
}

// resolveCollection expands arg into its items. It reports false when arg is
// not a collection, so the caller carries on with its ordinary handling.
//
//nolint:gocritic // env is passed by pointer from the callers that hold it
func resolveCollection(
	pl platforms.Platform, env *platforms.CmdEnv, arg string,
) (*collection, bool, error) {
	if deckID, isDeck := decks.ParseDeckURI(arg); isDeck {
		coll, err := loadDeckItems(pl, env, deckID)
		return coll, true, err
	}
	if !strings.Contains(arg, "://") {
		return nil, false, nil
	}
	// A launcher that is not running yet still owns its links; asking it for
	// the items is what reports why it cannot answer.
	launcher, found := inferLauncherForPathWithAvailability(pl, env, arg, false)
	if !found || launcher.Collection == nil {
		return nil, false, nil
	}

	ctx, cancel := randomMediaDBLookupContext(env)
	defer cancel()
	listed, err := launcher.Collection(ctx, env.Cfg, arg)
	if err != nil {
		return nil, true, fmt.Errorf("failed to list collection '%s': %w", arg, err)
	}
	items := make([]playlists.PlaylistItem, 0, len(listed))
	for _, item := range listed {
		items = append(items, playlists.PlaylistItem{Name: item.Name, ZapScript: item.Path})
	}

	name := arg
	if parsed, parseErr := virtualpath.ParseVirtualPathStr(arg); parseErr == nil && parsed.Name != "" {
		name = parsed.Name
	}
	return &collection{
		id:     arg,
		name:   name,
		items:  items,
		unsafe: env.Unsafe,
		media:  true,
	}, true, nil
}

// isDeckRandom reports whether cmd is a launch.random on a deck. A deck's
// items are ZapScript, so the roll queues a playlist of the item it picked
// instead of launching it.
func isDeckRandom(cmd *zapscript.Command) bool {
	if cmd.Name != zapscript.ZapScriptCmdLaunchRandom || len(cmd.Args) == 0 {
		return false
	}
	_, isDeck := decks.ParseDeckURI(cmd.Args[0])
	return isDeck
}

// randomFromCollection launches one random item of a collection.
//
// Media items launch directly. A deck's items are ZapScript that runs with
// the trust of the deck, which only a playlist carries, so the item is queued
// as a playlist of one the way a rotating launch.random queues its roll.
// The playlist has no deck ID: a refresh of the deck would otherwise replace
// the one item with the whole deck.
//
//nolint:gocritic // env is passed by pointer from the caller that holds it
func randomFromCollection(
	env *platforms.CmdEnv, coll *collection, launch func(launchTarget) error,
) (platforms.CmdResult, error) {
	if len(coll.items) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no items in collection: %s", coll.name)
	}

	if !coll.media {
		item, err := helpers.RandomElem(coll.items)
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to select random item: %w", err)
		}
		slot, err := mediaslot.Normalize(env.Cmd.AdvArgs.Get(zapscript.KeySlot))
		if err != nil {
			return platforms.CmdResult{}, fmt.Errorf("normalize media slot: %w", err)
		}
		pls := playlists.NewPlaylist(uuid.NewString(), coll.name, []playlists.PlaylistItem{item})
		pls.Slot = slot
		// Each roll has a fresh ID, so remembering it would only push out
		// playlists the user does come back to.
		pls.Ephemeral = true
		pls.Unsafe = coll.unsafe
		pls.AllowedCommands = env.AllowedCommands
		// A new roll every time the command runs, even when the playlist it
		// replaces would open on the same item.
		pls = playlists.Play(*pls)
		pls.ForceRelaunch = true
		if err := queuePlaylistUpdate(env, pls); err != nil {
			return platforms.CmdResult{}, err
		}
		return platforms.CmdResult{PlaylistChanged: true, Playlist: pls}, nil
	}

	selectItem := func() (database.SearchResult, error) {
		item, err := helpers.RandomElem(coll.items)
		if err != nil {
			return database.SearchResult{}, fmt.Errorf("failed to select random item: %w", err)
		}
		return database.SearchResult{Name: item.Name, Path: item.ZapScript}, nil
	}
	first, err := selectItem()
	if err != nil {
		return platforms.CmdResult{}, err
	}
	picked, err := launchRandomGameWithRetry(first, selectItem, func(candidate database.SearchResult) error {
		return launch(launchTarget{path: candidate.Path})
	})
	if err != nil {
		return platforms.CmdResult{MediaChanged: true},
			fmt.Errorf("failed to launch '%s': %w", picked.Path, err)
	}
	return platforms.CmdResult{MediaChanged: true}, nil
}
