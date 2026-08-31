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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

type mediaWriteClientConflictError struct {
	cause   error
	message string
}

func (e *mediaWriteClientConflictError) Error() string { return e.message }
func (e *mediaWriteClientConflictError) Unwrap() error { return e.cause }

func mediaWriteClientError(err error, requested database.MediaWriteOperation) error {
	var conflict *database.MediaWriteConflictError
	if !errors.As(err, &conflict) {
		return err
	}

	message := "media database maintenance in progress"
	switch {
	case conflict.Active == database.MediaWriteOperationIndexing && requested == database.MediaWriteOperationIndexing:
		message = "indexing already in progress"
	case conflict.Active == database.MediaWriteOperationIndexing:
		message = "media indexing is in progress"
	case conflict.Active == database.MediaWriteOperationScraping && requested == database.MediaWriteOperationScraping:
		message = "scraping already in progress"
	case conflict.Active == database.MediaWriteOperationScraping:
		message = "scraping is in progress"
	case conflict.Active == database.MediaWriteOperationOptimization:
		message = "database optimization in progress"
	}
	return models.ClientErr(&mediaWriteClientConflictError{cause: err, message: message})
}

func startIndexing(mediaDB database.MediaDBI) (*database.MediaWriteLease, error) {
	coordinator, err := database.GetMediaDBWriteCoordinator(mediaDB)
	if err != nil {
		return nil, fmt.Errorf("get media database write coordinator for indexing: %w", err)
	}
	lease, err := coordinator.AcquireMediaWrite(database.MediaWriteOperationIndexing)
	if err != nil {
		return nil, mediaWriteClientError(err, database.MediaWriteOperationIndexing)
	}
	if !statusInstance.startIfNotRunning() {
		lease.Release()
		return nil, models.ClientErrf("indexing already in progress")
	}
	return lease, nil
}

func startScraping(
	mediaDB database.MediaDBI, scraperID string, force bool,
) (*database.MediaWriteLease, error) {
	coordinator, err := database.GetMediaDBWriteCoordinator(mediaDB)
	if err != nil {
		return nil, fmt.Errorf("get media database write coordinator for scraping: %w", err)
	}
	lease, err := coordinator.AcquireMediaWrite(database.MediaWriteOperationScraping)
	if err != nil {
		return nil, mediaWriteClientError(err, database.MediaWriteOperationScraping)
	}
	if !scrapingStatusInstance.startIfNotRunning(scraperID, force) {
		lease.Release()
		return nil, models.ClientErrf("scraping already in progress")
	}
	return lease, nil
}
