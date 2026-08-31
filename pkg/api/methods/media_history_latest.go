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
	"bytes"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/rs/zerolog/log"
)

func HandleMediaHistoryLatest(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler signature
	trimmed := bytes.TrimSpace(env.Params)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) {
		return nil, models.ClientErr(validation.ErrInvalidParams)
	}

	entry, found, err := env.Database.UserDB.GetLatestMediaHistory()
	if err != nil {
		log.Error().Err(err).Msg("error getting latest media history")
		return nil, fmt.Errorf("error getting latest media history: %w", err)
	}
	if !found {
		return models.MediaHistoryLatestResponse{}, nil
	}

	return models.MediaHistoryLatestResponse{
		Entry: &models.MediaHistoryLatestEntry{
			SystemID:   entry.SystemID,
			SystemName: entry.SystemName,
			MediaName:  entry.MediaName,
			MediaPath:  entry.MediaPath,
			LauncherID: entry.LauncherID,
			StartedAt:  entry.StartTime.Format(time.RFC3339),
		},
	}, nil
}
