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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// HandleMediaMeta returns the full metadata graph for a single Media record:
// the Media itself, its parent MediaTitle, System, level-separated Tags, and
// level-separated Properties (with binary data base64-encoded inline).
func HandleMediaMeta(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	var params models.MediaMetaParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	ctx := env.Context
	db := env.Database.MediaDB

	row, err := db.GetMediaWithTitleAndSystem(ctx, params.MediaID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media: %w", err)
	}
	if row == nil {
		return nil, models.ClientErrf("media not found: %d", params.MediaID)
	}

	mediaTags, err := db.GetMediaTagsByMediaDBID(ctx, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media tags: %w", err)
	}

	titleTags, err := db.GetMediaTitleTagsByMediaTitleDBID(ctx, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title tags: %w", err)
	}

	mediaProps, err := db.GetMediaProperties(ctx, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media properties: %w", err)
	}

	titleProps, err := db.GetMediaTitleProperties(ctx, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}

	var secondarySlug *string
	if row.Title.SecondarySlug.Valid {
		secondarySlug = &row.Title.SecondarySlug.String
	}

	return models.MediaMetaResponse{
		Media: models.MediaMetaMediaResponse{
			ID:         row.DBID,
			Path:       row.Path,
			ParentDir:  row.ParentDir,
			IsMissing:  row.IsMissing,
			Tags:       mediaTags,
			Properties: mapMediaProperties(mediaProps),
			Title: models.MediaMetaTitleResponse{
				ID:            row.Title.DBID,
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
		},
	}, nil
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
		}
		if len(p.Binary) > 0 {
			encoded := base64.StdEncoding.EncodeToString(p.Binary)
			item.Data = &encoded
		}
		m[p.TypeTag] = item
	}
	return m
}
