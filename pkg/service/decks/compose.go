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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	mediatags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
)

// ErrMediaNotIndexed reports a path the media database does not hold.
var ErrMediaNotIndexed = errors.New("media is not indexed")

// TitleLaunchScript renders the explicit title launch for a game:
// **launch.title:<System>/<Title> (type:value)... The tags are the title's
// disambiguating tags, which always include the ones that make the file a
// distinct game, so the script names this game on any device. The argument
// is written through the ZapScript serializer, so a title holding a
// character the parser treats specially (a question mark, a comma between
// grouped tag values, a pipe) is quoted and survives as one argument.
func TitleLaunchScript(systemID, name string, tags []database.TagInfo) string {
	return zapscript.Command{
		Name: zapscript.ZapScriptCmdLaunchTitle,
		Args: []string{strings.TrimPrefix(database.BuildTitleZapScript(systemID, name, tags), "@")},
	}.String()
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
	tags = withGameVariantTags(tags, identity.Tags)
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

// withGameVariantTags adds to a file's title tags every tag of a type the file
// carries a game-variant tag of, when the type is not already there. Once the
// media database is current those types are always stored for the title, so
// this changes nothing. After an upgrade that changed the rule they can be
// missing until the one-time recompute has run, which is paused while a game
// is running, and a deck keeps the script it is given.
func withGameVariantTags(tags []database.TagInfo, fileTags []database.MediaIdentityTag) []database.TagInfo {
	present := make(map[string]struct{}, len(tags))
	for i := range tags {
		present[tags[i].Type] = struct{}{}
	}
	missing := make(map[string]struct{})
	for i := range fileTags {
		if _, ok := present[fileTags[i].Type]; ok {
			continue
		}
		if mediatags.IsGameVariantTag(fileTags[i].Type, fileTags[i].Value) {
			missing[fileTags[i].Type] = struct{}{}
		}
	}
	if len(missing) == 0 {
		return tags
	}
	out := slices.Clone(tags)
	for i := range fileTags {
		if _, ok := missing[fileTags[i].Type]; ok {
			out = append(out, database.TagInfo{
				Type: fileTags[i].Type, Tag: mediatags.UnpadTagValue(fileTags[i].Value),
			})
		}
	}
	// The same order the media database returns title tags in.
	slices.SortStableFunc(out, func(a, b database.TagInfo) int {
		if rank := cmp.Compare(database.TagTypeDisplayRank(a.Type), database.TagTypeDisplayRank(b.Type)); rank != 0 {
			return rank
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	return out
}
