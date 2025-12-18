// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/rs/zerolog/log"
)

func HandleInbox(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received inbox request")

	entries, err := env.Database.UserDB.GetInboxEntries()
	if err != nil {
		log.Error().Err(err).Msg("error getting inbox entries")
		return nil, errors.New("error getting inbox entries")
	}

	resp := models.InboxResponse{
		Entries: make([]models.InboxEntry, len(entries)),
	}

	for i, entry := range entries {
		resp.Entries[i] = models.InboxEntry{
			ID:        entry.DBID,
			Title:     entry.Title,
			Body:      entry.Body,
			CreatedAt: entry.CreatedAt,
		}
	}

	return resp, nil
}

func HandleInboxDelete(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received inbox delete request")

	var params models.DeleteInboxParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Error().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	err := env.Database.UserDB.DeleteInboxEntry(params.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete inbox entry: %w", err)
	}

	return NoContent{}, nil
}

func HandleInboxClear(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received inbox clear request")

	_, err := env.Database.UserDB.DeleteAllInboxEntries()
	if err != nil {
		return nil, fmt.Errorf("failed to clear inbox: %w", err)
	}

	return NoContent{}, nil
}
