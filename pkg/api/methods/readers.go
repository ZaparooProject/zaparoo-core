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
	"context"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

func HandleReaderWrite(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received reader write request")

	var params models.ReaderWriteParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	rs := env.State.ListReaders()
	if len(rs) == 0 {
		return nil, errors.New("no readers connected")
	}

	// Filter readers that have write capability
	var writeCapableReaders []string
	for _, readerID := range rs {
		reader, ok := env.State.GetReader(readerID)
		if !ok || reader == nil {
			continue
		}

		capabilities := reader.Capabilities()
		for _, capability := range capabilities {
			if capability == readers.CapabilityWrite {
				writeCapableReaders = append(writeCapableReaders, readerID)
				break
			}
		}
	}

	if len(writeCapableReaders) == 0 {
		return nil, errors.New("no readers with write capability connected")
	}

	// Select the last used reader from the write-capable list
	rid := writeCapableReaders[0]
	lt := env.State.GetLastScanned()

	if !lt.ScanTime.IsZero() && !lt.FromAPI {
		// Check if the last used reader is in our write-capable list
		for _, writeReader := range writeCapableReaders {
			if writeReader == lt.Source {
				rid = lt.Source
				break
			}
		}
	}

	reader, ok := env.State.GetReader(rid)
	if !ok || reader == nil {
		return nil, errors.New("reader not connected: " + rid)
	}

	t, err := reader.Write(params.Text)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Debug().Err(err).Msg("tag write cancelled")
		} else {
			log.Error().Err(err).Msg("error writing to reader")
		}
		return nil, errors.New("error writing to reader")
	}

	if t != nil {
		env.State.SetWroteToken(t)
	}

	return NoContent{}, nil
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

	return NoContent{}, nil
}

func HandleReaders(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received readers request")

	readerIDs := env.State.ListReaders()
	readerInfos := make([]models.ReaderInfo, 0, len(readerIDs))

	for _, readerID := range readerIDs {
		reader, ok := env.State.GetReader(readerID)
		if !ok || reader == nil {
			continue
		}

		capabilities := reader.Capabilities()
		capabilityStrings := make([]string, len(capabilities))
		for i, capability := range capabilities {
			capabilityStrings[i] = string(capability)
		}

		readerInfo := models.ReaderInfo{
			ID:           readerID,
			Info:         reader.Info(),
			Connected:    reader.Connected(),
			Capabilities: capabilityStrings,
		}

		readerInfos = append(readerInfos, readerInfo)
	}

	response := models.ReadersResponse{
		Readers: readerInfos,
	}

	return response, nil
}
