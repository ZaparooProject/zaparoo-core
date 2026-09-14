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

// Package decks holds the local deck feature: composing deck items from
// indexed media and, later, projecting deck membership into the media
// database and opening decks as playlists. It must not import pkg/zapscript.
package decks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
)

// ErrMediaNotIndexed reports a path the media database does not hold.
var ErrMediaNotIndexed = errors.New("media is not indexed")

// TitleLaunchScript renders the explicit title launch for a game:
// **launch.title:<System>/<Title> (type:value)... The tags are the title's
// disambiguating tags, which always include the ones that make the file a
// distinct game, so the script names this game on any device.
func TitleLaunchScript(systemID, name string, tags []database.TagInfo) string {
	return "**" + zapscript.ZapScriptCmdLaunchTitle + ":" +
		strings.TrimPrefix(database.BuildTitleZapScript(systemID, name, tags), "@")
}

// ComposeMediaItem builds the deck item for an indexed file: a script item
// carrying the title launch other devices resolve through their own index,
// and the anchor to the exact file so this device launches what was picked
// and the row can be re-linked after the media database is rebuilt.
func ComposeMediaItem(
	ctx context.Context, mediaDB database.MediaDBI, systemID, path string,
) (database.DeckItem, error) {
	identity, found, err := database.LookupMediaIdentity(ctx, mediaDB, systemID, path)
	if err != nil {
		return database.DeckItem{}, fmt.Errorf("resolve media identity: %w", err)
	}
	if !found {
		return database.DeckItem{}, fmt.Errorf("%w: %s/%s", ErrMediaNotIndexed, systemID, path)
	}
	tags, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, identity.CanonicalSystemID, path)
	if err != nil {
		return database.DeckItem{}, fmt.Errorf("resolve title tags: %w", err)
	}
	return database.DeckItem{
		Kind:      database.DeckItemKindScript,
		Name:      identity.DisplayName,
		ZapScript: TitleLaunchScript(identity.CanonicalSystemID, identity.DisplayName, tags),
		Anchor: database.DeckItemAnchor{
			SystemID:  identity.CanonicalSystemID,
			Path:      pathutil.CanonicalMediaPath(path),
			MediaName: identity.DisplayName,
			Tags:      identity.LegacyTags(),
		},
	}, nil
}
