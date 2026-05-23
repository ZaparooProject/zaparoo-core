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
	"context"
	"encoding/base64"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// HandleMediaMeta returns the full metadata graph for a single Media record:
// the Media itself, its parent MediaTitle, System, level-separated Tags, and
// level-separated Properties (with binary data base64-encoded inline).
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

	parentIDs := collectParentTitleIDs(resolved)
	allTitleIDs := appendUniqueIDs(titleIDs, parentIDs)

	mediaTags, err := db.GetMediaTagsByMediaDBIDs(env.Context, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get media tags: %w", err)
	}
	titleTags, err := db.GetMediaTitleTagsByMediaTitleDBIDs(env.Context, allTitleIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get title tags: %w", err)
	}
	mediaProps, err := db.GetMediaPropertiesByMediaDBIDs(env.Context, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get media properties: %w", err)
	}
	titleProps, err := db.GetMediaTitlePropertiesByMediaTitleDBIDs(env.Context, allTitleIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}

	parentTitles, err := fetchParentTitles(env.Context, db, parentIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent titles: %w", err)
	}

	if !params.Batch {
		row := resolved[0].Row
		parentTitle := parentTitles[row.Title.ParentDBID]
		return buildMediaMetaResponse(
			row,
			mediaTags[row.DBID], titleTags[row.Title.DBID],
			mediaProps[row.DBID], titleProps[row.Title.DBID],
			parentTitle, titleTags[row.Title.ParentDBID], titleProps[row.Title.ParentDBID],
		), nil
	}

	items := make([]models.MediaMetaBatchItemResponse, len(resolved))
	for i, item := range resolved {
		if item.Err != nil {
			errText := item.Err.Error()
			items[i].Error = &errText
			continue
		}
		parentTitle := parentTitles[item.Row.Title.ParentDBID]
		response := buildMediaMetaResponse(
			item.Row,
			mediaTags[item.Row.DBID], titleTags[item.Row.Title.DBID],
			mediaProps[item.Row.DBID], titleProps[item.Row.Title.DBID],
			parentTitle, titleTags[item.Row.Title.ParentDBID], titleProps[item.Row.Title.ParentDBID],
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
	mediaProps, err := db.GetMediaProperties(env.Context, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media properties: %w", err)
	}
	titleProps, err := db.GetMediaTitleProperties(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}

	var parentTitle *database.MediaTitle
	var parentTitleTags []database.TagInfo
	var parentTitleProps []database.MediaProperty
	if row.Title.ParentDBID != 0 {
		parentTitle, err = db.FindMediaTitleByDBID(env.Context, row.Title.ParentDBID)
		if err != nil {
			return nil, fmt.Errorf("failed to get parent title: %w", err)
		}
		if parentTitle != nil {
			parentTitleTags, err = db.GetMediaTitleTagsByMediaTitleDBID(env.Context, parentTitle.DBID)
			if err != nil {
				return nil, fmt.Errorf("failed to get parent title tags: %w", err)
			}
			parentTitleProps, err = db.GetMediaTitleProperties(env.Context, parentTitle.DBID)
			if err != nil {
				return nil, fmt.Errorf("failed to get parent title properties: %w", err)
			}
		}
	}

	return buildMediaMetaResponse(row, mediaTags, titleTags, mediaProps, titleProps,
		parentTitle, parentTitleTags, parentTitleProps), nil
}

func buildMediaMetaResponse(
	row *database.MediaFullRow,
	mediaTags []database.TagInfo,
	titleTags []database.TagInfo,
	mediaProps []database.MediaProperty,
	titleProps []database.MediaProperty,
	parentTitle *database.MediaTitle,
	parentTitleTags []database.TagInfo,
	parentTitleProps []database.MediaProperty,
) models.MediaMetaResponse {
	var secondarySlug *string
	if row.Title.SecondarySlug.Valid {
		secondarySlug = &row.Title.SecondarySlug.String
	}

	system := models.MediaMetaSystemResponse{
		ID:   row.System.SystemID,
		Name: row.System.Name,
	}

	immediateTitleResp := models.MediaMetaTitleResponse{
		Slug:          row.Title.Slug,
		SecondarySlug: secondarySlug,
		Name:          row.Title.Name,
		SlugLength:    row.Title.SlugLength,
		SlugWordCount: row.Title.SlugWordCount,
		System:        system,
		Tags:          titleTags,
		Properties:    mapMediaProperties(titleProps),
	}

	media := models.MediaMetaMediaResponse{
		Path:       row.Path,
		ParentDir:  row.ParentDir,
		IsMissing:  row.IsMissing,
		Tags:       mediaTags,
		Properties: mapMediaProperties(mediaProps),
	}

	if parentTitle != nil {
		var parentSecondarySlug *string
		if parentTitle.SecondarySlug.Valid {
			parentSecondarySlug = &parentTitle.SecondarySlug.String
		}
		media.Title = models.MediaMetaTitleResponse{
			Slug:          parentTitle.Slug,
			SecondarySlug: parentSecondarySlug,
			Name:          parentTitle.Name,
			SlugLength:    parentTitle.SlugLength,
			SlugWordCount: parentTitle.SlugWordCount,
			System:        system,
			Tags:          parentTitleTags,
			Properties:    mapMediaProperties(parentTitleProps),
		}
		media.AliasTitle = &immediateTitleResp
	} else {
		media.Title = immediateTitleResp
	}

	return models.MediaMetaResponse{Media: media}
}

func collectParentTitleIDs(resolved []resolvedMediaItem) []int64 {
	seen := make(map[int64]bool)
	var ids []int64
	for _, item := range resolved {
		if item.Row == nil || item.Row.Title.ParentDBID == 0 {
			continue
		}
		if !seen[item.Row.Title.ParentDBID] {
			seen[item.Row.Title.ParentDBID] = true
			ids = append(ids, item.Row.Title.ParentDBID)
		}
	}
	return ids
}

func appendUniqueIDs(a, b []int64) []int64 {
	if len(b) == 0 {
		return a
	}
	seen := make(map[int64]bool, len(a))
	for _, id := range a {
		seen[id] = true
	}
	result := a
	for _, id := range b {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

func fetchParentTitles(
	ctx context.Context, db database.MediaDBI, ids []int64,
) (map[int64]*database.MediaTitle, error) {
	if len(ids) == 0 {
		return map[int64]*database.MediaTitle{}, nil
	}
	return db.GetMediaTitlesByDBIDs(ctx, ids)
}

// mapMediaProperties converts a []database.MediaProperty slice into a map keyed
// by TypeTag (e.g. "property:description"). Binary data is base64-encoded and
// placed in Data; text-only properties have Data = nil.
func mapMediaProperties(props []database.MediaProperty) map[string]models.MediaMetaPropertyItem {
	m := make(map[string]models.MediaMetaPropertyItem, len(props))
	for _, p := range props {
		item := models.MediaMetaPropertyItem{
			Text:        p.Text,
			ContentType: p.ContentType,
			Extension:   mediaContentExtension(p.ContentType, p.Text),
		}
		if len(p.Binary) > 0 {
			encoded := base64.StdEncoding.EncodeToString(p.Binary)
			item.Data = &encoded
		}
		m[p.TypeTag] = item
	}
	return m
}
