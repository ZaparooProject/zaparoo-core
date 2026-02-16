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
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/rs/zerolog/log"
)

const defaultMediaHistoryLimit = 25

func HandleMediaHistory(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	limit := defaultMediaHistoryLimit
	var lastID int64
	var systemIDs []string

	if len(env.Params) > 0 {
		var params models.MediaHistoryParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return nil, validation.ErrInvalidParams
		}
		if err := validation.DefaultValidator.Validate(&params); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}

		if params.Limit != nil {
			limit = *params.Limit
		}

		if params.Cursor != nil {
			cursor, err := decodeCursor(*params.Cursor)
			if err != nil {
				return nil, fmt.Errorf("invalid cursor: %w", err)
			}
			if cursor != nil {
				lastID = *cursor
			}
		}

		if params.Systems != nil && len(*params.Systems) > 0 {
			fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
			systems, err := resolveSystems(*params.Systems, fuzzy)
			if err != nil {
				return nil, err
			}
			systemIDs = make([]string, len(systems))
			for i, sys := range systems {
				systemIDs[i] = sys.ID
			}
		}
	}

	// Fetch one extra to detect next page
	entries, err := env.Database.UserDB.GetMediaHistory(systemIDs, lastID, limit+1)
	if err != nil {
		log.Error().Err(err).Msg("error getting media history")
		return nil, fmt.Errorf("error getting media history: %w", err)
	}

	hasNextPage := len(entries) > limit
	if hasNextPage {
		entries = entries[:limit]
	}

	responseEntries := make([]models.MediaHistoryResponseEntry, 0, len(entries))
	for i := range entries {
		entry := &entries[i]
		startedAt := entry.StartTime.Format(time.RFC3339)

		var endedAt *string
		if entry.EndTime != nil {
			formatted := entry.EndTime.Format(time.RFC3339)
			endedAt = &formatted
		}

		responseEntries = append(responseEntries, models.MediaHistoryResponseEntry{
			SystemID:   entry.SystemID,
			SystemName: entry.SystemName,
			MediaName:  entry.MediaName,
			MediaPath:  entry.MediaPath,
			LauncherID: entry.LauncherID,
			StartedAt:  startedAt,
			EndedAt:    endedAt,
			PlayTime:   entry.PlayTime,
		})
	}

	var pagination *models.PaginationInfo
	if len(responseEntries) > 0 {
		var nextCursor *string
		if hasNextPage {
			lastEntry := entries[len(entries)-1]
			cursorStr, cursorErr := encodeCursor(lastEntry.DBID)
			if cursorErr != nil {
				log.Error().Err(cursorErr).Msg("failed to encode next cursor")
				return nil, fmt.Errorf("failed to generate next page cursor: %w", cursorErr)
			}
			nextCursor = &cursorStr
		}

		pagination = &models.PaginationInfo{
			NextCursor:  nextCursor,
			HasNextPage: hasNextPage,
			PageSize:    limit,
		}
	}

	return models.MediaHistoryResponse{
		Entries:    responseEntries,
		Pagination: pagination,
	}, nil
}
