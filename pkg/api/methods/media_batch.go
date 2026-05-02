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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

const (
	maxMediaMetaBatchItems  = 100
	maxMediaImageBatchItems = 50
)

type mediaRefParam struct {
	MediaID    *int64   `json:"mediaId,omitempty"`
	System     string   `json:"system,omitempty"`
	Path       string   `json:"path,omitempty"`
	ImageTypes []string `json:"imageTypes,omitempty"`
}

type mediaBatchParams struct {
	Items      []mediaRefParam `json:"items"`
	ImageTypes []string        `json:"imageTypes,omitempty"`
}

type parsedMediaRequest struct {
	Items      []mediaRefParam
	ImageTypes []string
	Batch      bool
}

type resolvedMediaItem struct {
	Row *database.MediaFullRow
	Err error
}

func parseMediaRequest(raw json.RawMessage, maxItems int) (parsedMediaRequest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return parsedMediaRequest{}, models.ClientErrf("invalid params: %w", err)
	}

	_, hasItems := fields["items"]
	hasTopRef := hasJSONField(fields, "mediaId") || hasJSONField(fields, "system") || hasJSONField(fields, "path")
	if hasItems && hasTopRef {
		return parsedMediaRequest{}, models.ClientErrf("invalid params: items cannot be mixed with top-level media ref")
	}

	if hasItems {
		var params mediaBatchParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return parsedMediaRequest{}, models.ClientErrf("invalid params: %w", err)
		}
		if len(params.Items) == 0 {
			return parsedMediaRequest{}, models.ClientErrf("invalid params: items is required")
		}
		if len(params.Items) > maxItems {
			return parsedMediaRequest{}, models.ClientErrf("invalid params: items exceeds max of %d", maxItems)
		}
		for i := range params.Items {
			if err := validateMediaRef(params.Items[i]); err != nil {
				return parsedMediaRequest{}, models.ClientErrf("invalid params: items[%d]: %w", i, err)
			}
			if err := validateImageTypes(params.Items[i].ImageTypes); err != nil {
				return parsedMediaRequest{}, models.ClientErrf("invalid params: items[%d]: %w", i, err)
			}
		}
		if err := validateImageTypes(params.ImageTypes); err != nil {
			return parsedMediaRequest{}, models.ClientErrf("invalid params: %w", err)
		}
		return parsedMediaRequest{Items: params.Items, ImageTypes: params.ImageTypes, Batch: true}, nil
	}

	var item mediaRefParam
	if err := json.Unmarshal(raw, &item); err != nil {
		return parsedMediaRequest{}, models.ClientErrf("invalid params: %w", err)
	}
	if err := validateMediaRef(item); err != nil {
		return parsedMediaRequest{}, models.ClientErrf("invalid params: %w", err)
	}
	if err := validateImageTypes(item.ImageTypes); err != nil {
		return parsedMediaRequest{}, models.ClientErrf("invalid params: %w", err)
	}
	return parsedMediaRequest{Items: []mediaRefParam{item}, ImageTypes: item.ImageTypes}, nil
}

func hasJSONField(fields map[string]json.RawMessage, name string) bool {
	_, ok := fields[name]
	return ok
}

func validateMediaRef(ref mediaRefParam) error {
	if ref.MediaID != nil {
		if *ref.MediaID <= 0 {
			return validation.ErrInvalidParams
		}
		if ref.System != "" || ref.Path != "" {
			return errors.New("mediaId cannot be mixed with system/path")
		}
		return nil
	}
	if ref.System == "" || ref.Path == "" {
		return errors.New("mediaId or system/path is required")
	}
	return nil
}

func validateImageTypes(types []string) error {
	for _, imageType := range types {
		if imageType == "" {
			return errors.New("imageTypes entries must be non-empty")
		}
	}
	return nil
}

func resolveMediaRefs(env *requests.RequestEnv, refs []mediaRefParam) ([]resolvedMediaItem, error) {
	resolved := make([]resolvedMediaItem, len(refs))
	mediaIDs := make([]int64, len(refs))
	uniqueIDs := make(map[int64]bool)

	pathGroups := make(map[string][]string)
	pathIndexes := make(map[string][]int)
	for i, ref := range refs {
		if ref.MediaID != nil {
			mediaIDs[i] = *ref.MediaID
			uniqueIDs[*ref.MediaID] = true
			continue
		}

		key := ref.System + "\x00" + ref.Path
		if len(pathIndexes[key]) == 0 {
			pathGroups[ref.System] = append(pathGroups[ref.System], ref.Path)
		}
		pathIndexes[key] = append(pathIndexes[key], i)
	}

	for systemID, paths := range pathGroups {
		system, err := env.Database.MediaDB.FindSystemBySystemID(systemID)
		if errors.Is(err, sql.ErrNoRows) {
			for _, path := range paths {
				for _, index := range pathIndexes[systemID+"\x00"+path] {
					resolved[index].Err = models.ClientErrf("system not found: %s", systemID)
				}
			}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve system: %w", err)
		}

		exact, err := env.Database.MediaDB.FindMediaBySystemAndPaths(env.Context, system.DBID, paths)
		if err != nil {
			return nil, fmt.Errorf("failed to find media: %w", err)
		}

		for _, path := range paths {
			media, ok := exact[path]
			if !ok {
				fallback, fallbackErr := resolveRelativeMediaPath(env, system, path)
				if fallbackErr != nil {
					for _, index := range pathIndexes[systemID+"\x00"+path] {
						resolved[index].Err = fallbackErr
					}
					continue
				}
				if fallback == nil {
					for _, index := range pathIndexes[systemID+"\x00"+path] {
						resolved[index].Err = models.ClientErrf("media not found: %s/%s", systemID, path)
					}
					continue
				}
				media = *fallback
			}

			for _, index := range pathIndexes[systemID+"\x00"+path] {
				mediaIDs[index] = media.DBID
				uniqueIDs[media.DBID] = true
			}
		}
	}

	ids := make([]int64, 0, len(uniqueIDs))
	for id := range uniqueIDs {
		ids = append(ids, id)
	}
	rows, err := env.Database.MediaDB.GetMediaWithTitleAndSystemByIDs(env.Context, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to get media: %w", err)
	}

	for i, id := range mediaIDs {
		if resolved[i].Err != nil {
			continue
		}
		row, ok := rows[id]
		if !ok {
			resolved[i].Err = models.ClientErrf("media not found: mediaId %d", id)
			continue
		}
		resolved[i].Row = &row
	}

	return resolved, nil
}

func uniqueMediaAndTitleIDs(resolved []resolvedMediaItem) (mediaIDs, titleIDs []int64) {
	mediaSeen := make(map[int64]bool)
	titleSeen := make(map[int64]bool)
	mediaIDs = make([]int64, 0, len(resolved))
	titleIDs = make([]int64, 0, len(resolved))
	for _, item := range resolved {
		if item.Row == nil {
			continue
		}
		if !mediaSeen[item.Row.DBID] {
			mediaSeen[item.Row.DBID] = true
			mediaIDs = append(mediaIDs, item.Row.DBID)
		}
		if !titleSeen[item.Row.Title.DBID] {
			titleSeen[item.Row.Title.DBID] = true
			titleIDs = append(titleIDs, item.Row.Title.DBID)
		}
	}
	return mediaIDs, titleIDs
}
