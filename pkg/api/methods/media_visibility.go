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
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// Counts embedded in a cursor are valid only for the same visibility mode and
// durable preference revision. Legacy cursors remain usable before any edits.
//
// This reads only MediaDB's own projection revision, not UserDB's. Browse and
// search never read UserDB preference rows directly, only what MediaDB has
// projected from them, so MediaDB's revision is the precise signal for "did
// the served result set change": it moves exactly when the tag/deck
// projection a browse or search actually reads has changed, and does not
// move on a UserDB write that left the projection alone (a repeated "unhide"
// of an already-visible entry, a deck edit that changed nothing, a synced
// deck write-back that matches what was already stored). UserDB keeps
// advancing its own DeviceState row with this same key for its own reasons
// (see deckTx), but nothing here reads that value anymore.
func browsePreferencesRevision(env *requests.RequestEnv) (string, error) {
	projection, err := env.Database.MediaDB.MediaPreferencesRevision(env.Context)
	if err != nil {
		return "", fmt.Errorf("read media preferences projection: %w", err)
	}
	return projection, nil
}

func validateBrowseVisibility(env *requests.RequestEnv, cursor *string) (string, error) {
	revision, err := browsePreferencesRevision(env)
	if err != nil {
		return "", err
	}
	if cursor == nil || *cursor == "" {
		return revision, nil
	}
	data, err := readBrowseCursorData(*cursor)
	if err != nil {
		return "", models.ClientErrf("invalid cursor: %w", err)
	}
	if data.PreferencesRevision != revision ||
		(data.IncludeHidden != nil && *data.IncludeHidden == env.ExcludeHidden) {
		return "", models.ClientErrf("library visibility changed; restart browse without cursor")
	}
	return revision, nil
}

func readBrowseCursorData(cursor string) (browseCursorData, error) {
	var data browseCursorData
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return data, fmt.Errorf("decode browse cursor: %w", err)
	}
	if err := json.Unmarshal(decoded, &data); err != nil {
		return data, fmt.Errorf("parse browse cursor: %w", err)
	}
	return data, nil
}

func stampVisibilityCursor(cursor, revision string, includeHidden bool) (string, error) {
	if cursor == "" {
		return "", nil // First index bucket starts a fresh browse, with no cached totals.
	}
	data, err := readBrowseCursorData(cursor)
	if err != nil {
		return "", err
	}
	data.IncludeHidden = &includeHidden
	data.PreferencesRevision = revision
	return encodeCursorData(&data)
}

// searchVisibility is the visibility mode and preference revision one search
// request runs under, carried into every cursor it hands back.
type searchVisibility struct {
	Revision      string
	IncludeHidden bool
}

// Search cursors carry the same stamp browse cursors do: a page taken under a
// different visibility mode, or after a preference edit, would skip or repeat
// rows. Cursors minted before any edit carry no revision and stay usable.
func validateSearchVisibility(
	env *requests.RequestEnv, cursor string, excludeHidden bool,
) (searchVisibility, error) {
	visibility := searchVisibility{IncludeHidden: !excludeHidden}
	revision, err := browsePreferencesRevision(env)
	if err != nil {
		return visibility, err
	}
	visibility.Revision = revision
	if cursor == "" {
		return visibility, nil
	}
	data, err := decodeCursorData(cursor)
	if err != nil {
		return visibility, err
	}
	if data == nil {
		return visibility, nil
	}
	if data.PreferencesRevision != revision ||
		(data.IncludeHidden != nil && *data.IncludeHidden == excludeHidden) {
		return visibility, models.ClientErrf("library visibility changed; restart search without cursor")
	}
	return visibility, nil
}

func mediaTagsHidden(mediaTags []database.TagInfo) bool {
	for _, tag := range mediaTags {
		if tag.Type == string(tags.TagTypeUser) && tag.Tag == string(tags.TagUserHidden) {
			return true
		}
	}
	return false
}

// stampBrowseVisibility stamps a finished browse result's cursors with the
// revision the browse ran under. It does no revision comparison of its own;
// runBrowseVisibility only calls it once it has confirmed the revision held
// for the whole run.
func stampBrowseVisibility(result any, revision string, includeHidden bool) (any, error) {
	switch response := result.(type) {
	case models.BrowseResults:
		if response.Pagination != nil && response.Pagination.NextCursor != nil {
			cursor, err := stampVisibilityCursor(*response.Pagination.NextCursor, revision, includeHidden)
			if err != nil {
				return nil, err
			}
			response.Pagination.NextCursor = &cursor
		}
		return response, nil
	case models.BrowseIndexResults:
		for i := range response.Groups {
			cursor, err := stampVisibilityCursor(response.Groups[i].Cursor, revision, includeHidden)
			if err != nil {
				return nil, err
			}
			response.Groups[i].Cursor = cursor
		}
		return response, nil
	default:
		return result, nil
	}
}

// runBrowseVisibility runs one browse or browse-index call under the
// preferences revision that was current when it started, and stamps the
// result with that revision on the way out.
//
// A cursor request is a continuation of a specific earlier page: if the
// revision moved underneath it, that continuation is no longer valid and the
// client is told to restart. A fresh request (cursor nil or empty) has
// nothing to continue, so telling the client to "restart without a cursor"
// would just have it redo what it already did. Instead, when the revision
// moved during a cursorless run, Core reruns the browse itself once under
// the new revision. The result of a run whose revision changed underneath it
// is never returned: a page can be built from a mix of old and new
// visibility partway through, so a changed-again result is discarded rather
// than stamped and handed back.
func runBrowseVisibility(
	env *requests.RequestEnv, cursor *string, browse func() (any, error),
) (any, error) {
	hasCursor := cursor != nil && *cursor != ""
	revision, err := validateBrowseVisibility(env, cursor)
	if err != nil {
		return nil, err
	}

	result, err := browse()
	if err != nil {
		return nil, err
	}
	current, err := browsePreferencesRevision(env)
	if err != nil {
		return nil, err
	}
	if current == revision {
		return stampBrowseVisibility(result, revision, !env.ExcludeHidden)
	}
	if hasCursor {
		return nil, models.ClientErrf("library visibility changed; restart browse without cursor")
	}

	log.Debug().Str("from", revision).Str("to", current).
		Msg("preferences revision changed during a cursorless browse; rerunning once")
	revision = current
	result, err = browse()
	if err != nil {
		return nil, err
	}
	current, err = browsePreferencesRevision(env)
	if err != nil {
		return nil, err
	}
	if current != revision {
		return nil, models.ClientErrf("library visibility changed during browse; try again")
	}
	return stampBrowseVisibility(result, revision, !env.ExcludeHidden)
}
