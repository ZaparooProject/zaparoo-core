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
	"encoding/json"
	"errors"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/rs/zerolog/log"
)

func HandleReaderWrite(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received reader write request")

	if len(env.Params) == 0 {
		return nil, ErrMissingParams
	}

	var params models.ReaderWriteParams
	err := json.Unmarshal(env.Params, &params)
	if err != nil {
		return nil, ErrInvalidParams
	}

	rs := env.State.ListReaders()
	if len(rs) == 0 {
		return nil, errors.New("no readers connected")
	}

	rid := rs[0]
	lt := env.State.GetLastScanned()

	if !lt.ScanTime.IsZero() && !lt.FromAPI {
		rid = lt.Source
	}

	reader, ok := env.State.GetReader(rid)
	if !ok || reader == nil {
		return nil, errors.New("reader not connected: " + rs[0])
	}

	t, err := reader.Write(params.Text)
	if err != nil {
		log.Error().Err(err).Msg("error writing to reader")
		return nil, errors.New("error writing to reader")
	}

	if t != nil {
		env.State.SetWroteToken(t)
	}

	return nil, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleReaderWriteCancel(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received reader write cancel request")

	rs := env.State.ListReaders()
	if len(rs) == 0 {
		return nil, errors.New("no readers connected")
	}

	rid := rs[0]
	reader, ok := env.State.GetReader(rid)
	if !ok || reader == nil {
		return nil, errors.New("reader not connected: " + rs[0])
	}

	reader.CancelWrite()

	return nil, nil
}
