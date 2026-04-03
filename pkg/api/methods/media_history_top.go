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

//nolint:gocritic // single-use parameter in API handler
func HandleMediaHistoryTop(env requests.RequestEnv) (any, error) {
	limit := defaultMediaHistoryLimit
	var systemIDs []string
	var since *time.Time

	if len(env.Params) > 0 {
		var params models.MediaHistoryTopParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErr(validation.ErrInvalidParams)
		}
		if err := validation.DefaultValidator.Validate(&params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}

		if params.Limit != nil {
			limit = *params.Limit
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

		if params.Since != nil {
			t, err := time.Parse(time.RFC3339, *params.Since)
			if err != nil {
				return nil, models.ClientErrf("invalid since timestamp: %w", err)
			}
			since = &t
		}
	}

	entries, err := env.Database.UserDB.GetMediaHistoryTop(systemIDs, since, limit)
	if err != nil {
		log.Error().Err(err).Msg("error getting media history top")
		return nil, fmt.Errorf("error getting media history top: %w", err)
	}

	responseEntries := make([]models.MediaHistoryTopEntry, 0, len(entries))
	for i := range entries {
		entry := &entries[i]
		responseEntries = append(responseEntries, models.MediaHistoryTopEntry{
			SystemID:      entry.SystemID,
			SystemName:    entry.SystemName,
			MediaName:     entry.MediaName,
			MediaPath:     entry.MediaPath,
			LastPlayedAt:  entry.LastPlayedAt.Format(time.RFC3339),
			TotalPlayTime: entry.TotalPlayTime,
			SessionCount:  entry.SessionCount,
		})
	}

	return models.MediaHistoryTopResponse{
		Entries: responseEntries,
	}, nil
}
