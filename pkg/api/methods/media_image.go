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
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// defaultImageTypes is the preference order used when no imageTypes param is provided.
// "image" is an alias for "boxart" (maps to the EmulationStation <image> field).
var defaultImageTypes = []string{"image", "boxart", "screenshot", "wheel", "titleshot", "map", "marquee", "fanart"}

// resolveImageTypeTag converts a short image type name to the full property TypeTag.
// "image" is treated as an alias for "boxart".
func resolveImageTypeTag(t string) string {
	if t == "image" {
		t = "boxart"
	}
	return "property:image-" + t
}

// buildPropsMap converts a []database.MediaProperty slice into a map keyed by TypeTag.
func buildPropsMap(props []database.MediaProperty) map[string]database.MediaProperty {
	m := make(map[string]database.MediaProperty, len(props))
	for _, p := range props {
		m[p.TypeTag] = p
	}
	return m
}

// HandleMediaImage returns a single best-match image for a media record as a
// base64-encoded blob. Image type preferences are supplied by the caller; if
// omitted the defaultImageTypes order is used. Media-level properties take
// priority over title-level properties for the same TypeTag. If a matching
// property has no inline binary data the file at the Text path is read from disk.
func HandleMediaImage(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	var params models.MediaImageParams
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

	mediaProps, err := db.GetMediaProperties(ctx, row.Media.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media properties: %w", err)
	}

	titleProps, err := db.GetMediaTitleProperties(ctx, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}

	prefs := params.ImageTypes
	if len(prefs) == 0 {
		prefs = defaultImageTypes
	}

	mediaMap := buildPropsMap(mediaProps)
	titleMap := buildPropsMap(titleProps)

	// Deduplicate resolved TypeTags while preserving order so we don't
	// attempt the same DB row twice (e.g. "image" and "boxart" both resolve
	// to "property:image-boxart").
	seen := make(map[string]bool, len(prefs))
	for _, t := range prefs {
		typeTag := resolveImageTypeTag(t)
		if seen[typeTag] {
			continue
		}
		seen[typeTag] = true

		// Media-level takes priority over title-level.
		prop, ok := mediaMap[typeTag]
		if !ok {
			prop, ok = titleMap[typeTag]
		}
		if !ok {
			continue
		}

		binary := prop.Binary
		if len(binary) == 0 && prop.Text != "" {
			binary, err = os.ReadFile(prop.Text)
			if err != nil {
				// File is gone — remove the stale property and retry from scratch
				// so the next preference in the list is evaluated cleanly.
				if _, fromMedia := mediaMap[typeTag]; fromMedia {
					_ = db.DeleteMediaProperty(ctx, row.Media.DBID, prop.TypeTagDBID)
				} else {
					_ = db.DeleteMediaTitleProperty(ctx, row.Title.DBID, prop.TypeTagDBID)
				}
				return HandleMediaImage(env)
			}
		}

		if len(binary) == 0 {
			continue
		}

		return models.MediaImageResponse{
			ContentType: prop.ContentType,
			Data:        base64.StdEncoding.EncodeToString(binary),
			TypeTag:     typeTag,
		}, nil
	}

	return nil, models.ClientErrf("no image found for media: %d", params.MediaID)
}
