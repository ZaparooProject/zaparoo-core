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
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
)

// The links of the media library's own lists. A user flag link lists the
// media the user gave that flag; the others are computed afresh each time.
const (
	SchemeFavorites   = "favorites"
	SchemeLiked       = "liked"
	SchemeDisliked    = "disliked"
	SchemePlayLater   = "playlater"
	SchemeHidden      = "hidden"
	SchemeRecommended = "recommended"
	SchemeSimilar     = "similar"
	SchemeVersions    = "versions"
	SchemeTop         = "top"
)

// The media a similar:// or versions:// link is about, besides a title or a
// path.
const (
	refActive = "active"
	refLast   = "last"
)

// libraryListTimeout bounds reading one library list for a command.
const libraryListTimeout = 10 * time.Second

// recommendedSections are the sections a recommended:// link may name.
var recommendedSections = map[string]bool{
	"seed": true, "series": true, "playlater": true, platforms.RecommendedQueue: true,
}

// flagSchemes maps each user flag link to the user flag it lists.
var flagSchemes = map[string]tags.TagValue{
	SchemeFavorites: tags.TagUserFavorite,
	SchemeLiked:     tags.TagUserLiked,
	SchemeDisliked:  tags.TagUserDisliked,
	SchemePlayLater: tags.TagUserPlayLater,
	SchemeHidden:    tags.TagUserHidden,
}

// ErrNoMediaLibrary reports a library link run where no media library is
// available to list it.
var ErrNoMediaLibrary = errors.New("media library is not available")

// parseLibraryLink splits a library link into its scheme, lowercased, and
// what follows it. ok is false for any other argument.
func parseLibraryLink(arg string) (scheme, rest string, ok bool) {
	raw, rest, found := strings.Cut(strings.TrimSpace(arg), "://")
	if !found {
		return "", "", false
	}
	scheme = strings.ToLower(raw)
	switch scheme {
	case SchemeRecommended, SchemeSimilar, SchemeVersions, SchemeTop:
		return scheme, strings.TrimSpace(rest), true
	}
	if _, isFlag := flagSchemes[scheme]; isFlag {
		return scheme, strings.TrimSpace(rest), true
	}
	return "", "", false
}

// flagForLink returns the user flag a user flag link lists.
func flagForLink(arg string) (tags.TagValue, bool) {
	scheme, rest, ok := parseLibraryLink(arg)
	if !ok || rest != "" {
		return "", false
	}
	flag, isFlag := flagSchemes[scheme]
	return flag, isFlag
}

// resolveLibraryCollection lists a library link. handled is false when arg
// is not one.
//
//nolint:gocritic // env is passed by pointer from the callers that hold it
func resolveLibraryCollection(
	pl platforms.Platform, env *platforms.CmdEnv, arg string,
) (coll *collection, handled bool, err error) {
	scheme, rest, ok := parseLibraryLink(arg)
	if !ok {
		return nil, false, nil
	}
	ctx, cancel := mediaDBLookupContextWithTimeout(env, libraryListTimeout)
	defer cancel()

	coll = &collection{unsafe: env.Unsafe, media: true, library: true}
	var items []platforms.LibraryItem
	switch scheme {
	case SchemeRecommended:
		section := strings.ToLower(rest)
		if section != "" && !recommendedSections[section] {
			return nil, true, fmt.Errorf("%w: unknown recommended section: %s", ErrInvalidArguments, rest)
		}
		coll.id, coll.name = SchemeRecommended+"://"+section, "Recommended"
		library, libErr := mediaLibrary(env)
		if libErr != nil {
			return nil, true, libErr
		}
		items, err = library.Recommended(ctx, section)
	case SchemeSimilar, SchemeVersions:
		library, libErr := mediaLibrary(env)
		if libErr != nil {
			return nil, true, libErr
		}
		ref, refErr := resolveMediaRef(ctx, pl, env, rest)
		if refErr != nil {
			return nil, true, refErr
		}
		coll.id = scheme + "://" + mediaRefKey(ref)
		if scheme == SchemeSimilar {
			coll.name = "Similar"
			items, err = library.Similar(ctx, ref)
		} else {
			coll.name = "Versions"
			items, err = library.Versions(ctx, ref)
		}
	case SchemeTop:
		library, libErr := mediaLibrary(env)
		if libErr != nil {
			return nil, true, libErr
		}
		systemID, sysErr := topSystem(rest)
		if sysErr != nil {
			return nil, true, sysErr
		}
		coll.id, coll.name = SchemeTop+"://"+systemID, "Most Played"
		items, err = library.MostPlayed(ctx, systemID)
	default:
		if rest != "" {
			return nil, true, fmt.Errorf("%w: %s:// takes nothing after it", ErrInvalidArguments, scheme)
		}
		library, libErr := mediaLibrary(env)
		if libErr != nil {
			return nil, true, libErr
		}
		coll.id, coll.name = scheme+"://", flagListName(scheme)
		items, err = library.Flagged(ctx, string(flagSchemes[scheme]))
	}
	if err != nil {
		return nil, true, fmt.Errorf("failed to list %s: %w", arg, err)
	}
	// The flag lists are the user's own and keep their place; the computed
	// ones change from one read to the next.
	_, isFlag := flagSchemes[scheme]
	coll.ephemeral = !isFlag
	for i := range items {
		if env.SystemPermitted != nil && !env.SystemPermitted(items[i].SystemID) {
			continue
		}
		coll.items = append(coll.items, playlists.PlaylistItem{Name: items[i].Name, ZapScript: items[i].Path})
		coll.rows = append(coll.rows, collectionRow{systemID: items[i].SystemID, mediaID: items[i].MediaID})
	}
	return coll, true, nil
}

func flagListName(scheme string) string {
	switch scheme {
	case SchemeFavorites:
		return "Favorites"
	case SchemeLiked:
		return "Liked"
	case SchemeDisliked:
		return "Disliked"
	case SchemePlayLater:
		return "Play Later"
	default:
		return "Hidden"
	}
}

//nolint:gocritic // env is passed by pointer from the callers that hold it
func mediaLibrary(env *platforms.CmdEnv) (platforms.MediaLibrary, error) {
	if env.Library == nil {
		return nil, ErrNoMediaLibrary
	}
	return env.Library, nil
}

// topSystem reads the system a top:// link names, if any.
func topSystem(rest string) (string, error) {
	if rest == "" {
		return "", nil
	}
	system, err := systemdefs.LookupSystem(rest)
	if err != nil {
		return "", fmt.Errorf("%w: unknown system: %s", ErrInvalidArguments, rest)
	}
	return system.ID, nil
}

// mediaRefKey names the media a computed list is about, for its playlist ID.
// It is the system and path, which every way of naming the media has, so a
// list about one game has one ID however the link named it.
func mediaRefKey(ref platforms.MediaRef) string {
	return ref.SystemID + "/" + pathutil.CanonicalMediaPath(ref.Path)
}

// resolveMediaRef finds the media a similar:// or versions:// link is about:
// the running media when empty or "active", the last played with "last", an
// indexed path, or else a SystemID/GameName title resolved the way
// launch.title resolves it.
//
//nolint:gocritic // env is passed by pointer from the callers that hold it
func resolveMediaRef(
	ctx context.Context, pl platforms.Platform, env *platforms.CmdEnv, raw string,
) (platforms.MediaRef, error) {
	switch strings.ToLower(raw) {
	case "", refActive:
		return activeMediaRef(env)
	case refLast:
		if env.Database == nil || env.Database.UserDB == nil {
			return platforms.MediaRef{}, ErrNoHistory
		}
		entry, err := getUniqueRecentMedia(env.Database.UserDB, 1)
		if err != nil {
			return platforms.MediaRef{}, err
		}
		return platforms.MediaRef{
			SystemID: entry.SystemID,
			Path:     pathutil.CanonicalMediaPath(entry.MediaPath),
		}, nil
	}
	if env.Database != nil && env.Database.MediaDB != nil {
		if row, found := findUniqueMediaByPath(ctx, env, pathutil.CanonicalMediaPath(raw)); found {
			return platforms.MediaRef{SystemID: row.SystemID, Path: row.Path, MediaID: row.DBID}, nil
		}
	}
	if valid, _, _ := isValidTitleFormat(raw); !valid {
		return platforms.MediaRef{}, fmt.Errorf(
			"%w: not an indexed path or a SystemID/GameName title: %s", ErrInvalidArguments, raw)
	}
	titleEnv := *env
	titleEnv.Cmd = zapscript.Command{Name: zapscript.ZapScriptCmdLaunchTitle, Args: []string{raw}}
	result, err := ResolveLaunchTitle(pl, &titleEnv)
	if err != nil {
		return platforms.MediaRef{}, err
	}
	return platforms.MediaRef{
		SystemID: result.Result.SystemID,
		Path:     result.Result.Path,
		MediaID:  result.Result.MediaID,
	}, nil
}

// activeMediaRef names the running media from its full active-media record,
// never looking it up again by name.
//
//nolint:gocritic // env is passed by pointer from the callers that hold it
func activeMediaRef(env *platforms.CmdEnv) (platforms.MediaRef, error) {
	if env.ActiveMedia == nil {
		return platforms.MediaRef{}, ErrNoActiveMedia
	}
	active := env.ActiveMedia()
	if active == nil || active.Path == "" || active.SystemID == "" {
		return platforms.MediaRef{}, ErrNoActiveMedia
	}
	return platforms.MediaRef{
		SystemID: active.SystemID,
		Path:     pathutil.CanonicalMediaPath(active.Path),
		MediaID:  active.MediaID,
	}, nil
}

// filterCollectionByTags keeps the items of a library list whose media
// matches filters, read the way search and browse filter media.
//
//nolint:gocritic // env is passed by pointer from the callers that hold it
func filterCollectionByTags(env *platforms.CmdEnv, coll *collection, filters []zapscript.TagFilter) error {
	if len(coll.items) == 0 || env.Database == nil || env.Database.MediaDB == nil {
		return nil
	}
	ids := make([]int64, 0, len(coll.rows))
	for i := range coll.rows {
		if coll.rows[i].mediaID > 0 {
			ids = append(ids, coll.rows[i].mediaID)
		}
	}
	ctx, cancel := randomMediaDBLookupContext(env)
	defer cancel()
	matched, err := env.Database.MediaDB.MatchMediaTagFilters(ctx, ids, filters)
	if err != nil {
		return fmt.Errorf("failed to filter %s by tags: %w", coll.name, err)
	}
	items := coll.items[:0:0]
	rows := coll.rows[:0:0]
	for i := range coll.rows {
		if _, ok := matched[coll.rows[i].mediaID]; ok {
			items = append(items, coll.items[i])
			rows = append(rows, coll.rows[i])
		}
	}
	coll.items, coll.rows = items, rows
	return nil
}
