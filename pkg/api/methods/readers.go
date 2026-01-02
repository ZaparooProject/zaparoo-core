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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

func HandleReaderWrite(
	params json.RawMessage,
	allReaders []readers.Reader,
	lastScanned *tokens.Token,
	setWroteToken func(*tokens.Token),
) (any, error) {
	var p models.ReaderWriteParams
	if err := validation.ValidateAndUnmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	var r readers.Reader
	var err error

	if p.ReaderID != nil && *p.ReaderID != "" {
		r, err = readers.SelectWriterStrict(allReaders, *p.ReaderID)
	} else {
		var prefs []string
		// TODO: remove IsZero check when lastScanned is token pointer in state
		if lastScanned != nil && !lastScanned.ScanTime.IsZero() && lastScanned.ReaderID != "" {
			prefs = []string{lastScanned.ReaderID}
		}
		r, err = readers.SelectWriterPreferred(allReaders, prefs)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to select writer: %w", err)
	}

	t, err := r.Write(p.Text)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Debug().Err(err).Msg("tag write cancelled")
		} else {
			log.Error().Err(err).Msg("error writing to reader")
		}
		return nil, errors.New("error writing to reader")
	}

	if t != nil {
		setWroteToken(t)
	}

	return NoContent{}, nil
}

func HandleReaderWriteCancel(
	params json.RawMessage,
	allReaders []readers.Reader,
) (any, error) {
	var p models.ReaderWriteCancelParams
	if err := validation.ValidateAndUnmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if p.ReaderID != nil && *p.ReaderID != "" {
		r, err := readers.SelectWriterStrict(allReaders, *p.ReaderID)
		if err != nil {
			return nil, fmt.Errorf("failed to select reader: %w", err)
		}
		r.CancelWrite()
	} else {
		writeCapable := readers.FilterByCapability(allReaders, readers.CapabilityWrite)
		for _, r := range writeCapable {
			r.CancelWrite()
		}
	}

	return NoContent{}, nil
}

func HandleReaders(allReaders []readers.Reader) (any, error) {
	readerInfos := make([]models.ReaderInfo, 0, len(allReaders))

	for _, r := range allReaders {
		if r == nil {
			continue
		}

		capabilities := r.Capabilities()
		capabilityStrings := make([]string, len(capabilities))
		for i, capability := range capabilities {
			capabilityStrings[i] = string(capability)
		}

		readerInfo := models.ReaderInfo{
			ID:           r.Device(), // TODO: replace with ReaderID field in next major version
			ReaderID:     r.ReaderID(),
			Driver:       r.Metadata().ID,
			Info:         r.Info(),
			Connected:    r.Connected(),
			Capabilities: capabilityStrings,
		}

		readerInfos = append(readerInfos, readerInfo)
	}

	response := models.ReadersResponse{
		Readers: readerInfos,
	}

	return response, nil
}
