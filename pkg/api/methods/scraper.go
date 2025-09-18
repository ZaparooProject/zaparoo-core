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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/scraper"
	"github.com/rs/zerolog/log"
)

// Global scraper service instance
var ScraperServiceInstance *scraper.ScraperService

// HandleScraperScrapeStart scrapes systems or specific media from MediaDB
//
//nolint:gocritic // single-use parameter in API handler
func HandleScraperScrapeStart(env requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	// Check if MediaDB exists
	if !env.Database.MediaDB.Exists() {
		return nil, errors.New("MediaDB not generated - run media.generate first")
	}

	var params models.ScraperStartParams
	if len(env.Params) > 0 {
		err := json.Unmarshal(env.Params, &params)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal params: %w", err)
		}
	}

	// If media ID is specified, scrape that specific media item
	if params.Media != nil {
		go func() {
			if err := ScraperServiceInstance.ScrapeGameByID(env.State.GetContext(), *params.Media); err != nil {
				log.Error().Err(err).Int64("mediaID", *params.Media).Msg("failed to scrape game")
			}
		}()

		return models.ScraperStartResponse{
			Started: true,
			MediaID: params.Media,
		}, nil
	}

	// Otherwise, scrape systems
	var systems []systemdefs.System
	if params.Systems == nil || len(*params.Systems) == 0 {
		systems = systemdefs.AllSystems()
	} else {
		for _, s := range *params.Systems {
			system, err := systemdefs.GetSystem(s)
			if err != nil {
				return nil, errors.New("error getting system: " + err.Error())
			}
			systems = append(systems, *system)
		}
	}

	// Start scraping in background for each system
	go func() {
		for _, system := range systems {
			if err := ScraperServiceInstance.ScrapeSystem(env.State.GetContext(), system.ID); err != nil {
				log.Error().Err(err).Str("systemID", system.ID).Msg("failed to scrape system")
			}
		}
	}()

	systemIDs := make([]string, len(systems))
	for i, system := range systems {
		systemIDs[i] = system.ID
	}

	return models.ScraperStartResponse{
		Started: true,
		Systems: systemIDs,
	}, nil
}

// HandleScraper returns current status of scraper job
//
//nolint:gocritic // single-use parameter in API handler
func HandleScraper(_ requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	progress := ScraperServiceInstance.GetProgress()
	return progress, nil
}

// HandleScraperCancel cancels active scraper job
//
//nolint:gocritic // single-use parameter in API handler
func HandleScraperCancel(_ requests.RequestEnv) (any, error) {
	if ScraperServiceInstance == nil {
		return nil, errors.New("scraper service not initialized")
	}

	err := ScraperServiceInstance.CancelScraping()
	if err != nil {
		return models.ScraperCancelResponse{
			Cancelled: false,
		}, fmt.Errorf("failed to cancel scraping: %w", err)
	}
	return models.ScraperCancelResponse{
		Cancelled: true,
	}, nil
}
