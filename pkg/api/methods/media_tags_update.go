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
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

func HandleMediaTagsUpdate(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	log.Info().Msg("received media tags update request")

	var params models.MediaTagsUpdateParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	if len(params.Add) == 0 && len(params.Remove) == 0 {
		return nil, models.ClientErrf("invalid params: add or remove is required")
	}
	if err := validateMediaRef(mediaRefParam{MediaID: params.MediaID, System: params.System, Path: params.Path}); err != nil {
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

	resolved, err := resolveMediaRefs(&env, []mediaRefParam{{
		MediaID: params.MediaID,
		System:  params.System,
		Path:    params.Path,
	}})
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
	for _, tag := range remove {
		if err := removeMediaUserTag(env.Database.MediaDB, row.DBID, tag); err != nil {
			return nil, err
		}
	}
	for _, tag := range add {
		if err := addMediaUserTag(env.Database.MediaDB, row.DBID, tag); err != nil {
			return nil, err
		}
	}

	fileTags, err := env.Database.MediaDB.GetMediaTagsByMediaDBID(env.Context, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media tags: %w", err)
	}
	titleTags, err := env.Database.MediaDB.GetMediaTitleTagsByMediaTitleDBID(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media title tags: %w", err)
	}

	return models.TagsResponse{Tags: append(fileTags, titleTags...)}, nil
}

func parseMutableUserTags(rawTags []string) ([]zapscript.TagFilter, error) {
	if len(rawTags) == 0 {
		return nil, nil
	}
	for _, raw := range rawTags {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("tag cannot be empty")
		}
		if strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "~") {
			return nil, fmt.Errorf("tag operators are not allowed for mutation: %q", raw)
		}
	}

	parsed, err := filters.ParseTagFilters(rawTags)
	if err != nil {
		return nil, err
	}
	for _, tag := range parsed {
		if tag.Type != string(tags.TagTypeUser) || tag.Value != string(tags.TagUserFavorite) {
			return nil, fmt.Errorf("only %s:%s can be mutated", tags.TagTypeUser, tags.TagUserFavorite)
		}
	}

	return parsed, nil
}

func addMediaUserTag(mediaDB database.MediaDBI, mediaDBID int64, tag zapscript.TagFilter) error {
	tagType, err := mediaDB.FindOrInsertTagType(database.TagType{
		Type:        tag.Type,
		IsExclusive: tags.IsExclusiveType(tags.TagType(tag.Type)),
	})
	if err != nil {
		return fmt.Errorf("failed to find or insert tag type: %w", err)
	}

	tagRow, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: tagType.DBID, Tag: tag.Value})
	if err != nil {
		return fmt.Errorf("failed to find or insert tag: %w", err)
	}

	if _, err = mediaDB.FindOrInsertMediaTag(database.MediaTag{MediaDBID: mediaDBID, TagDBID: tagRow.DBID}); err != nil {
		return fmt.Errorf("failed to find or insert media tag: %w", err)
	}

	return nil
}

func removeMediaUserTag(mediaDB database.MediaDBI, mediaDBID int64, tag zapscript.TagFilter) error {
	tagType, err := mediaDB.FindTagType(database.TagType{Type: tag.Type})
	if err != nil {
		return ignoreMissingTag(err)
	}

	tagRow, err := mediaDB.FindTag(database.Tag{TypeDBID: tagType.DBID, Tag: tag.Value})
	if err != nil {
		return ignoreMissingTag(err)
	}

	if err = mediaDB.DeleteMediaTag(mediaDBID, tagRow.DBID); err != nil {
		return fmt.Errorf("failed to delete media tag: %w", err)
	}

	return nil
}

func ignoreMissingTag(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
