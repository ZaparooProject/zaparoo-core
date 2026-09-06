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
)

// Counts embedded in a cursor are valid only for the same visibility mode and
// durable preference revision. Legacy cursors remain usable before any edits.
func browsePreferencesRevision(env *requests.RequestEnv) (string, error) {
	var revision string
	if env.Database.UserDB != nil {
		var err error
		revision, _, err = env.Database.UserDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
		if err != nil {
			return "", fmt.Errorf("read media preferences revision: %w", err)
		}
	}
	projection, err := env.Database.MediaDB.MediaPreferencesRevision(env.Context)
	if err != nil {
		return "", fmt.Errorf("read media preferences projection: %w", err)
	}
	if revision == "" && projection == "" {
		return "", nil
	}
	return revision + "/" + projection, nil
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

func mediaTagsHidden(tags []database.TagInfo) bool {
	for _, tag := range tags {
		if tag.Type == "user" && tag.Tag == "hidden" {
			return true
		}
	}
	return false
}

func stampBrowseVisibility(
	env *requests.RequestEnv, result any, revision string, includeHidden bool,
) (any, error) {
	current, err := browsePreferencesRevision(env)
	if err != nil {
		return nil, err
	}
	if current != revision {
		return nil, models.ClientErrf("library visibility changed; restart browse without cursor")
	}

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
