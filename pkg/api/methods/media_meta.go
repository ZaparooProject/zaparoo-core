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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// HandleMediaMeta returns the full metadata graph for a single Media record:
// the Media itself, its parent MediaTitle, System, level-separated Tags, and
// level-separated Properties. Binary payloads are not included; use media.image
// to fetch image bytes.
func HandleMediaMeta(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	params, err := parseMediaRequest(env.Params, maxMediaMetaBatchItems)
	if err != nil {
		return nil, err
	}
	if !params.Batch && params.Items[0].MediaID == nil {
		return handleMediaMetaSinglePath(&env, params.Items[0])
	}

	resolved, err := resolveMediaRefs(&env, params.Items)
	if err != nil {
		return nil, err
	}
	if !params.Batch && resolved[0].Err != nil {
		return nil, resolved[0].Err
	}

	mediaIDs, titleIDs := uniqueMediaAndTitleIDs(resolved)
	if params.Batch && len(mediaIDs) == 0 {
		return buildMediaMetaBatchErrorResponse(resolved), nil
	}

	db := env.Database.MediaDB
	mediaTags, err := db.GetMediaTagsByMediaDBIDs(env.Context, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get media tags: %w", err)
	}
	titleTags, err := db.GetMediaTitleTagsByMediaTitleDBIDs(env.Context, titleIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get title tags: %w", err)
	}
	mediaProps, err := db.GetMediaPropertyMetadataByMediaDBIDs(env.Context, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get media property metadata: %w", err)
	}
	titleProps, err := db.GetMediaTitlePropertyMetadataByMediaTitleDBIDs(env.Context, titleIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get title property metadata: %w", err)
	}

	if !params.Batch {
		return buildMediaMetaResponse(
			resolved[0].Row,
			mediaTags[resolved[0].Row.DBID], titleTags[resolved[0].Row.Title.DBID],
			mediaProps[resolved[0].Row.DBID], titleProps[resolved[0].Row.Title.DBID],
		), nil
	}

	items := make([]models.MediaMetaBatchItemResponse, len(resolved))
	for i, item := range resolved {
		if item.Err != nil {
			errText := item.Err.Error()
			items[i].Error = &errText
			continue
		}
		response := buildMediaMetaResponse(
			item.Row,
			mediaTags[item.Row.DBID], titleTags[item.Row.Title.DBID],
			mediaProps[item.Row.DBID], titleProps[item.Row.Title.DBID],
		)
		items[i].Media = &response.Media
	}
	return models.MediaMetaBatchResponse{Items: items}, nil
}

func buildMediaMetaBatchErrorResponse(resolved []resolvedMediaItem) models.MediaMetaBatchResponse {
	items := make([]models.MediaMetaBatchItemResponse, len(resolved))
	for i, item := range resolved {
		if item.Err == nil {
			continue
		}
		errText := item.Err.Error()
		items[i].Error = &errText
	}
	return models.MediaMetaBatchResponse{Items: items}
}

func handleMediaMetaSinglePath(env *requests.RequestEnv, ref mediaRefParam) (any, error) {
	db := env.Database.MediaDB
	row, err := resolveMediaBySystemAndPath(env, ref.System, ref.Path)
	if err != nil {
		return nil, err
	}

	mediaTags, err := db.GetMediaTagsByMediaDBID(env.Context, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media tags: %w", err)
	}
	titleTags, err := db.GetMediaTitleTagsByMediaTitleDBID(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title tags: %w", err)
	}
	mediaProps, err := db.GetMediaPropertyMetadata(env.Context, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media property metadata: %w", err)
	}
	titleProps, err := db.GetMediaTitlePropertyMetadata(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title property metadata: %w", err)
	}

	return buildMediaMetaResponse(row, mediaTags, titleTags, mediaProps, titleProps), nil
}

func buildMediaMetaResponse(
	row *database.MediaFullRow,
	mediaTags []database.TagInfo,
	titleTags []database.TagInfo,
	mediaProps []database.MediaProperty,
	titleProps []database.MediaProperty,
) models.MediaMetaResponse {
	var secondarySlug *string
	if row.Title.SecondarySlug.Valid {
		secondarySlug = &row.Title.SecondarySlug.String
	}

	return models.MediaMetaResponse{Media: models.MediaMetaMediaResponse{
		Path:       row.Path,
		ParentDir:  row.ParentDir,
		IsMissing:  row.IsMissing,
		Tags:       mediaTags,
		Properties: mapMediaProperties(mediaProps),
		Title: models.MediaMetaTitleResponse{
			Slug:          row.Title.Slug,
			SecondarySlug: secondarySlug,
			Name:          row.Title.Name,
			SlugLength:    row.Title.SlugLength,
			SlugWordCount: row.Title.SlugWordCount,
			System: models.MediaMetaSystemResponse{
				ID:   row.System.SystemID,
				Name: row.System.Name,
			},
			Tags:       titleTags,
			Properties: mapMediaProperties(titleProps),
		},
	}}
}

// mapMediaProperties converts a []database.MediaProperty slice into a map keyed
// by TypeTag (e.g. "property:description"). Binary payloads are not included.
func mapMediaProperties(props []database.MediaProperty) map[string]models.MediaMetaPropertyItem {
	m := make(map[string]models.MediaMetaPropertyItem, len(props))
	for _, p := range props {
		item := models.MediaMetaPropertyItem{
			Text:        p.Text,
			ContentType: p.ContentType,
			Extension:   mediaContentExtension(p.ContentType, p.Text),
			BlobSize:    p.BlobSize,
		}
		m[p.TypeTag] = item
	}
	return m
}
