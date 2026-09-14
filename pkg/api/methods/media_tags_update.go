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
	// a flag the model forbids beside a requested one is cleared with it.
	current, _, err := env.Database.UserDB.GetMediaUserData(row.System.SystemID, row.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read media user data: %w", err)
	}
	changes, err := resolveUserFlagChanges(add, remove, &current)
	if err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	for _, flag := range database.MediaUserFlags {
		value, changed := changes[flag]
		if !changed {
			continue
		}
		if udErr := env.Database.UserDB.SetMediaUserFlag(row.System.SystemID, row.Path, flag, value); udErr != nil {
			return nil, fmt.Errorf("failed to set media user %s: %w", flag, udErr)
		}
	}
	snapshotMediaUserIdentity(&env, row.System.SystemID, row.Path)
	effectiveAdd, effectiveRemove := userFlagTagRefs(changes)

	updateStarted := time.Now()
	if updateErr := env.Database.MediaDB.UpdateMediaTags(
		env.Context, row.DBID, effectiveRemove, effectiveAdd,
	); updateErr != nil {
		return nil, fmt.Errorf("failed to update media tag projection: %w", updateErr)
	}
	updateDuration := time.Since(updateStarted)
	if _, changed := changes[database.MediaUserFlagHidden]; changed && env.State != nil {
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

func mutableUserTagList() string {
	names := make([]string, 0, len(tags.MutableUserTags))
	for _, v := range tags.MutableUserTags {
		names = append(names, string(tags.TagTypeUser)+":"+string(v))
	}
	return strings.Join(names, ", ")
}

// resolveUserFlagChanges turns parsed add and remove tag lists into the flag
// changes to record. Adds win over removes. Setting disliked clears liked and
// favorite, and setting liked or favorite clears disliked, so the stored row
// can never hold a forbidden pair; an implied clear is only recorded when the
// current row holds that flag, so the projection write stays minimal. A
// request that asks for both sides of a pair at once is contradictory and
// refused.
func resolveUserFlagChanges(
	add, remove []database.MediaTagRef, current *database.MediaUserData,
) (map[database.MediaUserFlag]bool, error) {
	changes := make(map[database.MediaUserFlag]bool, len(add)+len(remove))
	for _, tag := range remove {
		changes[database.MediaUserFlag(tag.Tag)] = false
	}
	for _, tag := range add {
		changes[database.MediaUserFlag(tag.Tag)] = true
	}
	implyClear := func(flag database.MediaUserFlag) {
		if _, requested := changes[flag]; !requested && current.Flag(flag) {
			changes[flag] = false
		}
	}
	if changes[database.MediaUserFlagDisliked] {
		if changes[database.MediaUserFlagLiked] {
			return nil, errors.New("a game cannot be liked and disliked at once")
		}
		if changes[database.MediaUserFlagFavorite] {
			return nil, errors.New("a favorite cannot be disliked")
		}
		implyClear(database.MediaUserFlagLiked)
		implyClear(database.MediaUserFlagFavorite)
	}
	if changes[database.MediaUserFlagLiked] || changes[database.MediaUserFlagFavorite] {
		implyClear(database.MediaUserFlagDisliked)
	}
	return changes, nil
}

// userFlagTagRefs splits resolved flag changes into the user tags to add and
// remove from the media.db projection, in a stable order.
func userFlagTagRefs(changes map[database.MediaUserFlag]bool) (add, remove []database.MediaTagRef) {
	for _, flag := range database.MediaUserFlags {
		value, changed := changes[flag]
		if !changed {
			continue
		}
		ref := database.MediaTagRef{Type: string(tags.TagTypeUser), Tag: string(flag)}
		if value {
			add = append(add, ref)
		} else {
			remove = append(remove, ref)
		}
	}
	return add, remove
}
