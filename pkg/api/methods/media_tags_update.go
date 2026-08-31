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

	// Write the durable truth (UserDB) before the MediaDB projection. Since
	// user:favorite is the only mutable tag, any add wins over a simultaneous
	// remove. A failed projection remains repairable by the next reindex.
	favorite := len(add) > 0
	if udErr := setMediaUserFavorite(&env, row.System.SystemID, row.Path, favorite); udErr != nil {
		return nil, udErr
	}

	updateStarted := time.Now()
	if updateErr := env.Database.MediaDB.UpdateMediaTags(
		env.Context, row.DBID, remove, add,
	); updateErr != nil {
		return nil, fmt.Errorf("failed to update media tag projection: %w", updateErr)
	}
	updateDuration := time.Since(updateStarted)

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
		if tag.Type != string(tags.TagTypeUser) || tag.Value != string(tags.TagUserFavorite) {
			return nil, fmt.Errorf("only %s:%s can be mutated", tags.TagTypeUser, tags.TagUserFavorite)
		}
		refs = append(refs, database.MediaTagRef{Type: tag.Type, Tag: tag.Value})
	}

	return refs, nil
}
