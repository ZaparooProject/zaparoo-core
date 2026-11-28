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
	"errors"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
)

func HandleTokens(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Debug().Msg("received tokens request")

	resp := models.TokensResponse{
		Active:    make([]models.TokenResponse, 0),
		Modifiers: make([]models.LaunchModifierResponse, 0),
	}

	modifiers := env.State.LaunchModifiers()
	for i := range modifiers {
		resp.Modifiers = append(resp.Modifiers, state.LaunchModifierResponse(&modifiers[i]))
	}

	// Every card resting on a reader is active, oldest scan first. The last
	// token scanned is listed too when it is not one of them, such as a run
	// from the API or a scan from a reader that cannot report removal.
	listed := env.State.ReaderTokens()
	active := env.State.GetActiveCard()
	if !active.ScanTime.IsZero() && !slices.ContainsFunc(listed, func(t tokens.Token) bool {
		return t.ReaderID == active.ReaderID && helpers.TokensEqual(&t, &active)
	}) {
		listed = append(listed, active)
		slices.SortStableFunc(listed, func(a, b tokens.Token) int {
			return a.ScanTime.Compare(b.ScanTime)
		})
	}
	for i := range listed {
		text, data := zapscript.RedactToken(listed[i].Text, listed[i].Data)
		resp.Active = append(resp.Active, models.TokenResponse{
			Type:     listed[i].Type,
			UID:      listed[i].UID,
			Text:     text,
			Data:     data,
			ScanTime: listed[i].ScanTime,
			ReaderID: listed[i].ReaderID,
		})
	}

	last := env.State.GetLastScanned()
	if !last.ScanTime.IsZero() {
		text, data := zapscript.RedactToken(last.Text, last.Data)
		resp.Last = &models.TokenResponse{
			Type:     last.Type,
			UID:      last.UID,
			Text:     text,
			Data:     data,
			ScanTime: last.ScanTime,
			ReaderID: last.ReaderID,
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

	for i := range entries {
		// Redact on the way out as well as on the way in: rows written by
		// earlier versions still hold credentials in the clear.
		text, data := zapscript.RedactToken(entries[i].TokenValue, entries[i].TokenData)
		resp.Entries[i] = models.HistoryResponseEntry{
			Time:    entries[i].Time,
			Type:    entries[i].Type,
			UID:     entries[i].TokenID,
			Text:    text,
			Data:    data,
			Success: entries[i].Success,
		}
	}

	return resp, nil
}
