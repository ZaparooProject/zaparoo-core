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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/rs/zerolog/log"
)

// HandleMediaCleanTruncate removes all indexed media from the database.
func HandleMediaCleanTruncate(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler pattern
	log.Info().Msg("received media.clean.truncate request")

	// Refuse to truncate while indexing or optimization is active to avoid
	// corrupting in-flight state ("running" or "pending" are active statuses).
	status, statusErr := env.Database.MediaDB.GetIndexingStatus()
	if statusErr != nil {
		return nil, fmt.Errorf("failed to check indexing status: %w", statusErr)
	}
	if status == "running" || status == "pending" {
		return nil, models.ClientErrf("cannot truncate while media indexing is in progress")
	}

	if err := env.Database.MediaDB.Truncate(); err != nil {
		return nil, fmt.Errorf("failed to truncate media database: %w", err)
	}

	return NoContent{}, nil
}
