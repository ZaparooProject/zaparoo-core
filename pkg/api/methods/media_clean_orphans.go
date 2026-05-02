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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/rs/zerolog/log"
)

// HandleMediaCleanOrphans removes missing Media records and their associated
// orphaned data (tags, properties, fully-orphaned titles). Returns the number
// of missing Media rows deleted.
func HandleMediaCleanOrphans(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler pattern
	log.Info().Msg("received media.clean.orphans request")

	deleted, err := env.Database.MediaDB.CleanMediaOrphans(env.Context)
	if err != nil {
		if errors.Is(err, mediadb.ErrIndexingInProgress) ||
			errors.Is(err, mediadb.ErrOptimizationInProgress) ||
			errors.Is(err, mediadb.ErrTransactionActive) {
			return nil, models.ClientErrf("%w", err)
		}

		return nil, fmt.Errorf("failed to clean media orphans: %w", err)
	}

	return models.MediaCleanOrphansResponse{Deleted: deleted}, nil
}
