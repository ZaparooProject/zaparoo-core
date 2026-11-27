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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/rs/zerolog/log"
)

// ErrNotEditable reports an add or remove on a collection that cannot hold
// media the user puts there, such as a computed list or a launcher's.
var ErrNotEditable = fmt.Errorf("%w: collection cannot be edited", ErrInvalidArguments)

// cmdAdd puts the running media into a collection: a user flag list such as
// favorites://, or a deck made on this device.
//
//nolint:gocritic // single-use parameter in command handler
func cmdAdd(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	return editCollection(&env, true)
}

// cmdRemove takes the running media out of a collection.
//
//nolint:gocritic // single-use parameter in command handler
func cmdRemove(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	return editCollection(&env, false)
}

// editCollection adds the running media to the collection the command names,
// or removes it. Media already in the collection, or never in it, is left as
// it is and the command still succeeds.
//
//nolint:gocritic // env is passed by pointer from the commands that hold it
func editCollection(env *platforms.CmdEnv, add bool) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}
	link := env.Cmd.Args[0]
	if link == "" {
		return platforms.CmdResult{}, ErrRequiredArgs
	}

	flag, isFlag := flagForLink(link)
	deckRef, isDeck := decks.ParseDeckURI(link)
	if !isFlag && !isDeck {
		return platforms.CmdResult{}, fmt.Errorf("%w: %s", ErrNotEditable, link)
	}
	library, err := mediaLibrary(env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	ref, err := activeMediaRef(env)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	ctx, cancel := mediaDBLookupContextWithTimeout(env, libraryListTimeout)
	defer cancel()

	if isFlag {
		if flagErr := library.SetUserFlag(ctx, ref, string(flag), add); flagErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("failed to update %s: %w", link, flagErr)
		}
		log.Info().Str("collection", link).Bool("add", add).Str("path", ref.Path).Msg("collection updated")
		return platforms.CmdResult{}, nil
	}

	deck, err := resolveDeck(env, deckRef)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	changed, err := library.EditDeck(ctx, deck.DeckID, ref, add)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to update deck %s: %w", deck.Name, err)
	}
	log.Info().Str("deck", deck.DeckID).Bool("add", add).Bool("changed", changed).
		Str("path", ref.Path).Msg("deck updated")
	return platforms.CmdResult{}, nil
}
