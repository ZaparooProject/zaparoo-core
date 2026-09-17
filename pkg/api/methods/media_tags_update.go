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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// HandleMediaTagsUpdate adds or removes the mutable user tags on one indexed
// file, recording them in UserDB before updating the MediaDB tags, and returns
// the file's tags afterwards.
func HandleMediaTagsUpdate(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	started := time.Now()
	log.Info().Msg("received media tags update request")

	var params models.MediaTagsUpdateParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	if len(params.Add) == 0 && len(params.Remove) == 0 {
		return nil, models.ClientErrf("invalid params: add or remove is required")
	}
	mediaRef := mediaRefParam{
		MediaID: params.MediaID,
		System:  params.System,
		Path:    params.Path,
	}
	if err := validateMediaRef(mediaRef); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	add, err := parseMutableUserTags(params.Add)
	if err != nil {
		return nil, models.ClientErrf("invalid add tags: %w", err)
	}
	remove, err := parseMutableUserTags(params.Remove)
	if err != nil {
		return nil, models.ClientErrf("invalid remove tags: %w", err)
	}

	resolveStarted := time.Now()
	resolved, err := resolveMediaRefs(&env, []mediaRefParam{mediaRef})
	if err != nil {
		return nil, err
	}
	if len(resolved) != 1 || resolved[0].Err != nil || resolved[0].Row == nil {
		if len(resolved) == 1 && resolved[0].Err != nil {
			return nil, resolved[0].Err
		}
		return nil, models.ClientErrf("media not found")
	}

	row := resolved[0].Row
	resolveDuration := time.Since(resolveStarted)

	// Record changed preferences before their disposable projection. Adds win
	// over removes, matching UpdateMediaTags; unrelated flags stay intact, and
	// UserDB clears a flag the model forbids beside a requested one.
	changes, err := requestedUserFlagChanges(add, remove)
	if err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	updateStarted := time.Now()
	projected, applyErr := database.ApplyMediaUserFlags(
		env.Context, env.Database, row.System.SystemID, row.Path, row.DBID, changes,
	)
	// The snapshot never inserts, so it is safe even when the write failed,
	// and it keeps the identity of a recorded flag when only the projection did.
	snapshotMediaUserIdentity(&env, row.System.SystemID, row.Path)
	if applyErr != nil {
		return nil, fmt.Errorf("failed to apply media user flags: %w", applyErr)
	}
	updateDuration := time.Since(updateStarted)
	_, hiddenRequested := changes[database.MediaUserFlagHidden]
	_, hiddenProjected := projected[database.MediaUserFlagHidden]
	if (hiddenRequested || hiddenProjected) && env.State != nil {
		notifications.MediaVisibility(env.State.Notifications)
	}

	fetchStarted := time.Now()
	fileTags, err := env.Database.MediaDB.GetMediaTagsByMediaDBID(env.Context, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media tags: %w", err)
	}
	titleTags, err := env.Database.MediaDB.GetMediaTitleTagsByMediaTitleDBID(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media title tags: %w", err)
	}
	fetchDuration := time.Since(fetchStarted)

	log.Debug().
		Int64("mediaDBID", row.DBID).
		Dur("resolveDuration", resolveDuration).
		Dur("updateDuration", updateDuration).
		Dur("fetchDuration", fetchDuration).
		Dur("totalDuration", time.Since(started)).
		Msg("media tags update timing")

	return models.TagsResponse{Tags: append(fileTags, titleTags...)}, nil
}

// parseMutableUserTags parses a request's tag list, refusing search operators,
// deck membership and any tag a client may not set.
func parseMutableUserTags(rawTags []string) ([]database.MediaTagRef, error) {
	if len(rawTags) == 0 {
		return nil, nil
	}
	for _, raw := range rawTags {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, errors.New("tag cannot be empty")
		}
		if strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "~") {
			return nil, fmt.Errorf("tag operators are not allowed for mutation: %q", raw)
		}
	}

	parsed, err := filters.ParseTagFilters(rawTags)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tag filters: %w", err)
	}
	refs := make([]database.MediaTagRef, 0, len(parsed))
	for _, tag := range parsed {
		if tag.Type == string(tags.TagTypeUser) {
			if _, isDeck := tags.ParseDeckTag(tags.TagValue(tag.Value)); isDeck {
				return nil, errors.New("deck membership is managed through the decks methods")
			}
		}
		if tag.Type != string(tags.TagTypeUser) || !tags.IsMutableUserTag(tags.TagValue(tag.Value)) {
			return nil, fmt.Errorf("only these user tags can be mutated: %s", mutableUserTagList())
		}
		refs = append(refs, database.MediaTagRef{Type: tag.Type, Tag: tag.Value})
	}

	return refs, nil
}

// mutableUserTagList names the settable user tags for error messages.
func mutableUserTagList() string {
	names := make([]string, 0, len(tags.MutableUserTags))
	for _, v := range tags.MutableUserTags {
		names = append(names, string(tags.TagTypeUser)+":"+string(v))
	}
	return strings.Join(names, ", ")
}

// requestedUserFlagChanges turns parsed add and remove tag lists into the
// flag changes to record. Adds win over removes. A request that adds both
// sides of a forbidden pair is contradictory and refused; adding one side
// alone is how a client switches a reaction, and UserDB clears the other.
func requestedUserFlagChanges(add, remove []database.MediaTagRef) (map[database.MediaUserFlag]bool, error) {
	changes := make(map[database.MediaUserFlag]bool, len(add)+len(remove))
	for _, tag := range remove {
		changes[database.MediaUserFlag(tag.Tag)] = false
	}
	for _, tag := range add {
		changes[database.MediaUserFlag(tag.Tag)] = true
	}
	if changes[database.MediaUserFlagDisliked] {
		if changes[database.MediaUserFlagLiked] {
			return nil, errors.New("a game cannot be liked and disliked at once")
		}
		if changes[database.MediaUserFlagFavorite] {
			return nil, errors.New("a favorite cannot be disliked")
		}
	}
	return changes, nil
}
