// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-only
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

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/rs/zerolog/log"
)

func HandleTokens(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received tokens request")

	resp := models.TokensResponse{
		Active: make([]models.TokenResponse, 0),
	}

	active := env.State.GetActiveCard()
	if !active.ScanTime.IsZero() {
		resp.Active = append(resp.Active, models.TokenResponse{
			Type:     active.Type,
			UID:      active.UID,
			Text:     active.Text,
			Data:     active.Data,
			ScanTime: active.ScanTime,
		})
	}

	last := env.State.GetLastScanned()
	if !last.ScanTime.IsZero() {
		resp.Last = &models.TokenResponse{
			Type:     last.Type,
			UID:      last.UID,
			Text:     last.Text,
			Data:     last.Data,
			ScanTime: last.ScanTime,
		}
	}

	return resp, nil
}

func HandleHistory(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received history request")

	entries, err := env.Database.UserDB.GetHistory(0)
	if err != nil {
		log.Error().Err(err).Msgf("error getting history")
		return nil, errors.New("error getting history")
	}

	resp := models.HistoryResponse{
		Entries: make([]models.HistoryResponseEntry, len(entries)),
	}

	for i, e := range entries {
		resp.Entries[i] = models.HistoryResponseEntry{
			Time:    e.Time,
			Type:    e.Type,
			UID:     e.TokenID,
			Text:    e.TokenValue,
			Data:    e.TokenData,
			Success: e.Success,
		}
	}

	return resp, nil
}
